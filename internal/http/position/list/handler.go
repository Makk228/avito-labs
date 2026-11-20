// Package list implements the route retrieval endpoint.
package list

import (
	"net/http"

	"github.com/google/uuid"

	api "github.com/Makk228/avito-labs/internal/generated"
	"github.com/Makk228/avito-labs/internal/http/respond"
)

type Handler struct{ usecase usecase }

func New(uc usecase) *Handler { return &Handler{usecase: uc} }

func (h *Handler) Handle(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	values, err := h.usecase.List(r.Context(), id)
	if err != nil {
		respond.Error(w, r, err)
		return
	}

	positions := make([]api.TripPosition, 0, len(values))
	for _, value := range values {
		positions = append(positions, respond.Position(value))
	}

	respond.JSON(w, http.StatusOK, api.TripPositionsResponse{Positions: positions})
}
