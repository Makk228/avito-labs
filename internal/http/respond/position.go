package respond

import (
	api "github.com/Makk228/avito-labs/internal/generated"
	"github.com/Makk228/avito-labs/internal/model"
)

func Position(value model.Position) api.TripPosition {
	return api.TripPosition{
		Id:     value.ID,
		TripId: value.TripID,
		Coordinates: api.Coordinates{
			Latitude:  value.Coordinates.Latitude,
			Longitude: value.Coordinates.Longitude,
		},
		RecordedAt: value.RecordedAt.UTC(),
	}
}
