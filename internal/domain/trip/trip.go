package trip

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Status string

const (
	StatusActive    Status = "active"
	StatusCompleted Status = "completed"
)

type Point struct {
	Latitude  float64 `json:"latitude" validate:"gte=-90,lte=90"`
	Longitude float64 `json:"longitude" validate:"gte=-180,lte=180"`
}

type Trip struct {
	ID             uuid.UUID
	UserID         uuid.UUID
	DriverID       uuid.UUID
	Start          Point
	End            Point
	Price          int64
	Status         Status
	StartedAt      time.Time
	FinishedAt     *time.Time
	LastPositionAt *time.Time
}

type NewTrip struct {
	UserID   uuid.UUID `json:"user_id" validate:"required"`
	DriverID uuid.UUID `json:"driver_id" validate:"required"`
	Start    Point     `json:"start_point"`
	End      Point     `json:"end_point"`
	Price    int64     `json:"price" validate:"gte=0"`
}

func (n NewTrip) Fingerprint() string {
	parts := []string{
		n.UserID.String(),
		n.DriverID.String(),
		formatFloat(n.Start.Latitude),
		formatFloat(n.Start.Longitude),
		formatFloat(n.End.Latitude),
		formatFloat(n.End.Longitude),
		strconv.FormatInt(n.Price, 10),
	}

	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(sum[:])
}

func formatFloat(v float64) string {
	return strconv.FormatFloat(v, 'g', -1, 64)
}

type StatusChange struct {
	TripID    uuid.UUID
	From      *Status
	To        Status
	Reason    string
	ChangedAt time.Time
}

type IdempotencyRecord struct {
	RequestHash string
	TripID      uuid.UUID
}

// Finish applies the only permitted completion transition.
func (t *Trip) Finish(at time.Time) error {
	if t.Status != StatusActive {
		return ErrTripCompleted
	}

	t.Status = StatusCompleted
	t.FinishedAt = &at
	return nil
}
