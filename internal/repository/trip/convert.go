package trip

import (
	"github.com/jackc/pgx/v5"

	"github.com/Makk228/avito-labs/internal/domain/trip"
)

func scanTrip(row pgx.Row) (trip.Trip, error) {
	var (
		t      trip.Trip
		status string
	)
	err := row.Scan(
		&t.ID,
		&t.UserID,
		&t.DriverID,
		&t.Start.Latitude,
		&t.Start.Longitude,
		&t.End.Latitude,
		&t.End.Longitude,
		&t.Price,
		&status,
		&t.StartedAt,
		&t.FinishedAt,
	)
	if err != nil {
		return trip.Trip{}, err
	}
	t.Status = trip.Status(status)
	return t, nil
}
