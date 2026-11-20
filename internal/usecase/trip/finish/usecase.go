// Package finish completes an active trip and records its transition atomically.
package finish

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/Makk228/avito-labs/internal/domain/trip"
	"github.com/Makk228/avito-labs/internal/model"
)

type Dependencies struct {
	Transactions txManager
	Trips        tripRepository
	Clock        clock
	Logger       logger
	Metrics      metrics
}

type Usecase struct {
	tx      txManager
	trips   tripRepository
	clock   clock
	log     logger
	metrics metrics
}

func New(deps Dependencies) *Usecase {
	return &Usecase{
		tx:      deps.Transactions,
		trips:   deps.Trips,
		clock:   deps.Clock,
		log:     deps.Logger,
		metrics: deps.Metrics,
	}
}

func (u *Usecase) Finish(ctx context.Context, id uuid.UUID) (model.Trip, error) {
	var result model.Trip
	err := u.tx.Do(ctx, func(txCtx context.Context) error {
		value, err := u.trips.Get(txCtx, id)
		if err != nil {
			return fmt.Errorf("fetch trip: %w", err)
		}

		at := u.clock.Now()
		if err := value.Finish(at); err != nil {
			return err
		}

		updated, err := u.trips.FinishActive(txCtx, id, at)
		if err != nil {
			return fmt.Errorf("persist completion: %w", err)
		}

		if !updated.Updated {
			return trip.ErrTripCompleted
		}

		from := trip.StatusActive
		history := model.StatusChange{
			TripID:    id,
			From:      &from,
			To:        value.Status,
			Reason:    "trip finished",
			ChangedAt: at,
		}

		if err := u.trips.AddStatusChange(txCtx, history); err != nil {
			return fmt.Errorf("record completion: %w", err)
		}

		result = updated.Trip
		return nil
	})
	if err != nil {
		return model.Trip{}, fmt.Errorf("finish trip transaction: %w", err)
	}

	u.metrics.Completed(ctx)
	u.log.InfoContext(ctx, "trip finished", "trip_id", id.String())

	return result, nil
}
