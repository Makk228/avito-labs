//go:generate go run go.uber.org/mock/mockgen -source contract.go -destination mocks_test.go -package save_test

package save

import (
	"context"

	"github.com/Makk228/avito-labs/internal/model"
)

type usecase interface {
	Save(context.Context, model.NewPosition) (model.Position, error)
}
