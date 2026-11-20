package finish_test

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
	"github.com/Makk228/avito-labs/internal/usecase/trip/finish"
)

type contextKey struct{}

type fixture struct {
	t         *testing.T
	ctx       context.Context
	txCtx     context.Context
	active    model.Trip
	completed model.Trip
	at        time.Time
	repo      *MocktripRepository
	tx        *MocktxManager
	clock     *Mockclock
	log       *Mocklogger
	metrics   *Mockmetrics
}

func newFixture(t *testing.T) *fixture {
	t.Helper()

	ctrl := gomock.NewController(t)
	ctx := context.WithValue(context.Background(), contextKey{}, "request")
	completed := completedTrip()
	active := completed
	active.Status = trip.StatusActive
	active.FinishedAt = nil

	return &fixture{
		t:         t,
		ctx:       ctx,
		txCtx:     context.WithValue(ctx, contextKey{}, "transaction"),
		active:    active,
		completed: completed,
		at:        *completed.FinishedAt,
		repo:      NewMocktripRepository(ctrl),
		tx:        NewMocktxManager(ctrl),
		clock:     NewMockclock(ctrl),
		log:       NewMocklogger(ctrl),
		metrics:   NewMockmetrics(ctrl),
	}
}

func (f *fixture) usecase() *finish.Usecase {
	return finish.New(finish.Dependencies{
		Transactions: f.tx,
		Trips:        f.repo,
		Clock:        f.clock,
		Logger:       f.log,
		Metrics:      f.metrics,
	})
}

func (f *fixture) transaction(callbackError, resultError error) {
	f.tx.EXPECT().Do(f.ctx, gomock.Any()).DoAndReturn(
		func(_ context.Context, action func(context.Context) error) error {
			require.ErrorIs(f.t, action(f.txCtx), callbackError)
			return resultError
		},
	)
}

func (f *fixture) fetchActive() {
	f.repo.EXPECT().Get(f.txCtx, f.active.ID).Return(f.active, nil)
	f.clock.EXPECT().Now().Return(f.at)
}

func (f *fixture) update(result model.FinishTripResult, err error) {
	f.repo.EXPECT().FinishActive(f.txCtx, f.active.ID, f.at).Return(result, err)
}

func (f *fixture) history(err error) {
	from := trip.StatusActive
	f.repo.EXPECT().AddStatusChange(f.txCtx, model.StatusChange{
		TripID:    f.active.ID,
		From:      &from,
		To:        trip.StatusCompleted,
		Reason:    "trip finished",
		ChangedAt: f.at,
	}).Return(err)
}

func completedTrip() model.Trip {
	at := time.Date(2026, time.November, 1, 12, 0, 0, 0, time.UTC)
	return model.Trip{
		ID:         uuid.MustParse("00000000-0000-4000-8000-000000000001"),
		UserID:     uuid.MustParse("00000000-0000-4000-8000-000000000002"),
		DriverID:   uuid.MustParse("00000000-0000-4000-8000-000000000003"),
		Price:      100,
		Status:     trip.StatusCompleted,
		StartedAt:  at.Add(-time.Minute),
		FinishedAt: &at,
	}
}

func TestFinish(t *testing.T) {
	repositoryError := errors.New("database unavailable")
	completed := completedTrip()
	update := model.FinishTripResult{Trip: completed, Updated: true}

	tests := []struct {
		name    string
		setup   func(*fixture)
		want    model.Trip
		wantErr error
	}{
		{
			name: "completes the trip and records the exact transition",
			setup: func(f *fixture) {
				f.transaction(nil, nil)
				f.fetchActive()
				f.update(update, nil)
				f.history(nil)
				f.metrics.EXPECT().Completed(f.ctx)
				f.log.EXPECT().InfoContext(f.ctx, "trip finished", "trip_id", f.active.ID.String())
			},
			want: completed,
		},
		{
			name: "completed trip cannot be updated again",
			setup: func(f *fixture) {
				f.transaction(trip.ErrTripCompleted, trip.ErrTripCompleted)
				f.repo.EXPECT().Get(f.txCtx, f.active.ID).Return(f.completed, nil)
				f.clock.EXPECT().Now().Return(f.at)
			},
			wantErr: trip.ErrTripCompleted,
		},
		{
			name: "missing trip is not updated",
			setup: func(f *fixture) {
				f.transaction(trip.ErrTripNotFound, trip.ErrTripNotFound)
				f.repo.EXPECT().Get(f.txCtx, f.active.ID).Return(model.Trip{}, trip.ErrTripNotFound)
			},
			wantErr: trip.ErrTripNotFound,
		},
		{
			name: "fetch error preserves the repository cause",
			setup: func(f *fixture) {
				f.transaction(repositoryError, repositoryError)
				f.repo.EXPECT().Get(f.txCtx, f.active.ID).Return(model.Trip{}, repositoryError)
			},
			wantErr: repositoryError,
		},
		{
			name: "update error prevents history and business events",
			setup: func(f *fixture) {
				f.transaction(repositoryError, repositoryError)
				f.fetchActive()
				f.update(model.FinishTripResult{}, repositoryError)
			},
			wantErr: repositoryError,
		},
		{
			name: "concurrent completion reports a conflict",
			setup: func(f *fixture) {
				f.transaction(trip.ErrTripCompleted, trip.ErrTripCompleted)
				f.fetchActive()
				f.update(model.FinishTripResult{}, nil)
			},
			wantErr: trip.ErrTripCompleted,
		},
		{
			name: "history error rolls back completion",
			setup: func(f *fixture) {
				f.transaction(repositoryError, repositoryError)
				f.fetchActive()
				f.update(update, nil)
				f.history(repositoryError)
			},
			wantErr: repositoryError,
		},
		{
			name: "begin failure does not fetch the trip",
			setup: func(f *fixture) {
				f.tx.EXPECT().Do(f.ctx, gomock.Any()).Return(repositoryError)
			},
			wantErr: repositoryError,
		},
		{
			name: "commit failure does not increment metrics or log completion",
			setup: func(f *fixture) {
				f.transaction(nil, repositoryError)
				f.fetchActive()
				f.update(update, nil)
				f.history(nil)
			},
			wantErr: repositoryError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			tt.setup(f)

			got, err := f.usecase().Finish(f.ctx, f.active.ID)

			require.ErrorIs(t, err, tt.wantErr)
			assert.Equal(t, tt.want, got)
		})
	}
}
