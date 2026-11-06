package trip

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Masterminds/squirrel"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Makk228/avito-labs/internal/domain/trip"
	"github.com/Makk228/avito-labs/internal/infra/postgres/transaction"
)

func (r *TripRepository) Create(ctx context.Context, t trip.Trip) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	query, args, err := psql.Insert("trips").
		Columns(tripColumns...).
		Values(
			t.ID,
			t.UserID,
			t.DriverID,
			t.Start.Latitude,
			t.Start.Longitude,
			t.End.Latitude,
			t.End.Longitude,
			t.Price,
			string(t.Status),
			t.StartedAt,
			t.FinishedAt,
		).
		ToSql()
	if err != nil {
		return fmt.Errorf("build insert trip query: %w", err)
	}

	if _, err := transaction.Conn(ctx, r.pool).Exec(ctx, query, args...); err != nil {
		if isUniqueViolation(err, driverActiveTripIndex) {
			return trip.ErrDriverBusy
		}
		return fmt.Errorf("insert trip: %w", err)
	}
	return nil
}

func (r *TripRepository) FinishActive(ctx context.Context, id uuid.UUID, finishedAt time.Time) (trip.Trip, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	query, args, err := psql.Update("trips").
		Set("status", string(trip.StatusCompleted)).
		Set("finished_at", finishedAt).
		Set("updated_at", finishedAt).
		Where(squirrel.Eq{"id": id, "status": string(trip.StatusActive)}).
		Suffix("RETURNING " + strings.Join(tripColumns, ", ")).
		ToSql()
	if err != nil {
		return trip.Trip{}, false, fmt.Errorf("build finish trip query: %w", err)
	}

	t, err := scanTrip(transaction.Conn(ctx, r.pool).QueryRow(ctx, query, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return trip.Trip{}, false, nil
	}
	if err != nil {
		return trip.Trip{}, false, fmt.Errorf("finish trip: %w", err)
	}
	return t, true, nil
}

func (r *TripRepository) AddStatusChange(ctx context.Context, c trip.StatusChange) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	var from *string
	if c.From != nil {
		s := string(*c.From)
		from = &s
	}

	query, args, err := psql.Insert("trip_status_history").
		Columns("trip_id", "from_status", "to_status", "reason", "changed_at").
		Values(c.TripID, from, string(c.To), c.Reason, c.ChangedAt).
		ToSql()
	if err != nil {
		return fmt.Errorf("build insert status change query: %w", err)
	}

	if _, err := transaction.Conn(ctx, r.pool).Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("insert status change: %w", err)
	}
	return nil
}
