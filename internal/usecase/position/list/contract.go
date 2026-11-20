//go:generate go run go.uber.org/mock/mockgen -source contract.go -destination mocks_test.go -package list_test

package list

import (
	"context"

	"github.com/google/uuid"

	"github.com/Makk228/avito-labs/internal/model"
)

type tripRepository interface {
	Get(context.Context, uuid.UUID) (model.Trip, error)
}
type positionRepository interface {
	List(context.Context, uuid.UUID) ([]model.Position, error)
}
