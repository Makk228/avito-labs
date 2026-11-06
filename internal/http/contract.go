//go:generate go run go.uber.org/mock/mockgen -source contract.go -destination mocks_test.go -package http_test

package http

import (
	"context"
	"net/http"

	api "github.com/Makk228/avito-labs/internal/generated"
)

type createHandler interface {
	Handle(http.ResponseWriter, *http.Request, api.CreateTripParams)
}
type tripHandler interface {
	Handle(http.ResponseWriter, *http.Request, api.TripId)
}
type pinger interface{ Ping(context.Context) error }
