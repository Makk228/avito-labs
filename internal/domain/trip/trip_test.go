package trip_test

import (
	"errors"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Makk228/avito-labs/internal/domain/trip"
)

func TestFinish(t *testing.T) {
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name   string
		status trip.Status
		want   error
	}{{"active", trip.StatusActive, nil}, {"completed", trip.StatusCompleted, trip.ErrTripCompleted}, {"unknown", "unknown", trip.ErrTripCompleted}} {
		t.Run(tc.name, func(t *testing.T) {
			value := trip.Trip{ID: uuid.New(), Status: tc.status}
			before := value
			err := value.Finish(at)
			if !errors.Is(err, tc.want) {
				t.Fatalf("error=%v, want %v", err, tc.want)
			}
			if err != nil {
				if !reflect.DeepEqual(value, before) {
					t.Fatal("failed transition changed trip")
				}
				return
			}
			if value.Status != trip.StatusCompleted || value.FinishedAt == nil || !value.FinishedAt.Equal(at) || value.ID != before.ID {
				t.Fatalf("incorrect transition: %+v", value)
			}
		})
	}
}

func TestValidate(t *testing.T) {
	valid := trip.NewTrip{UserID: uuid.New(), DriverID: uuid.New(), Start: trip.Point{Latitude: 90, Longitude: -180}, End: trip.Point{Latitude: -90, Longitude: 180}}
	for _, tc := range []struct {
		name   string
		change func(*trip.NewTrip)
		field  string
	}{
		{"valid", func(*trip.NewTrip) {}, ""},
		{"user", func(n *trip.NewTrip) { n.UserID = uuid.Nil }, "user_id"},
		{"driver", func(n *trip.NewTrip) { n.DriverID = uuid.Nil }, "driver_id"},
		{"latitude", func(n *trip.NewTrip) { n.Start.Latitude = 90.01 }, "start_point.latitude"},
		{"longitude", func(n *trip.NewTrip) { n.End.Longitude = -180.01 }, "end_point.longitude"},
		{"nan", func(n *trip.NewTrip) { n.End.Latitude = math.NaN() }, "end_point.latitude"},
		{"infinity", func(n *trip.NewTrip) { n.Start.Longitude = math.Inf(1) }, "start_point.longitude"},
		{"price", func(n *trip.NewTrip) { n.Price = -1 }, "price"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n := valid
			tc.change(&n)
			err := n.Validate()
			if tc.field == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			var validation *trip.ValidationError
			if !errors.As(err, &validation) || validation.Field != tc.field || validation.Error() == "" {
				t.Fatalf("unexpected validation error: %v", err)
			}
		})
	}
}

func TestFingerprint(t *testing.T) {
	if err := (&trip.ValidationError{Message: "invalid body"}).Error(); err != "invalid body" {
		t.Fatal(err)
	}
	n := trip.NewTrip{UserID: uuid.New(), DriverID: uuid.New(), Price: 10}
	copyOfInput := n
	if n.Fingerprint() != copyOfInput.Fingerprint() || len(n.Fingerprint()) != 64 {
		t.Fatal("fingerprint must be stable SHA-256")
	}
	changed := n
	changed.Price++
	if n.Fingerprint() == changed.Fingerprint() {
		t.Fatal("price is missing from fingerprint")
	}
}
