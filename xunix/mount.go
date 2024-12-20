package xunix

import (
	"context"
	"fmt"

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

	//nolint we should panic if this isn't the case.
	return m.(mount.Interface)
}

type Mount struct {
	Source     string
	Mountpoint string
	ReadOnly   bool
}

// String returns the bind mount string for the mount.
func (m Mount) String() string {
	bind := fmt.Sprintf("%s:%s", m.Source, m.Mountpoint)
	if m.ReadOnly {
		bind += ":ro"
	}
	return bind
}

func MountFS(ctx context.Context, source, mountpoint, fstype string, options ...string) error {
	return Mounter(ctx).
		Mount(source, mountpoint, fstype, options)
}
