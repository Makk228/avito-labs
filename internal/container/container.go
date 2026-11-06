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
	transport "github.com/Makk228/avito-labs/internal/http"
	"github.com/Makk228/avito-labs/internal/http/middleware"
	createhandler "github.com/Makk228/avito-labs/internal/http/trip/create"
	finishhandler "github.com/Makk228/avito-labs/internal/http/trip/finish"
	gethandler "github.com/Makk228/avito-labs/internal/http/trip/get"
	"github.com/Makk228/avito-labs/internal/infra/postgres"
	"github.com/Makk228/avito-labs/internal/infra/postgres/transaction"
	"github.com/Makk228/avito-labs/internal/infra/telemetry"
	"github.com/Makk228/avito-labs/internal/repository/idempotency"
	triprepo "github.com/Makk228/avito-labs/internal/repository/trip"
	"github.com/Makk228/avito-labs/internal/usecase/trip/create"
	"github.com/Makk228/avito-labs/internal/usecase/trip/finish"
	"github.com/Makk228/avito-labs/internal/usecase/trip/get"
	"github.com/Makk228/avito-labs/internal/worker/keycleanup"
)

type Container struct {
	cfg       config.Config
	log       *slog.Logger
	pool      *pgxpool.Pool
	telemetry *telemetry.Providers
	server    *http.Server
	cleanup   *keycleanup.Worker
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
	createUC := create.New(tx, trips, keys, c.cfg.Idempotency.KeyTTL, c.log, counters)
	getUC := get.New(trips)
	finishUC := finish.New(tx, trips, c.log, counters)
	mw, err := middleware.New(c.log, c.telemetry.Traces, meter)
	if err != nil {
		return err
	}
	handler := transport.NewHandler(createhandler.New(createUC), gethandler.New(getUC), finishhandler.New(finishUC), c.pool, c.cfg.DB.QueryTimeout)
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
