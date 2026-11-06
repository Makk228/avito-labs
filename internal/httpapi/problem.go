package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	api "github.com/Makk228/avito-labs/internal/generated"
	"github.com/Makk228/avito-labs/internal/trip"
)

const (
	contentTypeJSON    = "application/json"
	contentTypeProblem = "application/problem+json"

	problemTypeBase = "https://tripgo.example/problems/"
)

type problemKind struct {
	status int
	slug   string
	title  string
	code   string
}

var (
	problemInvalidRequest      = problemKind{http.StatusBadRequest, "invalid-request", "Invalid request", "invalid_request"}
	problemTripNotFound        = problemKind{http.StatusNotFound, "trip-not-found", "Trip not found", "trip_not_found"}
	problemTripCompleted       = problemKind{http.StatusConflict, "trip-completed", "Trip completed", "trip_completed"}
	problemDriverBusy          = problemKind{http.StatusConflict, "driver-busy", "Driver busy", "driver_busy"}
	problemIdempotencyConflict = problemKind{http.StatusConflict, "idempotency-conflict", "Idempotency conflict", "idempotency_conflict"}
	problemInternal            = problemKind{http.StatusInternalServerError, "internal-error", "Internal Server Error", "internal_error"}

	problemRouteNotFound    = problemKind{http.StatusNotFound, "not-found", "Not found", "not_found"}
	problemMethodNotAllowed = problemKind{http.StatusMethodNotAllowed, "method-not-allowed", "Method not allowed", "method_not_allowed"}
)

const (
	tripNotFoundDetail        = "Trip was not found"
	tripCompletedDetail       = "Operation is not allowed for a completed trip"
	driverBusyDetail          = "Driver already has an active trip"
	idempotencyConflictDetail = "Idempotency-Key was already used with a different request body"
	internalErrorDetail       = "Internal server error"
)

func writeError(w http.ResponseWriter, r *http.Request, log *slog.Logger, err error) {
	switch {
	case errors.Is(err, trip.ErrTripNotFound):
		writeProblem(w, r, problemTripNotFound, tripNotFoundDetail)
	case errors.Is(err, trip.ErrTripCompleted):
		writeProblem(w, r, problemTripCompleted, tripCompletedDetail)
	case errors.Is(err, trip.ErrDriverBusy):
		writeProblem(w, r, problemDriverBusy, driverBusyDetail)
	case errors.Is(err, trip.ErrIdempotencyConflict):
		writeProblem(w, r, problemIdempotencyConflict, idempotencyConflictDetail)
	default:
		log.Error("request failed",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Any("error", err),
		)
		writeProblem(w, r, problemInternal, internalErrorDetail)
	}
}

func writeProblem(w http.ResponseWriter, r *http.Request, kind problemKind, detail string) {
	instance := r.URL.Path
	body := api.Problem{
		Type:     problemTypeBase + kind.slug,
		Title:    kind.title,
		Status:   int32(kind.status),
		Detail:   &detail,
		Instance: &instance,
		Code:     kind.code,
	}
	writeBody(w, kind.status, contentTypeProblem, body)
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	writeBody(w, status, contentTypeJSON, body)
}

func writeBody(w http.ResponseWriter, status int, contentType string, body any) {
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
