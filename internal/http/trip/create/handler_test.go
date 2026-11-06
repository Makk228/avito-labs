package create_test

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	"github.com/Makk228/avito-labs/internal/domain/trip"
	api "github.com/Makk228/avito-labs/internal/generated"
	"github.com/Makk228/avito-labs/internal/http/trip/create"
	"github.com/Makk228/avito-labs/internal/model"
)

const validBody = `{"user_id":"5cb72c04-7650-45c9-a79b-bcdba0631e0c","driver_id":"8860b315-ec86-42eb-a17c-7c163d721ff5","start_point":{"latitude":55,"longitude":37},"end_point":{"latitude":56,"longitude":38},"price":100}`

func TestHandler(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		replay bool
		status int
		code   string
	}{
		{name: "created", status: 201},
		{name: "replay", replay: true, status: 200},
		{name: "busy", err: trip.ErrDriverBusy, status: 409, code: "driver_busy"},
		{name: "internal", err: errors.New("password=secret"), status: 500, code: "internal_error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			uc := NewMockusecase(gomock.NewController(t))
			id := uuid.New()
			uc.EXPECT().Create(gomock.Any(), gomock.Any(), nil).Return(model.Trip{ID: id, Status: trip.StatusActive}, tc.replay, tc.err)
			w := httptest.NewRecorder()
			r := httptest.NewRequest("POST", "/api/v1/trips", strings.NewReader(validBody))
			create.New(uc).Handle(w, r, api.CreateTripParams{})
			if w.Code != tc.status {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			if tc.err == nil {
				var body api.Trip
				if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if body.Id != id {
					t.Fatal("wrong trip")
				}
				if !tc.replay && w.Header().Get("Location") != "/api/v1/trips/"+id.String() {
					t.Fatal("missing Location")
				}
			} else {
				var body api.Problem
				if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if body.Code != tc.code || strings.Contains(w.Body.String(), "secret") || w.Header().Get("Content-Type") != "application/problem+json" {
					t.Fatalf("bad problem: %s", w.Body.String())
				}
			}
		})
	}
}

func TestInvalidBody(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"empty", ""},
		{"syntax", "{"},
		{"null", "null"},
		{"array", "[]"},
		{"missing", "{}"},
		{"null field", strings.Replace(validBody, `"price":100`, `"price":null`, 1)},
		{"unknown", strings.Replace(validBody, `"price":100`, `"price":100,"unexpected":1`, 1)},
		{"uuid", strings.Replace(validBody, "5cb72c04-7650-45c9-a79b-bcdba0631e0c", "invalid", 1)},
		{"zero uuid", strings.Replace(validBody, "5cb72c04-7650-45c9-a79b-bcdba0631e0c", uuid.Nil.String(), 1)},
		{"latitude", strings.Replace(validBody, `"latitude":55`, `"latitude":91`, 1)},
		{"longitude", strings.Replace(validBody, `"longitude":38`, `"longitude":181`, 1)},
		{"price", strings.Replace(validBody, `"price":100`, `"price":-1`, 1)},
		{"fractional price", strings.Replace(validBody, `"price":100`, `"price":1.5`, 1)},
		{"nested missing", strings.Replace(validBody, `"latitude":55,`, "", 1)},
		{"trailing", validBody + ` {}`},
		{"large", strings.Repeat(" ", 65537)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			uc := NewMockusecase(gomock.NewController(t)) // No calls are allowed for invalid input.
			w := httptest.NewRecorder()
			r := httptest.NewRequest("POST", "/api/v1/trips", strings.NewReader(tc.body))
			create.New(uc).Handle(w, r, api.CreateTripParams{})
			var body api.Problem
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if w.Code != 400 || body.Code != "invalid_request" || body.Detail == nil {
				t.Fatalf("invalid response: %d %s", w.Code, w.Body.String())
			}
		})
	}
}
