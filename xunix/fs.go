package xunix

import (
	"context"

	"github.com/spf13/afero"
	"golang.org/x/sys/unix"
)

type fsKey struct{}

func WithFS(ctx context.Context, fs FS) context.Context {
	return context.WithValue(ctx, fsKey{}, fs)
}

func GetFS(ctx context.Context) FS {
	fs := ctx.Value(fsKey{})
	if fs == nil {
		return &osFS{&afero.OsFs{}}
	}

	//nolint we should panic if this isn't the case.
	return fs.(FS)
}

type FS interface {
	afero.Fs
	Mknod(path string, mode uint32, dev int) error
}

type osFS struct {
	*afero.OsFs
}

func (*osFS) Mknod(path string, mode uint32, dev int) error {
	return unix.Mknod(path, mode, dev)
}
