package httpapi

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/Makk228/avito-labs/internal/config"
	api "github.com/Makk228/avito-labs/internal/generated"
)

func NewServer(cfg config.HTTP, handler http.Handler, log *slog.Logger) *http.Server {
	return &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler,
		ReadTimeout:       cfg.ReadTimeout,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}
}

func NewRouter(h *Handler, log *slog.Logger) http.Handler {
	mux := chi.NewRouter()
	mux.Use(recoverer(log))

	mux.NotFound(func(w http.ResponseWriter, r *http.Request) {
		writeProblem(w, r, problemRouteNotFound, "Route was not found")
	})
	mux.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Allow", strings.Join(allowedMethods(mux, r.URL.Path), ", "))
		writeProblem(w, r, problemMethodNotAllowed, fmt.Sprintf("Method %s is not allowed for this route", r.Method))
	})

	return api.HandlerWithOptions(h, api.ChiServerOptions{
		BaseRouter:       mux,
		ErrorHandlerFunc: paramError,
	})
}

func allowedMethods(routes chi.Routes, path string) []string {
	var allowed []string
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		if routes.Match(chi.NewRouteContext(), method, path) {
			allowed = append(allowed, method)
		}
	}
	return allowed
}

func paramError(w http.ResponseWriter, r *http.Request, err error) {
	detail := "Request parameters are invalid"

	var invalidFormat *api.InvalidParamFormatError
	var tooMany *api.TooManyValuesForParamError
	switch {
	case errors.As(err, &invalidFormat):
		detail = fmt.Sprintf("Parameter %s must be a valid UUID", invalidFormat.ParamName)
	case errors.As(err, &tooMany):
		detail = fmt.Sprintf("Parameter %s must be passed once", tooMany.ParamName)
	}
	writeProblem(w, r, problemInvalidRequest, detail)
}

func recoverer(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				p := recover()
				if p == nil {
					return
				}
				if err, ok := p.(error); ok && errors.Is(err, http.ErrAbortHandler) {
					panic(p)
				}
				log.Error("panic in http handler",
					slog.String("method", r.Method),
					slog.String("path", r.URL.Path),
					slog.Any("panic", p),
					slog.String("stack", string(debug.Stack())),
				)
				writeProblem(w, r, problemInternal, internalErrorDetail)
			}()
			next.ServeHTTP(w, r)
		})
	}
}
