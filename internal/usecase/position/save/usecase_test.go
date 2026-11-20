package save_test

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
	"github.com/Makk228/avito-labs/internal/usecase/position/save"
)

type (
	contextKey struct{}
	fixture    struct {
		t       *testing.T
		ctx     context.Context
		txCtx   context.Context
		input   model.NewPosition
		value   model.Position
		tx      *MocktxManager
		trips   *MocktripRepository
		points  *MockpositionRepository
		queue   *Mockqueue
		clock   *Mockclock
		metrics *Mockmetrics
	}
)

func savedPosition() model.Position {
	return model.Position{
		ID:          42,
		TripID:      uuid.MustParse("00000000-0000-4000-8000-000000000001"),
		Coordinates: model.Point{Latitude: 59, Longitude: 30},
		RecordedAt:  time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC),
	}
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	ctx := context.WithValue(context.Background(), contextKey{}, "request")
	value := savedPosition()
	f := &fixture{
		t: t, ctx: ctx, txCtx: context.WithValue(ctx, contextKey{}, "transaction"),
		value: value,
		input: model.NewPosition{TripID: value.TripID, Coordinates: value.Coordinates, RecordedAt: value.RecordedAt},
		tx:    NewMocktxManager(ctrl), trips: NewMocktripRepository(ctrl),
		points: NewMockpositionRepository(ctrl), queue: NewMockqueue(ctrl),
		clock: NewMockclock(ctrl), metrics: NewMockmetrics(ctrl),
	}
	f.clock.EXPECT().Now().Return(value.RecordedAt)
	return f
}

func (f *fixture) usecase() *save.Usecase {
	return save.New(save.Dependencies{
		Transactions: f.tx, Trips: f.trips, Positions: f.points,
		Queue: f.queue, Clock: f.clock, Metrics: f.metrics,
	})
}

func (f *fixture) transaction(callbackErr, resultErr error) {
	f.tx.EXPECT().Do(f.ctx, gomock.Any()).DoAndReturn(func(_ context.Context, action func(context.Context) error) error {
		require.ErrorIs(f.t, action(f.txCtx), callbackErr)
		return resultErr
	})
}

func (f *fixture) activeTrip() {
	f.trips.EXPECT().GetForUpdate(f.txCtx, f.input.TripID).Return(model.Trip{ID: f.input.TripID, Status: trip.StatusActive}, nil)
}

func (f *fixture) savePoint() {
	f.points.EXPECT().Save(f.txCtx, f.input).Return(f.value, nil)
}

func TestSave(t *testing.T) {
	failure := errors.New("database unavailable")
	for _, tc := range []struct {
		name    string
		setup   func(*fixture)
		want    model.Position
		wantErr error
	}{
		{
			name: "committed coordinate cancels pending work and increments metric",
			setup: func(f *fixture) {
				f.transaction(nil, nil)
				f.activeTrip()
				f.savePoint()
				f.trips.EXPECT().AdvanceLastPosition(f.txCtx, f.input.TripID, f.input.RecordedAt).Return(nil)
				f.queue.EXPECT().Cancel(f.input.TripID)
				f.metrics.EXPECT().Saved(f.ctx)
			},
			want: savedPosition(),
		},
		{
			name: "missing trip",
			setup: func(f *fixture) {
				f.transaction(trip.ErrTripNotFound, trip.ErrTripNotFound)
				f.trips.EXPECT().GetForUpdate(f.txCtx, f.input.TripID).Return(model.Trip{}, trip.ErrTripNotFound)
			},
			wantErr: trip.ErrTripNotFound,
		},
		{
			name: "completed trip rejects coordinate",
			setup: func(f *fixture) {
				f.transaction(trip.ErrTripCompleted, trip.ErrTripCompleted)
				f.trips.EXPECT().GetForUpdate(f.txCtx, f.input.TripID).Return(model.Trip{Status: trip.StatusCompleted}, nil)
			},
			wantErr: trip.ErrTripCompleted,
		},
		{
			name: "insert failure",
			setup: func(f *fixture) {
				f.transaction(failure, failure)
				f.activeTrip()
				f.points.EXPECT().Save(f.txCtx, f.input).Return(model.Position{}, failure)
			},
			wantErr: failure,
		},
		{
			name: "watermark failure rolls back coordinate",
			setup: func(f *fixture) {
				f.transaction(failure, failure)
				f.activeTrip()
				f.savePoint()
				f.trips.EXPECT().AdvanceLastPosition(f.txCtx, f.input.TripID, f.input.RecordedAt).Return(failure)
			},
			wantErr: failure,
		},
		{
			name: "commit failure leaves queue and counters unchanged",
			setup: func(f *fixture) {
				f.transaction(nil, failure)
				f.activeTrip()
				f.savePoint()
				f.trips.EXPECT().AdvanceLastPosition(f.txCtx, f.input.TripID, f.input.RecordedAt).Return(nil)
			},
			wantErr: failure,
		},
		{
			name:    "begin failure",
			setup:   func(f *fixture) { f.tx.EXPECT().Do(f.ctx, gomock.Any()).Return(failure) },
			wantErr: failure,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			tc.setup(f)

			got, err := f.usecase().Save(f.ctx, f.input)

			require.ErrorIs(t, err, tc.wantErr)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestInvalidCoordinateDoesNotStartTransaction(t *testing.T) {
	f := newFixture(t)
	f.input.Coordinates.Latitude = 91

	got, err := f.usecase().Save(f.ctx, f.input)

	var validation *trip.ValidationError
	require.ErrorAs(t, err, &validation)
	assert.Equal(t, "coordinates.latitude", validation.Field)
	assert.Empty(t, got)
}
