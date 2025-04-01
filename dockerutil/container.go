package dockerutil

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/spf13/afero"
	"golang.org/x/xerrors"

	"cdr.dev/slog"
	"github.com/coder/envbox/xunix"
	"github.com/coder/retry"
)

const (
	runtime = "sysbox-runc"
	// Default CPU period for containers.
	DefaultCPUPeriod uint64 = 1e5
)

type ContainerConfig struct {
	Log        slog.Logger
	Mounts     []xunix.Mount
	Devices    []container.DeviceMapping
	Envs       []string
	Name       string
	Image      string
	WorkingDir string
	Hostname   string
	// HasInit dictates whether the entrypoint of the container is /sbin/init
	// or 'sleep infinity'.
	HasInit     bool
	CPUs        int64
	MemoryLimit int64
}

// CreateContainer creates a sysbox-runc container.
func CreateContainer(ctx context.Context, client Client, conf *ContainerConfig) (string, error) {
	host := &container.HostConfig{
		Runtime:    runtime,
		AutoRemove: true,
		Resources: container.Resources{
			Devices: conf.Devices,
			// Set resources for the inner container.
			// This is important for processes inside the container to know what they
			// have to work with.
			// TODO: Sysbox does not copy cpu.cfs_{period,quota}_us into syscont-cgroup-root cgroup.
			// These will not be visible inside the child container.
			// See: https://github.com/nestybox/sysbox/issues/582
			CPUPeriod: int64(DefaultCPUPeriod),
			CPUQuota:  conf.CPUs * int64(DefaultCPUPeriod),
			Memory:    conf.MemoryLimit,
		},
		ExtraHosts: []string{"host.docker.internal:host-gateway"},
		Binds:      generateBindMounts(conf.Mounts),
	}

	entrypoint := []string{"sleep", "infinity"}
	if conf.HasInit {
		entrypoint = []string{"/sbin/init"}
	}

	if conf.Hostname == "" {
		conf.Hostname = conf.Name
	}

	cnt := &container.Config{
		Image:      conf.Image,
		Entrypoint: entrypoint,
		Cmd:        []string{},
		Env:        conf.Envs,
		Hostname:   conf.Hostname,
		WorkingDir: conf.WorkingDir,
		Tty:        false,
		User:       "root",
	}

	c, err := client.ContainerCreate(ctx, cnt, host, nil, nil, conf.Name)
	if err != nil {
		return "", xerrors.Errorf("create container: %w", err)
	}
	return c.ID, nil
}

type BootstrapConfig struct {
	ContainerID string
	User        string
	Script      string
	Env         []string
	Detach      bool
	StdOutErr   io.Writer
}

// BoostrapContainer runs a script inside the container as the provided user.
// If conf.Script is empty then it is a noop.
func BootstrapContainer(ctx context.Context, client Client, conf BootstrapConfig) error {
	if conf.Script == "" {
		return nil
	}

	var err error
	for r, n := retry.New(time.Second, time.Second*2), 0; r.Wait(ctx) && n < 10; n++ {
		var out []byte
		out, err = ExecContainer(ctx, client, ExecConfig{
			ContainerID: conf.ContainerID,
			User:        conf.User,
			Cmd:         "/bin/sh",
			Args:        []string{"-s"},
			Stdin:       strings.NewReader(conf.Script),
			Env:         conf.Env,
			StdOutErr:   conf.StdOutErr,
			Detach:      conf.Detach,
		})
		if err != nil {
			err = xerrors.Errorf("boostrap container (%s): %w", out, err)
			continue
		}
		break
	}

	if err != nil {
		return xerrors.Errorf("timed out boostrapping container: %w", err)
	}

	return nil
}

// SetContainerQuota writes a quota to its correct location for the inner container.
// HACK: until https://github.com/nestybox/sysbox/issues/582 is resolved, we need to copy
// the CPU quota and period from the outer container to the inner container to ensure
// that applications inside the container know how much CPU they have to work with.
//
// For cgroupv2:
// - /sys/fs/cgroup/<subpath>/init.scope/cpu.max
//
// For cgroupv1:
// - /sys/fs/cgroup/cpu,cpuacct/<subpath>/syscont-cgroup-root/cpu.cfs_quota_us
// - /sys/fs/cgroup/cpu,cpuacct/<subpath>/syscont-cgroup-root/cpu.cfs_period_us
func SetContainerQuota(ctx context.Context, containerID string, quota xunix.CPUQuota) error {
	switch quota.CGroup {
	case xunix.CGroupV2:
		return setContainerQuotaCGroupV2(ctx, containerID, quota)
	case xunix.CGroupV1:
		return setContainerQuotaCGroupV1(ctx, containerID, quota)
	default:
		return xerrors.Errorf("Unknown cgroup %d", quota.CGroup)
	}
}

func setContainerQuotaCGroupV2(ctx context.Context, containerID string, quota xunix.CPUQuota) error {
	var (
		fs         = xunix.GetFS(ctx)
		cgroupBase = fmt.Sprintf("/sys/fs/cgroup/docker/%s/init.scope/", containerID)
	)

	var content string
	if quota.Quota < 0 {
		content = fmt.Sprintf("max %d\n", quota.Period)
	} else {
		content = fmt.Sprintf("%d %d\n", quota.Quota, quota.Period)
	}

	err := afero.WriteFile(fs, filepath.Join(cgroupBase, "cpu.max"), []byte(content), 0o644)
	if err != nil {
		return xerrors.Errorf("write cpu.max to inner container cgroup: %w", err)
	}

	return nil
}

func setContainerQuotaCGroupV1(ctx context.Context, containerID string, quota xunix.CPUQuota) error {
	var (
		fs         = xunix.GetFS(ctx)
		cgroupBase = fmt.Sprintf("/sys/fs/cgroup/cpu,cpuacct/docker/%s/syscont-cgroup-root/", containerID)
	)

	err := afero.WriteFile(fs, filepath.Join(cgroupBase, "cpu.cfs_period_us"), []byte(strconv.Itoa(quota.Period)), 0o644)
	if err != nil {
		return xerrors.Errorf("write cpu.cfs_period_us to inner container cgroup: %w", err)
	}

	err = afero.WriteFile(fs, filepath.Join(cgroupBase, "cpu.cfs_quota_us"), []byte(strconv.Itoa(quota.Quota)), 0o644)
	if err != nil {
		return xerrors.Errorf("write cpu.cfs_quota_us to inner container cgroup: %w", err)
	}

	return nil
}

func generateBindMounts(mounts []xunix.Mount) []string {
	binds := make([]string, 0, len(mounts))
	for _, mount := range mounts {
		bind := fmt.Sprintf("%s:%s", mount.Source, mount.Mountpoint)
		if mount.ReadOnly {
			bind += ":ro"
		}
		binds = append(binds, bind)
	}

	return binds
}
