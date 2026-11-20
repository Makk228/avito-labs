package config_test

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Makk228/avito-labs/internal/config"
)

func exampleEnvironment(t *testing.T) {
	t.Helper()
	data, err := os.ReadFile("../../.env.example")
	require.NoError(t, err)

	for line := range strings.SplitSeq(string(data), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok {
			t.Setenv(key, value)
		}
	}
}

func TestLoad(t *testing.T) {
	exampleEnvironment(t)

	cfg, err := config.Load()

	require.NoError(t, err)
	assert.Equal(t, ":8080", cfg.HTTP.Addr)
	assert.EqualValues(t, 10, cfg.DB.MaxConns)
	assert.EqualValues(t, 2, cfg.DB.MinConns)
	assert.Equal(t, "local", cfg.Telemetry.Version)
}

func TestLoadRejectsInvalidConfiguration(t *testing.T) {
	const invalidValue = "invalid"
	const ratioSampler = "traceidratio"
	const logLevelKey = "LOG_LEVEL"
	const samplerKey = "OTEL_TRACES_SAMPLER"
	const samplerArgKey = "OTEL_TRACES_SAMPLER_ARG"
	for _, tc := range []struct {
		name string
		env  map[string]string
		want string
	}{
		{
			name: "missing database",
			env:  map[string]string{"DATABASE_URL": ""},
			want: "DATABASE_URL",
		},
		{
			name: "minimum connections exceeds maximum",
			env:  map[string]string{"DATABASE_MIN_CONNS": "20"},
			want: "DATABASE_MIN_CONNS",
		},
		{
			name: "zero timeout",
			env:  map[string]string{"HTTP_READ_TIMEOUT": "0s"},
			want: "HTTP_READ_TIMEOUT",
		},
		{
			name: "invalid log level",
			env:  map[string]string{logLevelKey: invalidValue},
			want: "LogLevel",
		},
		{
			name: "custom log level",
			env:  map[string]string{logLevelKey: "INFO+1"},
			want: logLevelKey,
		},
		{
			name: "endpoint without scheme",
			env:  map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "localhost:4318"},
			want: "OTEL_EXPORTER_OTLP_ENDPOINT",
		},
		{
			name: "unsupported protocol",
			env:  map[string]string{"OTEL_EXPORTER_OTLP_PROTOCOL": invalidValue},
			want: "OTEL_EXPORTER_OTLP_PROTOCOL",
		},
		{
			name: "unsupported sampler",
			env:  map[string]string{samplerKey: invalidValue},
			want: samplerKey,
		},
		{
			name: "nonfinite ratio",
			env:  map[string]string{samplerKey: ratioSampler, samplerArgKey: "NaN"},
			want: samplerArgKey,
		},
		{
			name: "missing ratio",
			env:  map[string]string{samplerKey: ratioSampler, samplerArgKey: ""},
			want: samplerArgKey,
		},
		{
			name: "missing parent based ratio",
			env:  map[string]string{samplerKey: "parentbased_traceidratio", samplerArgKey: ""},
			want: samplerArgKey,
		},
		{
			name: "ratio above one",
			env:  map[string]string{samplerKey: ratioSampler, samplerArgKey: "1.1"},
			want: samplerArgKey,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			exampleEnvironment(t)
			for key, value := range tc.env {
				t.Setenv(key, value)
			}

			cfg, err := config.Load()

			require.ErrorContains(t, err, tc.want)
			assert.Empty(t, cfg)
		})
	}
}
