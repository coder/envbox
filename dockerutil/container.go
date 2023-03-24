package dockerutil

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	dockerclient "github.com/docker/docker/client"
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

type DockerClient interface {
	dockerclient.SystemAPIClient
	dockerclient.ContainerAPIClient
	dockerclient.ImageAPIClient
}

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
func CreateContainer(ctx context.Context, client DockerClient, conf *ContainerConfig) (string, error) {
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
}

// BoostrapContainer runs a script inside the container as the provided user.
// If conf.Script is empty then it is a noop.
func BootstrapContainer(ctx context.Context, client DockerClient, conf BootstrapConfig) error {
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

// copyCPUQuotaToInnerCGroup writes the contents of the following files to
// their corresponding locations under cgroupBase:
// - /sys/fs/cgroup/cpu,cpuacct/cpu.cfs_period_us
// - /sys/fs/cgroup/cpu,cpuacct/cpu.cfs_quota_us
//
// HACK: until https://github.com/nestybox/sysbox/issues/582 is resolved, we need to set cfs_quota_us
// and cfs_period_us inside the container to ensure that applications inside the container know how much
// CPU they have to work with.
func SetContainerCPUQuota(ctx context.Context, containerID string, quota, period int) error {
	var (
		fs         = xunix.GetFS(ctx)
		cgroupBase = fmt.Sprintf("/sys/fs/cgroup/cpu,cpuacct/docker/%s/syscont-cgroup-root/", containerID)
	)

	err := afero.WriteFile(fs, filepath.Join(cgroupBase, "cpu.cfs_period_us"), []byte(strconv.Itoa(period)), 0o644)
	if err != nil {
		return xerrors.Errorf("write cpu.cfs_period_us to inner container cgroup: %w", err)
	}

	err = afero.WriteFile(fs, filepath.Join(cgroupBase, "cpu.cfs_quota_us"), []byte(strconv.Itoa(quota)), 0o644)
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
