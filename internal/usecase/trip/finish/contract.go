//go:generate go run go.uber.org/mock/mockgen -source contract.go -destination mocks_test.go -package finish_test

package finish

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/Makk228/avito-labs/internal/model"
)

type tripRepository interface {
	Get(context.Context, uuid.UUID) (model.Trip, error)
	FinishActive(context.Context, uuid.UUID, time.Time) (model.Trip, bool, error)
	AddStatusChange(context.Context, model.StatusChange) error
}

type txManager interface {
	Do(context.Context, func(context.Context) error) error
}

type metrics interface{ Completed(context.Context) }
