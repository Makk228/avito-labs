// Package requeststate shares request error information within the transport.
package requeststate

import "context"

type (
	key   struct{}
	State struct{ Err error }
)

func With(ctx context.Context) context.Context { return context.WithValue(ctx, key{}, &State{}) }
func Record(ctx context.Context, err error) {
	if state, ok := ctx.Value(key{}).(*State); ok {
		state.Err = err
	}
}

func Error(ctx context.Context) error {
	if state, ok := ctx.Value(key{}).(*State); ok {
		return state.Err
	}
	return nil
}
