package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/Makk228/avito-labs/internal/config"
	"github.com/Makk228/avito-labs/internal/container"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "trip-service: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) != 0 && (len(args) != 2 || args[0] != "migrate") {
		return fmt.Errorf("unknown arguments %q, usage: trip-service [migrate up|down|reset|status]", args)
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	app, err := container.New(ctx, cfg)
	if err != nil {
		return err
	}
	if len(args) == 0 {
		return app.Run(ctx)
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		_ = app.Close(closeCtx)
	}()
	return app.Migrate(ctx, args[1])
}
