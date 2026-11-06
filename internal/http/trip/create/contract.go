//go:generate go run go.uber.org/mock/mockgen -source contract.go -destination mocks_test.go -package create_test

package create

import (
	"context"

	"github.com/google/uuid"

	"github.com/Makk228/avito-labs/internal/model"
)

type usecase interface {
	Create(context.Context, model.NewTrip, *uuid.UUID) (model.Trip, bool, error)
}
