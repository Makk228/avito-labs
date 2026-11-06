package finish_test

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	"github.com/Makk228/avito-labs/internal/domain/trip"
	api "github.com/Makk228/avito-labs/internal/generated"
	"github.com/Makk228/avito-labs/internal/http/trip/finish"
	"github.com/Makk228/avito-labs/internal/model"
)

func TestHandler(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
		code   string
	}{{"success", nil, 200, ""}, {"completed", trip.ErrTripCompleted, 409, "trip_completed"}, {"missing", trip.ErrTripNotFound, 404, "trip_not_found"}, {"internal", errors.New("SQL secret"), 500, "internal_error"}} {
		t.Run(tc.name, func(t *testing.T) {
			id := uuid.New()
			at := time.Now().UTC()
			uc := NewMockusecase(gomock.NewController(t))
			uc.EXPECT().Finish(gomock.Any(), id).Return(model.Trip{ID: id, Status: trip.StatusCompleted, FinishedAt: &at}, tc.err)
			w := httptest.NewRecorder()
			finish.New(uc).Handle(w, httptest.NewRequest("POST", "/api/v1/trips/"+id.String()+"/finish", nil), id)
			if w.Code != tc.status || strings.Contains(w.Body.String(), "secret") {
				t.Fatalf("bad response: %d %s", w.Code, w.Body.String())
			}
			if tc.err == nil {
				var value api.Trip
				if err := json.Unmarshal(w.Body.Bytes(), &value); err != nil {
					t.Fatal(err)
				}
				if value.Id != id || value.Status != api.Completed || value.FinishedAt.IsNull() {
					t.Fatalf("wrong trip: %+v", value)
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
