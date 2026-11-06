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

	"go.opentelemetry.io/otel/codes"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/Makk228/avito-labs/internal/http/middleware"
)

func TestSignalsAndRecovery(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		panic  bool
	}{{"ok", 200, false}, {"client error", 409, false}, {"server error", 500, false}, {"panic", 500, true}} {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			log := slog.New(slog.NewJSONHandler(&logs, nil)).With("service", "trip-service")
			exporter := tracetest.NewInMemoryExporter()
			tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
			reader := sdkmetric.NewManualReader()
			mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
			t.Cleanup(func() {
				if err := tp.Shutdown(context.Background()); err != nil {
					t.Error(err)
				}
				if err := mp.Shutdown(context.Background()); err != nil {
					t.Error(err)
				}
			})
			mw, err := middleware.New(log, tp, mp.Meter("test"))
			if err != nil {
				t.Fatal(err)
			}
			handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !trace.SpanContextFromContext(r.Context()).IsValid() {
					t.Error("missing request span")
				}
				// Collect while the handler is running: active requests must be 1.
				var data metricdata.ResourceMetrics
				if err := reader.Collect(r.Context(), &data); err != nil {
					t.Fatal(err)
				}
				assertActive(t, data, 1)
				if tc.panic {
					w.Header().Set("Location", "/private")
					w.WriteHeader(http.StatusOK)
					if _, err := w.Write([]byte("private partial response")); err != nil {
						t.Fatal(err)
					}
					panic("private panic detail")
				}
				w.WriteHeader(tc.status)
			}), func(*http.Request) string { return "/api/v1/trips/{tripId}" })
			w := httptest.NewRecorder()
			r := httptest.NewRequest("GET", "/api/v1/trips/123", nil)
			handler.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			if tc.panic && (!strings.Contains(w.Body.String(), "internal_error") || strings.Contains(w.Body.String(), "private") || w.Header().Get("Location") != "") {
				t.Fatal(w.Body.String())
			}
			var requestLog map[string]any
			requests := 0
			for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
				var entry map[string]any
				if err := json.Unmarshal([]byte(line), &entry); err != nil {
					t.Fatal(err)
				}
				if entry["msg"] == "request handled" {
					requests++
					requestLog = entry
				}
			}
			level := "INFO"
			if tc.status >= 500 {
				level = "ERROR"
			}
			if requests != 1 || requestLog["status"] != float64(tc.status) || requestLog["route"] != "/api/v1/trips/{tripId}" || requestLog["level"] != level {
				t.Fatalf("bad log: %s", logs.String())
			}
			spans := exporter.GetSpans()
			if len(spans) != 1 || spans[0].Name != "GET /api/v1/trips/{tripId}" {
				t.Fatalf("unexpected spans: %+v", spans)
			}
			if tc.status >= 400 && spans[0].Status.Code != codes.Error {
				t.Fatal("error span missing status")
			}
			for _, attr := range spans[0].Attributes {
				if attr.Key == "http.response.status_code" && attr.Value.AsInt64() != int64(tc.status) {
					t.Fatalf("incorrect span status: %+v", attr)
				}
			}
			var data metricdata.ResourceMetrics
			if err := reader.Collect(context.Background(), &data); err != nil {
				t.Fatal(err)
			}
			assertActive(t, data, 0)
			assertDuration(t, data, tc.status)
		})
	}
}

func assertActive(t *testing.T, data metricdata.ResourceMetrics, want int64) {
	t.Helper()
	for _, scope := range data.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name == "http.server.active_requests" {
				sum, ok := m.Data.(metricdata.Sum[int64])
				if !ok || len(sum.DataPoints) != 1 || sum.DataPoints[0].Value != want {
					t.Fatalf("active requests: %+v, want %d", m, want)
				}
				return
			}
		}
	}
	t.Fatal("active requests metric missing")
}

func assertDuration(t *testing.T, data metricdata.ResourceMetrics, expectedStatus int) {
	t.Helper()
	found := false
	for _, scope := range data.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name == "http.server.request.duration" {
				found = true
				hist, ok := m.Data.(metricdata.Histogram[float64])
				if !ok || len(hist.DataPoints) != 1 || hist.DataPoints[0].Count != 1 || m.Unit != "s" {
					t.Fatalf("bad histogram: %+v", m)
				}
				status, _ := hist.DataPoints[0].Attributes.Value("http.response.status_code")
				if status.AsInt64() != int64(expectedStatus) {
					t.Fatal("wrong status label")
				}
			}
		}
	}
	if !found {
		t.Fatal("duration metric missing")
	}
}
