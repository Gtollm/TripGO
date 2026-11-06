package requeststate

import (
	"context"
	"time"
)

type key struct{}

type State struct {
	Context   context.Context
	Started   time.Time
	Route     string
	Status    func() int
	Committed func() bool
	Err       error
	ErrorType string
	Stack     []byte
	Complete  []func()
}

func With(ctx context.Context, state *State) context.Context {
	return context.WithValue(ctx, key{}, state)
}

func From(ctx context.Context) *State {
	state, _ := ctx.Value(key{}).(*State)
	return state
}

func Fail(ctx context.Context, err error, kind string) {
	if state := From(ctx); state != nil {
		state.Err = err
		state.ErrorType = kind
	}
}
