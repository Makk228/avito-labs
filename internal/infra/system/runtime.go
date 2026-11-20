// Package system supplies the production clock and identifier source.
package system

import (
	"time"

	"github.com/google/uuid"
)

type Runtime struct{}

func (Runtime) Now() time.Time {
	return time.Now().UTC().Truncate(time.Microsecond)
}

func (Runtime) NewID() uuid.UUID {
	return uuid.New()
}
