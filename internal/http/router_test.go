package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
	"go.uber.org/mock/gomock"

	transport "github.com/Makk228/avito-labs/internal/http"
	"github.com/Makk228/avito-labs/internal/http/middleware"
)

func expectPing(db *Mockpinger, result error) {
	db.EXPECT().Ping(gomock.Cond(func(ctx context.Context) bool {
		_, ok := ctx.Deadline()
		return ok
	})).Return(result)
}

func TestRouterAndRequestLog(t *testing.T) {
	const statusField = "status"
	const codeField = "code"
	const infoLevel = "INFO"
	const tripsRoute = "/api/v1/trips"
	const readyRoute = "/ready"
	noDependencies := func(*Mockpinger, *http.Request) {}

	for _, tc := range []struct {
		name         string
		method       string
		path         string
		setup        func(*Mockpinger, *http.Request)
		wantStatus   int
		wantRoute    string
		wantBody     map[string]any
		wantAllow    string
		wantLevel    string
		wantLogError any
	}{
		{
			name:       "health",
			method:     http.MethodGet,
			path:       "/health",
			setup:      noDependencies,
			wantStatus: http.StatusOK,
			wantRoute:  "/health",
			wantBody:   map[string]any{statusField: "ok"},
			wantLevel:  infoLevel,
		},
		{
			name:       "ready",
			method:     http.MethodGet,
			path:       readyRoute,
			setup:      func(db *Mockpinger, _ *http.Request) { expectPing(db, nil) },
			wantStatus: http.StatusOK,
			wantRoute:  readyRoute,
			wantBody:   map[string]any{statusField: "ok"},
			wantLevel:  infoLevel,
		},
		{
			name:         "database unavailable",
			method:       http.MethodGet,
			path:         readyRoute,
			setup:        func(db *Mockpinger, _ *http.Request) { expectPing(db, errors.New("database unavailable (test)")) },
			wantStatus:   http.StatusServiceUnavailable,
			wantRoute:    readyRoute,
			wantBody:     map[string]any{statusField: "unavailable"},
			wantLevel:    "ERROR",
			wantLogError: "readiness database ping: database unavailable (test)",
		},
		{
			name:       "malformed UUID",
			method:     http.MethodGet,
			path:       tripsRoute + "/invalid",
			setup:      noDependencies,
			wantStatus: http.StatusBadRequest,
			wantRoute:  tripsRoute + "/{tripId}",
			wantBody:   map[string]any{codeField: "invalid_request"},
			wantLevel:  infoLevel,
		},
		{
			name:       "wrong method",
			method:     http.MethodPut,
			path:       tripsRoute,
			setup:      noDependencies,
			wantStatus: http.StatusMethodNotAllowed,
			wantRoute:  tripsRoute,
			wantBody:   map[string]any{codeField: "method_not_allowed"},
			wantAllow:  http.MethodPost,
			wantLevel:  infoLevel,
		},
		{
			name:       "unknown route",
			method:     http.MethodGet,
			path:       "/not-a-route",
			setup:      noDependencies,
			wantStatus: http.StatusNotFound,
			wantRoute:  "unmatched",
			wantBody:   map[string]any{codeField: "not_found"},
			wantLevel:  infoLevel,
		},
		{
			name:   "duplicate key",
			method: http.MethodPost,
			path:   tripsRoute,
			setup: func(_ *Mockpinger, r *http.Request) {
				r.Header.Add("Idempotency-Key", "5cb72c04-7650-45c9-a79b-bcdba0631e0c")
				r.Header.Add("Idempotency-Key", "8860b315-ec86-42eb-a17c-7c163d721ff5")
			},
			wantStatus: http.StatusBadRequest,
			wantRoute:  tripsRoute,
			wantBody:   map[string]any{codeField: "invalid_request"},
			wantLevel:  infoLevel,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			db := NewMockpinger(ctrl)
			r := httptest.NewRequest(tc.method, tc.path, nil)
			tc.setup(db, r)

			var logs bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logs, nil))
			mw, err := middleware.New(logger, tracenoop.NewTracerProvider(), metricnoop.NewMeterProvider().Meter("test"))
			require.NoError(t, err)
			h := transport.NewHandler(transport.Dependencies{
				Create:        NewMockcreateHandler(ctrl),
				Get:           NewMocktripHandler(ctrl),
				Finish:        NewMocktripHandler(ctrl),
				SavePosition:  NewMocktripHandler(ctrl),
				ListPositions: NewMocktripHandler(ctrl),
				Database:      db,
			}, time.Second)
			w := httptest.NewRecorder()

			transport.NewRouter(h, mw).ServeHTTP(w, r)

			assert.Equal(t, tc.wantStatus, w.Code)
			assert.Equal(t, tc.wantAllow, w.Header().Get("Allow"))
			var body map[string]any
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			assert.Subset(t, body, tc.wantBody)
			assert.NotContains(t, w.Body.String(), "database unavailable")

			var entry map[string]any
			require.NoError(t, json.Unmarshal(logs.Bytes(), &entry), "expected exactly one access log")
			assert.Equal(t, "request handled", entry["msg"])
			assert.Equal(t, tc.wantRoute, entry["route"])
			assert.Equal(t, float64(tc.wantStatus), entry[statusField])
			assert.Equal(t, tc.wantLevel, entry["level"])
			assert.Equal(t, tc.wantLogError, entry["error"])
		})
	}
}
