package xunix_test

import (
	"context"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"cdr.dev/slog/sloggers/slogtest"

	"github.com/coder/envbox/xunix"
	"github.com/coder/envbox/xunix/xunixfake"
)

func TestReadCPUQuota(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		Name     string
		Subpath  string
		FS       map[string]string
		Expected xunix.CPUQuota
		Error    string
	}{
		{
			Name:    "CGroupV1",
			Subpath: "docker/dummy",
			FS: map[string]string{
				xunix.CPUQuotaPathCGroupV1:  "150000\n",
				xunix.CPUPeriodPathCGroupV1: "100000\n",
			},
			Expected: xunix.CPUQuota{Quota: 150000, Period: 100000, CGroup: xunix.CGroupV1},
		},
		{
			Name:    "CGroupV1_Invalid",
			Subpath: "docker/dummy",
			FS: map[string]string{
				xunix.CPUQuotaPathCGroupV1:  "100000\n",
				xunix.CPUPeriodPathCGroupV1: "invalid\n",
			},
			Error: `period invalid not an int`,
		},
		{
			Name:    "CGroupV2",
			Subpath: "docker/dummy",
			FS: map[string]string{
				"/proc/self/cgroup":                             "0::/kubepods/pod/container\n",
				"/sys/fs/cgroup/kubepods/pod/container/cpu.max": "150000 100000\n",
			},
			Expected: xunix.CPUQuota{Quota: 150000, Period: 100000, CGroup: xunix.CGroupV2},
		},
		{
			Name:    "CGroupV2_Max",
			Subpath: "docker/dummy",
			FS: map[string]string{
				"/proc/self/cgroup":                             "0::/kubepods/pod/container\n",
				"/sys/fs/cgroup/kubepods/pod/container/cpu.max": "max 100000\n",
			},
			Expected: xunix.CPUQuota{Quota: -1, Period: 100000, CGroup: xunix.CGroupV2},
		},
		{
			Name:  "Empty",
			FS:    map[string]string{},
			Error: "file does not exist",
		},
	} {
		tc := tc
		t.Run(tc.Name, func(t *testing.T) {
			t.Parallel()
			log := slogtest.Make(t, &slogtest.Options{IgnoreErrors: true})
			tmpfs := &xunixfake.MemFS{MemMapFs: &afero.MemMapFs{}}
			ctx := xunix.WithFS(context.Background(), tmpfs)
			for path, content := range tc.FS {
				require.NoError(t, afero.WriteFile(tmpfs, path, []byte(content), 0o644))
			}
			actual, err := xunix.ReadCPUQuota(ctx, log)
			if tc.Error == "" {
				require.NoError(t, err)
				require.Equal(t, tc.Expected, actual)
			} else {
				require.ErrorContains(t, err, tc.Error)
				require.Zero(t, actual)
			}
		})
	}
}
