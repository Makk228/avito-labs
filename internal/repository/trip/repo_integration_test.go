//go:build integration

package trip_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	pgcontainer "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/Makk228/avito-labs/internal/domain/trip"
	"github.com/Makk228/avito-labs/internal/infra/postgres"
	"github.com/Makk228/avito-labs/internal/infra/postgres/transaction"
	"github.com/Makk228/avito-labs/internal/model"
	triprepo "github.com/Makk228/avito-labs/internal/repository/trip"
)

// Each test applies existing migrations in a separate schema, including its
// own goose version table. Shared service data is never cleared or deleted.
func database(t *testing.T) (*pgxpool.Pool, *triprepo.TripRepository, *transaction.TxManager) {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		container, err := pgcontainer.Run(ctx, "postgres:17-alpine",
			pgcontainer.WithDatabase("tripgo"),
			pgcontainer.WithUsername("tripgo"),
			pgcontainer.WithPassword("tripgo"),
			pgcontainer.BasicWaitStrategies(),
		)
		require.NoError(t, err)
		testcontainers.CleanupContainer(t, container)
		url, err = container.ConnectionString(ctx, "sslmode=disable")
		require.NoError(t, err)
	}

	ctx := context.Background()
	admin, err := pgxpool.New(ctx, url)
	require.NoError(t, err)
	t.Cleanup(admin.Close)

	identifier := pgx.Identifier{"test_" + uuid.New().String()}.Sanitize()
	_, err = admin.Exec(ctx, "CREATE SCHEMA "+identifier)
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, err := admin.Exec(cleanupCtx, "DROP SCHEMA "+identifier+" CASCADE")
		assert.NoError(t, err)
	})

	cfg, err := pgxpool.ParseConfig(url)
	require.NoError(t, err)
	cfg.ConnConfig.RuntimeParams["search_path"] = identifier
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	require.NoError(t, postgres.Migrate(ctx, pool, "up", slog.New(slog.NewTextHandler(io.Discard, nil))))

	return pool, triprepo.NewTripRepository(pool, 3*time.Second), transaction.NewTxManager(pool, 3*time.Second)
}

func value() trip.Trip {
	return trip.Trip{
		ID:        uuid.New(),
		UserID:    uuid.New(),
		DriverID:  uuid.New(),
		Status:    trip.StatusActive,
		Price:     100,
		StartedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

func historyCount(t *testing.T, pool *pgxpool.Pool, id uuid.UUID) int {
	t.Helper()
	var count int
	err := pool.QueryRow(context.Background(), "SELECT count(*) FROM trip_status_history WHERE trip_id=$1", id).Scan(&count)
	require.NoError(t, err)
	return count
}

func TestRepositoryLifecycle(t *testing.T) {
	pool, repo, tx := database(t)
	ctx := context.Background()
	original := value()

	err := tx.Do(ctx, func(ctx context.Context) error {
		if err := repo.Create(ctx, original); err != nil {
			return err
		}

		return repo.AddStatusChange(ctx, trip.StatusChange{
			TripID:    original.ID,
			To:        trip.StatusActive,
			Reason:    "created",
			ChangedAt: original.StartedAt,
		})
	})
	require.NoError(t, err)

	got, err := repo.Get(ctx, original.ID)
	require.NoError(t, err)
	// PostgreSQL preserves the instant; pgx may use the local time zone.
	got.StartedAt = got.StartedAt.UTC()
	assert.Equal(t, original, got)

	busy := value()
	busy.DriverID = original.DriverID
	require.ErrorIs(t, repo.Create(ctx, busy), trip.ErrDriverBusy)

	at := original.StartedAt.Add(time.Minute)
	finished, err := repo.FinishActive(ctx, original.ID, at)
	require.NoError(t, err)
	require.NotNil(t, finished.Trip.FinishedAt)
	finished.Trip.StartedAt = finished.Trip.StartedAt.UTC()
	finishedAt := finished.Trip.FinishedAt.UTC()
	finished.Trip.FinishedAt = &finishedAt
	wantFinished := original
	wantFinished.Status = trip.StatusCompleted
	wantFinished.FinishedAt = &at
	assert.Equal(t, model.FinishTripResult{Trip: wantFinished, Updated: true}, finished)

	repeated, err := repo.FinishActive(ctx, original.ID, at)
	require.NoError(t, err)
	assert.Empty(t, repeated)

	_, err = repo.Get(ctx, uuid.New())
	require.ErrorIs(t, err, trip.ErrTripNotFound)
	require.NoError(t, repo.Create(ctx, busy), "driver must be released")
	assert.Equal(t, 1, historyCount(t, pool, original.ID))

	active, err := repo.CountActive(ctx)
	require.NoError(t, err)
	assert.EqualValues(t, 1, active)
}

func TestTransactionRollsBackBothTables(t *testing.T) {
	pool, repo, tx := database(t)
	ctx := context.Background()
	original := value()

	err := tx.Do(ctx, func(ctx context.Context) error {
		if err := repo.Create(ctx, original); err != nil {
			return err
		}

		if err := repo.AddStatusChange(ctx, trip.StatusChange{
			TripID: original.ID, To: trip.StatusActive, ChangedAt: original.StartedAt,
		}); err != nil {
			return err
		}

		// A real CHECK violation aborts the transaction after both INSERTs.
		return repo.AddStatusChange(ctx, trip.StatusChange{
			TripID: original.ID, To: "invalid", ChangedAt: original.StartedAt,
		})
	})

	require.Error(t, err)
	_, err = repo.Get(ctx, original.ID)
	require.ErrorIs(t, err, trip.ErrTripNotFound)
	assert.Zero(t, historyCount(t, pool, original.ID))
}

func TestTransactionCancellation(t *testing.T) {
	_, repo, tx := database(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	original := value()

	err := tx.Do(ctx, func(ctx context.Context) error {
		if err := repo.Create(ctx, original); err != nil {
			return err
		}

		cancel()
		return ctx.Err()
	})

	require.ErrorIs(t, err, context.Canceled)
	_, err = repo.Get(context.Background(), original.ID)
	require.ErrorIs(t, err, trip.ErrTripNotFound)
}

func TestTransactionPanic(t *testing.T) {
	_, repo, tx := database(t)
	ctx := context.Background()
	original := value()

	require.PanicsWithValue(t, "test panic", func() {
		_ = tx.Do(ctx, func(ctx context.Context) error {
			if err := repo.Create(ctx, original); err != nil {
				return err
			}

			panic("test panic")
		})
	})

	_, err := repo.Get(ctx, original.ID)
	require.ErrorIs(t, err, trip.ErrTripNotFound)
}
