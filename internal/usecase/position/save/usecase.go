// Package save atomically records a coordinate and advances the trip watermark.
package save

import (
	"context"
	"fmt"

	"github.com/Makk228/avito-labs/internal/domain/trip"
	"github.com/Makk228/avito-labs/internal/model"
)

type Dependencies struct {
	Transactions txManager
	Trips        tripRepository
	Positions    positionRepository
	Queue        queue
	Clock        clock
	Metrics      metrics
}

type Usecase struct{ deps Dependencies }

func New(deps Dependencies) *Usecase { return &Usecase{deps: deps} }

func (u *Usecase) Save(ctx context.Context, in model.NewPosition) (model.Position, error) {
	if err := in.Validate(u.deps.Clock.Now()); err != nil {
		return model.Position{}, fmt.Errorf("validate position: %w", err)
	}

	var result model.Position
	err := u.deps.Transactions.Do(ctx, func(txCtx context.Context) error {
		// Lock serializes coordinate acceptance with a concurrent FinishActive.
		value, err := u.deps.Trips.GetForUpdate(txCtx, in.TripID)
		if err != nil {
			return fmt.Errorf("lock trip: %w", err)
		}

		if value.Status != trip.StatusActive {
			return trip.ErrTripCompleted
		}

		result, err = u.deps.Positions.Save(txCtx, in)
		if err != nil {
			return err
		}

		return u.deps.Trips.AdvanceLastPosition(txCtx, in.TripID, in.RecordedAt)
	})
	if err != nil {
		return model.Position{}, fmt.Errorf("save position transaction: %w", err)
	}

	// A rolled-back coordinate must not cancel pending work or count as saved.
	u.deps.Queue.Cancel(in.TripID)
	u.deps.Metrics.Saved(ctx)
	return result, nil
}
