package telemetry_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"go.opentelemetry.io/otel/trace"

	"github.com/Makk228/avito-labs/internal/infra/telemetry"
)

func TestLoggerCorrelation(t *testing.T) {
	var out bytes.Buffer
	id, err := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	if err != nil {
		t.Fatal(err)
	}
	span, err := trace.SpanIDFromHex("00f067aa0ba902b7")
	if err != nil {
		t.Fatal(err)
	}
	ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{TraceID: id, SpanID: span}))
	log := telemetry.NewLogger(&out, slog.LevelInfo).With("component", "test")
	log.DebugContext(ctx, "must be filtered")
	log.InfoContext(ctx, "correlated")
	var record map[string]any
	if err := json.Unmarshal(out.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record["trace_id"] != id.String() || record["span_id"] != span.String() || record["service"] != "trip-service" || record["msg"] != "correlated" || record["component"] != "test" {
		t.Fatalf("bad log: %s", out.String())
	}
}
