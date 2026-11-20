//go:generate go run go.uber.org/mock/mockgen -source contract.go -destination mocks_test.go -package positions_test

package positions

import (
	"context"
	"time"

	"github.com/Makk228/avito-labs/internal/model"
)

type repository interface {
	Stale(context.Context, time.Time, time.Duration) ([]model.StaleTrip, error)
}
type gateway interface {
	RequestPosition(context.Context, model.StaleTrip) error
}
type (
	clock  interface{ Now() time.Time }
	ticker interface {
		Start(time.Duration) <-chan time.Time
		Stop()
	}
)

type logger interface {
	InfoContext(context.Context, string, ...any)
	WarnContext(context.Context, string, ...any)
}
type metrics interface {
	StaleCheck(context.Context, string)
	QueueSize(context.Context, int64)
	ActiveWorkers(context.Context, int64)
	Request(context.Context, string, time.Duration)
}
