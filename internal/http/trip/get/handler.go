// Package get implements the get-trip HTTP endpoint.
package get

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/Makk228/avito-labs/internal/http/respond"
)

type Handler struct{ usecase usecase }

func New(uc usecase) *Handler { return &Handler{usecase: uc} }
func (h *Handler) Handle(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	t, err := h.usecase.Get(r.Context(), id)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, respond.Trip(t))
}
