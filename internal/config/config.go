// Package config parses and validates environment configuration.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/go-playground/validator/v10"
)

type Config struct {
	LogLevel        slog.Level    `env:"LOG_LEVEL,required,notEmpty" validate:"oneof=-4 0 4 8"`
	ShutdownTimeout time.Duration `env:"SHUTDOWN_TIMEOUT,required,notEmpty" validate:"gt=0"`
	HTTP            HTTP
	DB              DB
	Idempotency     Idempotency
	Telemetry       Telemetry
}

type HTTP struct {
	Addr              string        `env:"HTTP_ADDR,required,notEmpty" validate:"required"`
	ReadTimeout       time.Duration `env:"HTTP_READ_TIMEOUT,required,notEmpty" validate:"gt=0"`
	ReadHeaderTimeout time.Duration `env:"HTTP_READ_HEADER_TIMEOUT,required,notEmpty" validate:"gt=0"`
	WriteTimeout      time.Duration `env:"HTTP_WRITE_TIMEOUT,required,notEmpty" validate:"gt=0"`
	IdleTimeout       time.Duration `env:"HTTP_IDLE_TIMEOUT,required,notEmpty" validate:"gt=0"`
}

type DB struct {
	URL             string        `env:"DATABASE_URL,required,notEmpty" validate:"required"`
	MaxConns        int32         `env:"DATABASE_MAX_CONNS,required,notEmpty" validate:"gt=0"`
	MinConns        int32         `env:"DATABASE_MIN_CONNS,required,notEmpty" validate:"gte=0,ltefield=MaxConns"`
	MaxConnLifetime time.Duration `env:"DATABASE_MAX_CONN_LIFETIME,required,notEmpty" validate:"gt=0"`
	ConnectTimeout  time.Duration `env:"DATABASE_CONNECT_TIMEOUT,required,notEmpty" validate:"gt=0"`
	QueryTimeout    time.Duration `env:"DATABASE_QUERY_TIMEOUT,required,notEmpty" validate:"gt=0"`
}

type Idempotency struct {
	KeyTTL          time.Duration `env:"IDEMPOTENCY_KEY_TTL,required,notEmpty" validate:"gt=0"`
	CleanupInterval time.Duration `env:"IDEMPOTENCY_CLEANUP_INTERVAL,required,notEmpty" validate:"gt=0"`
}

type Telemetry struct {
	Version    string   `env:"SERVICE_VERSION,required,notEmpty" validate:"required"`
	Endpoint   string   `env:"OTEL_EXPORTER_OTLP_ENDPOINT,required,notEmpty" validate:"http_url"`
	Protocol   string   `env:"OTEL_EXPORTER_OTLP_PROTOCOL,required,notEmpty" validate:"oneof=grpc http/protobuf"`
	Sampler    string   `env:"OTEL_TRACES_SAMPLER,required,notEmpty" validate:"oneof=always_on always_off traceidratio parentbased_always_on parentbased_always_off parentbased_traceidratio"`
	SamplerArg *float64 `env:"OTEL_TRACES_SAMPLER_ARG" validate:"required_if=Sampler traceidratio,required_if=Sampler parentbased_traceidratio,omitempty,gte=0,lte=1"`
}

func Load() (Config, error) {
	var cfg Config
	parseErr := env.ParseWithOptions(&cfg, env.Options{Environment: environment()})

	rules := validator.New(validator.WithRequiredStructEnabled())
	rules.RegisterTagNameFunc(func(field reflect.StructField) string {
		name, _, _ := strings.Cut(field.Tag.Get("env"), ",")
		if name != "" {
			return name
		}

		return field.Name
	})

	if err := errors.Join(parseErr, rules.Struct(cfg)); err != nil {
		return Config{}, fmt.Errorf("invalid configuration: %w", err)
	}

	return cfg, nil
}

func environment() map[string]string {
	values := make(map[string]string)
	for _, entry := range os.Environ() {
		key, value, found := strings.Cut(entry, "=")
		if found {
			values[key] = strings.TrimSpace(value)
		}
	}

	return values
}
