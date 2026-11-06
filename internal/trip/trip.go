package trip

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
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

var (
	ErrTripNotFound        = errors.New("trip not found")
	ErrTripCompleted       = errors.New("trip is already completed")
	ErrDriverBusy          = errors.New("driver already has an active trip")
	ErrIdempotencyConflict = errors.New("idempotency key was already used with a different request")
)

type Point struct {
	Latitude  float64
	Longitude float64
}

type Trip struct {
	ID         uuid.UUID
	UserID     uuid.UUID
	DriverID   uuid.UUID
	Start      Point
	End        Point
	Price      int64
	Status     Status
	StartedAt  time.Time
	FinishedAt *time.Time
}

type NewTrip struct {
	UserID   uuid.UUID
	DriverID uuid.UUID
	Start    Point
	End      Point
	Price    int64
}

func (n NewTrip) fingerprint() string {
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
