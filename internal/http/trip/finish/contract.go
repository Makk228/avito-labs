//go:generate go run go.uber.org/mock/mockgen -source contract.go -destination mocks_test.go -package finish_test

package finish

import (
	"context"

	"github.com/google/uuid"

	"github.com/Makk228/avito-labs/internal/model"
)

type usecase interface {
	Finish(context.Context, uuid.UUID) (model.Trip, error)
}
