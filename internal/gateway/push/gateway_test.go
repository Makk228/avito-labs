package push

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/Makk228/avito-labs/internal/model"
)

type trackedBody struct {
	io.Reader
	closed bool
}

func (b *trackedBody) Close() error { b.closed = true; return nil }

func TestRequestPosition(t *testing.T) {
	ctx := context.Background()
	trip := model.StaleTrip{
		ID:       uuid.MustParse("00000000-0000-4000-8000-000000000001"),
		DriverID: uuid.MustParse("00000000-0000-4000-8000-000000000002"),
	}
	requestID := uuid.MustParse("00000000-0000-4000-8000-000000000003")
	for _, tc := range []struct {
		name         string
		status       int
		transportErr error
		check        func(*testing.T, error)
	}{
		{name: "accepted", status: http.StatusAccepted, check: func(t *testing.T, err error) { require.NoError(t, err) }},
		{name: "bad request", status: http.StatusBadRequest, check: checkHTTPError(http.StatusBadRequest)},
		{name: "throttled without retry", status: http.StatusTooManyRequests, check: checkHTTPError(http.StatusTooManyRequests)},
		{name: "unavailable without retry", status: http.StatusServiceUnavailable, check: checkHTTPError(http.StatusServiceUnavailable)},
		{name: "redirect without second request", status: http.StatusTemporaryRedirect, check: checkHTTPError(http.StatusTemporaryRedirect)},
		{name: "timeout", transportErr: context.DeadlineExceeded, check: func(t *testing.T, err error) { require.ErrorIs(t, err, context.DeadlineExceeded) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			client, ids := NewMockclient(ctrl), NewMockidGenerator(ctrl)
			ids.EXPECT().NewID().Return(requestID)
			body := &trackedBody{Reader: strings.NewReader("{}")}
			client.EXPECT().Do(gomock.Cond(func(req *http.Request) bool {
				assert.Equal(t, ctx, req.Context())
				assert.Equal(t, http.MethodPost, req.Method)
				assert.Equal(t, "http://push.test/otlp/api/v1/push", req.URL.String())
				assert.Equal(t, "application/json", req.Header.Get("Content-Type"))
				data, err := io.ReadAll(req.Body)
				assert.NoError(t, err)
				assert.NoError(t, req.Body.Close())
				return assert.JSONEq(t, `{
                    "request_id":"00000000-0000-4000-8000-000000000003",
                    "recipient_id":"00000000-0000-4000-8000-000000000002",
                    "kind":"REQUEST_POSITION",
                    "data":{"trip_id":"00000000-0000-4000-8000-000000000001"}
                }`, string(data))
			})).Return(&http.Response{StatusCode: tc.status, Body: body}, tc.transportErr)
			client.EXPECT().CloseIdleConnections()
			gateway := &Gateway{client: client, ids: ids, endpoint: "http://push.test/otlp/api/v1/push"}

			err := gateway.RequestPosition(ctx, trip)
			gateway.Close()

			assert.True(t, body.closed)
			tc.check(t, err)
		})
	}
}

func TestMalformedEndpointPreservesRequestError(t *testing.T) {
	ctrl := gomock.NewController(t)
	ids := NewMockidGenerator(ctrl)
	ids.EXPECT().NewID().Return(uuid.Nil)
	gateway := &Gateway{client: NewMockclient(ctrl), ids: ids, endpoint: "://invalid"}

	err := gateway.RequestPosition(context.Background(), model.StaleTrip{})

	require.ErrorContains(t, err, "build push request")
	var statusError *HTTPError
	assert.False(t, errors.As(err, &statusError))
}

func checkHTTPError(status int) func(*testing.T, error) {
	return func(t *testing.T, err error) {
		t.Helper()
		var statusError *HTTPError
		require.ErrorAs(t, err, &statusError)
		assert.Equal(t, status, statusError.StatusCode)
	}
}
