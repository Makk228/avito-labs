package config_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Makk228/avito-labs/internal/config"
)

func TestPositionConfigurationRejectsInvalidValues(t *testing.T) {
	for _, tc := range []struct{ key, value string }{
		{key: "POSITION_CHECK_INTERVAL", value: "0s"},
		{key: "POSITION_STALE_AFTER", value: "-1s"},
		{key: "POSITION_WORKERS", value: "0"},
		{key: "POSITION_QUEUE_SIZE", value: "0"},
		{key: "PUSH_HTTP_URL", value: ""},
		{key: "PUSH_HTTP_URL", value: "localhost:8090"},
		{key: "PUSH_REQUEST_TIMEOUT", value: "0s"},
	} {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			exampleEnvironment(t)
			t.Setenv(tc.key, tc.value)

			cfg, err := config.Load()

			require.ErrorContains(t, err, tc.key)
			assert.Empty(t, cfg)
		})
	}
}
