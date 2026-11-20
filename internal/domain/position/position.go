// Package position defines recorded route points and their validation.
package position

import (
	"errors"
	"reflect"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"

	"github.com/Makk228/avito-labs/internal/domain/trip"
)

const FutureTolerance = time.Minute

type Position struct {
	ID          int64
	TripID      uuid.UUID
	Coordinates trip.Point
	RecordedAt  time.Time
}

type NewPosition struct {
	TripID      uuid.UUID  `json:"trip_id" validate:"required"`
	Coordinates trip.Point `json:"coordinates"`
	RecordedAt  time.Time  `json:"recorded_at" validate:"required"`
}

func (p NewPosition) Validate(now time.Time) error {
	rules := validator.New(validator.WithRequiredStructEnabled())
	rules.RegisterTagNameFunc(func(field reflect.StructField) string { return field.Tag.Get("json") })
	err := rules.Struct(p)
	if err != nil {
		var fields validator.ValidationErrors
		if !errors.As(err, &fields) {
			return err
		}

		first := fields[0]
		return &trip.ValidationError{
			Field:   strings.TrimPrefix(first.Namespace(), "NewPosition."),
			Message: "is invalid (" + first.Tag() + ")",
		}
	}

	if p.RecordedAt.After(now.Add(FutureTolerance)) {
		return &trip.ValidationError{Field: "recorded_at", Message: "must not be more than one minute in the future"}
	}

	return nil
}
