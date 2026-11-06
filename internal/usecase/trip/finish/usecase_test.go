package finish_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	"github.com/Makk228/avito-labs/internal/domain/trip"
	"github.com/Makk228/avito-labs/internal/model"
	"github.com/Makk228/avito-labs/internal/usecase/trip/finish"
)

func TestFinish(t *testing.T) {
	failure := errors.New("database failure")
	for _, tc := range []struct {
		name                                           string
		status                                         trip.Status
		getErr, updateErr, historyErr, commitErr, want error
		lost                                           bool
	}{
		{name: "success", status: trip.StatusActive},
		{name: "completed", status: trip.StatusCompleted, want: trip.ErrTripCompleted},
		{name: "missing", getErr: trip.ErrTripNotFound, want: trip.ErrTripNotFound},
		{name: "fetch error", getErr: failure, want: failure},
		{name: "update error", status: trip.StatusActive, updateErr: failure, want: failure},
		{name: "concurrent completion", status: trip.StatusActive, lost: true, want: trip.ErrTripCompleted},
		{name: "history error", status: trip.StatusActive, historyErr: failure, want: failure},
		{name: "commit error", status: trip.StatusActive, commitErr: failure, want: failure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			repo := NewMocktripRepository(ctrl)
			tx := NewMocktxManager(ctrl)
			metrics := NewMockmetrics(ctrl)
			id := uuid.New()
			value := model.Trip{ID: id, Status: tc.status}
			tx.EXPECT().Do(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, fn func(context.Context) error) error {
				if err := fn(ctx); err != nil {
					return err
				}
				return tc.commitErr
			})
			repo.EXPECT().Get(gomock.Any(), id).Return(value, tc.getErr)
			if tc.getErr == nil && tc.status == trip.StatusActive {
				repo.EXPECT().FinishActive(gomock.Any(), id, gomock.Any()).DoAndReturn(func(_ context.Context, _ uuid.UUID, at time.Time) (model.Trip, bool, error) {
					completed := value
					if err := completed.Finish(at); err != nil {
						t.Fatal(err)
					}
					return completed, !tc.lost, tc.updateErr
				})
				if tc.updateErr == nil && !tc.lost {
					repo.EXPECT().AddStatusChange(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, c model.StatusChange) error {
						if c.TripID != id || c.From == nil || *c.From != trip.StatusActive || c.To != trip.StatusCompleted {
							t.Fatalf("bad history: %+v", c)
						}
						return tc.historyErr
					})
				}
			}
			if tc.want == nil {
				metrics.EXPECT().Completed(gomock.Any())
			}
			got, err := finish.New(tx, repo, slog.New(slog.NewTextHandler(io.Discard, nil)), metrics).Finish(context.Background(), id)
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
			if err == nil && (got.ID != id || got.Status != trip.StatusCompleted || got.FinishedAt == nil) {
				t.Fatalf("bad result: %+v", got)
			}
			if err != nil && got.ID != uuid.Nil {
				t.Fatal("uncommitted result returned")
			}
		})
	}
}
