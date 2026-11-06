package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	metricnoop "go.opentelemetry.io/otel/metric/noop"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
	"go.uber.org/mock/gomock"

	api "github.com/Makk228/avito-labs/internal/generated"
	transport "github.com/Makk228/avito-labs/internal/http"
	"github.com/Makk228/avito-labs/internal/http/middleware"
)

func TestRouterAndRequestLog(t *testing.T) {
	const tripsRoute = "/api/v1/trips"
	const readyRoute = "/ready"
	for _, tc := range []struct {
		name, method, path, route, problem string
		status                             int
		databaseFailure                    bool
	}{
		{name: "health", method: http.MethodGet, path: "/health", route: "/health", status: 200},
		{name: "ready", method: http.MethodGet, path: readyRoute, route: readyRoute, status: 200},
		{name: "database unavailable", method: http.MethodGet, path: readyRoute, route: readyRoute, status: 503, databaseFailure: true},
		{name: "malformed UUID", method: http.MethodGet, path: tripsRoute + "/invalid", route: tripsRoute + "/{tripId}", status: 400, problem: "invalid_request"},
		{name: "wrong method", method: http.MethodPut, path: tripsRoute, route: tripsRoute, status: 405, problem: "method_not_allowed"},
		{name: "unknown route", method: http.MethodGet, path: "/not-a-route", route: "unmatched", status: 404, problem: "not_found"},
		{name: "duplicate key", method: http.MethodPost, path: tripsRoute, route: tripsRoute, status: 400, problem: "invalid_request"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			db := NewMockpinger(ctrl)
			if tc.path == readyRoute {
				db.EXPECT().Ping(gomock.Any()).DoAndReturn(func(ctx context.Context) error {
					if _, ok := ctx.Deadline(); !ok {
						t.Error("readiness call has no timeout")
					}
					if tc.databaseFailure {
						return errors.New("database unavailable (test)")
					}
					return nil
				})
			}
			var logs bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logs, nil))
			mw, err := middleware.New(logger, tracenoop.NewTracerProvider(), metricnoop.NewMeterProvider().Meter("test"))
			if err != nil {
				t.Fatal(err)
			}
			h := transport.NewHandler(NewMockcreateHandler(ctrl), NewMocktripHandler(ctrl), NewMocktripHandler(ctrl), db, time.Second)
			router := transport.NewRouter(h, mw)
			r := httptest.NewRequest(tc.method, tc.path, nil)
			if tc.name == "duplicate key" {
				r.Header.Add("Idempotency-Key", "5cb72c04-7650-45c9-a79b-bcdba0631e0c")
				r.Header.Add("Idempotency-Key", "8860b315-ec86-42eb-a17c-7c163d721ff5")
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			if tc.problem != "" {
				var problem api.Problem
				if err := json.Unmarshal(w.Body.Bytes(), &problem); err != nil {
					t.Fatal(err)
				}
				if problem.Code != tc.problem {
					t.Fatalf("problem=%+v", problem)
				}
			}
			if tc.status == 405 && w.Header().Get("Allow") != http.MethodPost {
				t.Fatal("missing Allow header")
			}
			assertRequestLog(t, logs.Bytes(), tc.route, tc.status, tc.databaseFailure)
			if strings.Contains(w.Body.String(), "database unavailable") {
				t.Fatal("database error leaked into response")
			}
		})
	}
}

func assertRequestLog(t *testing.T, data []byte, route string, status int, databaseFailure bool) {
	t.Helper()
	var entry map[string]any
	if err := json.Unmarshal(data, &entry); err != nil {
		t.Fatalf("expected exactly one JSON request log: %v", err)
	}
	level := "INFO"
	if status >= 500 {
		level = "ERROR"
	}
	if entry["msg"] != "request handled" || entry["route"] != route || entry["status"] != float64(status) || entry["level"] != level {
		t.Fatalf("incorrect log: %s", data)
	}
	if databaseFailure && !strings.Contains(entry["error"].(string), "database unavailable") {
		t.Fatalf("missing internal error: %s", data)
	}
}
