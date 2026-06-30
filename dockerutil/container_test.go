package dockerutil_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/coder/envbox/dockerutil"
	"github.com/coder/envbox/dockerutil/dockerfake"
	"github.com/coder/envbox/xunix"
	"github.com/coder/envbox/xunix/xunixfake"
)

func TestCreateContainerStopSignal(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name               string
		hasInit            bool
		expectedEntrypoint []string
		expectedStopSignal string
	}{
		{
			name:               "SystemdInit",
			hasInit:            true,
			expectedEntrypoint: []string{"/sbin/init"},
			expectedStopSignal: "SIGRTMIN+3",
		},
		{
			name:               "NoInit",
			hasInit:            false,
			expectedEntrypoint: []string{"sleep", "infinity"},
			expectedStopSignal: "",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var created *container.Config
			client := dockerfake.MockClient{
				ContainerCreateFn: func(_ context.Context, config *container.Config, _ *container.HostConfig, _ *network.NetworkingConfig, _ *v1.Platform, _ string) (container.CreateResponse, error) {
					created = config
					return container.CreateResponse{ID: "container-id"}, nil
				},
			}

			id, err := dockerutil.CreateContainer(context.Background(), client, &dockerutil.ContainerConfig{
				Name:    "workspace",
				Image:   "ubuntu",
				HasInit: tc.hasInit,
			})
			require.NoError(t, err)
			require.Equal(t, "container-id", id)
			require.NotNil(t, created)
			require.Equal(t, tc.expectedEntrypoint, []string(created.Entrypoint))
			require.Equal(t, tc.expectedStopSignal, created.StopSignal)
		})
	}
}

func TestSetContainerQuota(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		Name        string
		Quota       xunix.CPUQuota
		ContainerID string
		ExpectedFS  map[string]string
		Error       string
	}{
		{
			Name: "CGroupV1",
			Quota: xunix.CPUQuota{
				Quota:  150000,
				Period: 100000,
				CGroup: xunix.CGroupV1,
			},
			ContainerID: "dummy",
			ExpectedFS: map[string]string{
				"/sys/fs/cgroup/cpu,cpuacct/docker/dummy/syscont-cgroup-root/cpu.cfs_quota_us":  "150000",
				"/sys/fs/cgroup/cpu,cpuacct/docker/dummy/syscont-cgroup-root/cpu.cfs_period_us": "100000",
			},
		},
		{
			Name: "CGroupV2",
			Quota: xunix.CPUQuota{
				Quota:  150000,
				Period: 100000,
				CGroup: xunix.CGroupV2,
			},
			ContainerID: "dummy",
			ExpectedFS: map[string]string{
				"/sys/fs/cgroup/docker/dummy/init.scope/cpu.max": "150000 100000",
			},
		},
		{
			Name: "CGroupV2Max",
			Quota: xunix.CPUQuota{
				Quota:  -1,
				Period: 100000,
				CGroup: xunix.CGroupV2,
			},
			ContainerID: "dummy",
			ExpectedFS: map[string]string{
				"/sys/fs/cgroup/docker/dummy/init.scope/cpu.max": "max 100000",
			},
		},
	} {
		tc := tc
		t.Run(tc.Name, func(t *testing.T) {
			t.Parallel()
			tmpfs := &xunixfake.MemFS{MemMapFs: &afero.MemMapFs{}}
			ctx := xunix.WithFS(context.Background(), tmpfs)
			err := dockerutil.SetContainerQuota(ctx, tc.ContainerID, tc.Quota)
			if tc.Error == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.Error)
			}
			for path, content := range tc.ExpectedFS {
				actualContent, err := afero.ReadFile(tmpfs, path)
				require.NoError(t, err)
				require.Equal(t, content, string(bytes.TrimSpace(actualContent)))
			}
		})
	}
}
