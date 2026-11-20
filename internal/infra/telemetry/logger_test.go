package telemetry_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"

	"github.com/Makk228/avito-labs/internal/infra/telemetry"
)

func TestLoggerCorrelation(t *testing.T) {
	var out bytes.Buffer
	id, err := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	require.NoError(t, err)
	span, err := trace.SpanIDFromHex("00f067aa0ba902b7")
	require.NoError(t, err)
	ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: id,
		SpanID:  span,
	}))
	log := telemetry.NewLogger(&out, slog.LevelInfo).With("component", "test")

	log.DebugContext(ctx, "must be filtered")
	log.InfoContext(ctx, "correlated")

	var record map[string]any
	require.NoError(t, json.Unmarshal(out.Bytes(), &record))
	assert.Equal(t, id.String(), record["trace_id"])
	assert.Equal(t, span.String(), record["span_id"])
	assert.Equal(t, "trip-service", record["service"])
	assert.Equal(t, "correlated", record["msg"])
	assert.Equal(t, "test", record["component"])
}
