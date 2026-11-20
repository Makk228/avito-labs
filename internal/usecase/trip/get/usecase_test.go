package get_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/Makk228/avito-labs/internal/domain/trip"
	"github.com/Makk228/avito-labs/internal/model"
	"github.com/Makk228/avito-labs/internal/usecase/trip/get"
)

func TestGet(t *testing.T) {
	id := uuid.MustParse("00000000-0000-4000-8000-000000000001")
	value := model.Trip{ID: id, Status: trip.StatusActive}
	repositoryError := errors.New("database unavailable")
	ctx := context.Background()

	tests := []struct {
		name    string
		setup   func(*MocktripRepository)
		want    model.Trip
		wantErr error
	}{
		{
			name: "returns the complete trip",
			setup: func(repo *MocktripRepository) {
				repo.EXPECT().Get(ctx, id).Return(value, nil)
			},
			want: value,
		},
		{
			name: "missing trip preserves the domain error",
			setup: func(repo *MocktripRepository) {
				repo.EXPECT().Get(ctx, id).Return(model.Trip{}, trip.ErrTripNotFound)
			},
			wantErr: trip.ErrTripNotFound,
		},
		{
			name: "repository error returns no trip",
			setup: func(repo *MocktripRepository) {
				repo.EXPECT().Get(ctx, id).Return(model.Trip{}, repositoryError)
			},
			wantErr: repositoryError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := NewMocktripRepository(gomock.NewController(t))
			tt.setup(repo)
			uc := get.New(repo)

			got, err := uc.Get(ctx, id)

			require.ErrorIs(t, err, tt.wantErr)
			assert.Equal(t, tt.want, got)
		})
	}
}
