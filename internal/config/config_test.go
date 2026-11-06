package config_test

import (
	"os"
	"strings"
	"testing"

	"github.com/Makk228/avito-labs/internal/config"
)

func TestLoad(t *testing.T) {
	const invalidValue = "invalid"
	const samplerKey = "OTEL_TRACES_SAMPLER"
	for _, tc := range []struct {
		name string
		env  map[string]string
		want string
	}{
		{"valid", nil, ""},
		{"missing", map[string]string{"DATABASE_URL": ""}, "DATABASE_URL"},
		{"pool", map[string]string{"DATABASE_MIN_CONNS": "20"}, "DATABASE_MIN_CONNS"},
		{"timeout", map[string]string{"HTTP_READ_TIMEOUT": "0s"}, "HTTP_READ_TIMEOUT"},
		{"level", map[string]string{"LOG_LEVEL": invalidValue}, "LOG_LEVEL"},
		{"endpoint", map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "localhost:4318"}, "OTEL_EXPORTER_OTLP_ENDPOINT"},
		{"protocol", map[string]string{"OTEL_EXPORTER_OTLP_PROTOCOL": invalidValue}, "OTEL_EXPORTER_OTLP_PROTOCOL"},
		{"sampler", map[string]string{samplerKey: invalidValue}, samplerKey},
		{"ratio", map[string]string{samplerKey: "traceidratio", "OTEL_TRACES_SAMPLER_ARG": "NaN"}, "OTEL_TRACES_SAMPLER_ARG"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, err := os.ReadFile("../../.env.example")
			if err != nil {
				t.Fatal(err)
			}
			for line := range strings.SplitSeq(string(data), "\n") {
				key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
				if ok {
					t.Setenv(key, value)
				}
			}
			for key, value := range tc.env {
				t.Setenv(key, value)
			}
			cfg, err := config.Load()
			if tc.want != "" {
				if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("want %s, got %v", tc.want, err)
				}
				return
			}
			if err != nil || cfg.HTTP.Addr == "" || cfg.DB.URL == "" || cfg.Telemetry.Version == "" {
				t.Fatalf("invalid config: %+v %v", cfg, err)
			}
		})
	}
}
