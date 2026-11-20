// Package domainerror is the single domain-to-HTTP error mapping.
package domainerror

import (
	"errors"
	"net/http"

	"github.com/Makk228/avito-labs/internal/domain/trip"
	api "github.com/Makk228/avito-labs/internal/generated"
)

const problemTypeBase = "https://tripgo.example/problems/"

type definition struct {
	cause  error
	status int32
	slug   string
	title  string
	code   string
	detail string
}

var domainProblems = []definition{
	{
		cause:  trip.ErrTripNotFound,
		status: http.StatusNotFound,
		slug:   "trip-not-found",
		title:  "Trip not found",
		code:   "trip_not_found",
		detail: "Trip was not found",
	},
	{
		cause:  trip.ErrTripCompleted,
		status: http.StatusConflict,
		slug:   "trip-completed",
		title:  "Trip completed",
		code:   "trip_completed",
		detail: "Operation is not allowed for a completed trip",
	},
	{
		cause:  trip.ErrDriverBusy,
		status: http.StatusConflict,
		slug:   "driver-busy",
		title:  "Driver busy",
		code:   "driver_busy",
		detail: "Driver already has an active trip",
	},
	{
		cause:  trip.ErrIdempotencyConflict,
		status: http.StatusConflict,
		slug:   "idempotency-conflict",
		title:  "Idempotency conflict",
		code:   "idempotency_conflict",
		detail: "Idempotency-Key was already used with a different request body",
	},
}

func Problem(err error) (int, api.Problem) {
	var validation *trip.ValidationError
	if errors.As(err, &validation) {
		return (definition{
			status: http.StatusBadRequest,
			slug:   "invalid-request",
			title:  "Invalid request",
			code:   "invalid_request",
			detail: validation.Error(),
		}).problem()
	}

	for _, candidate := range domainProblems {
		if errors.Is(err, candidate.cause) {
			return candidate.problem()
		}
	}

	return (definition{
		status: http.StatusInternalServerError,
		slug:   "internal-error",
		title:  "Internal Server Error",
		code:   "internal_error",
		detail: "Internal server error",
	}).problem()
}

func (d definition) problem() (int, api.Problem) {
	return int(d.status), api.Problem{
		Type:   problemTypeBase + d.slug,
		Title:  d.title,
		Status: d.status,
		Code:   d.code,
		Detail: &d.detail,
	}
}
