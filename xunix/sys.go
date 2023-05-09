package xunix

import (
	"bytes"
	"context"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/afero"
	"golang.org/x/xerrors"
)

type CPUQuota struct {
	Quota  int
	Period int
	CGroup CGroup
}

const (
	CPUPeriodPathCGroupV1 = "/sys/fs/cgroup/cpu,cpuacct/cpu.cfs_period_us"
	CPUQuotaPathCGroupV1  = "/sys/fs/cgroup/cpu,cpuacct/cpu.cfs_quota_us"
	CPUMaxPathCGroupV2    = "/sys/fs/cgroup/"
)

type CGroup string

const (
	CGroupV1 CGroup = "cgroupv1"
	CGroupV2 CGroup = "cgroupv2"
)

func ReadCPUQuota(ctx context.Context) (CPUQuota, error) {
	// Try first to read the cgroupv2 version.
	if quota, err := readCPUQuotaCGroupV2(ctx); err == nil {
		// TODO: log this somewhere
		return quota, nil
	}

	// Fall back to cgroupv1
	return readCPUQuotaCGroupV1(ctx)
}

func readCPUQuotaCGroupV2(ctx context.Context) (CPUQuota, error) {
	fs := GetFS(ctx)
	self, err := ReadCGroupSelf(ctx, "") // TODO: should we just go from the first line?
	if err != nil {
		return CPUQuota{}, xerrors.Errorf("")
	}

	maxStr, err := afero.ReadFile(fs, filepath.Join("/sys/fs/cgroup/", self, "cpu.max"))
	if err != nil {
		return CPUQuota{}, xerrors.Errorf("read cpu.max outside container: %w", err)
	}

	list := strings.Split(string(maxStr), " ")
	if len(list) != 2 {
		return CPUQuota{}, xerrors.Errorf("expected cpu.max to have exactly two entries, got: %s", string(maxStr))
	}

	var quota int
	var period int

	if list[0] == "max" {
		quota = -1
	} else {
		quota, err = strconv.Atoi(list[0])
		if err != nil {
			return CPUQuota{}, xerrors.Errorf("quota %s not an int: %w", list[0], err)
		}
	}

	period, err = strconv.Atoi(list[1])
	if err != nil {
		return CPUQuota{}, xerrors.Errorf("period %s not an int: %w", list[1], err)
	}

	return CPUQuota{Quota: quota, Period: period, CGroup: CGroupV2}, nil
}

func readCPUQuotaCGroupV1(ctx context.Context) (CPUQuota, error) {
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

// readCGroup reads the lines of /proc/self/cgroup where the third field
// (separated by `:`) of the line starts with prefix.
// If prefix is empty, we just check the first line.
// pid is a string so you can use `self` to look at your own cgroup.
func ReadCGroupSelf(ctx context.Context, prefix string) (string, error) {
	fs := GetFS(ctx)
	raw, err := afero.ReadFile(fs, "/proc/self/cgroup")
	if err != nil {
		return "", xerrors.Errorf("read /proc/self/cgroup: %w", err)
	}

	lines := bytes.Split(raw, []byte("\n"))
	if len(lines) == 0 {
		return "", xerrors.Errorf("unexpected content of /proc/self/cgroup: %s", string(raw))
	}

	// Loop through all the lines
	for _, line := range lines {
		fields := bytes.Split(line, []byte(":"))
		if len(fields) != 3 {
			return "", xerrors.Errorf("expected 3 fields in last line of /proc/self/cgroup: %s", string(raw))
		}

		// An empty prefix will always match.
		if !bytes.HasPrefix(fields[2], []byte(prefix)) {
			continue
		}

		return string(fields[2]), nil
	}

	return "", xerrors.Errorf("no cgroup found with prefix %s", prefix)
}
