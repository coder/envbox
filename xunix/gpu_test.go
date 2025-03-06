package xunix_test

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
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

			// fakeUsrLibFiles are files that we do not expect to be returned
			// bind mounts for.
			fakeUsrLibFiles = []string{
				filepath.Join(usrLibMountpoint, "libcurl-gnutls.so"),
				filepath.Join(usrLibMountpoint, "libglib.so"),
			}

			// allUsrLibFiles are all the files that should be written to the
			// "mounted" /usr/lib directory. It includes files that shouldn't
			// be returned.
			allUsrLibFiles = append(expectedUsrLibFiles, fakeUsrLibFiles...)
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

		for _, file := range allUsrLibFiles {
			_, err = fs.Create(file)
			require.NoError(t, err)
		}
		for _, mp := range mounter.MountPoints {
			_, err = fs.Create(mp.Path)
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
		for _, file := range fakeUsrLibFiles {
			require.NotContains(t, binds, mount.MountPoint{
				Path: file,
				Opts: []string{"ro"},
			})
		}
	})
}

func Test_SameDirSymlinks(t *testing.T) {
	t.Parallel()

	var (
		ctx = context.Background()
		// We need to test with a real filesystem as the fake one doesn't
		// support creating symlinks.
		tmpDir = t.TempDir()
		// We do test with the interface though!
		afs = xunix.GetFS(ctx)
	)

	// Create some files in the temporary directory.
	_, err := os.Create(filepath.Join(tmpDir, "file1.real"))
	require.NoError(t, err, "create file")
	_, err = os.Create(filepath.Join(tmpDir, "file2.real"))
	require.NoError(t, err, "create file2")
	_, err = os.Create(filepath.Join(tmpDir, "file3.real"))
	require.NoError(t, err, "create file3")
	_, err = os.Create(filepath.Join(tmpDir, "file4.real"))
	require.NoError(t, err, "create file4")

	// Create a symlink to the file in the temporary directory.
	// This needs to be done by the real os package.
	err = os.Symlink(filepath.Join(tmpDir, "file1.real"), filepath.Join(tmpDir, "file1.link1"))
	require.NoError(t, err, "create first symlink")

	// Create another symlink to the previous symlink.
	err = os.Symlink(filepath.Join(tmpDir, "file1.link1"), filepath.Join(tmpDir, "file1.link2"))
	require.NoError(t, err, "create second symlink")

	// Create a symlink to a file outside of the temporary directory.
	err = os.MkdirAll(filepath.Join(tmpDir, "dir"), 0o755)
	require.NoError(t, err, "create dir")
	// Create a symlink from file2 to inside the dir.
	err = os.Symlink(filepath.Join(tmpDir, "file2.real"), filepath.Join(tmpDir, "dir", "file2.link1"))
	require.NoError(t, err, "create dir symlink")

	// Create a symlink with a relative path. To do this, we need to
	// change the working directory to the temporary directory.
	oldWorkingDir, err := os.Getwd()
	require.NoError(t, err, "get working dir")
	// Change the working directory to the temporary directory.
	require.NoError(t, os.Chdir(tmpDir), "change working dir")
	err = os.Symlink(filepath.Join(tmpDir, "file4.real"), "file4.link1")
	require.NoError(t, err, "create relative symlink")
	// Change the working directory back to the original.
	require.NoError(t, os.Chdir(oldWorkingDir), "change working dir back")

	for _, tt := range []struct {
		name     string
		expected []string
	}{
		{
			// Two symlinks to the same file.
			name: "file1.real",
			expected: []string{
				filepath.Join(tmpDir, "file1.link1"),
				filepath.Join(tmpDir, "file1.link2"),
			},
		},
		{
			// Mid-way in the symlink chain.
			name: "file1.link1",
			expected: []string{
				filepath.Join(tmpDir, "file1.link2"),
			},
		},
		{
			// End of the symlink chain.
			name:     "file1.link2",
			expected: []string{},
		},
		{
			// Symlink to a file outside of the temporary directory.
			name:     "file2.real",
			expected: []string{},
		},
		{
			// No symlinks to this file.
			name:     "file3.real",
			expected: []string{},
		},
		{
			// One relative symlink.
			name:     "file4.real",
			expected: []string{filepath.Join(tmpDir, "file4.link1")},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fullPath := filepath.Join(tmpDir, tt.name)
			actual, err := xunix.SameDirSymlinks(afs, fullPath)
			require.NoError(t, err, "find symlink")
			sort.Strings(actual)
			assert.Equal(t, tt.expected, actual, "find symlinks %q", tt.name)
		})
	}
}
