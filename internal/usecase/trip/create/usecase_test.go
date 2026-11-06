package create_test

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
	"github.com/Makk228/avito-labs/internal/usecase/trip/create"
)

func TestCreate(t *testing.T) {
	storageErr := errors.New("repository failure")
	for _, tc := range []struct {
		name                       string
		saveErr, historyErr, txErr error
		invalid                    bool
	}{
		{name: "success"}, {name: "driver busy", saveErr: trip.ErrDriverBusy}, {name: "repository failure", saveErr: storageErr}, {name: "history failure", historyErr: storageErr}, {name: "commit failure", txErr: storageErr}, {name: "invalid", invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			repo := NewMocktripRepository(ctrl)
			tx := NewMocktxManager(ctrl)
			keys := NewMockidempotencyStore(ctrl)
			metrics := NewMockmetrics(ctrl)
			in := model.NewTrip{UserID: uuid.New(), DriverID: uuid.New(), Price: 42}
			if tc.invalid {
				in.Price = -1
			} else {
				tx.EXPECT().Do(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, fn func(context.Context) error) error {
					if err := fn(ctx); err != nil {
						return err
					}
					return tc.txErr
				})
				repo.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, value model.Trip) error {
					if value.ID == uuid.Nil || value.UserID != in.UserID || value.DriverID != in.DriverID || value.Price != 42 || value.Status != trip.StatusActive || value.StartedAt.IsZero() {
						t.Fatalf("invalid trip: %+v", value)
					}
					return tc.saveErr
				})
				if tc.saveErr == nil {
					repo.EXPECT().AddStatusChange(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, c model.StatusChange) error {
						if c.TripID == uuid.Nil || c.From != nil || c.To != trip.StatusActive || c.ChangedAt.IsZero() {
							t.Fatalf("invalid history: %+v", c)
						}
						return tc.historyErr
					})
				}
				if tc.saveErr == nil && tc.historyErr == nil && tc.txErr == nil {
					metrics.EXPECT().Created(gomock.Any())
				}
			}
			uc := create.New(tx, repo, keys, time.Hour, slog.New(slog.NewTextHandler(io.Discard, nil)), metrics)
			got, replay, err := uc.Create(context.Background(), in, nil)
			want := errors.Join(tc.saveErr, tc.historyErr, tc.txErr)
			if tc.invalid {
				var validation *trip.ValidationError
				if !errors.As(err, &validation) {
					t.Fatalf("want validation, got %v", err)
				}
			} else {
				assertCreateError(t, err, want, tc.saveErr, tc.historyErr, tc.txErr)
			}
			if replay {
				t.Fatal("new trip marked replayed")
			}
			if err == nil && got.ID == uuid.Nil {
				t.Fatal("missing trip")
			}
			if err != nil && got.ID != uuid.Nil {
				t.Fatal("trip returned after rollback")
			}
		})
	}
}

func TestIdempotencyReplay(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		name := "replay"
		if conflict {
			name = "conflict"
		}
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			repo := NewMocktripRepository(ctrl)
			tx := NewMocktxManager(ctrl)
			keys := NewMockidempotencyStore(ctrl)
			metrics := NewMockmetrics(ctrl)
			in := model.NewTrip{UserID: uuid.New(), DriverID: uuid.New()}
			key, id := uuid.New(), uuid.New()
			tx.EXPECT().Do(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) })
			keys.EXPECT().Reserve(gomock.Any(), key, in.Fingerprint(), gomock.Any(), gomock.Any()).Return(false, nil)
			hash := in.Fingerprint()
			if conflict {
				hash = "different"
			}
			keys.EXPECT().Get(gomock.Any(), key).Return(model.IdempotencyRecord{RequestHash: hash, TripID: id}, nil)
			if !conflict {
				repo.EXPECT().Get(gomock.Any(), id).Return(model.Trip{ID: id}, nil)
			}
			uc := create.New(tx, repo, keys, time.Hour, slog.New(slog.NewTextHandler(io.Discard, nil)), metrics)
			got, replay, err := uc.Create(context.Background(), in, &key)
			if conflict {
				if !errors.Is(err, trip.ErrIdempotencyConflict) || replay {
					t.Fatalf("unexpected conflict: %v %v", replay, err)
				}
				return
			}
			if err != nil || !replay || got.ID != id {
				t.Fatalf("incorrect replay: %+v %v %v", got, replay, err)
			}
		})
	}
}

func assertCreateError(t *testing.T, got, want error, causes ...error) {
	t.Helper()
	if (got == nil) != (want == nil) {
		t.Fatalf("unexpected error: %v", got)
	}
	for _, cause := range causes {
		if cause != nil && !errors.Is(got, cause) {
			t.Fatalf("got %v, want cause %v", got, cause)
		}
	}
}
