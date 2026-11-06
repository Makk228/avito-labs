package respond

import (
	"time"

	"github.com/oapi-codegen/nullable"

	api "github.com/Makk228/avito-labs/internal/generated"
	"github.com/Makk228/avito-labs/internal/model"
)

func Trip(t model.Trip) api.Trip {
	res := api.Trip{
		Id:       t.ID,
		UserId:   t.UserID,
		DriverId: t.DriverID,
		StartPoint: api.Coordinates{
			Latitude:  t.Start.Latitude,
			Longitude: t.Start.Longitude,
		},
		EndPoint: api.Coordinates{
			Latitude:  t.End.Latitude,
			Longitude: t.End.Longitude,
		},
		Price:          t.Price,
		Status:         api.TripStatus(t.Status),
		StartedAt:      t.StartedAt.UTC(),
		FinishedAt:     nullable.NewNullNullable[time.Time](),
		LastPositionAt: nullable.NewNullNullable[time.Time](),
	}
	if t.FinishedAt != nil {
		res.FinishedAt = nullable.NewNullableWithValue(t.FinishedAt.UTC())
	}
	return res
}
