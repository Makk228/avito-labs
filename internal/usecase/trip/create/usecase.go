// Package create creates trips and their initial status in one transaction.
package create

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/Makk228/avito-labs/internal/domain/trip"
)

type Usecase struct {
	tx      txManager
	trips   tripRepository
	keys    idempotencyStore
	keyTTL  time.Duration
	now     func() time.Time
	log     *slog.Logger
	metrics metrics
}

func New(tx txManager, trips tripRepository, keys idempotencyStore, keyTTL time.Duration, log *slog.Logger, counters metrics) *Usecase {
	return &Usecase{tx: tx, trips: trips, keys: keys, keyTTL: keyTTL, now: now, log: log, metrics: counters}
}

func now() time.Time { return time.Now().UTC().Truncate(time.Microsecond) }
func (s *Usecase) Create(ctx context.Context, in trip.NewTrip, key *uuid.UUID) (t trip.Trip, replayed bool, err error) {
	if err := in.Validate(); err != nil {
		return trip.Trip{}, false, fmt.Errorf("create trip: %w", err)
	}
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		if key != nil {
			prev, found, err := s.lookupKey(ctx, *key, in)
			if err != nil {
				return fmt.Errorf("create trip operation: %w", err)
			}
			if found {
				t, replayed = prev, true
				return nil
			}
		}

		startedAt := s.now()
		t = trip.Trip{
			ID:        uuid.New(),
			UserID:    in.UserID,
			DriverID:  in.DriverID,
			Start:     in.Start,
			End:       in.End,
			Price:     in.Price,
			Status:    trip.StatusActive,
			StartedAt: startedAt,
		}
		if err := s.trips.Create(ctx, t); err != nil {
			return fmt.Errorf("create trip operation: %w", err)
		}
		if err := s.trips.AddStatusChange(ctx, trip.StatusChange{
			TripID:    t.ID,
			To:        trip.StatusActive,
			Reason:    "trip created",
			ChangedAt: startedAt,
		}); err != nil {
			return fmt.Errorf("create trip operation: %w", err)
		}
		if key != nil {
			if err := s.keys.Bind(ctx, *key, t.ID); err != nil {
				return fmt.Errorf("create trip operation: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return trip.Trip{}, false, fmt.Errorf("create trip: %w", err)
	}
	if !replayed {
		s.metrics.Created(ctx)
		s.log.InfoContext(ctx, "trip created", slog.String("trip_id", t.ID.String()))
	}
	return t, replayed, nil
}

func (s *Usecase) lookupKey(ctx context.Context, key uuid.UUID, in trip.NewTrip) (trip.Trip, bool, error) {
	hash := in.Fingerprint()
	createdAt := s.now()

	reserved, err := s.keys.Reserve(ctx, key, hash, createdAt, createdAt.Add(s.keyTTL))
	if err != nil {
		return trip.Trip{}, false, fmt.Errorf("create trip: %w", err)
	}
	if reserved {
		return trip.Trip{}, false, nil
	}

	rec, err := s.keys.Get(ctx, key)
	if err != nil {
		return trip.Trip{}, false, fmt.Errorf("create trip: %w", err)
	}
	if rec.RequestHash != hash {
		return trip.Trip{}, false, trip.ErrIdempotencyConflict
	}
	t, err := s.trips.Get(ctx, rec.TripID)
	if err != nil {
		return trip.Trip{}, false, fmt.Errorf("get trip by idempotency key: %w", err)
	}
	return t, true, nil
}
