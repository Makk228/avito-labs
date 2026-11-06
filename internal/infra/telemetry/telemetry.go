// Package telemetry configures explicit OpenTelemetry providers and structured logging.
package telemetry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/Makk228/avito-labs/internal/config"
)

type Providers struct {
	Traces *sdktrace.TracerProvider
	Meters *sdkmetric.MeterProvider
}

func New(ctx context.Context, cfg config.Telemetry, log *slog.Logger) (*Providers, error) {
	endpoint, err := url.Parse(cfg.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("parse OTLP endpoint: %w", err)
	}
	// OTel exposes its SDK diagnostic hook only as a process-wide callback.
	// Providers and application dependencies remain explicit, never global.
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		log.WarnContext(context.WithoutCancel(ctx), "telemetry degraded", slog.Any("error", err))
	}))
	var (
		traces  sdktrace.SpanExporter
		metrics sdkmetric.Exporter
	)
	if cfg.Protocol == "grpc" {
		traces, err = otlptracegrpc.New(ctx, otlptracegrpc.WithEndpointURL(cfg.Endpoint))
	} else {
		traces, err = otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(endpoint.JoinPath("v1/traces").String()))
	}
	if err != nil {
		return nil, fmt.Errorf("create trace exporter: %w", err)
	}
	if cfg.Protocol == "grpc" {
		metrics, err = otlpmetricgrpc.New(ctx, otlpmetricgrpc.WithEndpointURL(cfg.Endpoint))
	} else {
		metrics, err = otlpmetrichttp.New(ctx, otlpmetrichttp.WithEndpointURL(endpoint.JoinPath("v1/metrics").String()))
	}
	if err != nil {
		return nil, errors.Join(fmt.Errorf("create metric exporter: %w", err), traces.Shutdown(ctx))
	}
	res, err := resource.New(ctx, resource.WithFromEnv(), resource.WithAttributes(attribute.String("service.name", "trip-service"), attribute.String("service.version", cfg.Version)))
	if err != nil {
		return nil, errors.Join(fmt.Errorf("create telemetry resource: %w", err), traces.Shutdown(ctx), metrics.Shutdown(ctx))
	}
	// The SDK reads OTEL_TRACES_SAMPLER and OTEL_TRACES_SAMPLER_ARG.
	tp := sdktrace.NewTracerProvider(sdktrace.WithResource(res), sdktrace.WithBatcher(traces))
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithResource(res), sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metrics)))
	return &Providers{Traces: tp, Meters: mp}, nil
}

func (p *Providers) Shutdown(ctx context.Context) error {
	return errors.Join(p.Meters.Shutdown(ctx), p.Traces.Shutdown(ctx))
}

type Counters struct {
	created   metric.Int64Counter
	completed metric.Int64Counter
}

func NewCounters(meter metric.Meter, trips activeTrips) (*Counters, error) {
	created, err := meter.Int64Counter("tripgo.trips.created", metric.WithUnit("{trip}"))
	if err != nil {
		return nil, fmt.Errorf("create trip counter: %w", err)
	}
	completed, err := meter.Int64Counter("tripgo.trips.completed", metric.WithUnit("{trip}"))
	if err != nil {
		return nil, fmt.Errorf("create completion counter: %w", err)
	}
	gauge, err := meter.Int64ObservableGauge("tripgo.trips.active", metric.WithUnit("{trip}"))
	if err != nil {
		return nil, fmt.Errorf("create active trips gauge: %w", err)
	}
	if _, err := meter.RegisterCallback(func(ctx context.Context, observer metric.Observer) error {
		count, err := trips.CountActive(ctx)
		if err != nil {
			return fmt.Errorf("observe active trips: %w", err)
		}
		observer.ObserveInt64(gauge, count)
		return nil
	}, gauge); err != nil {
		return nil, fmt.Errorf("register active trips gauge: %w", err)
	}
	return &Counters{created: created, completed: completed}, nil
}
func (c *Counters) Created(ctx context.Context)   { c.created.Add(ctx, 1) }
func (c *Counters) Completed(ctx context.Context) { c.completed.Add(ctx, 1) }
