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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
					assert.NoError(t, proto.Unmarshal(body, &data))
					assert.NotEmpty(t, data.ResourceSpans)
				case prefix + "/v1/metrics":
					var data metricpb.ExportMetricsServiceRequest
					assert.NoError(t, proto.Unmarshal(body, &data))
					assert.NotEmpty(t, data.ResourceMetrics)
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
			cfg := config.Telemetry{
				Endpoint: server.URL + prefix,
				Protocol: "http/protobuf",
				Version:  "test",
			}

			providers, err := telemetry.New(ctx, cfg, slog.New(slog.NewJSONHandler(io.Discard, nil)))
			require.NoError(t, err)
			defer func() {
				assert.NoError(t, providers.Shutdown(ctx))
			}()
			_, span := providers.Traces.Tracer("test").Start(ctx, "export-test")
			span.End()
			counter, err := providers.Meters.Meter("test").Int64Counter("export.test")
			require.NoError(t, err)
			counter.Add(ctx, 1)
			require.NoError(t, providers.Traces.ForceFlush(ctx))
			require.NoError(t, providers.Meters.ForceFlush(ctx))
			mu.Lock()
			defer mu.Unlock()
			assert.Equal(t, 1, paths[prefix+"/v1/traces"])
			assert.Equal(t, 1, paths[prefix+"/v1/metrics"])
		})
	}
}
