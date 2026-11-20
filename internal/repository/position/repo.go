// Package position stores and retrieves the recorded route of a trip.
package position

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/Makk228/avito-labs/internal/infra/postgres/transaction"
	"github.com/Makk228/avito-labs/internal/model"
)

type Repository struct {
	pool    database
	timeout time.Duration
}

func New(pool database, timeout time.Duration) *Repository {
	return &Repository{pool: pool, timeout: timeout}
}

func (r *Repository) Save(ctx context.Context, in model.NewPosition) (model.Position, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	var value model.Position
	err := transaction.Conn(ctx, r.pool).QueryRow(ctx, `
        INSERT INTO trip_positions (trip_id, latitude, longitude, recorded_at)
        VALUES ($1, $2, $3, $4)
        RETURNING id, trip_id, latitude, longitude, recorded_at`,
		in.TripID, in.Coordinates.Latitude, in.Coordinates.Longitude, in.RecordedAt,
	).Scan(&value.ID, &value.TripID, &value.Coordinates.Latitude, &value.Coordinates.Longitude, &value.RecordedAt)
	if err != nil {
		return model.Position{}, fmt.Errorf("insert trip position: %w", err)
	}

	return value, nil
}

func (r *Repository) List(ctx context.Context, id uuid.UUID) ([]model.Position, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	rows, err := transaction.Conn(ctx, r.pool).Query(ctx, `
        SELECT id, trip_id, latitude, longitude, recorded_at
        FROM trip_positions WHERE trip_id = $1 ORDER BY recorded_at, id`, id)
	if err != nil {
		return nil, fmt.Errorf("select route: %w", err)
	}
	defer rows.Close()

	values := make([]model.Position, 0)
	for rows.Next() {
		var value model.Position
		if err := rows.Scan(&value.ID, &value.TripID, &value.Coordinates.Latitude, &value.Coordinates.Longitude, &value.RecordedAt); err != nil {
			return nil, fmt.Errorf("scan route point: %w", err)
		}

		values = append(values, value)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read route: %w", err)
	}

	return values, nil
}
