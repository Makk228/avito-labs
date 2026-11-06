//go:build integration

package container_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	pgcontainer "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/Makk228/avito-labs/internal/config"
	"github.com/Makk228/avito-labs/internal/container"
)

func TestShutdownCollectsGaugeBeforeClosingDatabase(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	pg, err := pgcontainer.Run(ctx, "postgres:17-alpine", pgcontainer.WithDatabase("tripgo"), pgcontainer.WithUsername("tripgo"), pgcontainer.WithPassword("tripgo"), pgcontainer.BasicWaitStrategies())
	if err != nil {
		t.Fatal(err)
	}
	testcontainers.CleanupContainer(t, pg)
	databaseURL, err := pg.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(http.StatusOK)
	}))
	defer collector.Close()
	cfg := config.Config{
		ShutdownTimeout: 5 * time.Second,
		HTTP:            config.HTTP{Addr: "127.0.0.1:0", ReadTimeout: time.Second, ReadHeaderTimeout: time.Second, WriteTimeout: time.Second, IdleTimeout: time.Second},
		DB:              config.DB{URL: databaseURL, MaxConns: 2, MinConns: 0, MaxConnLifetime: time.Minute, ConnectTimeout: 5 * time.Second, QueryTimeout: time.Second},
		Idempotency:     config.Idempotency{KeyTTL: time.Hour, CleanupInterval: time.Hour},
		Telemetry:       config.Telemetry{Endpoint: collector.URL, Protocol: "http/protobuf", Version: "test"},
	}
	app, err := container.New(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Migrate(ctx, "up"); err != nil {
		t.Fatal(err)
	}
	// A canceled lifecycle context deterministically triggers graceful shutdown.
	// MeterProvider.Shutdown performs a final collection of the database gauge.
	runCtx, stop := context.WithCancel(ctx)
	stop()
	if err := app.Run(runCtx); err != nil {
		t.Fatalf("graceful shutdown failed: %v", err)
	}
}
