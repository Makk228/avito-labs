// Package positions polls stale trips with a bounded FIFO and fixed worker pool.
package positions

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/Makk228/avito-labs/internal/model"
)

const (
	Queued    = "queued"
	Duplicate = "duplicate"
	Dropped   = "dropped"
	Success   = "success"
	Failed    = "error"
	Cancelled = "cancelled"
)

type Config struct {
	CheckInterval   time.Duration
	StaleAfter      time.Duration
	Workers         int
	QueueSize       int
	RequestTimeout  time.Duration
	ShutdownTimeout time.Duration
}

type Dependencies struct {
	Trips   repository
	Push    gateway
	Clock   clock
	Ticker  ticker
	Logger  logger
	Metrics metrics
	Tracer  trace.Tracer
}

type task struct {
	trip model.StaleTrip
	link trace.SpanContext
}

type Worker struct {
	deps Dependencies
	cfg  Config
	jobs chan *task

	mu        sync.Mutex
	pending   map[uuid.UUID]*task
	accepting bool
	active    int64

	force     chan struct{}
	forceOnce sync.Once
	done      chan struct{}
	runErr    error // written before done is closed, read after receiving from done
}

func New(deps Dependencies, cfg Config) *Worker {
	return &Worker{
		deps:      deps,
		cfg:       cfg,
		jobs:      make(chan *task, cfg.QueueSize),
		pending:   make(map[uuid.UUID]*task),
		accepting: true,
		force:     make(chan struct{}),
		done:      make(chan struct{}),
	}
}

// Offer never waits for capacity. Pointer identity protects a newer task from
// an older cancelled task still physically buffered in the channel (ABA).
func (w *Worker) Offer(ctx context.Context, trip model.StaleTrip) string {
	w.mu.Lock()
	defer w.mu.Unlock()

	if _, exists := w.pending[trip.ID]; exists {
		return Duplicate
	}

	if !w.accepting {
		return Dropped
	}

	item := &task{trip: trip, link: trace.SpanContextFromContext(ctx)}
	select {
	case w.jobs <- item:
		w.pending[trip.ID] = item
		w.deps.Metrics.QueueSize(ctx, int64(len(w.jobs)))
		return Queued
	default:
		return Dropped
	}
}

// Cancel invalidates queued work after the coordinate transaction commits.
// A request already claimed by a worker may already have reached the device.
func (w *Worker) Cancel(id uuid.UUID) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.pending, id)
}

// Run is called exactly once. Only this goroutine scans and closes jobs.
func (w *Worker) Run(ctx context.Context) {
	defer close(w.done)
	w.mu.Lock()
	w.deps.Metrics.QueueSize(ctx, int64(len(w.jobs)))
	w.deps.Metrics.ActiveWorkers(ctx, w.active)
	w.mu.Unlock()

	workCtx, cancelWork := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelWork()

	var workers sync.WaitGroup
	for range w.cfg.Workers {
		workers.Go(func() { w.consume(workCtx) })
	}

	w.deps.Logger.InfoContext(ctx, "position scanner started")
	w.scan(ctx)

	w.mu.Lock()
	w.accepting = false
	close(w.jobs)
	w.mu.Unlock()

	drained := make(chan struct{})
	go func() { workers.Wait(); close(drained) }()
	drainCtx, cancelDrain := context.WithTimeout(context.WithoutCancel(ctx), w.cfg.ShutdownTimeout)
	defer cancelDrain()

	select {
	case <-drained:
	case <-w.force:
		w.runErr = context.DeadlineExceeded
		cancelWork()
		<-drained
	case <-drainCtx.Done():
		w.runErr = drainCtx.Err()
		cancelWork()
		<-drained
	}

	w.deps.Logger.InfoContext(ctx, "position scanner and workers stopped")
}

// Wait is called after the scanner context is cancelled, using the remaining
// application shutdown budget. At the deadline all outstanding I/O is cancelled.
func (w *Worker) Wait(ctx context.Context) error {
	select {
	case <-w.done:
		return w.runErr
	case <-ctx.Done():
		w.forceOnce.Do(func() { close(w.force) })
		<-w.done
		return ctx.Err()
	}
}

func (w *Worker) scan(ctx context.Context) {
	ticks := w.deps.Ticker.Start(w.cfg.CheckInterval)
	defer w.deps.Ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
			if ctx.Err() != nil {
				return
			}
			w.check(ctx)
		}
	}
}

func (w *Worker) check(ctx context.Context) {
	ctx, span := w.deps.Tracer.Start(ctx, "positions.scan")
	defer span.End()

	trips, err := w.deps.Trips.Stale(ctx, w.deps.Clock.Now(), w.cfg.StaleAfter)
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			w.deps.Logger.WarnContext(ctx, "stale trips query failed", "error", err)
			span.RecordError(err)
			span.SetStatus(codes.Error, "database_error")
		}
		return
	}

	for _, trip := range trips {
		if ctx.Err() != nil {
			return
		}

		result := w.Offer(ctx, trip)
		w.deps.Metrics.StaleCheck(ctx, result)
		if result == Dropped {
			w.deps.Logger.WarnContext(ctx, "position queue full; trip will be checked on next tick", "trip_id", trip.ID.String())
		}
	}
}

func (w *Worker) consume(ctx context.Context) {
	for item := range w.jobs {
		if !w.claim(ctx, item) {
			w.deps.Metrics.Request(ctx, Cancelled, 0)
			continue
		}

		if ctx.Err() == nil {
			w.request(ctx, item)
		}

		w.release(ctx, item)
	}
}

func (w *Worker) claim(ctx context.Context, item *task) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.deps.Metrics.QueueSize(ctx, int64(len(w.jobs)))

	if w.pending[item.trip.ID] != item {
		return false
	}

	w.active++
	w.deps.Metrics.ActiveWorkers(ctx, w.active)
	return true
}

func (w *Worker) release(ctx context.Context, item *task) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.pending[item.trip.ID] == item {
		delete(w.pending, item.trip.ID)
	}
	w.active--
	w.deps.Metrics.ActiveWorkers(ctx, w.active)
}

func (w *Worker) request(ctx context.Context, item *task) {
	options := []trace.SpanStartOption{trace.WithNewRoot()}
	if item.link.IsValid() {
		options = append(options, trace.WithLinks(trace.Link{SpanContext: item.link}))
	}

	ctx, span := w.deps.Tracer.Start(ctx, "positions.request", options...)
	defer span.End()
	ctx, cancel := context.WithTimeout(ctx, w.cfg.RequestTimeout)
	defer cancel()

	started := w.deps.Clock.Now()
	result := Success
	if err := w.deps.Push.RequestPosition(ctx, item.trip); err != nil {
		result = Failed
		span.RecordError(err)
		span.SetStatus(codes.Error, Failed)
		w.deps.Logger.WarnContext(ctx, "position push failed", "trip_id", item.trip.ID.String(), "error", err)
	}

	w.deps.Metrics.Request(ctx, result, w.deps.Clock.Now().Sub(started))
}
