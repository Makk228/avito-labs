package position_test

import (
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Makk228/avito-labs/internal/domain/trip"
	"github.com/Makk228/avito-labs/internal/model"
)

func validInput() model.NewPosition {
	return model.NewPosition{
		TripID:      uuid.MustParse("00000000-0000-4000-8000-000000000001"),
		Coordinates: model.Point{Latitude: 90, Longitude: -180},
		RecordedAt:  time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC),
	}
}

func TestValidRecordedTimes(t *testing.T) {
	input := validInput()
	now := input.RecordedAt
	for _, at := range []time.Time{now.Add(-24 * time.Hour), now, now.Add(time.Minute)} {
		input.RecordedAt = at
		require.NoError(t, input.Validate(now))
	}
}

func TestValidation(t *testing.T) {
	for _, tc := range []struct {
		name      string
		change    func(*model.NewPosition)
		wantField string
	}{
		{name: "missing trip", change: func(p *model.NewPosition) { p.TripID = uuid.Nil }, wantField: "trip_id"},
		{name: "missing time", change: func(p *model.NewPosition) { p.RecordedAt = time.Time{} }, wantField: "recorded_at"},
		{name: "future beyond tolerance", change: func(p *model.NewPosition) { p.RecordedAt = p.RecordedAt.Add(time.Minute + time.Nanosecond) }, wantField: "recorded_at"},
		{name: "latitude", change: func(p *model.NewPosition) { p.Coordinates.Latitude = 91 }, wantField: "coordinates.latitude"},
		{name: "longitude", change: func(p *model.NewPosition) { p.Coordinates.Longitude = -181 }, wantField: "coordinates.longitude"},
		{name: "nan", change: func(p *model.NewPosition) { p.Coordinates.Latitude = math.NaN() }, wantField: "coordinates.latitude"},
		{name: "infinity", change: func(p *model.NewPosition) { p.Coordinates.Longitude = math.Inf(1) }, wantField: "coordinates.longitude"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := validInput()
			now := input.RecordedAt
			tc.change(&input)

			err := input.Validate(now)

			var validation *trip.ValidationError
			require.ErrorAs(t, err, &validation)
			assert.Equal(t, tc.wantField, validation.Field)
		})
	}
}
