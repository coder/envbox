package xunix_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
	"k8s.io/mount-utils"

	"cdr.dev/slog/sloggers/slogtest"

	"github.com/coder/envbox/xunix"
	"github.com/coder/envbox/xunix/xunixfake"
)

func TestGPUEnvs(t *testing.T) {
	t.Parallel()

	t.Run("OK", func(t *testing.T) {
		t.Parallel()

		ctx := xunix.WithEnvironFn(context.Background(), func() []string {
			return []string{
				"NVIDIA_TEST=1",
				"VULKAN_TEST=1",
				"LIBGL_TEST=1",
				"TEST_NVIDIA=1",
				"nvidia_test=1",
			}
		})

		envs := xunix.GPUEnvs(ctx)
		require.Contains(t, envs, "NVIDIA_TEST=1")
		require.Contains(t, envs, "TEST_NVIDIA=1")
		require.Contains(t, envs, "nvidia_test=1")
		require.NotContains(t, envs, "VULKAN_TEST=1")
		require.NotContains(t, envs, "LIBGL_TEST=1")
	})
}

func TestGPUs(t *testing.T) {
	t.Parallel()

	t.Run("OK", func(t *testing.T) {
		t.Parallel()

		var (
			fs               = &xunixfake.MemFS{MemMapFs: &afero.MemMapFs{}}
			mounter          = &mount.FakeMounter{}
			log              = slogtest.Make(t, nil)
			usrLibMountpoint = "/var/coder/usr/lib"
			// expectedUsrLibFiles are files that we expect to be returned bind mounts
			// for.
			expectedUsrLibFiles = []string{
				filepath.Join(usrLibMountpoint, "nvidia", "libglxserver_nvidia.so"),
				filepath.Join(usrLibMountpoint, "libnvidia-ml.so"),
				filepath.Join(usrLibMountpoint, "nvidia", "libglxserver_nvidia.so.1"),
			}

			// fakeUsrLibFiles are files that should be written to the "mounted"
			// /usr/lib directory. It includes files that shouldn't be returned.
			fakeUsrLibFiles = append([]string{
				filepath.Join(usrLibMountpoint, "libcurl-gnutls.so"),
			}, expectedUsrLibFiles...)
		)

		ctx := xunix.WithFS(context.Background(), fs)
		ctx = xunix.WithMounter(ctx, mounter)

		mounter.MountPoints = []mount.MountPoint{
			{
				Device: "/dev/sda1",
				Path:   "/usr/local/nvidia",
				Opts:   []string{"ro"},
			},
			{
				Device: "/dev/sda2",
				Path:   "/etc/hosts",
			},
			{
				Path: "/dev/nvidia0",
			},
			{
				Path: "/dev/nvidia1",
			},
		}

		err := fs.MkdirAll(filepath.Join(usrLibMountpoint, "nvidia"), 0o755)
		require.NoError(t, err)

		for _, file := range fakeUsrLibFiles {
			_, err = fs.Create(file)
			require.NoError(t, err)
		}

		devices, binds, err := xunix.GPUs(ctx, log, usrLibMountpoint)
		require.NoError(t, err)
		require.Len(t, devices, 2, "unexpected 2 nvidia devices")
		require.Len(t, binds, 4, "expected 4 nvidia binds")
		require.Contains(t, binds, mount.MountPoint{
			Device: "/dev/sda1",
			Path:   "/usr/local/nvidia",
			Opts:   []string{"ro"},
		})
		for _, file := range expectedUsrLibFiles {
			require.Contains(t, binds, mount.MountPoint{
				Path: file,
				Opts: []string{"ro"},
			})
		}
	})
}
