//go:generate go run go.uber.org/mock/mockgen -source contract.go -destination mocks_test.go -package push

package push

import (
	"net/http"

	"github.com/google/uuid"
)

type client interface {
	Do(*http.Request) (*http.Response, error)
	CloseIdleConnections()
}
type idGenerator interface{ NewID() uuid.UUID }
