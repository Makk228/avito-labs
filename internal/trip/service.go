package trip

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
)

type TxManager interface {
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}

type Repository interface {
	Create(ctx context.Context, t Trip) error
	Get(ctx context.Context, id uuid.UUID) (Trip, error)
	FinishActive(ctx context.Context, id uuid.UUID, finishedAt time.Time) (t Trip, ok bool, err error)
	AddStatusChange(ctx context.Context, c StatusChange) error
}

type IdempotencyStore interface {
	Reserve(ctx context.Context, key uuid.UUID, requestHash string, createdAt, expiresAt time.Time) (reserved bool, err error)
	Get(ctx context.Context, key uuid.UUID) (IdempotencyRecord, error)
	Bind(ctx context.Context, key, tripID uuid.UUID) error
	DeleteExpired(ctx context.Context, now time.Time) (int64, error)
}

type Service struct {
	tx     TxManager
	trips  Repository
	keys   IdempotencyStore
	keyTTL time.Duration
	now    func() time.Time
}

func NewService(tx TxManager, trips Repository, keys IdempotencyStore, keyTTL time.Duration) *Service {
	return &Service{
		tx:     tx,
		trips:  trips,
		keys:   keys,
		keyTTL: keyTTL,
		now:    now,
	}
}

func now() time.Time {
	return time.Now().UTC().Truncate(time.Microsecond)
}

func (s *Service) Create(ctx context.Context, in NewTrip, key *uuid.UUID) (t Trip, replayed bool, err error) {
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		if key != nil {
			prev, found, err := s.lookupKey(ctx, *key, in)
			if err != nil {
				return err
			}
			if found {
				t, replayed = prev, true
				return nil
			}
		}

		startedAt := s.now()
		t = Trip{
			ID:        uuid.New(),
			UserID:    in.UserID,
			DriverID:  in.DriverID,
			Start:     in.Start,
			End:       in.End,
			Price:     in.Price,
			Status:    StatusActive,
			StartedAt: startedAt,
		}
		if err := s.trips.Create(ctx, t); err != nil {
			return err
		}
		if err := s.trips.AddStatusChange(ctx, StatusChange{
			TripID:    t.ID,
			To:        StatusActive,
			Reason:    "trip created",
			ChangedAt: startedAt,
		}); err != nil {
			return err
		}
		if key != nil {
			if err := s.keys.Bind(ctx, *key, t.ID); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return Trip{}, false, err
	}
	return t, replayed, nil
}

func (s *Service) lookupKey(ctx context.Context, key uuid.UUID, in NewTrip) (Trip, bool, error) {
	hash := in.fingerprint()
	createdAt := s.now()

	reserved, err := s.keys.Reserve(ctx, key, hash, createdAt, createdAt.Add(s.keyTTL))
	if err != nil {
		return Trip{}, false, err
	}
	if reserved {
		return Trip{}, false, nil
	}

	rec, err := s.keys.Get(ctx, key)
	if err != nil {
		return Trip{}, false, err
	}
	if rec.RequestHash != hash {
		return Trip{}, false, ErrIdempotencyConflict
	}
	t, err := s.trips.Get(ctx, rec.TripID)
	if err != nil {
		return Trip{}, false, fmt.Errorf("get trip by idempotency key: %w", err)
	}
	return t, true, nil
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (Trip, error) {
	return s.trips.Get(ctx, id)
}

func (s *Service) Finish(ctx context.Context, id uuid.UUID) (Trip, error) {
	var t Trip
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		finishedAt := s.now()

		finished, ok, err := s.trips.FinishActive(ctx, id, finishedAt)
		if err != nil {
			return err
		}
		if !ok {
			if _, err := s.trips.Get(ctx, id); err != nil {
				return err
			}
			return ErrTripCompleted
		}

		from := StatusActive
		if err := s.trips.AddStatusChange(ctx, StatusChange{
			TripID:    id,
			From:      &from,
			To:        StatusCompleted,
			Reason:    "trip finished",
			ChangedAt: finishedAt,
		}); err != nil {
			return err
		}
		t = finished
		return nil
	})
	if err != nil {
		return Trip{}, err
	}
	return t, nil
}

func (s *Service) RunKeyCleanup(ctx context.Context, interval time.Duration, log *slog.Logger) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			deleted, err := s.keys.DeleteExpired(ctx, s.now())
			switch {
			case errors.Is(err, context.Canceled):
				return
			case err != nil:
				log.Warn("idempotency keys cleanup failed", slog.Any("error", err))
			case deleted > 0:
				log.Debug("expired idempotency keys deleted", slog.Int64("count", deleted))
			}
		}
	}
}
