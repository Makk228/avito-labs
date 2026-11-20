package list_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/Makk228/avito-labs/internal/domain/trip"
	"github.com/Makk228/avito-labs/internal/model"
	"github.com/Makk228/avito-labs/internal/usecase/position/list"
)

func TestList(t *testing.T) {
	ctx := context.Background()
	id := uuid.MustParse("00000000-0000-4000-8000-000000000001")
	points := []model.Position{{
		ID: 42, TripID: id, Coordinates: model.Point{Latitude: 59, Longitude: 30},
		RecordedAt: time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC),
	}}
	failure := errors.New("database unavailable")
	for _, tc := range []struct {
		name    string
		setup   func(*MocktripRepository, *MockpositionRepository)
		want    []model.Position
		wantErr error
	}{
		{
			name: "completed trip retains route",
			setup: func(trips *MocktripRepository, positions *MockpositionRepository) {
				trips.EXPECT().Get(ctx, id).Return(model.Trip{ID: id, Status: trip.StatusCompleted}, nil)
				positions.EXPECT().List(ctx, id).Return(points, nil)
			},
			want: points,
		},
		{
			name: "empty route",
			setup: func(trips *MocktripRepository, positions *MockpositionRepository) {
				trips.EXPECT().Get(ctx, id).Return(model.Trip{ID: id, Status: trip.StatusActive}, nil)
				positions.EXPECT().List(ctx, id).Return([]model.Position{}, nil)
			},
			want: []model.Position{},
		},
		{
			name: "missing trip",
			setup: func(trips *MocktripRepository, _ *MockpositionRepository) {
				trips.EXPECT().Get(ctx, id).Return(model.Trip{}, trip.ErrTripNotFound)
			},
			wantErr: trip.ErrTripNotFound,
		},
		{
			name: "storage failure",
			setup: func(trips *MocktripRepository, positions *MockpositionRepository) {
				trips.EXPECT().Get(ctx, id).Return(model.Trip{ID: id}, nil)
				positions.EXPECT().List(ctx, id).Return(nil, failure)
			},
			wantErr: failure,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			trips, positions := NewMocktripRepository(ctrl), NewMockpositionRepository(ctrl)
			tc.setup(trips, positions)
			uc := list.New(list.Dependencies{Trips: trips, Positions: positions})

			got, err := uc.List(ctx, id)

			require.ErrorIs(t, err, tc.wantErr)
			assert.Equal(t, tc.want, got)
		})
	}
}
