// Package create implements the create-trip HTTP endpoint.
package create

import (
	"net/http"

	api "github.com/Makk228/avito-labs/internal/generated"
	"github.com/Makk228/avito-labs/internal/http/respond"
)

type Handler struct{ usecase usecase }

func New(uc usecase) *Handler { return &Handler{usecase: uc} }

func (h *Handler) Handle(w http.ResponseWriter, r *http.Request, params api.CreateTripParams) {
	in, err := decodeCreateTrip(w, r)
	if err != nil {
		respond.Invalid(w, r, err.Error())
		return
	}

	result, err := h.usecase.Create(r.Context(), in, params.IdempotencyKey)
	if err != nil {
		respond.Error(w, r, err)
		return
	}

	if !result.Created {
		respond.JSON(w, http.StatusOK, respond.Trip(result.Trip))
		return
	}

	w.Header().Set("Location", "/api/v1/trips/"+result.Trip.ID.String())
	respond.JSON(w, http.StatusCreated, respond.Trip(result.Trip))
}
