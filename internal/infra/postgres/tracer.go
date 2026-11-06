package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"
)

// QueryTracer adapts otelpgx's legacy metric conventions at the pool boundary.
// Repositories remain unaware of spans, metrics and SQL telemetry attributes.
type QueryTracer struct {
	*otelpgx.Tracer
	duration metric.Float64Histogram
}
type (
	queryKey  struct{}
	queryInfo struct {
		start      time.Time
		operation  string
		collection string
	}
)

func NewQueryTracer(tp trace.TracerProvider, meter metric.Meter) (*QueryTracer, error) {
	duration, err := meter.Float64Histogram("db.client.operation.duration", metric.WithUnit("s"))
	if err != nil {
		return nil, fmt.Errorf("create database histogram: %w", err)
	}
	base := otelpgx.NewTracer(otelpgx.WithTracerProvider(tp), otelpgx.WithMeterProvider(noop.NewMeterProvider()), otelpgx.WithTrimSQLInSpanName(), otelpgx.WithDisableSQLStatementInAttributes(), otelpgx.WithDisableConnectionDetailsInAttributes())
	return &QueryTracer{Tracer: base, duration: duration}, nil
}

func (t *QueryTracer) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	ctx = t.Tracer.TraceQueryStart(ctx, conn, data)
	op, collection := SQLAttributes(data.SQL)
	trace.SpanFromContext(ctx).SetAttributes(attribute.String("db.operation.name", op), attribute.String("db.collection.name", collection))
	return context.WithValue(ctx, queryKey{}, queryInfo{start: time.Now(), operation: op, collection: collection})
}

func (t *QueryTracer) TraceQueryEnd(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryEndData) {
	info, _ := ctx.Value(queryKey{}).(queryInfo)
	errorType := ""
	if data.Err != nil && !errors.Is(data.Err, pgx.ErrNoRows) {
		errorType = "database_error"
		var pgErr *pgconn.PgError
		if errors.As(data.Err, &pgErr) {
			errorType = pgErr.Code
		}
		span := trace.SpanFromContext(ctx)
		span.SetAttributes(attribute.String("error.type", errorType))
		span.SetStatus(codes.Error, errorType)
	}
	t.duration.Record(ctx, time.Since(info.start).Seconds(), metric.WithAttributes(attribute.String("db.operation.name", info.operation), attribute.String("db.collection.name", info.collection), attribute.String("error.type", errorType)))
	t.Tracer.TraceQueryEnd(ctx, conn, data)
}

// SQLAttributes only returns fixed identifiers; SQL values never become labels.
func SQLAttributes(sql string) (string, string) {
	fields := strings.Fields(sql)
	if len(fields) == 0 {
		return "UNKNOWN", ""
	}
	op := strings.ToUpper(fields[0])
	collection := ""
	for _, name := range []string{"trips", "trip_status_history", "idempotency_keys"} {
		for _, field := range fields {
			if strings.Trim(field, "\"(),;") == name {
				collection = name
				break
			}
		}
		if collection != "" {
			break
		}
	}
	switch op {
	case "SELECT", "INSERT", "UPDATE", "DELETE", "BEGIN", "COMMIT", "ROLLBACK":
	default:
		op = "OTHER"
	}
	return op, collection
}
