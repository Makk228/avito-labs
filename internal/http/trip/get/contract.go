//go:generate go run go.uber.org/mock/mockgen -source contract.go -destination mocks_test.go -package get_test

package get

import (
	"context"

	"github.com/google/uuid"

	"github.com/Makk228/avito-labs/internal/model"
)

type usecase interface {
	Get(context.Context, uuid.UUID) (model.Trip, error)
}
