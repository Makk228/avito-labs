// Package save implements the coordinate ingestion endpoint.
package save

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/Makk228/avito-labs/internal/http/respond"
)

type Handler struct{ usecase usecase }

func New(uc usecase) *Handler { return &Handler{usecase: uc} }

func (h *Handler) Handle(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	in, err := decode(w, r, id)
	if err != nil {
		respond.Invalid(w, r, err.Error())
		return
	}

	value, err := h.usecase.Save(r.Context(), in)
	if err != nil {
		respond.Error(w, r, err)
		return
	}

	respond.JSON(w, http.StatusCreated, respond.Position(value))
}
