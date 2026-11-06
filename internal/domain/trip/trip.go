package trip

import (
	"crypto/sha256"
	"encoding/hex"
	"math"
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

// Validate checks the domain constraints, including non-finite coordinates.
func (n NewTrip) Validate() error {
	if n.UserID == uuid.Nil {
		return &ValidationError{Field: "user_id", Message: "must not be empty"}
	}
	if n.DriverID == uuid.Nil {
		return &ValidationError{Field: "driver_id", Message: "must not be empty"}
	}
	for _, p := range []struct {
		name  string
		point Point
	}{{"start_point", n.Start}, {"end_point", n.End}} {
		if math.IsNaN(p.point.Latitude) || math.IsInf(p.point.Latitude, 0) || p.point.Latitude < -90 || p.point.Latitude > 90 {
			return &ValidationError{Field: p.name + ".latitude", Message: "must be between -90 and 90"}
		}
		if math.IsNaN(p.point.Longitude) || math.IsInf(p.point.Longitude, 0) || p.point.Longitude < -180 || p.point.Longitude > 180 {
			return &ValidationError{Field: p.name + ".longitude", Message: "must be between -180 and 180"}
		}
	}
	if n.Price < 0 {
		return &ValidationError{Field: "price", Message: "must be greater than or equal to 0"}
	}
	return nil
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
