//go:build integration

package push

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/Makk228/avito-labs/internal/infra/system"
	"github.com/Makk228/avito-labs/internal/model"
)

func TestRealHTTPContractAndTracePropagation(t *testing.T) {
	received := make(chan command, 1)
	headers := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/base/api/v1/push", r.URL.Path)
		assert.Equal(t, http.MethodPost, r.Method)
		var value command
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&value))
		received <- value
		headers <- r.Header.Get("traceparent")
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	defer func() { assert.NoError(t, tp.Shutdown(context.Background())) }()
	gateway, err := New(Dependencies{IDs: system.Runtime{}, Traces: tp}, Config{URL: server.URL + "/base", Timeout: time.Second})
	require.NoError(t, err)
	defer gateway.Close()
	ctx, parent := tp.Tracer("test").Start(context.Background(), "parent")
	trip := model.StaleTrip{ID: uuid.New(), DriverID: uuid.New()}

	require.NoError(t, gateway.RequestPosition(ctx, trip))
	parent.End()

	value := <-received
	assert.Equal(t, trip.DriverID, value.RecipientID)
	assert.Equal(t, "REQUEST_POSITION", value.Kind)
	assert.Equal(t, map[string]string{"trip_id": trip.ID.String()}, value.Data)
	_, err = uuid.Parse(value.RequestID)
	require.NoError(t, err)
	assert.Contains(t, <-headers, parent.SpanContext().TraceID().String())
	require.Len(t, exporter.GetSpans(), 2)
}

func TestRealHTTPRequestStopsOnContextCancellation(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(entered)
		<-release
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()
	tp := sdktrace.NewTracerProvider()
	defer func() { assert.NoError(t, tp.Shutdown(context.Background())) }()
	gateway, err := New(Dependencies{IDs: system.Runtime{}, Traces: tp}, Config{URL: server.URL, Timeout: time.Second})
	require.NoError(t, err)
	defer gateway.Close()
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- gateway.RequestPosition(ctx, model.StaleTrip{ID: uuid.New(), DriverID: uuid.New()}) }()
	<-entered

	cancel()
	require.ErrorIs(t, <-result, context.Canceled)
	close(release)
}
