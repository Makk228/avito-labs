package create_test

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
	"github.com/Makk228/avito-labs/internal/http/trip/create"
	"github.com/Makk228/avito-labs/internal/model"
)

const validBody = `{"user_id":"5cb72c04-7650-45c9-a79b-bcdba0631e0c",
"driver_id":"8860b315-ec86-42eb-a17c-7c163d721ff5",
"start_point":{"latitude":55,"longitude":37},
"end_point":{"latitude":56,"longitude":38},
"price":100}`

func input() model.NewTrip {
	return model.NewTrip{
		UserID:   uuid.MustParse("5cb72c04-7650-45c9-a79b-bcdba0631e0c"),
		DriverID: uuid.MustParse("8860b315-ec86-42eb-a17c-7c163d721ff5"),
		Start:    model.Point{Latitude: 55, Longitude: 37},
		End:      model.Point{Latitude: 56, Longitude: 38},
		Price:    100,
	}
}

func TestHandlerSuccess(t *testing.T) {
	id := uuid.MustParse("00000000-0000-4000-8000-000000000001")
	key := uuid.MustParse("00000000-0000-4000-8000-000000000002")
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	value := model.Trip{
		ID: id, UserID: input().UserID, DriverID: input().DriverID,
		Start: input().Start, End: input().End, Price: 100,
		Status: trip.StatusActive, StartedAt: at,
	}

	for _, tc := range []struct {
		name         string
		result       model.CreateTripResult
		key          *uuid.UUID
		wantStatus   int
		wantLocation string
	}{
		{
			name:         "created without key",
			result:       model.CreateTripResult{Trip: value, Created: true},
			wantStatus:   http.StatusCreated,
			wantLocation: "/api/v1/trips/" + id.String(),
		},
		{
			name:         "created with key",
			key:          &key,
			result:       model.CreateTripResult{Trip: value, Created: true},
			wantStatus:   http.StatusCreated,
			wantLocation: "/api/v1/trips/" + id.String(),
		},
		{
			name:       "existing key",
			key:        &key,
			result:     model.CreateTripResult{Trip: value},
			wantStatus: http.StatusOK,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/api/v1/trips", strings.NewReader(validBody))
			w := httptest.NewRecorder()
			uc := NewMockusecase(gomock.NewController(t))
			uc.EXPECT().Create(r.Context(), input(), tc.key).Return(tc.result, nil)

			create.New(uc).Handle(w, r, api.CreateTripParams{IdempotencyKey: tc.key})

			assert.Equal(t, tc.wantStatus, w.Code)
			assert.Equal(t, tc.wantLocation, w.Header().Get("Location"))
			assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
			assert.JSONEq(t, `{"id":"00000000-0000-4000-8000-000000000001",
"user_id":"5cb72c04-7650-45c9-a79b-bcdba0631e0c",
"driver_id":"8860b315-ec86-42eb-a17c-7c163d721ff5",
"start_point":{"latitude":55,"longitude":37},
"end_point":{"latitude":56,"longitude":38},
"price":100,
"status":"active",
"started_at":"2026-01-01T00:00:00Z",
"finished_at":null,
"last_position_at":null}`, w.Body.String())
		})
	}
}

func TestHandlerErrors(t *testing.T) {
	for _, tc := range []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{
			name:       "busy driver",
			err:        trip.ErrDriverBusy,
			wantStatus: http.StatusConflict,
			wantCode:   "driver_busy",
		},
		{
			name:       "internal error",
			err:        errors.New("password=secret"),
			wantStatus: http.StatusInternalServerError,
			wantCode:   "internal_error",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/api/v1/trips", strings.NewReader(validBody))
			w := httptest.NewRecorder()
			uc := NewMockusecase(gomock.NewController(t))
			uc.EXPECT().Create(r.Context(), input(), nil).Return(model.CreateTripResult{}, tc.err)

			create.New(uc).Handle(w, r, api.CreateTripParams{})

			var body api.Problem
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			assert.Equal(t, tc.wantStatus, w.Code)
			assert.Equal(t, tc.wantCode, body.Code)
			assert.EqualValues(t, tc.wantStatus, body.Status)
			assert.Equal(t, "application/problem+json", w.Header().Get("Content-Type"))
			assert.NotContains(t, w.Body.String(), "secret")
			assert.Empty(t, w.Header().Get("Location"))
		})
	}
}

func TestInvalidBody(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{name: "empty", body: ""},
		{name: "syntax", body: "{"},
		{name: "null", body: "null"},
		{name: "array", body: "[]"},
		{name: "missing", body: "{}"},
		{name: "null field", body: strings.Replace(validBody, `"price":100`, `"price":null`, 1)},
		{name: "unknown", body: strings.Replace(validBody, `"price":100`, `"price":100,"unexpected":1`, 1)},
		{name: "uuid", body: strings.Replace(validBody, "5cb72c04-7650-45c9-a79b-bcdba0631e0c", "invalid", 1)},
		{name: "zero uuid", body: strings.Replace(validBody, "5cb72c04-7650-45c9-a79b-bcdba0631e0c", uuid.Nil.String(), 1)},
		{name: "latitude", body: strings.Replace(validBody, `"latitude":55`, `"latitude":91`, 1)},
		{name: "longitude", body: strings.Replace(validBody, `"longitude":38`, `"longitude":181`, 1)},
		{name: "price", body: strings.Replace(validBody, `"price":100`, `"price":-1`, 1)},
		{name: "fractional price", body: strings.Replace(validBody, `"price":100`, `"price":1.5`, 1)},
		{name: "nested missing", body: strings.Replace(validBody, `"latitude":55,`, "", 1)},
		{name: "trailing", body: validBody + ` {}`},
		{name: "large", body: strings.Repeat(" ", 65537)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			uc := NewMockusecase(gomock.NewController(t))
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodPost, "/api/v1/trips", strings.NewReader(tc.body))

			create.New(uc).Handle(w, r, api.CreateTripParams{})

			var body api.Problem
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Equal(t, "invalid_request", body.Code)
			require.NotNil(t, body.Detail)
			assert.NotEmpty(t, *body.Detail)
		})
	}
}
