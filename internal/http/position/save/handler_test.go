package save_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/Makk228/avito-labs/internal/domain/trip"
	api "github.com/Makk228/avito-labs/internal/generated"
	"github.com/Makk228/avito-labs/internal/http/position/save"
	"github.com/Makk228/avito-labs/internal/model"
)

const validBody = `{"coordinates":{"latitude":59,"longitude":30},"recorded_at":"2026-11-20T12:00:00Z"}`

func TestHandlerCreated(t *testing.T) {
	id := uuid.MustParse("00000000-0000-4000-8000-000000000001")
	at := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	input := model.NewPosition{TripID: id, Coordinates: model.Point{Latitude: 59, Longitude: 30}, RecordedAt: at}
	value := model.Position{ID: 42, TripID: id, Coordinates: input.Coordinates, RecordedAt: at}
	r := httptest.NewRequest(http.MethodPost, "/api/v1/trips/"+id.String()+"/positions", strings.NewReader(validBody))
	w := httptest.NewRecorder()
	uc := NewMockusecase(gomock.NewController(t))
	uc.EXPECT().Save(r.Context(), input).Return(value, nil)

	save.New(uc).Handle(w, r, id)

	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
	assert.JSONEq(t, `{
        "id":42,"trip_id":"00000000-0000-4000-8000-000000000001",
        "coordinates":{"latitude":59,"longitude":30},"recorded_at":"2026-11-20T12:00:00Z"
    }`, w.Body.String())
}

func TestHandlerProblems(t *testing.T) {
	id := uuid.MustParse("00000000-0000-4000-8000-000000000001")
	input := model.NewPosition{
		TripID: id, Coordinates: model.Point{Latitude: 59, Longitude: 30},
		RecordedAt: time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC),
	}
	for _, tc := range []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{name: "missing", err: trip.ErrTripNotFound, wantStatus: http.StatusNotFound, wantCode: "trip_not_found"},
		{name: "completed", err: trip.ErrTripCompleted, wantStatus: http.StatusConflict, wantCode: "trip_completed"},
		{name: "invalid coordinates or time", err: &trip.ValidationError{Field: "recorded_at", Message: "is in the future"}, wantStatus: http.StatusBadRequest, wantCode: "invalid_request"},
		{name: "internal", err: errors.New("SQL password=secret"), wantStatus: http.StatusInternalServerError, wantCode: "internal_error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/api/v1/trips/"+id.String()+"/positions", strings.NewReader(validBody))
			w := httptest.NewRecorder()
			uc := NewMockusecase(gomock.NewController(t))
			uc.EXPECT().Save(r.Context(), input).Return(model.Position{}, tc.err)

			save.New(uc).Handle(w, r, id)

			var problem api.Problem
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &problem))
			assert.Equal(t, tc.wantStatus, w.Code)
			assert.Equal(t, tc.wantCode, problem.Code)
			assert.Equal(t, "application/problem+json", w.Header().Get("Content-Type"))
			assert.NotContains(t, w.Body.String(), "secret")
		})
	}
}

func TestMalformedBodyNeverCallsUsecase(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{name: "empty"},
		{name: "null", body: "null"},
		{name: "syntax", body: "{"},
		{name: "array", body: "[]"},
		{name: "missing coordinates", body: `{"recorded_at":"2026-11-20T12:00:00Z"}`},
		{name: "missing latitude", body: strings.Replace(validBody, `"latitude":59,`, "", 1)},
		{name: "null longitude", body: strings.Replace(validBody, `"longitude":30`, `"longitude":null`, 1)},
		{name: "missing time", body: `{"coordinates":{"latitude":0,"longitude":0}}`},
		{name: "null time", body: strings.Replace(validBody, `"2026-11-20T12:00:00Z"`, "null", 1)},
		{name: "invalid time", body: strings.Replace(validBody, "2026-11-20T12:00:00Z", "yesterday", 1)},
		{name: "unknown field", body: strings.Replace(validBody, `"latitude":59`, `"latitude":59,"altitude":10`, 1)},
		{name: "trailing json", body: validBody + " {}"},
		{name: "too large", body: strings.Repeat(" ", 65537)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := uuid.MustParse("00000000-0000-4000-8000-000000000001")
			r := httptest.NewRequest(http.MethodPost, "/api/v1/trips/"+id.String()+"/positions", strings.NewReader(tc.body))
			w := httptest.NewRecorder()
			uc := NewMockusecase(gomock.NewController(t))

			save.New(uc).Handle(w, r, id)

			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Contains(t, w.Body.String(), "invalid_request")
		})
	}
}
