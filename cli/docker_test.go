package cli_test

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"

	"github.com/coder/envbox/cli"
	"github.com/coder/envbox/cli/clitest"
	"github.com/coder/envbox/xunix"
	"github.com/coder/envbox/xunix/xunixfake"
	dockertypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/pkg/namesgenerator"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
	testingexec "k8s.io/utils/exec/testing"
)

func TestDocker(t *testing.T) {
	t.Parallel()

	// Test the basic use case. This test doesn't test much beyond
	// establishing that the framework is returning a default
	// successful test. This makes it easier for individual tests
	// to test various criteria of the command without extensive
	// setup.
	t.Run("OK", func(t *testing.T) {
		t.Parallel()

		name := namesgenerator.GetRandomName(1)
		ctx, cmd := clitest.New(t, "docker",
			"--image=ubuntu",
			"--username=root",
			fmt.Sprintf("--container-name=%s", name),
			"--agent-token=hi",
		)

		err := cmd.ExecuteContext(ctx)
		require.NoError(t, err)
	})

	// Test that dockerd is configured correctly.
	t.Run("DockerdConfigured", func(t *testing.T) {
		t.Parallel()

		var (
			name       = namesgenerator.GetRandomName(1)
			nl         = clitest.GetNetLink(t)
			bridgeCIDR = "172.31.0.129/30"
		)

		ctx, cmd := clitest.New(t, "docker",
			"--image=ubuntu",
			"--username=root",
			fmt.Sprintf("--container-name=%s", name),
			"--agent-token=hi",
			fmt.Sprintf("--bridge-cidr=%s", bridgeCIDR),
		)

		execer := clitest.Execer(ctx)
		execer.AddCommands(&xunixfake.FakeCmd{
			FakeCmd: &testingexec.FakeCmd{
				Argv: []string{
					"dockerd",
					"--debug",
					"--log-level=debug",
					fmt.Sprintf("--mtu=%d", nl.Attrs().MTU),
					"--userns-remap=coder",
					"--storage-driver=overlay2",
					fmt.Sprintf("--bip=%s", bridgeCIDR),
				},
			},
		})

		err := cmd.ExecuteContext(ctx)
		require.NoError(t, err)
		execer.AssertCommandsCalled(t)
	})

	// Test that the oom_score_adj of the envbox
	// process is set to an extremely undesirable
	// number for the OOM killer.
	t.Run("SetOOMScore", func(t *testing.T) {
		t.Parallel()

		name := namesgenerator.GetRandomName(1)
		ctx, cmd := clitest.New(t, "docker",
			"--image=ubuntu",
			"--username=root",
			fmt.Sprintf("--container-name=%s", name),
			"--agent-token=hi",
		)

		fs := clitest.FS(ctx)

		err := cmd.ExecuteContext(ctx)
		require.NoError(t, err)

		score, err := afero.ReadFile(fs, "/proc/self/oom_score_adj")
		require.NoError(t, err)
		require.Equal(t, []byte("-1000"), score)
	})

	// Test that user-provided env vars are passed through.
	// It is valid to specify a wildcard so that all matching
	// env vars are passed through.
	t.Run("PassesThroughEnvVars", func(t *testing.T) {
		t.Parallel()
		var (
			cntEnvs = []string{
				"FOO=bar",
				"CODER_VAR=baz",
				"bar=123",
				// Test that wildcard works.
				"KUBERNETES_*",
				"US_*",
			}

			osEnvs = append([]string{
				"USER=root",
				"USA=yay",
				"HOME=/root",
				"PATH=/usr/bin:/sbin:/bin",
				"KUBERNETES_SERVICE_HOST=10.0.0.1",
				"KUBERNETES_PORT=tcp://10.0.0.1:443",
				"KUBERNETES_PORT_443_TCP_PORT=443",
				// Don't include the wildcards.
			}, cntEnvs[:3]...)

			expectedEnvs = []string{
				"CODER_AGENT_TOKEN=hi",
				"FOO=bar",
				"CODER_VAR=baz",
				"bar=123",
				"KUBERNETES_SERVICE_HOST=10.0.0.1",
				"KUBERNETES_PORT=tcp://10.0.0.1:443",
				"KUBERNETES_PORT_443_TCP_PORT=443",
			}
		)

		name := namesgenerator.GetRandomName(1)
		ctx, cmd := clitest.New(t, "docker",
			"docker",
			"--image=ubuntu",
			"--username=root",
			fmt.Sprintf("--container-name=%s", name),
			"--agent-token=hi",
			fmt.Sprintf("--envs=%s", strings.Join(cntEnvs, ",")),
		)

		ctx = xunix.WithEnvironFn(ctx, func() []string { return osEnvs })

		client := clitest.DockerClient(t, ctx)
		var called bool
		client.ContainerCreateFn = func(_ context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, _ *v1.Platform, containerName string) (container.ContainerCreateCreatedBody, error) {
			if containerName == "workspace_cvm" {
				called = true
				require.Equal(t, expectedEnvs, config.Env)
			}
			return container.ContainerCreateCreatedBody{}, nil
		}

		err := cmd.ExecuteContext(ctx)
		require.NoError(t, err)
		require.True(t, called, "create function was not called")
	})

	// Test that we parse mounts correctly.
	t.Run("Mounts", func(t *testing.T) {
		t.Parallel()

		var (
			userMounts     = []string{"/home/coder:/home/coder", "/etc/hosts:/etc/hosts:ro", "/etc/hostname:/idc/where:ro", "/usr/src:/a/b/c"}
			name           = namesgenerator.GetRandomName(1)
			expectedMounts = append([]string{"/var/lib/coder/docker:/var/lib/docker", "/var/lib/coder/containers:/var/lib/containers"}, userMounts...)
		)
		ctx, cmd := clitest.New(t, "docker",
			"docker",
			"--image=ubuntu",
			"--username=root",
			fmt.Sprintf("--container-name=%s", name),
			"--agent-token=hi",
			fmt.Sprintf("--mounts=%s", strings.Join(userMounts, ",")),
		)

		var (
			client = clitest.DockerClient(t, ctx)
			fs     = clitest.FS(ctx)
		)

		for _, mount := range userMounts {
			src := strings.Split(mount, ":")[0]

			err := afero.WriteFile(fs, src, []byte("hi"), 0o777)
			require.NoError(t, err)
		}

		// Set the exec response from inspecting the image to some ID
		// greater than 0.
		client.ContainerExecAttachFn = func(_ context.Context, execID string, config dockertypes.ExecStartCheck) (dockertypes.HijackedResponse, error) {
			return dockertypes.HijackedResponse{
				Reader: bufio.NewReader(strings.NewReader("root:x:1001:1001:root:/root:/bin/bash")),
				Conn:   &net.IPConn{},
			}, nil
		}

		var called bool
		client.ContainerCreateFn = func(_ context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, _ *v1.Platform, containerName string) (container.ContainerCreateCreatedBody, error) {
			if containerName == "workspace_cvm" {
				called = true
				require.Equal(t, expectedMounts, hostConfig.Binds)
			}

			return container.ContainerCreateCreatedBody{}, nil
		}

		err := cmd.ExecuteContext(ctx)
		require.NoError(t, err)
		require.True(t, called, "container create fn not called")

		fi, err := fs.Stat("/home/coder")
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0o755), fi.Mode().Perm())
		// Check that we're calling chown and shifting the ID.
		owner, ok := fs.GetFileOwner("/home/coder")
		require.True(t, ok)
		require.Equal(t, cli.UserNamespaceOffset+1001, owner.UID)
		require.Equal(t, cli.UserNamespaceOffset+1001, owner.GID)
	})

	// Test that we remount /sys once we pull the image so that
	// sysbox can use it properly.
	t.Run("RemountSysfs", func(t *testing.T) {
		t.Parallel()

		name := namesgenerator.GetRandomName(1)
		ctx, cmd := clitest.New(t, "docker",
			"--image=ubuntu",
			"--username=root",
			fmt.Sprintf("--container-name=%s", name),
			"--agent-token=hi",
		)

		mounter := clitest.Mounter(ctx)

		err := cmd.ExecuteContext(ctx)
		require.NoError(t, err)

		actions := mounter.GetLog()
		require.Len(t, actions, 1)
		action := actions[0]
		require.Equal(t, "mount", action.Action)
		require.Equal(t, "", action.FSType)
		require.Equal(t, "/sys", action.Source)
		require.Equal(t, "/sys", action.Target)
	})

	// Test that devices are created and passed through to the docker
	// daemon.
	t.Run("Devices", func(t *testing.T) {
		t.Parallel()

		name := namesgenerator.GetRandomName(1)
		ctx, cmd := clitest.New(t, "docker",
			"docker",
			"--image=ubuntu",
			"--username=root",
			fmt.Sprintf("--container-name=%s", name),
			"--agent-token=hi",
			"--add-tun",
			"--add-fuse",
		)

		var (
			client          = clitest.DockerClient(t, ctx)
			fs              = clitest.FS(ctx)
			expectedDevices = []container.DeviceMapping{
				{
					PathOnHost:        cli.OuterTUNPath,
					PathInContainer:   cli.InnerTUNPath,
					CgroupPermissions: "rwm",
				},
				{
					PathOnHost:        cli.OuterFUSEPath,
					PathInContainer:   cli.InnerFUSEPath,
					CgroupPermissions: "rwm",
				},
			}
		)

		var called bool
		client.ContainerCreateFn = func(_ context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, _ *v1.Platform, containerName string) (container.ContainerCreateCreatedBody, error) {
			if containerName == "workspace_cvm" {
				called = true
				require.Equal(t, expectedDevices, hostConfig.Devices)
			}

			return container.ContainerCreateCreatedBody{}, nil
		}

		err := cmd.ExecuteContext(ctx)
		require.NoError(t, err)
		require.True(t, called, "container create fn not called")

		// Check that we're calling chown and shifting the ID to
		// it maps to root of the inner container.
		owner, ok := fs.GetFileOwner(cli.OuterFUSEPath)
		require.True(t, ok)
		require.Equal(t, cli.UserNamespaceOffset, owner.UID)
		require.Equal(t, cli.UserNamespaceOffset, owner.GID)

		owner, ok = fs.GetFileOwner(cli.OuterTUNPath)
		require.True(t, ok)
		require.Equal(t, cli.UserNamespaceOffset, owner.UID)
		require.Equal(t, cli.UserNamespaceOffset, owner.GID)
	})

	// Tests that 'sleep infinity' is used if /sbin/init
	// isn't detected.
	t.Run("NoInit", func(t *testing.T) {
		t.Parallel()

		name := namesgenerator.GetRandomName(1)
		ctx, cmd := clitest.New(t, "docker",
			"docker",
			"--image=ubuntu",
			"--username=root",
			fmt.Sprintf("--container-name=%s", name),
			"--agent-token=hi",
			"--add-tun",
			"--add-fuse",
		)

		var (
			client     = clitest.DockerClient(t, ctx)
			statExecID = "hi"
		)

		client.ContainerExecCreateFn = func(_ context.Context, container string, config dockertypes.ExecConfig) (dockertypes.IDResponse, error) {
			if config.Cmd[0] == "stat" {
				return dockertypes.IDResponse{
					ID: statExecID,
				}, nil
			}
			return dockertypes.IDResponse{}, nil
		}

		// Set the exec response from inspecting the image to some ID
		// greater than 0.
		client.ContainerExecInspectFn = func(_ context.Context, execID string) (dockertypes.ContainerExecInspect, error) {
			if execID == statExecID {
				return dockertypes.ContainerExecInspect{ExitCode: 1}, nil
			}

			return dockertypes.ContainerExecInspect{}, nil
		}

		var called bool
		client.ContainerCreateFn = func(_ context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, _ *v1.Platform, containerName string) (container.ContainerCreateCreatedBody, error) {
			if containerName == "workspace_cvm" {
				called = true
				require.Equal(t, []string{"sleep", "infinity"}, []string(config.Entrypoint))
			}

			return container.ContainerCreateCreatedBody{}, nil
		}

		err := cmd.ExecuteContext(ctx)
		require.NoError(t, err)
		require.True(t, called, "container create fn not called")
	})
}
