package create_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/Makk228/avito-labs/internal/domain/trip"
	"github.com/Makk228/avito-labs/internal/model"
	"github.com/Makk228/avito-labs/internal/usecase/trip/create"
)

const keyTTL = time.Hour

type contextKey struct{}

type fixture struct {
	t       *testing.T
	ctx     context.Context
	txCtx   context.Context
	input   model.NewTrip
	key     *uuid.UUID
	value   model.Trip
	at      time.Time
	repo    *MocktripRepository
	tx      *MocktxManager
	keys    *MockidempotencyStore
	clock   *Mockclock
	ids     *MockidGenerator
	log     *Mocklogger
	metrics *Mockmetrics
}

func newFixture(t *testing.T) *fixture {
	t.Helper()

	ctrl := gomock.NewController(t)
	ctx := context.WithValue(context.Background(), contextKey{}, "request")
	at := time.Date(2026, time.November, 1, 12, 0, 0, 0, time.UTC)
	input := model.NewTrip{
		UserID:   uuid.MustParse("00000000-0000-4000-8000-000000000001"),
		DriverID: uuid.MustParse("00000000-0000-4000-8000-000000000002"),
		Start:    model.Point{Latitude: 55, Longitude: 37},
		End:      model.Point{Latitude: 56, Longitude: 38},
		Price:    100,
	}

	return &fixture{
		t:     t,
		ctx:   ctx,
		txCtx: context.WithValue(ctx, contextKey{}, "transaction"),
		input: input,
		at:    at,
		value: model.Trip{
			ID:        uuid.MustParse("00000000-0000-4000-8000-000000000003"),
			UserID:    input.UserID,
			DriverID:  input.DriverID,
			Start:     input.Start,
			End:       input.End,
			Price:     input.Price,
			Status:    trip.StatusActive,
			StartedAt: at,
		},
		repo:    NewMocktripRepository(ctrl),
		tx:      NewMocktxManager(ctrl),
		keys:    NewMockidempotencyStore(ctrl),
		clock:   NewMockclock(ctrl),
		ids:     NewMockidGenerator(ctrl),
		log:     NewMocklogger(ctrl),
		metrics: NewMockmetrics(ctrl),
	}
}

func (f *fixture) usecase() *create.Usecase {
	return create.New(create.Dependencies{
		Transactions: f.tx,
		Trips:        f.repo,
		Idempotency:  f.keys,
		Clock:        f.clock,
		IDs:          f.ids,
		Logger:       f.log,
		Metrics:      f.metrics,
	}, create.Config{KeyTTL: keyTTL})
}

func (f *fixture) transaction(callbackError, resultError error) {
	f.tx.EXPECT().Do(f.ctx, gomock.Any()).DoAndReturn(
		func(_ context.Context, action func(context.Context) error) error {
			require.ErrorIs(f.t, action(f.txCtx), callbackError)
			return resultError
		},
	)
}

func (f *fixture) expectNewTrip() {
	f.ids.EXPECT().NewID().Return(f.value.ID)
	f.clock.EXPECT().Now().Return(f.at)
}

func (f *fixture) expectHistory(err error) {
	f.repo.EXPECT().AddStatusChange(f.txCtx, model.StatusChange{
		TripID:    f.value.ID,
		To:        trip.StatusActive,
		Reason:    "trip created",
		ChangedAt: f.at,
	}).Return(err)
}

func (f *fixture) expectBusinessEvent() {
	f.metrics.EXPECT().Created(f.ctx)
	f.log.EXPECT().InfoContext(f.ctx, "trip created", "trip_id", f.value.ID.String())
}

func (f *fixture) reserveKey(reserved bool, err error) uuid.UUID {
	key := uuid.MustParse("00000000-0000-4000-8000-000000000004")
	f.key = &key
	f.clock.EXPECT().Now().Return(f.at)
	f.keys.EXPECT().Reserve(f.txCtx, key, f.input.Fingerprint(), f.at, f.at.Add(keyTTL)).Return(reserved, err)
	return key
}

func TestCreate(t *testing.T) {
	databaseError := errors.New("database unavailable")
	expected := newTripResult()

	tests := []struct {
		name    string
		setup   func(*fixture)
		want    model.CreateTripResult
		wantErr error
	}{
		{
			name: "creates trip and history before counting the committed result",
			setup: func(f *fixture) {
				f.transaction(nil, nil)
				f.expectNewTrip()
				f.repo.EXPECT().Create(f.txCtx, f.value).Return(nil)
				f.expectHistory(nil)
				f.expectBusinessEvent()
			},
			want: expected,
		},
		{
			name: "driver busy rolls back without history or business events",
			setup: func(f *fixture) {
				f.transaction(trip.ErrDriverBusy, trip.ErrDriverBusy)
				f.expectNewTrip()
				f.repo.EXPECT().Create(f.txCtx, f.value).Return(trip.ErrDriverBusy)
			},
			wantErr: trip.ErrDriverBusy,
		},
		{
			name: "repository failure returns no uncommitted trip",
			setup: func(f *fixture) {
				f.transaction(databaseError, databaseError)
				f.expectNewTrip()
				f.repo.EXPECT().Create(f.txCtx, f.value).Return(databaseError)
			},
			wantErr: databaseError,
		},
		{
			name: "history failure rolls back the trip",
			setup: func(f *fixture) {
				f.transaction(databaseError, databaseError)
				f.expectNewTrip()
				f.repo.EXPECT().Create(f.txCtx, f.value).Return(nil)
				f.expectHistory(databaseError)
			},
			wantErr: databaseError,
		},
		{
			name: "begin failure does not call dependencies inside the transaction",
			setup: func(f *fixture) {
				f.tx.EXPECT().Do(f.ctx, gomock.Any()).Return(databaseError)
			},
			wantErr: databaseError,
		},
		{
			name: "commit failure does not increment metrics or log creation",
			setup: func(f *fixture) {
				f.transaction(nil, databaseError)
				f.expectNewTrip()
				f.repo.EXPECT().Create(f.txCtx, f.value).Return(nil)
				f.expectHistory(nil)
			},
			wantErr: databaseError,
		},
		{
			name: "reserves and binds a new idempotency key atomically",
			setup: func(f *fixture) {
				f.transaction(nil, nil)
				key := f.reserveKey(true, nil)
				f.expectNewTrip()
				f.repo.EXPECT().Create(f.txCtx, f.value).Return(nil)
				f.expectHistory(nil)
				f.keys.EXPECT().Bind(f.txCtx, key, f.value.ID).Return(nil)
				f.expectBusinessEvent()
			},
			want: expected,
		},
		{
			name: "existing key returns the saved trip without creating another",
			setup: func(f *fixture) {
				f.transaction(nil, nil)
				key := f.reserveKey(false, nil)
				f.keys.EXPECT().Get(f.txCtx, key).Return(model.IdempotencyRecord{RequestHash: f.input.Fingerprint(), TripID: f.value.ID}, nil)
				f.repo.EXPECT().Get(f.txCtx, f.value.ID).Return(f.value, nil)
			},
			want: model.CreateTripResult{Trip: expected.Trip},
		},
		{
			name: "different request for the same key conflicts",
			setup: func(f *fixture) {
				f.transaction(trip.ErrIdempotencyConflict, trip.ErrIdempotencyConflict)
				key := f.reserveKey(false, nil)
				f.keys.EXPECT().Get(f.txCtx, key).Return(model.IdempotencyRecord{RequestHash: "different"}, nil)
			},
			wantErr: trip.ErrIdempotencyConflict,
		},
		{
			name: "reservation failure stops creation",
			setup: func(f *fixture) {
				f.transaction(databaseError, databaseError)
				f.reserveKey(false, databaseError)
			},
			wantErr: databaseError,
		},
		{
			name: "key lookup failure stops creation",
			setup: func(f *fixture) {
				f.transaction(databaseError, databaseError)
				key := f.reserveKey(false, nil)
				f.keys.EXPECT().Get(f.txCtx, key).Return(model.IdempotencyRecord{}, databaseError)
			},
			wantErr: databaseError,
		},
		{
			name: "saved trip lookup failure preserves its cause",
			setup: func(f *fixture) {
				f.transaction(trip.ErrTripNotFound, trip.ErrTripNotFound)
				key := f.reserveKey(false, nil)
				f.keys.EXPECT().Get(f.txCtx, key).Return(model.IdempotencyRecord{RequestHash: f.input.Fingerprint(), TripID: f.value.ID}, nil)
				f.repo.EXPECT().Get(f.txCtx, f.value.ID).Return(model.Trip{}, trip.ErrTripNotFound)
			},
			wantErr: trip.ErrTripNotFound,
		},
		{
			name: "binding failure does not report a committed creation",
			setup: func(f *fixture) {
				f.transaction(databaseError, databaseError)
				key := f.reserveKey(true, nil)
				f.expectNewTrip()
				f.repo.EXPECT().Create(f.txCtx, f.value).Return(nil)
				f.expectHistory(nil)
				f.keys.EXPECT().Bind(f.txCtx, key, f.value.ID).Return(databaseError)
			},
			wantErr: databaseError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			tt.setup(f)

			got, err := f.usecase().Create(f.ctx, f.input, f.key)

			require.ErrorIs(t, err, tt.wantErr)
			assert.Equal(t, tt.want, got)
		})
	}
}

func newTripResult() model.CreateTripResult {
	input := model.NewTrip{
		UserID:   uuid.MustParse("00000000-0000-4000-8000-000000000001"),
		DriverID: uuid.MustParse("00000000-0000-4000-8000-000000000002"),
		Start:    model.Point{Latitude: 55, Longitude: 37},
		End:      model.Point{Latitude: 56, Longitude: 38},
		Price:    100,
	}

	return model.CreateTripResult{
		Created: true,
		Trip: model.Trip{
			ID:        uuid.MustParse("00000000-0000-4000-8000-000000000003"),
			UserID:    input.UserID,
			DriverID:  input.DriverID,
			Start:     input.Start,
			End:       input.End,
			Price:     input.Price,
			Status:    trip.StatusActive,
			StartedAt: time.Date(2026, time.November, 1, 12, 0, 0, 0, time.UTC),
		},
	}
}

func TestCreateRejectsInvalidInputBeforeTransaction(t *testing.T) {
	f := newFixture(t)
	f.input.Price = -1

	result, err := f.usecase().Create(f.ctx, f.input, nil)

	var validation *trip.ValidationError
	require.ErrorAs(t, err, &validation)
	assert.Equal(t, "price", validation.Field)
	assert.Equal(t, model.CreateTripResult{}, result)
}
