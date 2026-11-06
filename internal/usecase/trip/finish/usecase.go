// Package finish completes an active trip and records its transition atomically.
package finish

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/Makk228/avito-labs/internal/domain/trip"
	"github.com/Makk228/avito-labs/internal/model"
)

type Usecase struct {
	tx      txManager
	trips   tripRepository
	log     *slog.Logger
	metrics metrics
}

func New(tx txManager, trips tripRepository, log *slog.Logger, counters metrics) *Usecase {
	return &Usecase{tx: tx, trips: trips, log: log, metrics: counters}
}

func (u *Usecase) Finish(ctx context.Context, id uuid.UUID) (model.Trip, error) {
	var result model.Trip
	err := u.tx.Do(ctx, func(ctx context.Context) error {
		t, err := u.trips.Get(ctx, id)
		if err != nil {
			return fmt.Errorf("fetch trip: %w", err)
		}
		at := time.Now().UTC().Truncate(time.Microsecond)
		if err := t.Finish(at); err != nil {
			return err
		}
		updated, ok, err := u.trips.FinishActive(ctx, id, at)
		if err != nil {
			return fmt.Errorf("persist completion: %w", err)
		}
		if !ok {
			return trip.ErrTripCompleted
		}
		from := trip.StatusActive
		if err := u.trips.AddStatusChange(ctx, model.StatusChange{TripID: id, From: &from, To: t.Status, Reason: "trip finished", ChangedAt: at}); err != nil {
			return fmt.Errorf("record completion: %w", err)
		}
		result = updated
		return nil
	})
	if err != nil {
		return model.Trip{}, fmt.Errorf("finish trip transaction: %w", err)
	}
	u.metrics.Completed(ctx)
	u.log.InfoContext(ctx, "trip finished", slog.String("trip_id", id.String()))
	return result, nil
}
