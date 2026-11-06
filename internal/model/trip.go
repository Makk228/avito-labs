// Package model contains values exchanged between application layers.
package model

import "github.com/Makk228/avito-labs/internal/domain/trip"

type (
	Trip              = trip.Trip
	NewTrip           = trip.NewTrip
	Point             = trip.Point
	StatusChange      = trip.StatusChange
	IdempotencyRecord = trip.IdempotencyRecord
)
