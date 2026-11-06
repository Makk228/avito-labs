// Package keycleanup maintains idempotency records inherited from lab 1.
package keycleanup

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

type Worker struct {
	keys     store
	interval time.Duration
	log      *slog.Logger
}

func New(keys store, interval time.Duration, log *slog.Logger) *Worker {
	return &Worker{keys: keys, interval: interval, log: log}
}

func (s *Worker) Run(ctx context.Context) {
	s.log.InfoContext(ctx, "idempotency cleanup started")
	defer s.log.InfoContext(ctx, "idempotency cleanup stopped")
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			deleted, err := s.keys.DeleteExpired(ctx, time.Now().UTC())
			switch {
			case errors.Is(err, context.Canceled):
				return
			case err != nil:
				s.log.WarnContext(ctx, "idempotency keys cleanup failed", slog.Any("error", err))
			case deleted > 0:
				s.log.DebugContext(ctx, "expired idempotency keys deleted", slog.Int64("count", deleted))
			}
		}
	}
}
