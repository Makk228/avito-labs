package model

import (
	"github.com/google/uuid"

	"github.com/Makk228/avito-labs/internal/domain/position"
)

type (
	Position    = position.Position
	NewPosition = position.NewPosition
)

type StaleTrip struct {
	ID       uuid.UUID
	DriverID uuid.UUID
}
