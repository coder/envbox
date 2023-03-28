package xunix

import (
	"context"
	"io/fs"
	"os"

	"github.com/spf13/afero"
	"golang.org/x/sys/unix"
)

type FS interface {
	afero.Fs
	Mknod(path string, mode uint32, dev int) error
	LStat(path string) (fs.FileInfo, error)
	Readlink(path string) (string, error)
}

type fsKey struct{}

func WithFS(ctx context.Context, f FS) context.Context {
	return context.WithValue(ctx, fsKey{}, f)
}

func GetFS(ctx context.Context) FS {
	f := ctx.Value(fsKey{})
	if f == nil {
		return &osFS{&afero.OsFs{}}
	}

	//nolint we should panic if this isn't the case.
	return f.(FS)
}

type osFS struct {
	*afero.OsFs
}

func (*osFS) Mknod(path string, mode uint32, dev int) error {
	return unix.Mknod(path, mode, dev)
}

func (*osFS) LStat(path string) (fs.FileInfo, error) {
	return os.Lstat(path)
}

func (*osFS) Readlink(path string) (string, error) {
	return os.Readlink(path)
}
