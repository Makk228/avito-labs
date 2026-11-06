package get_test

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
	"github.com/Makk228/avito-labs/internal/http/trip/get"
	"github.com/Makk228/avito-labs/internal/model"
)

func TestHandler(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
		code   string
	}{{"success", nil, 200, ""}, {"missing", trip.ErrTripNotFound, 404, "trip_not_found"}, {"internal", errors.New("SQL secret"), 500, "internal_error"}} {
		t.Run(tc.name, func(t *testing.T) {
			id := uuid.New()
			uc := NewMockusecase(gomock.NewController(t))
			uc.EXPECT().Get(gomock.Any(), id).Return(model.Trip{ID: id}, tc.err)
			w := httptest.NewRecorder()
			get.New(uc).Handle(w, httptest.NewRequest("GET", "/api/v1/trips/"+id.String(), nil), id)
			if w.Code != tc.status || strings.Contains(w.Body.String(), "secret") {
				t.Fatalf("bad response: %d %s", w.Code, w.Body.String())
			}
			if tc.err == nil {
				var value api.Trip
				if err := json.Unmarshal(w.Body.Bytes(), &value); err != nil {
					t.Fatal(err)
				}
				if value.Id != id {
					t.Fatal("wrong trip")
				}
			} else {
				var p api.Problem
				if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
					t.Fatal(err)
				}
				if p.Code != tc.code {
					t.Fatal(p.Code)
				}
			}
		})
	}
}
