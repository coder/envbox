package xunix

import (
	"bytes"
	"context"
	"strconv"

	"github.com/spf13/afero"
	"golang.org/x/xerrors"
)

type CPUQuota struct {
	Quota  int
	Period int
}

const (
	CPUPeriodPath = "/sys/fs/cgroup/cpu,cpuacct/cpu.cfs_period_us"
	CPUQuotaPath  = "/sys/fs/cgroup/cpu,cpuacct/cpu.cfs_quota_us"
)

func ReadCPUQuota(ctx context.Context) (CPUQuota, error) {
	fs := GetFS(ctx)
	periodStr, err := afero.ReadFile(fs, "/sys/fs/cgroup/cpu,cpuacct/cpu.cfs_period_us")
	if err != nil {
		return CPUQuota{}, xerrors.Errorf("read cpu.cfs_period_us outside container: %w", err)
	}

	quotaStr, err := afero.ReadFile(fs, "/sys/fs/cgroup/cpu,cpuacct/cpu.cfs_quota_us")
	if err != nil {
		return CPUQuota{}, xerrors.Errorf("read cpu.cfs_quota_us outside container: %w", err)
	}

	period, err := strconv.Atoi(string(bytes.TrimSpace(periodStr)))
	if err != nil {
		return CPUQuota{}, xerrors.Errorf("period %s not an int: %w", periodStr, err)
	}

	quota, err := strconv.Atoi(string(bytes.TrimSpace(quotaStr)))
	if err != nil {
		return CPUQuota{}, xerrors.Errorf("quota %s not an int: %w", quotaStr, err)
	}

	return CPUQuota{
		Quota:  quota,
		Period: period,
	}, nil
}
