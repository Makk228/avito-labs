package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/oapi-codegen/nullable"

	api "github.com/Makk228/avito-labs/internal/generated"
	"github.com/Makk228/avito-labs/internal/trip"
)

type Pinger interface {
	Ping(ctx context.Context) error
}

type Handler struct {
	trips        *trip.Service
	db           Pinger
	readyTimeout time.Duration
	log          *slog.Logger
}

var _ api.ServerInterface = (*Handler)(nil)

func NewHandler(trips *trip.Service, db Pinger, readyTimeout time.Duration, log *slog.Logger) *Handler {
	return &Handler{
		trips:        trips,
		db:           db,
		readyTimeout: readyTimeout,
		log:          log,
	}
}

func (h *Handler) CreateTrip(w http.ResponseWriter, r *http.Request, params api.CreateTripParams) {
	in, err := decodeCreateTrip(w, r)
	if err != nil {
		writeProblem(w, r, problemInvalidRequest, err.Error())
		return
	}

	t, replayed, err := h.trips.Create(r.Context(), in, params.IdempotencyKey)
	if err != nil {
		writeError(w, r, h.log, err)
		return
	}

	if replayed {
		writeJSON(w, http.StatusOK, toAPITrip(t))
		return
	}
	h.log.Info("trip created", slog.String("trip_id", t.ID.String()))
	w.Header().Set("Location", "/api/v1/trips/"+t.ID.String())
	writeJSON(w, http.StatusCreated, toAPITrip(t))
}

func (h *Handler) GetTrip(w http.ResponseWriter, r *http.Request, tripID api.TripId) {
	t, err := h.trips.Get(r.Context(), tripID)
	if err != nil {
		writeError(w, r, h.log, err)
		return
	}
	writeJSON(w, http.StatusOK, toAPITrip(t))
}

func (h *Handler) FinishTrip(w http.ResponseWriter, r *http.Request, tripID api.TripId) {
	t, err := h.trips.Finish(r.Context(), tripID)
	if err != nil {
		writeError(w, r, h.log, err)
		return
	}
	h.log.Info("trip finished", slog.String("trip_id", t.ID.String()))
	writeJSON(w, http.StatusOK, toAPITrip(t))
}

func (h *Handler) Health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, api.HealthResponse{Status: api.Ok})
}

func (h *Handler) Ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), h.readyTimeout)
	defer cancel()

	if err := h.db.Ping(ctx); err != nil {
		h.log.Warn("readiness check failed", slog.Any("error", err))
		writeJSON(w, http.StatusServiceUnavailable, api.HealthResponse{Status: api.Unavailable})
		return
	}
	writeJSON(w, http.StatusOK, api.HealthResponse{Status: api.Ok})
}

func toAPITrip(t trip.Trip) api.Trip {
	res := api.Trip{
		Id:       t.ID,
		UserId:   t.UserID,
		DriverId: t.DriverID,
		StartPoint: api.Coordinates{
			Latitude:  t.Start.Latitude,
			Longitude: t.Start.Longitude,
		},
		EndPoint: api.Coordinates{
			Latitude:  t.End.Latitude,
			Longitude: t.End.Longitude,
		},
		Price:          t.Price,
		Status:         api.TripStatus(t.Status),
		StartedAt:      t.StartedAt.UTC(),
		FinishedAt:     nullable.NewNullNullable[time.Time](),
		LastPositionAt: nullable.NewNullNullable[time.Time](),
	}
	if t.FinishedAt != nil {
		res.FinishedAt = nullable.NewNullableWithValue(t.FinishedAt.UTC())
	}
	return res
}
