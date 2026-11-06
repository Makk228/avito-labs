package get_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	"github.com/Makk228/avito-labs/internal/domain/trip"
	"github.com/Makk228/avito-labs/internal/model"
	"github.com/Makk228/avito-labs/internal/usecase/trip/get"
)

func TestGet(t *testing.T) {
	for _, wantErr := range []error{nil, trip.ErrTripNotFound, errors.New("database unavailable")} {
		name := "success"
		if wantErr != nil {
			name = wantErr.Error()
		}
		t.Run(name, func(t *testing.T) {
			repo := NewMocktripRepository(gomock.NewController(t))
			id := uuid.New()
			want := model.Trip{ID: id, Status: trip.StatusActive}
			repo.EXPECT().Get(gomock.Any(), id).Return(want, wantErr)
			got, err := get.New(repo).Get(context.Background(), id)
			if !errors.Is(err, wantErr) {
				t.Fatalf("got %v, want %v", err, wantErr)
			}
			if err == nil && got.ID != id {
				t.Fatalf("wrong trip: %+v", got)
			}
			if err != nil && got.ID != uuid.Nil {
				t.Fatal("trip leaked on failure")
			}
		})
	}
}
