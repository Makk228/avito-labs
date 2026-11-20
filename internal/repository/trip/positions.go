package trip

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/Makk228/avito-labs/internal/infra/postgres/transaction"
	"github.com/Makk228/avito-labs/internal/model"
)

// Staleness and ordering use the same expression as the partial covering index.
// The strict comparison excludes a trip exactly on the staleness boundary.
const staleTripsQuery = `
SELECT id, driver_id
FROM trips
WHERE status = 'active'
  AND COALESCE(last_position_at, started_at) <
      $1::timestamptz - make_interval(secs => $2::double precision)
ORDER BY COALESCE(last_position_at, started_at), id`

func (r *TripRepository) Stale(ctx context.Context, now time.Time, staleAfter time.Duration) ([]model.StaleTrip, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	rows, err := transaction.Conn(ctx, r.pool).Query(ctx, staleTripsQuery, now, staleAfter.Seconds())
	if err != nil {
		return nil, fmt.Errorf("select stale trips: %w", err)
	}
	defer rows.Close()

	values := make([]model.StaleTrip, 0)
	for rows.Next() {
		var value model.StaleTrip
		if err := rows.Scan(&value.ID, &value.DriverID); err != nil {
			return nil, fmt.Errorf("scan stale trip: %w", err)
		}

		values = append(values, value)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read stale trips: %w", err)
	}

	return values, nil
}

func (r *TripRepository) AdvanceLastPosition(ctx context.Context, id uuid.UUID, at time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	_, err := transaction.Conn(ctx, r.pool).Exec(ctx, `
        UPDATE trips
        SET last_position_at = GREATEST(last_position_at, $2), updated_at = now()
        WHERE id = $1`, id, at)
	if err != nil {
		return fmt.Errorf("advance last position: %w", err)
	}

	return nil
}
