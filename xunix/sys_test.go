package xunix_test

import (
	"context"
	"strconv"
	"testing"

	"github.com/coder/envbox/xunix"
	"github.com/coder/envbox/xunix/xunixfake"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestReadCPUQuota(t *testing.T) {
	t.Parallel()

	t.Run("OK", func(t *testing.T) {
		var (
			fs  = &xunixfake.MemFS{MemMapFs: &afero.MemMapFs{}}
			ctx = xunix.WithFS(context.Background(), fs)
		)

		const (
			period     = 1234
			quota      = 5678
			periodPath = "/sys/fs/cgroup/cpu,cpuacct/cpu.cfs_period_us"
			quotaPath  = "/sys/fs/cgroup/cpu,cpuacct/cpu.cfs_quota_us"
		)

		err := afero.WriteFile(fs, periodPath, []byte(strconv.Itoa(period)), 0o644)
		require.NoError(t, err)

		err = afero.WriteFile(fs, quotaPath, []byte(strconv.Itoa(quota)), 0o644)
		require.NoError(t, err)

		cpuQuota, err := xunix.ReadCPUQuota(ctx)
		require.NoError(t, err)

		require.Equal(t, period, cpuQuota.Period)
		require.Equal(t, quota, cpuQuota.Quota)
	})
}
