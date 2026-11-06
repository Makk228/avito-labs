package domainerror_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/Makk228/avito-labs/internal/domain/trip"
	"github.com/Makk228/avito-labs/internal/http/domainerror"
)

func TestProblem(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"missing", trip.ErrTripNotFound, 404, "trip_not_found"},
		{"completed", trip.ErrTripCompleted, 409, "trip_completed"},
		{"busy", trip.ErrDriverBusy, 409, "driver_busy"},
		{"idempotency", trip.ErrIdempotencyConflict, 409, "idempotency_conflict"},
		{"validation", &trip.ValidationError{Field: "price", Message: "must be nonnegative"}, 400, "invalid_request"},
		{"internal", errors.New("secret password=postgres SQL SELECT"), 500, "internal_error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, body := domainerror.Problem(fmt.Errorf("usecase: %w", tc.err))
			if status != tc.status || int(body.Status) != status || body.Code != tc.code || body.Detail == nil || body.Title == "" || body.Type == "" {
				t.Fatalf("bad problem: %d %+v", status, body)
			}
			if status == 500 && (strings.Contains(*body.Detail, "secret") || *body.Detail != "Internal server error") {
				t.Fatalf("internal error leaked: %+v", body)
			}
		})
	}
}
