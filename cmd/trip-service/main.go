package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Makk228/avito-labs/internal/config"
	"github.com/Makk228/avito-labs/internal/httpapi"
	"github.com/Makk228/avito-labs/internal/postgres"
	"github.com/Makk228/avito-labs/internal/trip"
)

const serviceName = "trip-service"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", serviceName, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel})).
		With(slog.String("service", serviceName))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	switch {
	case len(args) == 0:
		return serve(ctx, stop, cfg, log)
	case args[0] == "migrate" && len(args) == 2:
		return migrate(ctx, cfg, log, args[1])
	default:
		return fmt.Errorf("unknown arguments %q, usage: %s [migrate up|down|reset|status]", args, serviceName)
	}
}

func migrate(ctx context.Context, cfg config.Config, log *slog.Logger, command string) error {
	pool, err := postgres.NewPool(ctx, cfg.DB)
	if err != nil {
		return err
	}
	defer pool.Close()

	return postgres.Migrate(ctx, pool, command, log)
}

func serve(ctx context.Context, stopSignals context.CancelFunc, cfg config.Config, log *slog.Logger) error {
	pool, err := postgres.NewPool(ctx, cfg.DB)
	if err != nil {
		return err
	}

	listener, err := net.Listen("tcp", cfg.HTTP.Addr)
	if err != nil {
		pool.Close()
		return fmt.Errorf("listen on %s: %w", cfg.HTTP.Addr, err)
	}

	txManager := postgres.NewTxManager(pool, cfg.DB.QueryTimeout)
	trips := trip.NewService(
		txManager,
		postgres.NewTripRepository(pool, cfg.DB.QueryTimeout),
		postgres.NewIdempotencyRepository(pool, cfg.DB.QueryTimeout),
		cfg.Idempotency.KeyTTL,
	)
	handler := httpapi.NewHandler(trips, pool, cfg.DB.QueryTimeout, log)
	server := httpapi.NewServer(cfg.HTTP, httpapi.NewRouter(handler, log), log)

	bgCtx, stopBackground := context.WithCancel(context.Background())
	defer stopBackground()
	var background sync.WaitGroup
	background.Go(func() {
		trips.RunKeyCleanup(bgCtx, cfg.Idempotency.CleanupInterval, log)
	})

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- server.Serve(listener)
	}()
	log.Info("http server started", slog.String("addr", listener.Addr().String()))

	var runErr error
	select {
	case <-ctx.Done():
		log.Info("shutdown signal received", slog.String("timeout", cfg.ShutdownTimeout.String()))
	case err := <-serveErr:
		runErr = fmt.Errorf("http server: %w", err)
	}
	stopSignals()

	shutdown(server, stopBackground, &background, pool, cfg.ShutdownTimeout, log)
	return runErr
}

func shutdown(
	server *http.Server,
	stopBackground context.CancelFunc,
	background *sync.WaitGroup,
	pool *pgxpool.Pool,
	timeout time.Duration,
	log *slog.Logger,
) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		_ = server.Close()
	}

	stopBackground()
	backgroundStopped := waitDone(ctx, background.Wait)
	poolClosed := waitDone(ctx, pool.Close)

	if ctx.Err() != nil || !backgroundStopped || !poolClosed {
		log.Error("shutdown timeout exceeded, exiting forcibly", slog.String("timeout", timeout.String()))
		return
	}
	log.Info("service stopped")
}

func waitDone(ctx context.Context, fn func()) bool {
	done := make(chan struct{})
	go func() {
		fn()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-ctx.Done():
		return false
	}
}
