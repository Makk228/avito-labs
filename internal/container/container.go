// Package container is the application's composition root.
package container

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Makk228/avito-labs/internal/config"
	"github.com/Makk228/avito-labs/internal/gateway/push"
	transport "github.com/Makk228/avito-labs/internal/http"
	"github.com/Makk228/avito-labs/internal/http/middleware"
	listhandler "github.com/Makk228/avito-labs/internal/http/position/list"
	savehandler "github.com/Makk228/avito-labs/internal/http/position/save"
	createhandler "github.com/Makk228/avito-labs/internal/http/trip/create"
	finishhandler "github.com/Makk228/avito-labs/internal/http/trip/finish"
	gethandler "github.com/Makk228/avito-labs/internal/http/trip/get"
	"github.com/Makk228/avito-labs/internal/infra/postgres"
	"github.com/Makk228/avito-labs/internal/infra/postgres/transaction"
	"github.com/Makk228/avito-labs/internal/infra/system"
	"github.com/Makk228/avito-labs/internal/infra/telemetry"
	"github.com/Makk228/avito-labs/internal/repository/idempotency"
	positionrepo "github.com/Makk228/avito-labs/internal/repository/position"
	triprepo "github.com/Makk228/avito-labs/internal/repository/trip"
	"github.com/Makk228/avito-labs/internal/usecase/position/list"
	"github.com/Makk228/avito-labs/internal/usecase/position/save"
	"github.com/Makk228/avito-labs/internal/usecase/trip/create"
	"github.com/Makk228/avito-labs/internal/usecase/trip/finish"
	"github.com/Makk228/avito-labs/internal/usecase/trip/get"
	"github.com/Makk228/avito-labs/internal/worker/keycleanup"
	"github.com/Makk228/avito-labs/internal/worker/positions"
)

type Container struct {
	cfg       config.Config
	log       *slog.Logger
	pool      *pgxpool.Pool
	telemetry *telemetry.Providers
	server    *http.Server
	cleanup   *keycleanup.Worker
	positions *positions.Worker
	push      *push.Gateway
}

func New(ctx context.Context, cfg config.Config) (*Container, error) {
	log := telemetry.NewLogger(os.Stdout, cfg.LogLevel)
	providers, err := telemetry.New(ctx, cfg.Telemetry, log)
	if err != nil {
		return nil, err
	}

	c := &Container{cfg: cfg, log: log, telemetry: providers}
	if err := c.build(ctx); err != nil {
		closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cfg.ShutdownTimeout)
		defer cancel()
		return nil, errors.Join(err, c.Close(closeCtx))
	}

	return c, nil
}

func (c *Container) build(ctx context.Context) error {
	meter := c.telemetry.Meters.Meter("trip-service")
	tracer, err := postgres.NewQueryTracer(c.telemetry.Traces, meter)
	if err != nil {
		return err
	}

	poolCfg, err := postgres.PoolConfig(c.cfg.DB, tracer)
	if err != nil {
		return err
	}

	c.pool, err = pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return fmt.Errorf("create database pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, c.cfg.DB.ConnectTimeout)
	defer cancel()
	if err := c.pool.Ping(pingCtx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}

	tx := transaction.NewTxManager(c.pool, c.cfg.DB.QueryTimeout)
	trips := triprepo.NewTripRepository(c.pool, c.cfg.DB.QueryTimeout)
	keys := idempotency.NewIdempotencyRepository(c.pool, c.cfg.DB.QueryTimeout)
	counters, err := telemetry.NewCounters(meter, trips)
	if err != nil {
		return err
	}

	runtime := system.Runtime{}
	positionMetrics, err := telemetry.NewPositionMetrics(meter)
	if err != nil {
		return err
	}

	c.push, err = push.New(push.Dependencies{IDs: runtime, Traces: c.telemetry.Traces}, push.Config{
		URL:     c.cfg.Push.URL,
		Timeout: c.cfg.Push.RequestTimeout,
	})
	if err != nil {
		return err
	}

	c.positions = positions.New(positions.Dependencies{
		Trips:   trips,
		Push:    c.push,
		Clock:   runtime,
		Ticker:  &system.Ticker{},
		Logger:  c.log,
		Metrics: positionMetrics,
		Tracer:  c.telemetry.Traces.Tracer("positions"),
	}, positions.Config{
		CheckInterval:   c.cfg.Positions.CheckInterval,
		StaleAfter:      c.cfg.Positions.StaleAfter,
		Workers:         c.cfg.Positions.Workers,
		QueueSize:       c.cfg.Positions.QueueSize,
		RequestTimeout:  c.cfg.Push.RequestTimeout,
		ShutdownTimeout: c.cfg.ShutdownTimeout,
	})
	points := positionrepo.New(c.pool, c.cfg.DB.QueryTimeout)
	saveUC := save.New(save.Dependencies{
		Transactions: tx,
		Trips:        trips,
		Positions:    points,
		Queue:        c.positions,
		Clock:        runtime,
		Metrics:      positionMetrics,
	})
	listUC := list.New(list.Dependencies{Trips: trips, Positions: points})
	createUC := create.New(create.Dependencies{
		Transactions: tx,
		Trips:        trips,
		Idempotency:  keys,
		Clock:        runtime,
		IDs:          runtime,
		Logger:       c.log,
		Metrics:      counters,
	}, create.Config{KeyTTL: c.cfg.Idempotency.KeyTTL})
	getUC := get.New(trips)
	finishUC := finish.New(finish.Dependencies{
		Transactions: tx,
		Trips:        trips,
		Clock:        runtime,
		Logger:       c.log,
		Metrics:      counters,
	})
	mw, err := middleware.New(c.log, c.telemetry.Traces, meter)
	if err != nil {
		return err
	}

	handler := transport.NewHandler(transport.Dependencies{
		Create:        createhandler.New(createUC),
		Get:           gethandler.New(getUC),
		Finish:        finishhandler.New(finishUC),
		SavePosition:  savehandler.New(saveUC),
		ListPositions: listhandler.New(listUC),
		Database:      c.pool,
	}, c.cfg.DB.QueryTimeout)
	c.server = transport.NewServer(c.cfg.HTTP, transport.NewRouter(handler, mw), c.log)
	c.cleanup = keycleanup.New(keys, c.cfg.Idempotency.CleanupInterval, c.log)
	return nil
}

func (c *Container) Migrate(ctx context.Context, command string) error {
	ctx, cancel := context.WithTimeout(ctx, c.cfg.DB.QueryTimeout)
	defer cancel()
	return postgres.Migrate(ctx, c.pool, command, c.log)
}

func (c *Container) Run(ctx context.Context) error {
	listener, err := net.Listen("tcp", c.cfg.HTTP.Addr)
	if err != nil {
		closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), c.cfg.ShutdownTimeout)
		defer cancel()
		return errors.Join(fmt.Errorf("listen: %w", err), c.Close(closeCtx))
	}

	bgCtx, stop := context.WithCancel(context.WithoutCancel(ctx))
	var background sync.WaitGroup
	background.Go(func() { c.cleanup.Run(bgCtx) })
	background.Go(func() { c.positions.Run(bgCtx) })
	serveErr := make(chan error, 1)
	go func() { serveErr <- c.server.Serve(listener) }()
	c.log.InfoContext(ctx, "http server started", slog.String("addr", listener.Addr().String()))
	var runErr error
	select {
	case <-ctx.Done():
	case err := <-serveErr:
		if !errors.Is(err, http.ErrServerClosed) {
			runErr = fmt.Errorf("http server: %w", err)
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), c.cfg.ShutdownTimeout)
	defer cancel()
	if err := c.server.Shutdown(shutdownCtx); err != nil {
		runErr = errors.Join(runErr, err, c.server.Close())
	}

	stop()
	if err := c.positions.Wait(shutdownCtx); err != nil {
		c.log.ErrorContext(shutdownCtx, "position workers exceeded shutdown budget", slog.Any("error", err))
		runErr = errors.Join(runErr, err)
	}
	done := make(chan struct{})
	go func() { background.Wait(); close(done) }()
	select {
	case <-done:
	case <-shutdownCtx.Done():
		runErr = errors.Join(runErr, shutdownCtx.Err())
	}

	return errors.Join(runErr, c.Close(shutdownCtx))
}

func (c *Container) Close(ctx context.Context) error {
	var closeErr error
	if c.push != nil {
		c.push.Close()
	}
	// Final metric collection calls CountActive, so telemetry must flush while
	// the pool is still open. Tracing is shut down after metric collection.
	if c.telemetry != nil {
		closeErr = c.telemetry.Shutdown(ctx)
	}

	if c.pool != nil {
		done := make(chan struct{})
		go func() { c.pool.Close(); close(done) }()
		select {
		case <-done:
		case <-ctx.Done():
			c.log.ErrorContext(ctx, "shutdown timeout exceeded, exiting forcibly", slog.Any("error", ctx.Err()))
			return errors.Join(closeErr, fmt.Errorf("close database pool: %w", ctx.Err()))
		}
	}

	if closeErr != nil {
		if ctx.Err() != nil {
			c.log.ErrorContext(ctx, "shutdown timeout exceeded, exiting forcibly", slog.Any("error", closeErr))
		}

		return fmt.Errorf("shutdown telemetry: %w", closeErr)
	}

	c.log.InfoContext(ctx, "service stopped")
	return nil
}
