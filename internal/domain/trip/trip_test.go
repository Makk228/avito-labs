package trip_test

import (
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Makk228/avito-labs/internal/domain/trip"
)

func TestFinish(t *testing.T) {
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	id := uuid.MustParse("00000000-0000-4000-8000-000000000001")
	for _, tc := range []struct {
		name    string
		in      trip.Trip
		want    trip.Trip
		wantErr error
	}{
		{
			name: "active",
			in:   trip.Trip{ID: id, Status: trip.StatusActive},
			want: trip.Trip{ID: id, Status: trip.StatusCompleted, FinishedAt: &at},
		},
		{
			name:    "completed",
			in:      trip.Trip{ID: id, Status: trip.StatusCompleted, FinishedAt: &at},
			want:    trip.Trip{ID: id, Status: trip.StatusCompleted, FinishedAt: &at},
			wantErr: trip.ErrTripCompleted,
		},
		{
			name:    "unknown status",
			in:      trip.Trip{ID: id, Status: "unknown"},
			want:    trip.Trip{ID: id, Status: "unknown"},
			wantErr: trip.ErrTripCompleted,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value := tc.in

			err := value.Finish(at)

			require.ErrorIs(t, err, tc.wantErr)
			assert.Equal(t, tc.want, value)
		})
	}
}

func validInput() trip.NewTrip {
	return trip.NewTrip{
		UserID:   uuid.MustParse("00000000-0000-4000-8000-000000000001"),
		DriverID: uuid.MustParse("00000000-0000-4000-8000-000000000002"),
		Start:    trip.Point{Latitude: 90, Longitude: -180},
		End:      trip.Point{Latitude: -90, Longitude: 180},
	}
}

func TestValidateAcceptsBoundaries(t *testing.T) {
	require.NoError(t, validInput().Validate())
}

func TestValidateRejectsInvalidInput(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*trip.NewTrip)
		want   trip.ValidationError
	}{
		{
			name:   "user",
			change: func(n *trip.NewTrip) { n.UserID = uuid.Nil },
			want:   trip.ValidationError{Field: "user_id", Message: "must not be empty"},
		},
		{
			name:   "driver",
			change: func(n *trip.NewTrip) { n.DriverID = uuid.Nil },
			want:   trip.ValidationError{Field: "driver_id", Message: "must not be empty"},
		},
		{
			name:   "latitude",
			change: func(n *trip.NewTrip) { n.Start.Latitude = 90.01 },
			want:   trip.ValidationError{Field: "start_point.latitude", Message: "must be between -90 and 90"},
		},
		{
			name:   "longitude",
			change: func(n *trip.NewTrip) { n.End.Longitude = -180.01 },
			want:   trip.ValidationError{Field: "end_point.longitude", Message: "must be between -180 and 180"},
		},
		{
			name:   "nan",
			change: func(n *trip.NewTrip) { n.End.Latitude = math.NaN() },
			want:   trip.ValidationError{Field: "end_point.latitude", Message: "must be between -90 and 90"},
		},
		{
			name:   "infinity",
			change: func(n *trip.NewTrip) { n.Start.Longitude = math.Inf(1) },
			want:   trip.ValidationError{Field: "start_point.longitude", Message: "must be between -180 and 180"},
		},
		{
			name:   "negative price",
			change: func(n *trip.NewTrip) { n.Price = -1 },
			want:   trip.ValidationError{Field: "price", Message: "must be greater than or equal to 0"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := validInput()
			tc.change(&input)

			err := input.Validate()

			var validation *trip.ValidationError
			require.ErrorAs(t, err, &validation)
			assert.Equal(t, &tc.want, validation)
		})
	}
}

func TestFingerprint(t *testing.T) {
	input := validInput()
	input.Price = 10
	same := input
	changed := input
	changed.Price++

	assert.Len(t, input.Fingerprint(), 64)
	assert.Equal(t, input.Fingerprint(), same.Fingerprint())
	assert.NotEqual(t, input.Fingerprint(), changed.Fingerprint())
}

func TestValidationErrorMessage(t *testing.T) {
	assert.EqualError(t, &trip.ValidationError{Message: "invalid body"}, "invalid body")
	assert.EqualError(t, &trip.ValidationError{Field: "price", Message: "must be nonnegative"}, "price must be nonnegative")
}
