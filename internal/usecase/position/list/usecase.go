// Package list retrieves a route, distinguishing an empty route from a missing trip.
package list

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/Makk228/avito-labs/internal/model"
)

type Dependencies struct {
	Trips     tripRepository
	Positions positionRepository
}

type Usecase struct{ deps Dependencies }

func New(deps Dependencies) *Usecase { return &Usecase{deps: deps} }

func (u *Usecase) List(ctx context.Context, id uuid.UUID) ([]model.Position, error) {
	if _, err := u.deps.Trips.Get(ctx, id); err != nil {
		return nil, fmt.Errorf("find route trip: %w", err)
	}

	values, err := u.deps.Positions.List(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("list route: %w", err)
	}

	return values, nil
}
