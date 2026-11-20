//go:build integration

package trip_test

import (
	"context"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Makk228/avito-labs/internal/domain/trip"
	"github.com/Makk228/avito-labs/internal/infra/postgres"
	"github.com/Makk228/avito-labs/internal/model"
	positionrepo "github.com/Makk228/avito-labs/internal/repository/position"
	"github.com/Makk228/avito-labs/internal/usecase/position/save"
)

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

type observedEffects struct {
	cancelled atomic.Int64
	saved     atomic.Int64
}

func (e *observedEffects) Cancel(uuid.UUID)      { e.cancelled.Add(1) }
func (e *observedEffects) Saved(context.Context) { e.saved.Add(1) }

func TestStaleTripSelection(t *testing.T) {
	_, repo, _ := database(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	staleAt, freshAt, boundary := now.Add(-2*time.Minute), now.Add(-30*time.Second), now.Add(-time.Minute)
	noPositions := value()
	noPositions.StartedAt = now.Add(-10 * time.Minute)
	stale := value()
	stale.StartedAt, stale.LastPositionAt = now.Add(-20*time.Minute), &staleAt
	fresh := value()
	fresh.StartedAt, fresh.LastPositionAt = now.Add(-time.Hour), &freshAt
	newTrip := value()
	newTrip.StartedAt = freshAt
	exactBoundary := value()
	exactBoundary.StartedAt = boundary
	completed := value()
	completed.Status, completed.FinishedAt = trip.StatusCompleted, &staleAt

	for _, in := range []model.Trip{noPositions, stale, fresh, newTrip, exactBoundary, completed} {
		require.NoError(t, repo.Create(ctx, in))
	}

	got, err := repo.Stale(ctx, now, time.Minute)

	require.NoError(t, err)
	assert.Equal(t, []model.StaleTrip{
		{ID: noPositions.ID, DriverID: noPositions.DriverID},
		{ID: stale.ID, DriverID: stale.DriverID},
	}, got)
}

func TestPositionsAreAtomicMonotonicAndSorted(t *testing.T) {
	pool, trips, tx := database(t)
	points := positionrepo.New(pool, time.Second)
	effects := &observedEffects{}
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	uc := save.New(save.Dependencies{
		Transactions: tx, Trips: trips, Positions: points, Queue: effects, Clock: fixedClock{now: now}, Metrics: effects,
	})
	ctx := context.Background()
	original := value()
	require.NoError(t, trips.Create(ctx, original))
	recent := model.NewPosition{TripID: original.ID, Coordinates: model.Point{Latitude: 59, Longitude: 30}, RecordedAt: now}
	old := recent
	old.RecordedAt = now.Add(-time.Minute)

	first, err := uc.Save(ctx, recent)
	require.NoError(t, err)
	older, err := uc.Save(ctx, old)
	require.NoError(t, err)
	sameTime, err := uc.Save(ctx, recent)
	require.NoError(t, err)
	route, err := points.List(ctx, original.ID)
	require.NoError(t, err)

	require.Len(t, route, 3)
	assert.Equal(t, []int64{older.ID, first.ID, sameTime.ID}, []int64{route[0].ID, route[1].ID, route[2].ID})
	assert.True(t, route[0].RecordedAt.Equal(old.RecordedAt))
	assert.Equal(t, recent.Coordinates, route[1].Coordinates)
	got, err := trips.Get(ctx, original.ID)
	require.NoError(t, err)
	require.NotNil(t, got.LastPositionAt)
	assert.True(t, now.Equal(*got.LastPositionAt), "late coordinate must not move the watermark backwards")
	assert.EqualValues(t, 3, effects.cancelled.Load())
	assert.EqualValues(t, 3, effects.saved.Load())

	empty, err := points.List(ctx, uuid.New())
	require.NoError(t, err)
	assert.Equal(t, []model.Position{}, empty)
}

func TestWatermarkFailureRollsBackTheInsertedPoint(t *testing.T) {
	pool, trips, tx := database(t)
	ctx := context.Background()
	original := value()
	require.NoError(t, trips.Create(ctx, original))
	_, err := pool.Exec(ctx, `
        CREATE FUNCTION reject_watermark() RETURNS trigger LANGUAGE plpgsql AS $$
        BEGIN RAISE EXCEPTION 'injected watermark failure'; END;
        $$;
        CREATE TRIGGER reject_watermark BEFORE UPDATE OF last_position_at ON trips
        FOR EACH ROW EXECUTE FUNCTION reject_watermark();`)
	require.NoError(t, err)
	points := positionrepo.New(pool, time.Second)
	effects := &observedEffects{}
	uc := save.New(save.Dependencies{
		Transactions: tx, Trips: trips, Positions: points, Queue: effects,
		Clock: fixedClock{now: original.StartedAt}, Metrics: effects,
	})

	saved, err := uc.Save(ctx, model.NewPosition{
		TripID: original.ID, Coordinates: model.Point{}, RecordedAt: original.StartedAt,
	})

	require.ErrorContains(t, err, "injected watermark failure")
	assert.Empty(t, saved)
	route, err := points.List(ctx, original.ID)
	require.NoError(t, err)
	assert.Empty(t, route)
	got, err := trips.Get(ctx, original.ID)
	require.NoError(t, err)
	assert.Nil(t, got.LastPositionAt)
	assert.Zero(t, effects.saved.Load())
	assert.Zero(t, effects.cancelled.Load())
}

type signallingClock struct {
	now     time.Time
	entered chan struct{}
}

func (c signallingClock) Now() time.Time { close(c.entered); return c.now }

func TestConcurrentCompletionPreventsCoordinateAcceptance(t *testing.T) {
	pool, trips, tx := database(t)
	ctx := context.Background()
	original := value()
	require.NoError(t, trips.Create(ctx, original))
	effects := &observedEffects{}
	entered := make(chan struct{})
	result := make(chan error, 1)
	uc := save.New(save.Dependencies{
		Transactions: tx, Trips: trips, Positions: positionrepo.New(pool, time.Second), Queue: effects,
		Clock: signallingClock{now: original.StartedAt, entered: entered}, Metrics: effects,
	})

	err := tx.Do(ctx, func(txCtx context.Context) error {
		_, err := trips.GetForUpdate(txCtx, original.ID)
		if err != nil {
			return err
		}

		_, err = trips.FinishActive(txCtx, original.ID, original.StartedAt.Add(time.Minute))
		if err != nil {
			return err
		}

		go func() {
			_, err := uc.Save(ctx, model.NewPosition{TripID: original.ID, RecordedAt: original.StartedAt})
			result <- err
		}()
		<-entered
		return nil
	})
	require.NoError(t, err)

	require.ErrorIs(t, <-result, trip.ErrTripCompleted)
	assert.Zero(t, effects.saved.Load())
	assert.Zero(t, effects.cancelled.Load())
}

func TestPositionMigrationsCanBeRolledBackAndAppliedAgain(t *testing.T) {
	pool, _, _ := database(t)
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	require.NoError(t, postgres.Migrate(ctx, pool, "down", log)) // concurrent index
	require.NoError(t, postgres.Migrate(ctx, pool, "down", log)) // positions and watermark
	var count int
	err := pool.QueryRow(ctx, `
        SELECT count(*) FROM information_schema.columns
        WHERE table_schema=current_schema() AND table_name='trips' AND column_name='last_position_at'`).Scan(&count)
	require.NoError(t, err)
	assert.Zero(t, count)
	require.NoError(t, postgres.Migrate(ctx, pool, "up", log))

	var indexExists bool
	err = pool.QueryRow(ctx, `
        SELECT EXISTS(SELECT 1 FROM pg_indexes
        WHERE schemaname=current_schema() AND indexname='trips_active_last_position_idx')`).Scan(&indexExists)
	require.NoError(t, err)
	assert.True(t, indexExists)
}
