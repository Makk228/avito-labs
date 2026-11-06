//go:build integration

package telemetry_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	metricpb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	tracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/protobuf/proto"

	"github.com/Makk228/avito-labs/internal/config"
	"github.com/Makk228/avito-labs/internal/infra/telemetry"
)

// Exercise real exporter HTTP requests and protobuf payloads, including a base
// path. This catches SDK endpoint semantics changes that in-memory SDK tests miss.
func TestOTLPHTTPExport(t *testing.T) {
	t.Setenv("OTEL_TRACES_SAMPLER", "always_on")
	t.Setenv("OTEL_METRIC_EXPORT_INTERVAL", "600000")
	for _, prefix := range []string{"", "/otlp"} {
		t.Run("prefix="+prefix, func(t *testing.T) {
			var mu sync.Mutex
			paths := map[string]int{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				switch r.URL.Path {
				case prefix + "/v1/traces":
					var data tracepb.ExportTraceServiceRequest
					if err := proto.Unmarshal(body, &data); err != nil || len(data.ResourceSpans) == 0 {
						t.Errorf("invalid trace payload: %v", err)
					}
				case prefix + "/v1/metrics":
					var data metricpb.ExportMetricsServiceRequest
					if err := proto.Unmarshal(body, &data); err != nil || len(data.ResourceMetrics) == 0 {
						t.Errorf("invalid metric payload: %v", err)
					}
				default:
					t.Errorf("incorrect OTLP path: %s", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
					return
				}
				mu.Lock()
				paths[r.URL.Path]++
				mu.Unlock()
				w.Header().Set("Content-Type", "application/x-protobuf")
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			providers, err := telemetry.New(ctx, config.Telemetry{Endpoint: server.URL + prefix, Protocol: "http/protobuf", Version: "test"}, slog.New(slog.NewJSONHandler(io.Discard, nil)))
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := providers.Shutdown(ctx); err != nil {
					t.Error(err)
				}
			}()
			_, span := providers.Traces.Tracer("test").Start(ctx, "export-test")
			span.End()
			counter, err := providers.Meters.Meter("test").Int64Counter("export.test")
			if err != nil {
				t.Fatal(err)
			}
			counter.Add(ctx, 1)
			if err := providers.Traces.ForceFlush(ctx); err != nil {
				t.Fatal(err)
			}
			if err := providers.Meters.ForceFlush(ctx); err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			defer mu.Unlock()
			if paths[prefix+"/v1/traces"] != 1 || paths[prefix+"/v1/metrics"] != 1 {
				t.Fatalf("missing signal requests: %v", paths)
			}
		})
	}
}
