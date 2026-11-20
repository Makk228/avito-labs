package trip

import (
	"context"
	"errors"
	"fmt"

	"github.com/Masterminds/squirrel"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Makk228/avito-labs/internal/domain/trip"
	"github.com/Makk228/avito-labs/internal/infra/postgres/transaction"
)

func (r *TripRepository) Get(ctx context.Context, id uuid.UUID) (trip.Trip, error) {
	return r.get(ctx, id, false)
}

func (r *TripRepository) GetForUpdate(ctx context.Context, id uuid.UUID) (trip.Trip, error) {
	return r.get(ctx, id, true)
}

func (r *TripRepository) get(ctx context.Context, id uuid.UUID, lock bool) (trip.Trip, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	builder := psql.Select(tripColumns...).
		From("trips").
		Where(squirrel.Eq{"id": id})
	if lock {
		builder = builder.Suffix("FOR UPDATE")
	}

	query, args, err := builder.ToSql()
	if err != nil {
		return trip.Trip{}, fmt.Errorf("build select trip query: %w", err)
	}

	t, err := scanTrip(transaction.Conn(ctx, r.pool).QueryRow(ctx, query, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return trip.Trip{}, trip.ErrTripNotFound
	}

	if err != nil {
		return trip.Trip{}, fmt.Errorf("select trip: %w", err)
	}

	return t, nil
}

func (r *TripRepository) CountActive(ctx context.Context) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var count int64
	if err := transaction.Conn(ctx, r.pool).QueryRow(ctx, "SELECT count(*) FROM trips WHERE status = $1", trip.StatusActive).Scan(&count); err != nil {
		return 0, fmt.Errorf("count active trips: %w", err)
	}

	return count, nil
}
