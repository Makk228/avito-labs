package get_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/Makk228/avito-labs/internal/domain/trip"
	api "github.com/Makk228/avito-labs/internal/generated"
	"github.com/Makk228/avito-labs/internal/http/trip/get"
	"github.com/Makk228/avito-labs/internal/model"
)

func TestHandlerSuccess(t *testing.T) {
	id := uuid.MustParse("00000000-0000-4000-8000-000000000001")
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	value := model.Trip{
		ID:        id,
		UserID:    uuid.MustParse("00000000-0000-4000-8000-000000000002"),
		DriverID:  uuid.MustParse("00000000-0000-4000-8000-000000000003"),
		Start:     model.Point{Latitude: 55, Longitude: 37},
		End:       model.Point{Latitude: 56, Longitude: 38},
		Price:     100,
		Status:    trip.StatusActive,
		StartedAt: at,
	}

	r := httptest.NewRequest(http.MethodGet, "/api/v1/trips/"+id.String()+"", nil)
	w := httptest.NewRecorder()
	uc := NewMockusecase(gomock.NewController(t))
	uc.EXPECT().Get(r.Context(), id).Return(value, nil)

	get.New(uc).Handle(w, r, id)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
	assert.JSONEq(t, `{"id":"00000000-0000-4000-8000-000000000001",
"user_id":"00000000-0000-4000-8000-000000000002",
"driver_id":"00000000-0000-4000-8000-000000000003",
"start_point":{"latitude":55,"longitude":37},
"end_point":{"latitude":56,"longitude":38},
"price":100,
"status":"active",
"started_at":"2026-01-01T00:00:00Z",
"finished_at":null,
"last_position_at":null}`, w.Body.String())
}

func TestHandlerErrors(t *testing.T) {
	for _, tc := range []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{
			name:       "missing trip",
			err:        trip.ErrTripNotFound,
			wantStatus: http.StatusNotFound,
			wantCode:   "trip_not_found",
		},
		{
			name:       "internal error",
			err:        errors.New("SQL secret"),
			wantStatus: http.StatusInternalServerError,
			wantCode:   "internal_error",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := uuid.MustParse("00000000-0000-4000-8000-000000000001")
			r := httptest.NewRequest(http.MethodGet, "/api/v1/trips/"+id.String()+"", nil)
			w := httptest.NewRecorder()
			uc := NewMockusecase(gomock.NewController(t))
			uc.EXPECT().Get(r.Context(), id).Return(model.Trip{}, tc.err)

			get.New(uc).Handle(w, r, id)

			var body api.Problem
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			assert.Equal(t, tc.wantStatus, w.Code)
			assert.Equal(t, tc.wantCode, body.Code)
			assert.EqualValues(t, tc.wantStatus, body.Status)
			assert.Equal(t, "application/problem+json", w.Header().Get("Content-Type"))
			assert.NotContains(t, w.Body.String(), "secret")
		})
	}
}
