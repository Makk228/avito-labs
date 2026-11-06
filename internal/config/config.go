package config

import (
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	LogLevel        slog.Level
	ShutdownTimeout time.Duration

	HTTP        HTTP
	DB          DB
	Idempotency Idempotency
	Telemetry   Telemetry
}

type HTTP struct {
	Addr              string
	ReadTimeout       time.Duration
	ReadHeaderTimeout time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
}

type DB struct {
	URL             string
	MaxConns        int32
	MinConns        int32
	MaxConnLifetime time.Duration
	ConnectTimeout  time.Duration
	QueryTimeout    time.Duration
}

type Idempotency struct {
	KeyTTL          time.Duration
	CleanupInterval time.Duration
}

type Telemetry struct {
	Version  string
	Endpoint string
	Protocol string
}

func Load() (Config, error) {
	var r reader

	cfg := Config{
		LogLevel:        r.logLevel("LOG_LEVEL"),
		ShutdownTimeout: r.duration("SHUTDOWN_TIMEOUT"),
		HTTP: HTTP{
			Addr:              r.str("HTTP_ADDR"),
			ReadTimeout:       r.duration("HTTP_READ_TIMEOUT"),
			ReadHeaderTimeout: r.duration("HTTP_READ_HEADER_TIMEOUT"),
			WriteTimeout:      r.duration("HTTP_WRITE_TIMEOUT"),
			IdleTimeout:       r.duration("HTTP_IDLE_TIMEOUT"),
		},
		DB: DB{
			URL:             r.str("DATABASE_URL"),
			MaxConns:        r.int32("DATABASE_MAX_CONNS", 1),
			MinConns:        r.int32("DATABASE_MIN_CONNS", 0),
			MaxConnLifetime: r.duration("DATABASE_MAX_CONN_LIFETIME"),
			ConnectTimeout:  r.duration("DATABASE_CONNECT_TIMEOUT"),
			QueryTimeout:    r.duration("DATABASE_QUERY_TIMEOUT"),
		},
		Idempotency: Idempotency{
			KeyTTL:          r.duration("IDEMPOTENCY_KEY_TTL"),
			CleanupInterval: r.duration("IDEMPOTENCY_CLEANUP_INTERVAL"),
		},
	}

	cfg.Telemetry = Telemetry{Version: r.str("SERVICE_VERSION"), Endpoint: r.str("OTEL_EXPORTER_OTLP_ENDPOINT"), Protocol: r.str("OTEL_EXPORTER_OTLP_PROTOCOL")}
	endpoint, err := url.Parse(cfg.Telemetry.Endpoint)
	if err != nil || endpoint.Host == "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") {
		r.fail("OTEL_EXPORTER_OTLP_ENDPOINT must be an absolute http(s) URL")
	}
	if cfg.Telemetry.Protocol != "grpc" && cfg.Telemetry.Protocol != "http/protobuf" {
		r.fail("OTEL_EXPORTER_OTLP_PROTOCOL must be grpc or http/protobuf")
	}
	sampler := r.str("OTEL_TRACES_SAMPLER")
	switch sampler {
	case "always_on", "always_off", "traceidratio", "parentbased_always_on", "parentbased_always_off", "parentbased_traceidratio":
	default:
		r.fail("OTEL_TRACES_SAMPLER is invalid")
	}
	if sampler == "traceidratio" || sampler == "parentbased_traceidratio" {
		ratio, err := strconv.ParseFloat(r.str("OTEL_TRACES_SAMPLER_ARG"), 64)
		if err != nil || math.IsNaN(ratio) || ratio < 0 || ratio > 1 {
			r.fail("OTEL_TRACES_SAMPLER_ARG must be between 0 and 1")
		}
	}
	if cfg.DB.MinConns > cfg.DB.MaxConns {
		r.fail("DATABASE_MIN_CONNS (%d) must not exceed DATABASE_MAX_CONNS (%d)", cfg.DB.MinConns, cfg.DB.MaxConns)
	}

	if err := errors.Join(r.errs...); err != nil {
		return Config{}, fmt.Errorf("invalid configuration:\n%w", err)
	}
	return cfg, nil
}

type reader struct {
	errs []error
}

func (r *reader) fail(format string, args ...any) {
	r.errs = append(r.errs, fmt.Errorf(format, args...))
}

func (r *reader) str(key string) string {
	v, ok := os.LookupEnv(key)
	v = strings.TrimSpace(v)
	if !ok || v == "" {
		r.fail("%s is required", key)
		return ""
	}
	return v
}

func (r *reader) duration(key string) time.Duration {
	raw := r.str(key)
	if raw == "" {
		return 0
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		r.fail("%s: invalid duration %q", key, raw)
		return 0
	}
	if d <= 0 {
		r.fail("%s must be positive, got %s", key, raw)
		return 0
	}
	return d
}

func (r *reader) int32(key string, minValue int32) int32 {
	raw := r.str(key)
	if raw == "" {
		return 0
	}
	n, err := strconv.ParseInt(raw, 10, 32)
	if err != nil {
		r.fail("%s: invalid integer %q", key, raw)
		return 0
	}
	if int32(n) < minValue {
		r.fail("%s must be at least %d, got %d", key, minValue, n)
		return 0
	}
	return int32(n)
}

func (r *reader) logLevel(key string) slog.Level {
	raw := r.str(key)
	if raw == "" {
		return slog.LevelInfo
	}
	var level slog.Level
	if err := level.UnmarshalText([]byte(raw)); err != nil {
		r.fail("%s: unknown log level %q (want debug, info, warn or error)", key, raw)
		return slog.LevelInfo
	}
	return level
}
