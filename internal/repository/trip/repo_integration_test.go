//go:build integration

package trip_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	pgcontainer "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/Makk228/avito-labs/internal/domain/trip"
	"github.com/Makk228/avito-labs/internal/infra/postgres"
	"github.com/Makk228/avito-labs/internal/infra/postgres/transaction"
	triprepo "github.com/Makk228/avito-labs/internal/repository/trip"
)

// Every test uses a separate schema with the existing migrations, including its
// own goose version table. No shared service data is read, truncated or deleted.
func database(t *testing.T) (*pgxpool.Pool, *triprepo.TripRepository, *transaction.TxManager) {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		container, err := pgcontainer.Run(ctx, "postgres:17-alpine", pgcontainer.WithDatabase("tripgo"), pgcontainer.WithUsername("tripgo"), pgcontainer.WithPassword("tripgo"), pgcontainer.BasicWaitStrategies())
		if err != nil {
			t.Fatal(err)
		}
		testcontainers.CleanupContainer(t, container)
		url, err = container.ConnectionString(ctx, "sslmode=disable")
		if err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	schema := "test_" + uuid.New().String()
	identifier := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+identifier); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		defer admin.Close()
		if _, err := admin.Exec(cleanupCtx, "DROP SCHEMA "+identifier+" CASCADE"); err != nil {
			t.Error(err)
		}
	})
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = identifier
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := postgres.Migrate(ctx, pool, "up", slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatal(err)
	}
	return pool, triprepo.NewTripRepository(pool, 3*time.Second), transaction.NewTxManager(pool, 3*time.Second)
}

func value() trip.Trip {
	return trip.Trip{ID: uuid.New(), UserID: uuid.New(), DriverID: uuid.New(), Status: trip.StatusActive, Price: 100, StartedAt: time.Now().UTC().Truncate(time.Microsecond)}
}

func TestRepositoryLifecycle(t *testing.T) {
	pool, repo, tx := database(t)
	ctx := context.Background()
	original := value()
	if err := tx.Do(ctx, func(ctx context.Context) error {
		if err := repo.Create(ctx, original); err != nil {
			return err
		}
		return repo.AddStatusChange(ctx, trip.StatusChange{TripID: original.ID, To: trip.StatusActive, Reason: "created", ChangedAt: original.StartedAt})
	}); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Get(ctx, original.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != original.ID || got.UserID != original.UserID || got.Price != 100 || got.Status != trip.StatusActive || !got.StartedAt.Equal(original.StartedAt) || got.FinishedAt != nil {
		t.Fatalf("wrong fetched trip: %+v", got)
	}
	busy := value()
	busy.DriverID = original.DriverID
	if err := repo.Create(ctx, busy); !errors.Is(err, trip.ErrDriverBusy) {
		t.Fatalf("want busy, got %v", err)
	}
	at := time.Now().UTC().Truncate(time.Microsecond)
	finished, ok, err := repo.FinishActive(ctx, original.ID, at)
	if err != nil || !ok || finished.Status != trip.StatusCompleted || finished.FinishedAt == nil || !finished.FinishedAt.Equal(at) {
		t.Fatalf("bad finish: %+v %v %v", finished, ok, err)
	}
	if _, ok, err := repo.FinishActive(ctx, original.ID, at); err != nil || ok {
		t.Fatalf("second update: %v %v", ok, err)
	}
	if _, err := repo.Get(ctx, uuid.New()); !errors.Is(err, trip.ErrTripNotFound) {
		t.Fatalf("want missing, got %v", err)
	}
	if err := repo.Create(ctx, busy); err != nil {
		t.Fatalf("driver must be released: %v", err)
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM trip_status_history WHERE trip_id=$1", original.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("history count %d: %v", count, err)
	}
	active, err := repo.CountActive(ctx)
	if err != nil || active != 1 {
		t.Fatalf("active=%d: %v", active, err)
	}
}

func TestTransactionRollsBackBothTables(t *testing.T) {
	pool, repo, tx := database(t)
	ctx := context.Background()
	original := value()
	err := tx.Do(ctx, func(ctx context.Context) error {
		if err := repo.Create(ctx, original); err != nil {
			return err
		}
		if err := repo.AddStatusChange(ctx, trip.StatusChange{TripID: original.ID, To: trip.StatusActive, ChangedAt: original.StartedAt}); err != nil {
			return err
		}
		// A real CHECK violation aborts the transaction after both INSERTs.
		return repo.AddStatusChange(ctx, trip.StatusChange{TripID: original.ID, To: "invalid", ChangedAt: original.StartedAt})
	})
	if err == nil {
		t.Fatal("expected PostgreSQL constraint violation")
	}
	if _, err := repo.Get(ctx, original.ID); !errors.Is(err, trip.ErrTripNotFound) {
		t.Fatalf("trip survived rollback: %v", err)
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM trip_status_history WHERE trip_id=$1", original.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("history survived rollback: count=%d err=%v", count, err)
	}
}

func TestTransactionCancellationAndPanic(t *testing.T) {
	const panicMode = "panic"
	_, repo, tx := database(t)
	for _, mode := range []string{"canceled", panicMode} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			original := value()
			func() {
				defer func() {
					if recovered := recover(); mode == panicMode && recovered != "test panic" {
						t.Fatalf("panic=%v", recovered)
					}
				}()
				err := tx.Do(ctx, func(ctx context.Context) error {
					if err := repo.Create(ctx, original); err != nil {
						return err
					}
					if mode == panicMode {
						panic("test panic")
					}
					cancel()
					return ctx.Err()
				})
				if mode == "canceled" && !errors.Is(err, context.Canceled) {
					t.Fatalf("error=%v", err)
				}
			}()
			if _, err := repo.Get(context.Background(), original.ID); !errors.Is(err, trip.ErrTripNotFound) {
				t.Fatalf("trip survived: %v", err)
			}
		})
	}
}
