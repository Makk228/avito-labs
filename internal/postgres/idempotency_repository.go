package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Masterminds/squirrel"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Makk228/avito-labs/internal/trip"
)

type IdempotencyRepository struct {
	pool    *pgxpool.Pool
	timeout time.Duration
}

func NewIdempotencyRepository(pool *pgxpool.Pool, timeout time.Duration) *IdempotencyRepository {
	return &IdempotencyRepository{pool: pool, timeout: timeout}
}

func (r *IdempotencyRepository) Reserve(ctx context.Context, key uuid.UUID, requestHash string, createdAt, expiresAt time.Time) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	query, args, err := psql.Insert("idempotency_keys").
		Columns("idempotency_key", "request_hash", "created_at", "expires_at").
		Values(key, requestHash, createdAt, expiresAt).
		Suffix(`ON CONFLICT (idempotency_key) DO UPDATE
			SET request_hash = EXCLUDED.request_hash,
			    trip_id      = NULL,
			    created_at   = EXCLUDED.created_at,
			    expires_at   = EXCLUDED.expires_at
			WHERE idempotency_keys.expires_at <= EXCLUDED.created_at
			RETURNING idempotency_key`).
		ToSql()
	if err != nil {
		return false, fmt.Errorf("build reserve idempotency key query: %w", err)
	}

	var reserved uuid.UUID
	err = conn(ctx, r.pool).QueryRow(ctx, query, args...).Scan(&reserved)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("reserve idempotency key: %w", err)
	}
	return true, nil
}

func (r *IdempotencyRepository) Get(ctx context.Context, key uuid.UUID) (trip.IdempotencyRecord, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	query, args, err := psql.Select("request_hash", "trip_id").
		From("idempotency_keys").
		Where(squirrel.Eq{"idempotency_key": key}).
		ToSql()
	if err != nil {
		return trip.IdempotencyRecord{}, fmt.Errorf("build select idempotency key query: %w", err)
	}

	var (
		rec    trip.IdempotencyRecord
		tripID *uuid.UUID
	)
	if err := conn(ctx, r.pool).QueryRow(ctx, query, args...).Scan(&rec.RequestHash, &tripID); err != nil {
		return trip.IdempotencyRecord{}, fmt.Errorf("select idempotency key: %w", err)
	}
	if tripID == nil {
		return trip.IdempotencyRecord{}, errors.New("idempotency key is not bound to a trip")
	}
	rec.TripID = *tripID
	return rec, nil
}

func (r *IdempotencyRepository) Bind(ctx context.Context, key, tripID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	query, args, err := psql.Update("idempotency_keys").
		Set("trip_id", tripID).
		Where(squirrel.Eq{"idempotency_key": key}).
		ToSql()
	if err != nil {
		return fmt.Errorf("build bind idempotency key query: %w", err)
	}

	tag, err := conn(ctx, r.pool).Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("bind idempotency key: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("bind idempotency key: %d rows affected", tag.RowsAffected())
	}
	return nil
}

func (r *IdempotencyRepository) DeleteExpired(ctx context.Context, now time.Time) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	query, args, err := psql.Delete("idempotency_keys").
		Where(squirrel.LtOrEq{"expires_at": now}).
		ToSql()
	if err != nil {
		return 0, fmt.Errorf("build delete expired idempotency keys query: %w", err)
	}

	tag, err := conn(ctx, r.pool).Exec(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("delete expired idempotency keys: %w", err)
	}
	return tag.RowsAffected(), nil
}
