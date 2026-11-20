package keycleanup_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/Makk228/avito-labs/internal/worker/keycleanup"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

func TestAlreadyCancelledWorkerStopsWithoutAccessingStore(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	log := slog.New(slog.NewJSONHandler(io.Discard, nil))
	worker := keycleanup.New(nil, time.Hour, log)

	worker.Run(ctx)
}
