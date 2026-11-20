//go:generate go run go.uber.org/mock/mockgen -source contract.go -destination mocks_test.go -package save_test

package save

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/Makk228/avito-labs/internal/model"
)

type txManager interface {
	Do(context.Context, func(context.Context) error) error
}
type tripRepository interface {
	GetForUpdate(context.Context, uuid.UUID) (model.Trip, error)
	AdvanceLastPosition(context.Context, uuid.UUID, time.Time) error
}
type positionRepository interface {
	Save(context.Context, model.NewPosition) (model.Position, error)
}
type (
	queue   interface{ Cancel(uuid.UUID) }
	clock   interface{ Now() time.Time }
	metrics interface{ Saved(context.Context) }
)
