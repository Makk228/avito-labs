//go:generate go run go.uber.org/mock/mockgen -source contract.go -destination mocks_test.go -package create_test

package create

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/Makk228/avito-labs/internal/model"
)

type tripRepository interface {
	Create(context.Context, model.Trip) error
	Get(context.Context, uuid.UUID) (model.Trip, error)
	AddStatusChange(context.Context, model.StatusChange) error
}

type txManager interface {
	Do(context.Context, func(context.Context) error) error
}

type idempotencyStore interface {
	Reserve(context.Context, uuid.UUID, string, time.Time, time.Time) (bool, error)
	Get(context.Context, uuid.UUID) (model.IdempotencyRecord, error)
	Bind(context.Context, uuid.UUID, uuid.UUID) error
}

type metrics interface{ Created(context.Context) }
