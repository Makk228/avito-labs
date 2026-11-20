package list_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"

	"github.com/Makk228/avito-labs/internal/domain/trip"
	"github.com/Makk228/avito-labs/internal/http/position/list"
	"github.com/Makk228/avito-labs/internal/model"
)

func TestHandler(t *testing.T) {
	id := uuid.MustParse("00000000-0000-4000-8000-000000000001")
	for _, tc := range []struct {
		name       string
		result     []model.Position
		err        error
		wantStatus int
		wantBody   string
	}{
		{
			name: "route",
			result: []model.Position{{
				ID: 42, TripID: id, Coordinates: model.Point{Latitude: 59, Longitude: 30},
				RecordedAt: time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC),
			}},
			wantStatus: http.StatusOK,
			wantBody:   `{"positions":[{"id":42,"trip_id":"00000000-0000-4000-8000-000000000001","coordinates":{"latitude":59,"longitude":30},"recorded_at":"2026-11-20T12:00:00Z"}]}`,
		},
		{name: "empty array", wantStatus: http.StatusOK, wantBody: `{"positions":[]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/api/v1/trips/"+id.String()+"/positions", nil)
			w := httptest.NewRecorder()
			uc := NewMockusecase(gomock.NewController(t))
			uc.EXPECT().List(r.Context(), id).Return(tc.result, tc.err)

			list.New(uc).Handle(w, r, id)

			assert.Equal(t, tc.wantStatus, w.Code)
			assert.JSONEq(t, tc.wantBody, w.Body.String())
		})
	}
}

func TestHandlerProblems(t *testing.T) {
	for _, tc := range []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{name: "missing", err: trip.ErrTripNotFound, wantStatus: http.StatusNotFound, wantCode: "trip_not_found"},
		{name: "internal", err: errors.New("SQL secret"), wantStatus: http.StatusInternalServerError, wantCode: "internal_error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := uuid.MustParse("00000000-0000-4000-8000-000000000001")
			r := httptest.NewRequest(http.MethodGet, "/api/v1/trips/"+id.String()+"/positions", nil)
			w := httptest.NewRecorder()
			uc := NewMockusecase(gomock.NewController(t))
			uc.EXPECT().List(r.Context(), id).Return(nil, tc.err)

			list.New(uc).Handle(w, r, id)

			assert.Equal(t, tc.wantStatus, w.Code)
			assert.Contains(t, w.Body.String(), tc.wantCode)
			assert.NotContains(t, w.Body.String(), "secret")
			assert.Equal(t, "application/problem+json", w.Header().Get("Content-Type"))
		})
	}
}
