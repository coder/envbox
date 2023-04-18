package xunix

import (
	"context"
	"fmt"
	"os"
)

type environKey struct{}

type EnvironFn func() []string

func WithEnvironFn(ctx context.Context, fn EnvironFn) context.Context {
	return context.WithValue(ctx, environKey{}, fn)
}

func Environ(ctx context.Context) []string {
	fn := ctx.Value(environKey{})
	if fn == nil {
		return os.Environ()
	}

	//nolint we should panic if this isn't the case.
	return fn.(EnvironFn)()
}

type Env struct {
	Name  string
	Value string
}

func (e Env) String() string {
	return fmt.Sprintf("%s=%s", e.Name, e.Value)
}

func MustLookupEnv(e string) string {
	env, ok := os.LookupEnv(e)
	if !ok {
		panic(fmt.Sprintf("%q env var not found", e))
	}
	return env
}
