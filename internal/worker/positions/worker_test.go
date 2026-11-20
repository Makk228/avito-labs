package positions_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/goleak"
	"go.uber.org/mock/gomock"

	"github.com/Makk228/avito-labs/internal/model"
	"github.com/Makk228/avito-labs/internal/worker/positions"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

type fixture struct {
	t           *testing.T
	ctx         context.Context
	cancel      context.CancelFunc
	ticks       chan time.Time
	started     chan struct{}
	stopped     chan struct{}
	now         time.Time
	cfg         positions.Config
	repo        *Mockrepository
	push        *Mockgateway
	metrics     *Mockmetrics
	logger      *Mocklogger
	pool        *positions.Worker
	exporter    *tracetest.InMemoryExporter
	wantStopErr error
}

func newFixture(t *testing.T, workers, size int) *fixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	ctx, cancel := context.WithCancel(context.Background())
	f := &fixture{
		t: t, ctx: ctx, cancel: cancel,
		ticks: make(chan time.Time), started: make(chan struct{}), stopped: make(chan struct{}),
		now: time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC),
		cfg: positions.Config{
			CheckInterval: 30 * time.Second, StaleAfter: time.Minute,
			Workers: workers, QueueSize: size, RequestTimeout: time.Second,
			ShutdownTimeout: 5 * time.Second,
		},
		repo: NewMockrepository(ctrl), push: NewMockgateway(ctrl),
		metrics: NewMockmetrics(ctrl), logger: NewMocklogger(ctrl),
		exporter: tracetest.NewInMemoryExporter(),
	}
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(f.exporter))
	t.Cleanup(func() { assert.NoError(t, tp.Shutdown(context.Background())) })
	clock := NewMockclock(ctrl)
	clock.EXPECT().Now().Return(f.now).AnyTimes()
	ticker := NewMockticker(ctrl)
	ticker.EXPECT().Start(f.cfg.CheckInterval).DoAndReturn(func(time.Duration) <-chan time.Time {
		close(f.started)
		return f.ticks
	})
	ticker.EXPECT().Stop().Do(func() { close(f.stopped) })
	f.logger.EXPECT().InfoContext(gomock.Any(), "position scanner started")
	f.logger.EXPECT().InfoContext(gomock.Any(), "position scanner and workers stopped")
	f.metrics.EXPECT().QueueSize(gomock.Any(), gomock.Any()).AnyTimes()
	f.metrics.EXPECT().ActiveWorkers(gomock.Any(), gomock.Any()).Do(func(_ context.Context, n int64) {
		assert.GreaterOrEqual(t, n, int64(0))
		assert.LessOrEqual(t, n, int64(workers))
	}).AnyTimes()
	f.pool = positions.New(positions.Dependencies{
		Trips: f.repo, Push: f.push, Clock: clock, Ticker: ticker,
		Logger: f.logger, Metrics: f.metrics, Tracer: tp.Tracer("test"),
	}, f.cfg)
	return f
}

func (f *fixture) start() {
	f.t.Helper()
	go f.pool.Run(f.ctx)
	receive(f.t, f.started)
	f.t.Cleanup(func() {
		f.cancel()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		assert.ErrorIs(f.t, f.pool.Wait(ctx), f.wantStopErr)
	})
}

func receive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for synchronization")
		var zero T
		return zero
	}
}

func (f *fixture) tick() {
	f.t.Helper()
	select {
	case f.ticks <- f.now:
	case <-time.After(5 * time.Second):
		f.t.Fatal("scanner did not accept a tick")
	}
}

func candidate(n byte) model.StaleTrip {
	id := uuid.MustParse("00000000-0000-4000-8000-000000000001")
	id[15] = n
	driver := id
	driver[14] = 1
	return model.StaleTrip{ID: id, DriverID: driver}
}

func TestScannerReportsDuplicatesAndOverflowWithoutBlocking(t *testing.T) {
	f := newFixture(t, 1, 1)
	first, second, overflow := candidate(1), candidate(2), candidate(3)
	entered, release, dropped, processed := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	f.repo.EXPECT().Stale(gomock.Any(), f.now, f.cfg.StaleAfter).Return([]model.StaleTrip{first}, nil)
	f.push.EXPECT().RequestPosition(gomock.Any(), first).DoAndReturn(func(ctx context.Context, _ model.StaleTrip) error {
		_, ok := ctx.Deadline()
		assert.True(t, ok)
		close(entered)
		<-release
		return nil
	})
	f.metrics.EXPECT().StaleCheck(gomock.Any(), positions.Queued).Times(2)
	f.metrics.EXPECT().StaleCheck(gomock.Any(), positions.Duplicate)
	f.metrics.EXPECT().StaleCheck(gomock.Any(), positions.Dropped).Do(func(context.Context, string) { close(dropped) })
	f.logger.EXPECT().WarnContext(gomock.Any(), "position queue full; trip will be checked on next tick", "trip_id", overflow.ID.String())
	f.metrics.EXPECT().Request(gomock.Any(), positions.Success, time.Duration(0)).Times(2)
	f.push.EXPECT().RequestPosition(gomock.Any(), second).DoAndReturn(func(context.Context, model.StaleTrip) error {
		close(processed)
		return nil
	})

	f.start()
	f.tick()
	receive(t, entered)
	f.repo.EXPECT().Stale(gomock.Any(), f.now, f.cfg.StaleAfter).Return([]model.StaleTrip{first, second, overflow}, nil)
	f.tick()
	receive(t, dropped) // Push is still blocked: the scanner finished its offers.
	close(release)
	receive(t, processed)
}

func TestCancelledBufferedTaskNeverCallsPush(t *testing.T) {
	f := newFixture(t, 1, 1)
	trip := candidate(1)
	cancelled := make(chan struct{})
	f.metrics.EXPECT().Request(gomock.Any(), positions.Cancelled, time.Duration(0)).Do(func(context.Context, string, time.Duration) { close(cancelled) })

	require.Equal(t, positions.Queued, f.pool.Offer(f.ctx, trip))
	f.pool.Cancel(trip.ID)
	f.start()

	receive(t, cancelled)
}

func TestCancelledTaskCannotRemoveItsReplacement(t *testing.T) {
	f := newFixture(t, 1, 2)
	trip := candidate(1)
	processed := make(chan struct{})
	f.metrics.EXPECT().Request(gomock.Any(), positions.Cancelled, time.Duration(0))
	f.metrics.EXPECT().Request(gomock.Any(), positions.Success, time.Duration(0)).Do(func(context.Context, string, time.Duration) { close(processed) })
	f.push.EXPECT().RequestPosition(gomock.Any(), trip).Return(nil)

	require.Equal(t, positions.Queued, f.pool.Offer(f.ctx, trip))
	f.pool.Cancel(trip.ID)
	require.Equal(t, positions.Queued, f.pool.Offer(f.ctx, trip))
	f.start()

	receive(t, processed)
}

func TestFixedPoolProcessesEveryTask(t *testing.T) {
	const workers, count = 3, 12
	f := newFixture(t, workers, count)
	entered := make(chan uuid.UUID, count)
	permits := make(chan struct{})
	completed := make(chan struct{})
	var active, peak atomic.Int64
	var metrics sync.WaitGroup
	metrics.Add(count)
	f.metrics.EXPECT().Request(gomock.Any(), positions.Success, time.Duration(0)).Do(func(context.Context, string, time.Duration) {
		metrics.Done()
	}).Times(count)
	for n := byte(1); n <= count; n++ {
		trip := candidate(n)
		f.push.EXPECT().RequestPosition(gomock.Any(), trip).DoAndReturn(func(ctx context.Context, in model.StaleTrip) error {
			deadline, ok := ctx.Deadline()
			assert.True(t, ok)
			assert.True(t, deadline.After(time.Now()))
			current := active.Add(1)
			for previous := peak.Load(); current > previous; previous = peak.Load() {
				if peak.CompareAndSwap(previous, current) {
					break
				}
			}
			entered <- in.ID
			<-permits
			active.Add(-1)
			return nil
		})
		require.Equal(t, positions.Queued, f.pool.Offer(f.ctx, trip))
	}
	f.start()

	seen := make(map[uuid.UUID]bool)
	for range workers {
		seen[receive(t, entered)] = true
	}
	for range count {
		permits <- struct{}{}
	}
	for range count - workers {
		seen[receive(t, entered)] = true
	}
	go func() { metrics.Wait(); close(completed) }()
	receive(t, completed)

	assert.Len(t, seen, count)
	assert.EqualValues(t, workers, peak.Load())
	assert.Zero(t, active.Load())
}

func TestPushFailureDoesNotStopWorker(t *testing.T) {
	f := newFixture(t, 1, 2)
	first, second := candidate(1), candidate(2)
	failure := errors.New("push unavailable")
	finished := make(chan struct{})
	f.push.EXPECT().RequestPosition(gomock.Any(), first).Return(failure)
	f.push.EXPECT().RequestPosition(gomock.Any(), second).Return(nil)
	f.logger.EXPECT().WarnContext(gomock.Any(), "position push failed", "trip_id", first.ID.String(), "error", failure)
	f.metrics.EXPECT().Request(gomock.Any(), positions.Failed, time.Duration(0))
	f.metrics.EXPECT().Request(gomock.Any(), positions.Success, time.Duration(0)).Do(func(context.Context, string, time.Duration) { close(finished) })
	require.Equal(t, positions.Queued, f.pool.Offer(f.ctx, first))
	require.Equal(t, positions.Queued, f.pool.Offer(f.ctx, second))

	f.start()
	receive(t, finished)
}

func TestTicksNeverStartOverlappingScans(t *testing.T) {
	f := newFixture(t, 1, 1)
	entered, release, second := make(chan struct{}), make(chan struct{}), make(chan struct{})
	f.repo.EXPECT().Stale(gomock.Any(), f.now, f.cfg.StaleAfter).DoAndReturn(func(context.Context, time.Time, time.Duration) ([]model.StaleTrip, error) {
		close(entered)
		<-release
		return nil, nil
	})
	f.repo.EXPECT().Stale(gomock.Any(), f.now, f.cfg.StaleAfter).DoAndReturn(func(context.Context, time.Time, time.Duration) ([]model.StaleTrip, error) {
		close(second)
		return nil, nil
	})
	f.start()
	f.tick()
	receive(t, entered)
	select {
	case f.ticks <- f.now:
		t.Fatal("scanner accepted another tick before finishing the first scan")
	default:
	}
	close(release)
	f.tick()
	receive(t, second)
}

func TestShutdownDrainsBufferedTasks(t *testing.T) {
	f := newFixture(t, 1, 2)
	entered, release := make(chan struct{}), make(chan struct{})
	first, second := candidate(1), candidate(2)
	f.push.EXPECT().RequestPosition(gomock.Any(), first).DoAndReturn(func(context.Context, model.StaleTrip) error {
		close(entered)
		<-release
		return nil
	})
	f.push.EXPECT().RequestPosition(gomock.Any(), second).Return(nil)
	f.metrics.EXPECT().Request(gomock.Any(), positions.Success, time.Duration(0)).Times(2)
	require.Equal(t, positions.Queued, f.pool.Offer(f.ctx, first))
	require.Equal(t, positions.Queued, f.pool.Offer(f.ctx, second))
	f.start()
	receive(t, entered)

	f.cancel()
	receive(t, f.stopped)
	close(release)
	require.NoError(t, f.pool.Wait(context.Background()))
}

func TestShutdownBudgetCancelsInFlightRequest(t *testing.T) {
	f := newFixture(t, 1, 2)
	entered := make(chan struct{})
	trip := candidate(1)
	f.push.EXPECT().RequestPosition(gomock.Any(), trip).DoAndReturn(func(ctx context.Context, _ model.StaleTrip) error {
		close(entered)
		<-ctx.Done()
		return ctx.Err()
	})
	f.logger.EXPECT().WarnContext(gomock.Any(), "position push failed", "trip_id", trip.ID.String(), "error", context.Canceled)
	f.metrics.EXPECT().Request(gomock.Any(), positions.Failed, time.Duration(0))
	require.Equal(t, positions.Queued, f.pool.Offer(f.ctx, trip))
	f.start()
	receive(t, entered)
	f.cancel()

	budget, cancel := context.WithCancel(context.Background())
	cancel()
	f.wantStopErr = context.DeadlineExceeded
	require.ErrorIs(t, f.pool.Wait(budget), context.Canceled)
}

func TestWorkerSpanLinksToScanWithoutBecomingItsChild(t *testing.T) {
	f := newFixture(t, 1, 1)
	trip := candidate(1)
	processed := make(chan struct{})
	f.repo.EXPECT().Stale(gomock.Any(), f.now, f.cfg.StaleAfter).Return([]model.StaleTrip{trip}, nil)
	f.metrics.EXPECT().StaleCheck(gomock.Any(), positions.Queued)
	f.push.EXPECT().RequestPosition(gomock.Any(), trip).Return(nil)
	f.metrics.EXPECT().Request(gomock.Any(), positions.Success, time.Duration(0)).Do(func(context.Context, string, time.Duration) { close(processed) })
	f.start()
	f.tick()
	receive(t, processed)
	f.cancel()
	require.NoError(t, f.pool.Wait(context.Background()))

	spans := f.exporter.GetSpans()
	require.Len(t, spans, 2)
	byName := make(map[string]tracetest.SpanStub)
	for _, span := range spans {
		byName[span.Name] = span
	}
	scan, task := byName["positions.scan"], byName["positions.request"]
	require.Len(t, task.Links, 1)
	assert.Equal(t, scan.SpanContext, task.Links[0].SpanContext)
	assert.False(t, task.Parent.IsValid())
	assert.NotEqual(t, scan.SpanContext.TraceID(), task.SpanContext.TraceID())
	assert.Equal(t, trace.SpanKindInternal, task.SpanKind)
}

func TestCoordinateCancelsTaskWhilePoolIsRunning(t *testing.T) {
	f := newFixture(t, 1, 2)
	first, waiting := candidate(1), candidate(2)
	entered, release, cancelled := make(chan struct{}), make(chan struct{}), make(chan struct{})
	f.push.EXPECT().RequestPosition(gomock.Any(), first).DoAndReturn(func(context.Context, model.StaleTrip) error {
		close(entered)
		<-release
		return nil
	})
	f.metrics.EXPECT().Request(gomock.Any(), positions.Success, time.Duration(0))
	f.metrics.EXPECT().Request(gomock.Any(), positions.Cancelled, time.Duration(0)).Do(func(context.Context, string, time.Duration) {
		close(cancelled)
	})
	require.Equal(t, positions.Queued, f.pool.Offer(f.ctx, first))
	f.start()
	receive(t, entered)

	require.Equal(t, positions.Queued, f.pool.Offer(f.ctx, waiting))
	f.pool.Cancel(waiting.ID)
	close(release)

	receive(t, cancelled)
}
