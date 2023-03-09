package xunix

import (
	"context"

	mount "k8s.io/mount-utils"
)

type mounterKey struct{}

func WithMounter(ctx context.Context, i mount.Interface) context.Context {
	return context.WithValue(ctx, mounterKey{}, i)
}

func Mounter(ctx context.Context) mount.Interface {
	m := ctx.Value(mounterKey{})
	if m == nil {
		return mount.New("/bin/mount")
	}

	return m.(mount.Interface)
}

type Mount struct {
	Source     string
	Mountpoint string
	ReadOnly   bool
}

func MountFS(ctx context.Context, source, mountpoint, fstype string, options ...string) error {
	return Mounter(ctx).
		Mount(source, mountpoint, fstype, options)
}
