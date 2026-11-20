package telemetry

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type PositionMetrics struct {
	saved    metric.Int64Counter
	checks   metric.Int64Counter
	queue    metric.Int64Gauge
	active   metric.Int64Gauge
	requests metric.Int64Counter
	duration metric.Float64Histogram
}

func NewPositionMetrics(meter metric.Meter) (*PositionMetrics, error) {
	var m PositionMetrics
	var err error
	m.saved, err = meter.Int64Counter("tripgo.positions.saved", metric.WithUnit("{position}"))
	if err != nil {
		return nil, fmt.Errorf("create positions counter: %w", err)
	}

	m.checks, err = meter.Int64Counter("tripgo.stale_checks", metric.WithUnit("{check}"))
	if err != nil {
		return nil, fmt.Errorf("create stale checks counter: %w", err)
	}

	m.queue, err = meter.Int64Gauge("tripgo.position_requests.queue.size", metric.WithUnit("{task}"))
	if err != nil {
		return nil, fmt.Errorf("create position queue gauge: %w", err)
	}

	m.active, err = meter.Int64Gauge("tripgo.position_requests.workers.active", metric.WithUnit("{worker}"))
	if err != nil {
		return nil, fmt.Errorf("create active workers gauge: %w", err)
	}

	m.requests, err = meter.Int64Counter("tripgo.position_requests", metric.WithUnit("{request}"))
	if err != nil {
		return nil, fmt.Errorf("create position requests counter: %w", err)
	}

	m.duration, err = meter.Float64Histogram("tripgo.position_requests.duration", metric.WithUnit("s"))
	if err != nil {
		return nil, fmt.Errorf("create position request histogram: %w", err)
	}

	return &m, nil
}

func (m *PositionMetrics) Saved(ctx context.Context) { m.saved.Add(ctx, 1) }
func (m *PositionMetrics) StaleCheck(ctx context.Context, result string) {
	m.checks.Add(ctx, 1, metric.WithAttributes(attribute.String("result", result)))
}
func (m *PositionMetrics) QueueSize(ctx context.Context, value int64) { m.queue.Record(ctx, value) }
func (m *PositionMetrics) ActiveWorkers(ctx context.Context, value int64) {
	m.active.Record(ctx, value)
}

func (m *PositionMetrics) Request(ctx context.Context, result string, duration time.Duration) {
	attrs := metric.WithAttributes(attribute.String("result", result))
	m.requests.Add(ctx, 1, attrs)
	if result != "cancelled" {
		m.duration.Record(ctx, duration.Seconds(), attrs)
	}
}
