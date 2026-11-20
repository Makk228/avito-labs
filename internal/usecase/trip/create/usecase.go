// Package create creates trips and their initial status in one transaction.
package create

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/Makk228/avito-labs/internal/domain/trip"
	"github.com/Makk228/avito-labs/internal/model"
)

type Dependencies struct {
	Transactions txManager
	Trips        tripRepository
	Idempotency  idempotencyStore
	Clock        clock
	IDs          idGenerator
	Logger       logger
	Metrics      metrics
}

type Config struct {
	KeyTTL time.Duration
}

type Usecase struct {
	tx      txManager
	trips   tripRepository
	keys    idempotencyStore
	clock   clock
	ids     idGenerator
	log     logger
	metrics metrics
	keyTTL  time.Duration
}

func New(deps Dependencies, cfg Config) *Usecase {
	return &Usecase{
		tx:      deps.Transactions,
		trips:   deps.Trips,
		keys:    deps.Idempotency,
		clock:   deps.Clock,
		ids:     deps.IDs,
		log:     deps.Logger,
		metrics: deps.Metrics,
		keyTTL:  cfg.KeyTTL,
	}
}

func (u *Usecase) Create(ctx context.Context, in model.NewTrip, key *uuid.UUID) (model.CreateTripResult, error) {
	if err := in.Validate(); err != nil {
		return model.CreateTripResult{}, fmt.Errorf("validate new trip: %w", err)
	}

	var result model.CreateTripResult
	err := u.tx.Do(ctx, func(txCtx context.Context) error {
		if key != nil {
			existing, err := u.lookupKey(txCtx, *key, in)
			if err != nil {
				return err
			}

			if existing != nil {
				result.Trip = *existing
				return nil
			}
		}

		value := model.Trip{
			ID:        u.ids.NewID(),
			UserID:    in.UserID,
			DriverID:  in.DriverID,
			Start:     in.Start,
			End:       in.End,
			Price:     in.Price,
			Status:    trip.StatusActive,
			StartedAt: u.clock.Now(),
		}

		if err := u.trips.Create(txCtx, value); err != nil {
			return fmt.Errorf("save trip: %w", err)
		}

		history := model.StatusChange{
			TripID:    value.ID,
			To:        trip.StatusActive,
			Reason:    "trip created",
			ChangedAt: value.StartedAt,
		}

		if err := u.trips.AddStatusChange(txCtx, history); err != nil {
			return fmt.Errorf("save initial status: %w", err)
		}

		if key != nil {
			if err := u.keys.Bind(txCtx, *key, value.ID); err != nil {
				return fmt.Errorf("bind idempotency key: %w", err)
			}
		}

		result = model.CreateTripResult{Trip: value, Created: true}
		return nil
	})
	if err != nil {
		return model.CreateTripResult{}, fmt.Errorf("create trip transaction: %w", err)
	}

	if result.Created {
		u.metrics.Created(ctx)
		u.log.InfoContext(ctx, "trip created", "trip_id", result.Trip.ID.String())
	}

	return result, nil
}

func (u *Usecase) lookupKey(ctx context.Context, key uuid.UUID, in model.NewTrip) (*model.Trip, error) {
	hash := in.Fingerprint()
	createdAt := u.clock.Now()

	reserved, err := u.keys.Reserve(ctx, key, hash, createdAt, createdAt.Add(u.keyTTL))
	if err != nil {
		return nil, fmt.Errorf("reserve idempotency key: %w", err)
	}

	if reserved {
		return nil, nil
	}

	record, err := u.keys.Get(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("fetch idempotency key: %w", err)
	}

	if record.RequestHash != hash {
		return nil, trip.ErrIdempotencyConflict
	}

	value, err := u.trips.Get(ctx, record.TripID)
	if err != nil {
		return nil, fmt.Errorf("fetch saved idempotent trip: %w", err)
	}

	return &value, nil
}
