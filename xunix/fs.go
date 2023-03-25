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
