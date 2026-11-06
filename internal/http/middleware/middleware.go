// Package middleware implements Recover → Tracing → Logging → Metrics.
package middleware

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strconv"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/Makk228/avito-labs/internal/http/requeststate"
	"github.com/Makk228/avito-labs/internal/http/respond"
)

type Middleware struct {
	log      *slog.Logger
	tracer   trace.TracerProvider
	duration metric.Float64Histogram
	active   metric.Int64UpDownCounter
}
type (
	stateKey struct{}
	response struct {
		http.ResponseWriter
		body    bytes.Buffer
		status  int
		written bool
		ctx     context.Context
		route   string
	}
)

func (w *response) WriteHeader(status int) {
	if !w.written {
		w.status = status
		w.written = true
	}
}

func (w *response) Write(body []byte) (int, error) {
	if !w.written {
		w.WriteHeader(http.StatusOK)
	}
	return w.body.Write(body)
}
func (w *response) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func New(log *slog.Logger, tp trace.TracerProvider, meter metric.Meter) (*Middleware, error) {
	duration, err := meter.Float64Histogram("http.server.request.duration", metric.WithUnit("s"), metric.WithExplicitBucketBoundaries(.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10))
	if err != nil {
		return nil, fmt.Errorf("create request histogram: %w", err)
	}
	active, err := meter.Int64UpDownCounter("http.server.active_requests", metric.WithUnit("{request}"))
	if err != nil {
		return nil, fmt.Errorf("create active requests metric: %w", err)
	}
	return &Middleware{log: log, tracer: tp, duration: duration, active: active}, nil
}

// Wrap resolves only registered route templates before entering instrumentation.
func (m *Middleware) Wrap(next http.Handler, route func(*http.Request) string) http.Handler {
	chain := m.recoverer(m.tracing(m.logging(m.metrics(next))))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state := &response{ResponseWriter: w, status: http.StatusOK, ctx: r.Context(), route: route(r)}
		chain.ServeHTTP(state, r.WithContext(context.WithValue(requeststate.With(r.Context()), stateKey{}, state)))
		// These JSON endpoints have bounded responses. Buffering lets Recover
		// replace a partially written response without leaking its contents.
		w.WriteHeader(state.status)
		_, _ = w.Write(state.body.Bytes())
	})
}

func (m *Middleware) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if p := recover(); p != nil {
				if err, ok := p.(error); ok && errors.Is(err, http.ErrAbortHandler) {
					panic(p)
				}
				state := r.Context().Value(stateKey{}).(*response)
				state.body.Reset()
				state.written = false
				for name := range w.Header() {
					w.Header().Del(name)
				}
				m.log.ErrorContext(state.ctx, "panic in http handler", slog.Any("panic", p), slog.String("stack", string(debug.Stack())))
				respond.Error(w, r, errors.New("handler panic"))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (m *Middleware) tracing(next http.Handler) http.Handler {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state := r.Context().Value(stateKey{}).(*response)
		state.ctx = r.Context()
		span := trace.SpanFromContext(r.Context())
		span.SetAttributes(attribute.String("http.route", state.route))
		complete := false
		defer func() {
			if !complete {
				state.status = http.StatusInternalServerError
			}
			span.SetAttributes(attribute.Int("http.response.status_code", state.status))
			if state.status >= 400 {
				errorType := strconv.Itoa(state.status)
				span.SetStatus(codes.Error, errorType)
				span.SetAttributes(attribute.String("error.type", errorType))
			}
		}()
		next.ServeHTTP(w, r)
		complete = true
	})
	return otelhttp.NewHandler(inner, "HTTP", otelhttp.WithTracerProvider(m.tracer), otelhttp.WithMeterProvider(noop.NewMeterProvider()), otelhttp.WithPropagators(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{})), otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
		return r.Method + " " + r.Context().Value(stateKey{}).(*response).route
	}))
}

func (m *Middleware) logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		state := r.Context().Value(stateKey{}).(*response)
		defer func() {
			level := slog.LevelInfo
			if state.status >= 500 {
				level = slog.LevelError
			}
			attrs := []any{slog.String("method", r.Method), slog.String("route", state.route), slog.Int("status", state.status), slog.Int64("duration_ms", time.Since(start).Milliseconds())}
			if err := requeststate.Error(r.Context()); err != nil && state.status >= 500 {
				attrs = append(attrs, slog.Any("error", err))
			}
			m.log.Log(r.Context(), level, "request handled", attrs...)
		}()
		next.ServeHTTP(w, r)
	})
}

func (m *Middleware) metrics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		state := r.Context().Value(stateKey{}).(*response)
		attrs := []attribute.KeyValue{attribute.String("http.request.method", r.Method), attribute.String("http.route", state.route)}
		m.active.Add(r.Context(), 1, metric.WithAttributes(attrs...))
		complete := false
		defer func() {
			if !complete {
				state.status = http.StatusInternalServerError
			}
			m.active.Add(r.Context(), -1, metric.WithAttributes(attrs...))
			attrs = append(attrs, attribute.Int("http.response.status_code", state.status))
			m.duration.Record(r.Context(), time.Since(start).Seconds(), metric.WithAttributes(attrs...))
		}()
		next.ServeHTTP(w, r)
		complete = true
	})
}
