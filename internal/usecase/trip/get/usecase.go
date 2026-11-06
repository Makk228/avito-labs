// Package get retrieves a trip without depending on a transport or database.
package get

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/Makk228/avito-labs/internal/model"
)

type Usecase struct{ trips tripRepository }

func New(trips tripRepository) *Usecase { return &Usecase{trips: trips} }

func (u *Usecase) Get(ctx context.Context, id uuid.UUID) (model.Trip, error) {
	t, err := u.trips.Get(ctx, id)
	if err != nil {
		return model.Trip{}, fmt.Errorf("get trip: %w", err)
	}
	return t, nil
}
