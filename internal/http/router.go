// Package http connects generated API routes to action-specific handlers.
package http

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Makk228/avito-labs/internal/config"
	api "github.com/Makk228/avito-labs/internal/generated"
	"github.com/Makk228/avito-labs/internal/http/middleware"
	"github.com/Makk228/avito-labs/internal/http/requeststate"
	"github.com/Makk228/avito-labs/internal/http/respond"
)

type Handler struct {
	create       createHandler
	get          tripHandler
	finish       tripHandler
	db           pinger
	readyTimeout time.Duration
}

func NewHandler(create createHandler, get, finish tripHandler, db pinger, timeout time.Duration) *Handler {
	return &Handler{create: create, get: get, finish: finish, db: db, readyTimeout: timeout}
}

func (h *Handler) CreateTrip(w http.ResponseWriter, r *http.Request, p api.CreateTripParams) {
	h.create.Handle(w, r, p)
}

func (h *Handler) GetTrip(w http.ResponseWriter, r *http.Request, id api.TripId) {
	h.get.Handle(w, r, id)
}

func (h *Handler) FinishTrip(w http.ResponseWriter, r *http.Request, id api.TripId) {
	h.finish.Handle(w, r, id)
}

func (h *Handler) Health(w http.ResponseWriter, _ *http.Request) {
	respond.JSON(w, http.StatusOK, api.HealthResponse{Status: api.Ok})
}

func (h *Handler) Ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), h.readyTimeout)
	defer cancel()
	if err := h.db.Ping(ctx); err != nil {
		requeststate.Record(r.Context(), fmt.Errorf("readiness database ping: %w", err))
		respond.JSON(w, http.StatusServiceUnavailable, api.HealthResponse{Status: api.Unavailable})
		return
	}
	respond.JSON(w, http.StatusOK, api.HealthResponse{Status: api.Ok})
}

func NewServer(cfg config.HTTP, handler http.Handler, log *slog.Logger) *http.Server {
	return &http.Server{Addr: cfg.Addr, Handler: handler, ReadTimeout: cfg.ReadTimeout, ReadHeaderTimeout: cfg.ReadHeaderTimeout, WriteTimeout: cfg.WriteTimeout, IdleTimeout: cfg.IdleTimeout, ErrorLog: slog.NewLogLogger(log.Handler(), slog.LevelWarn)}
}

func NewRouter(h *Handler, mw *middleware.Middleware) http.Handler {
	mux := chi.NewRouter()
	mux.NotFound(func(w http.ResponseWriter, r *http.Request) {
		respond.Routing(w, r, http.StatusNotFound, "not-found", "Not found", "not_found", "Route was not found")
	})
	mux.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		var allowed []string
		for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
			if mux.Match(chi.NewRouteContext(), method, r.URL.Path) {
				allowed = append(allowed, method)
			}
		}
		w.Header().Set("Allow", strings.Join(allowed, ", "))
		respond.Routing(w, r, http.StatusMethodNotAllowed, "method-not-allowed", "Method not allowed", "method_not_allowed", fmt.Sprintf("Method %s is not allowed for this route", r.Method))
	})
	router := api.HandlerWithOptions(h, api.ChiServerOptions{BaseRouter: mux, ErrorHandlerFunc: paramError})
	return mw.Wrap(router, func(r *http.Request) string {
		for _, method := range []string{r.Method, http.MethodGet, http.MethodPost} {
			ctx := chi.NewRouteContext()
			if mux.Match(ctx, method, r.URL.Path) {
				return ctx.RoutePattern()
			}
		}
		return "unmatched"
	})
}

func paramError(w http.ResponseWriter, r *http.Request, err error) {
	detail := "Request parameters are invalid"
	var invalid *api.InvalidParamFormatError
	var many *api.TooManyValuesForParamError
	switch {
	case errors.As(err, &invalid):
		detail = fmt.Sprintf("Parameter %s must be a valid UUID", invalid.ParamName)
	case errors.As(err, &many):
		detail = fmt.Sprintf("Parameter %s must be passed once", many.ParamName)
	}
	respond.Invalid(w, r, detail)
}
