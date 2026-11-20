package save

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/Makk228/avito-labs/internal/model"
)

const maxBodyBytes = 64 << 10

// Pointers distinguish a missing/null field from valid zero coordinates.
type request struct {
	Coordinates *struct {
		Latitude  *float64 `json:"latitude"`
		Longitude *float64 `json:"longitude"`
	} `json:"coordinates"`
	RecordedAt *time.Time `json:"recorded_at"`
}

func decode(w http.ResponseWriter, r *http.Request, id uuid.UUID) (model.NewPosition, error) {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	decoder.DisallowUnknownFields()
	var req request
	if err := decoder.Decode(&req); err != nil {
		return model.NewPosition{}, fmt.Errorf("invalid position JSON: %w", err)
	}

	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return model.NewPosition{}, errors.New("request must contain exactly one JSON object")
	}

	if req.Coordinates == nil || req.Coordinates.Latitude == nil || req.Coordinates.Longitude == nil || req.RecordedAt == nil {
		return model.NewPosition{}, errors.New("coordinates, latitude, longitude and recorded_at are required")
	}

	return model.NewPosition{
		TripID:      id,
		Coordinates: model.Point{Latitude: *req.Coordinates.Latitude, Longitude: *req.Coordinates.Longitude},
		RecordedAt:  req.RecordedAt.UTC(),
	}, nil
}
