// Package domainerror is the single domain-to-HTTP error mapping.
package domainerror

import (
	"errors"
	"net/http"

	"github.com/Makk228/avito-labs/internal/domain/trip"
	api "github.com/Makk228/avito-labs/internal/generated"
)

func Problem(err error) (int, api.Problem) {
	status, slug, title, code, detail := http.StatusInternalServerError, "internal-error", "Internal Server Error", "internal_error", "Internal server error"
	var validation *trip.ValidationError
	switch {
	case errors.As(err, &validation):
		status, slug, title, code, detail = http.StatusBadRequest, "invalid-request", "Invalid request", "invalid_request", validation.Error()
	case errors.Is(err, trip.ErrTripNotFound):
		status, slug, title, code, detail = http.StatusNotFound, "trip-not-found", "Trip not found", "trip_not_found", "Trip was not found"
	case errors.Is(err, trip.ErrTripCompleted):
		status, slug, title, code, detail = http.StatusConflict, "trip-completed", "Trip completed", "trip_completed", "Operation is not allowed for a completed trip"
	case errors.Is(err, trip.ErrDriverBusy):
		status, slug, title, code, detail = http.StatusConflict, "driver-busy", "Driver busy", "driver_busy", "Driver already has an active trip"
	case errors.Is(err, trip.ErrIdempotencyConflict):
		status, slug, title, code, detail = http.StatusConflict, "idempotency-conflict", "Idempotency conflict", "idempotency_conflict", "Idempotency-Key was already used with a different request body"
	}
	return status, api.Problem{Type: "https://tripgo.example/problems/" + slug, Title: title, Status: int32(status), Detail: &detail, Code: code}
}
