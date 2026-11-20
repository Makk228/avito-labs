// Package push sends a single HTTP location command, without retrying.
package push

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/Makk228/avito-labs/internal/model"
)

const maxResponseBytes = 4 << 10

type Config struct {
	URL     string
	Timeout time.Duration
}

type Dependencies struct {
	IDs    idGenerator
	Traces trace.TracerProvider
}

type Gateway struct {
	client   client
	ids      idGenerator
	endpoint string
}

type command struct {
	RequestID   string            `json:"request_id"`
	RecipientID uuid.UUID         `json:"recipient_id"`
	Kind        string            `json:"kind"`
	Data        map[string]string `json:"data"`
}

type HTTPError struct{ StatusCode int }

func (e *HTTPError) Error() string { return fmt.Sprintf("push returned HTTP %d", e.StatusCode) }

func New(deps Dependencies, cfg Config) (*Gateway, error) {
	endpoint, err := url.Parse(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("parse push URL: %w", err)
	}

	transport := &http.Transport{
		DialContext:           (&net.Dialer{Timeout: cfg.Timeout, KeepAlive: cfg.Timeout}).DialContext,
		TLSHandshakeTimeout:   cfg.Timeout,
		ResponseHeaderTimeout: cfg.Timeout,
		IdleConnTimeout:       cfg.Timeout,
		MaxIdleConnsPerHost:   http.DefaultMaxIdleConnsPerHost,
	}
	httpClient := &http.Client{
		Timeout: cfg.Timeout,
		Transport: otelhttp.NewTransport(transport,
			otelhttp.WithTracerProvider(deps.Traces),
			otelhttp.WithPropagators(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{})),
		),
		// Redirects must not send a second command or expose the recipient.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	return &Gateway{client: httpClient, ids: deps.IDs, endpoint: endpoint.JoinPath("api/v1/push").String()}, nil
}

func (g *Gateway) RequestPosition(ctx context.Context, trip model.StaleTrip) error {
	body, err := json.Marshal(command{
		RequestID:   g.ids.NewID().String(),
		RecipientID: trip.DriverID,
		Kind:        "REQUEST_POSITION",
		Data:        map[string]string{"trip_id": trip.ID.String()},
	})
	if err != nil {
		return fmt.Errorf("encode push command: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build push request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	res, err := g.client.Do(req)
	if err != nil {
		if res != nil && res.Body != nil {
			_ = res.Body.Close()
		}
		return fmt.Errorf("push transport: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusAccepted {
		return &HTTPError{StatusCode: res.StatusCode}
	}

	if _, err := io.Copy(io.Discard, io.LimitReader(res.Body, maxResponseBytes)); err != nil {
		return fmt.Errorf("read push response: %w", err)
	}

	return nil
}

func (g *Gateway) Close() { g.client.CloseIdleConnections() }
