// Package trip stores trips and their status history in PostgreSQL.
package trip

import (
	"errors"
	"time"

	"github.com/Masterminds/squirrel"
	"github.com/jackc/pgx/v5/pgconn"
)

const driverActiveTripIndex = "trips_driver_active_uniq"

var tripColumns = []string{
	"id",
	"user_id",
	"driver_id",
	"start_latitude",
	"start_longitude",
	"end_latitude",
	"end_longitude",
	"price",
	"status",
	"started_at",
	"finished_at",
}

type TripRepository struct {
	pool    database
	timeout time.Duration
}

func NewTripRepository(pool database, timeout time.Duration) *TripRepository {
	return &TripRepository{pool: pool, timeout: timeout}
}

var psql = squirrel.StatementBuilder.PlaceholderFormat(squirrel.Dollar)

func isUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == constraint
}
