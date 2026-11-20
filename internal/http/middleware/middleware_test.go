package middleware_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/codes"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/Makk228/avito-labs/internal/http/middleware"
)

const route = "/api/v1/trips/{tripId}"

type signals struct {
	logs       bytes.Buffer
	exporter   *tracetest.InMemoryExporter
	reader     *sdkmetric.ManualReader
	middleware *middleware.Middleware
}

func newSignals(t *testing.T) *signals {
	t.Helper()
	s := &signals{
		exporter: tracetest.NewInMemoryExporter(),
		reader:   sdkmetric.NewManualReader(),
	}

	log := slog.New(slog.NewJSONHandler(&s.logs, nil)).With("service", "trip-service")
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(s.exporter))
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(s.reader))
	t.Cleanup(func() {
		assert.NoError(t, tp.Shutdown(context.Background()))
		assert.NoError(t, mp.Shutdown(context.Background()))
	})
	var err error
	s.middleware, err = middleware.New(log, tp, mp.Meter("test"))
	require.NoError(t, err)
	return s
}

func (s *signals) collect(t *testing.T) metricdata.ResourceMetrics {
	t.Helper()
	var data metricdata.ResourceMetrics
	require.NoError(t, s.reader.Collect(context.Background(), &data))
	return data
}

func TestSignals(t *testing.T) {
	for _, tc := range []struct {
		name         string
		status       int
		wantLevel    string
		wantSpanCode codes.Code
	}{
		{name: "ok", status: http.StatusOK, wantLevel: "INFO", wantSpanCode: codes.Unset},
		{name: "client error", status: http.StatusConflict, wantLevel: "INFO", wantSpanCode: codes.Error},
		{name: "server error", status: http.StatusInternalServerError, wantLevel: "ERROR", wantSpanCode: codes.Error},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newSignals(t)
			handler := s.middleware.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.True(t, trace.SpanContextFromContext(r.Context()).IsValid())
				assertActive(t, s.collect(t), 1)
				w.WriteHeader(tc.status)
			}), func(*http.Request) string { return route })
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/trips/123", nil))

			assert.Equal(t, tc.status, w.Code)
			s.assertRequest(t, tc.status, tc.wantLevel, tc.wantSpanCode)
		})
	}
}

func TestRecoveryDiscardsPartialResponse(t *testing.T) {
	s := newSignals(t)
	handler := s.middleware.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		assertActive(t, s.collect(t), 1)
		w.Header().Set("Location", "/private")
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte("private partial response"))
		require.NoError(t, err)
		panic("private panic detail")
	}), func(*http.Request) string { return route })
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/trips/123", nil))

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "internal_error")
	assert.NotContains(t, w.Body.String(), "private")
	assert.Empty(t, w.Header().Get("Location"))
	assert.Contains(t, s.logs.String(), "panic in http handler")
	s.assertRequest(t, http.StatusInternalServerError, "ERROR", codes.Error)
}

func (s *signals) assertRequest(t *testing.T, status int, level string, spanCode codes.Code) {
	t.Helper()
	var requests []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(s.logs.String()), "\n") {
		var entry map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &entry))
		if entry["msg"] == "request handled" {
			requests = append(requests, entry)
		}
	}

	require.Len(t, requests, 1)
	assert.Equal(t, float64(status), requests[0]["status"])
	assert.Equal(t, route, requests[0]["route"])
	assert.Equal(t, level, requests[0]["level"])

	spans := s.exporter.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, "GET "+route, spans[0].Name)
	assert.Equal(t, spanCode, spans[0].Status.Code)
	attributes := make(map[string]any)
	for _, attr := range spans[0].Attributes {
		attributes[string(attr.Key)] = attr.Value.AsInterface()
	}

	assert.Equal(t, int64(status), attributes["http.response.status_code"])

	data := s.collect(t)
	assertActive(t, data, 0)
	assertDuration(t, data, status)
}

func metric(t *testing.T, data metricdata.ResourceMetrics, name string) metricdata.Metrics {
	t.Helper()
	for _, scope := range data.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name == name {
				return m
			}
		}
	}

	t.Fatalf("metric %s missing", name)
	return metricdata.Metrics{}
}

func assertActive(t *testing.T, data metricdata.ResourceMetrics, want int64) {
	t.Helper()
	m := metric(t, data, "http.server.active_requests")
	sum, ok := m.Data.(metricdata.Sum[int64])
	require.True(t, ok, "unexpected metric: %+v", m)
	require.Len(t, sum.DataPoints, 1)
	assert.Equal(t, want, sum.DataPoints[0].Value)
}

func assertDuration(t *testing.T, data metricdata.ResourceMetrics, expectedStatus int) {
	t.Helper()
	m := metric(t, data, "http.server.request.duration")
	hist, ok := m.Data.(metricdata.Histogram[float64])
	require.True(t, ok, "unexpected metric: %+v", m)
	require.Len(t, hist.DataPoints, 1)
	assert.EqualValues(t, 1, hist.DataPoints[0].Count)
	assert.Equal(t, "s", m.Unit)
	status, found := hist.DataPoints[0].Attributes.Value("http.response.status_code")
	require.True(t, found)
	assert.Equal(t, int64(expectedStatus), status.AsInt64())
}
