package keycleanup

import (
	"context"
	"time"
)

type store interface {
	DeleteExpired(context.Context, time.Time) (int64, error)
}
