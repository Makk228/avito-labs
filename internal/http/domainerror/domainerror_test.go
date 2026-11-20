package domainerror_test

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Makk228/avito-labs/internal/domain/trip"
	"github.com/Makk228/avito-labs/internal/http/domainerror"
)

func TestProblem(t *testing.T) {
	for _, tc := range []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
		wantDetail string
	}{
		{
			name:       "missing",
			err:        trip.ErrTripNotFound,
			wantStatus: http.StatusNotFound,
			wantCode:   "trip_not_found",
			wantDetail: "Trip was not found",
		},
		{
			name:       "completed",
			err:        trip.ErrTripCompleted,
			wantStatus: http.StatusConflict,
			wantCode:   "trip_completed",
			wantDetail: "Operation is not allowed for a completed trip",
		},
		{
			name:       "busy",
			err:        trip.ErrDriverBusy,
			wantStatus: http.StatusConflict,
			wantCode:   "driver_busy",
			wantDetail: "Driver already has an active trip",
		},
		{
			name:       "idempotency",
			err:        trip.ErrIdempotencyConflict,
			wantStatus: http.StatusConflict,
			wantCode:   "idempotency_conflict",
			wantDetail: "Idempotency-Key was already used with a different request body",
		},
		{
			name:       "validation",
			err:        &trip.ValidationError{Field: "price", Message: "must be nonnegative"},
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_request",
			wantDetail: "price must be nonnegative",
		},
		{
			name:       "internal details are hidden",
			err:        errors.New("secret password=postgres SQL SELECT"),
			wantStatus: http.StatusInternalServerError,
			wantCode:   "internal_error",
			wantDetail: "Internal server error",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, body := domainerror.Problem(fmt.Errorf("usecase: %w", tc.err))

			assert.Equal(t, tc.wantStatus, status)
			assert.EqualValues(t, status, body.Status)
			assert.Equal(t, tc.wantCode, body.Code)
			require.NotNil(t, body.Detail)
			assert.Equal(t, tc.wantDetail, *body.Detail)
			assert.NotEmpty(t, body.Title)
			assert.NotEmpty(t, body.Type)
		})
	}
}
