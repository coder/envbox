package xunix_test

import (
	"context"
	"testing"

	"github.com/spf13/afero"
	"k8s.io/mount-utils"

	"github.com/coder/envbox/xunix"
	"github.com/coder/envbox/xunix/xunixfake"
)

func TestGPUEnvs(t *testing.T) {
	t.Parallel()

	t.Run("OK", func(t *testing.T) {
		t.Parallel()

		var (
		// fs  = &xunixfake.MemFS{MemMapFs: &afero.MemMapFs{}}
		// ctx = xunix.WithFS(context.Background(), fs)
		)

	})
}

func TestGPUs(t *testing.T) {
	t.Parallel()

	t.Run("OK", func(t *testing.T) {
		var (
			fs      = &xunixfake.MemFS{MemMapFs: &afero.MemMapFs{}}
			mounter = &mount.FakeMounter{}
		)

		ctx := xunix.WithFS(context.Background(), fs)
		ctx = xunix.WithMounter(ctx, mounter)

		mounter.MountPoints = []mount.MountPoint{
			{},
			{},
			{},
			{},
			{},
			{},
			{},
			{},
		}

	})
}
