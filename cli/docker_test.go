package cli_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	dockertypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
	"k8s.io/mount-utils"
	testingexec "k8s.io/utils/exec/testing"

	"github.com/coder/envbox/cli"
	"github.com/coder/envbox/cli/clitest"
	"github.com/coder/envbox/dockerutil"
	"github.com/coder/envbox/xunix"
	"github.com/coder/envbox/xunix/xunixfake"
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

		ctx, cmd := clitest.New(t, "docker",
			"--image=ubuntu",
			"--username=root",
			"--agent-token=hi",
		)

		err := cmd.ExecuteContext(ctx)
		require.NoError(t, err)
	})

	// Test that dockerd is configured correctly.
	t.Run("DockerdConfigured", func(t *testing.T) {
		t.Parallel()

		var (
			nl         = clitest.GetNetLink(t)
			bridgeCIDR = "172.31.0.129/30"
		)

		ctx, cmd := clitest.New(t, "docker",
			"--image=ubuntu",
			"--username=root",
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

		ctx, cmd := clitest.New(t, "docker",
			"--image=ubuntu",
			"--username=root",
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
				"FOO",
				"CODER_VAR",
				"bar",
				// Test that wildcard works.
				"KUBERNETES_*",
				"US_*",
			}

			expectedEnvs = []string{
				"CODER_AGENT_TOKEN=hi",
				"FOO=bar",
				"CODER_VAR=baz",
				"bar=123",
				"KUBERNETES_SERVICE_HOST=10.0.0.1",
				"KUBERNETES_PORT=tcp://10.0.0.1:443",
				"KUBERNETES_PORT_443_TCP_PORT=443",
			}

			osEnvs = (append([]string{
				"USER=root",
				"USA=yay",
				"HOME=/root",
				"PATH=/usr/bin:/sbin:/bin",
				// Don't include the wildcards.
			}, expectedEnvs...))
		)

		ctx, cmd := clitest.New(t, "docker",
			"docker",
			"--image=ubuntu",
			"--username=root",
			"--agent-token=hi",
			fmt.Sprintf("--envs=%s", strings.Join(cntEnvs, ",")),
		)

		ctx = xunix.WithEnvironFn(ctx, func() []string { return osEnvs })

		client := clitest.DockerClient(t, ctx)
		var called bool
		client.ContainerCreateFn = func(_ context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, _ *v1.Platform, containerName string) (container.ContainerCreateCreatedBody, error) {
			if containerName == cli.InnerContainerName {
				called = true
				require.ElementsMatch(t, expectedEnvs, config.Env)
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
			expectedMounts = append([]string{"/var/lib/coder/docker:/var/lib/docker", "/var/lib/coder/containers:/var/lib/containers"}, userMounts...)
		)
		ctx, cmd := clitest.New(t, "docker",
			"docker",
			"--image=ubuntu",
			"--username=root",
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
			if containerName == cli.InnerContainerName {
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

		ctx, cmd := clitest.New(t, "docker",
			"--image=ubuntu",
			"--username=root",
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

		ctx, cmd := clitest.New(t, "docker",
			"docker",
			"--image=ubuntu",
			"--username=root",
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
			if containerName == cli.InnerContainerName {
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

		ctx, cmd := clitest.New(t, "docker",
			"docker",
			"--image=ubuntu",
			"--username=root",
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
			if containerName == cli.InnerContainerName {
				called = true
				require.Equal(t, []string{"sleep", "infinity"}, []string(config.Entrypoint))
			}

			return container.ContainerCreateCreatedBody{}, nil
		}

		err := cmd.ExecuteContext(ctx)
		require.NoError(t, err)
		require.True(t, called, "container create fn not called")
	})

	t.Run("DockerAuth", func(t *testing.T) {
		t.Parallel()

		ctx, cmd := clitest.New(t, "docker",
			"--image=ubuntu",
			"--username=root",
			"--agent-token=hi",
			fmt.Sprintf("--image-secret=%s", rawDockerAuth),
		)

		raw := []byte(`{"username":"_json_key","password":"{\"type\": \"service_account\", \"project_id\": \"some-test\", \"private_key_id\": \"blahblah\", \"private_key\": \"-----BEGIN PRIVATE KEY-----mykey-----END PRIVATE KEY-----\", \"client_email\": \"test@test.iam.gserviceaccount.com\", \"client_id\": \"123\", \"auth_uri\": \"https://accounts.google.com/o/oauth2/auth\", \"token_uri\": \"https://oauth2.googleapis.com/token\", \"auth_provider_x509_cert_url\": \"https://www.googleapis.com/oauth2/v1/certs\", \"client_x509_cert_url\": \"https://www.googleapis.com/robot/v1/metadata/x509/test.iam.gserviceaccount.com\" }","auth":"X2pzb25fa2V5OnsKCgkidHlwZSI6ICJzZXJ2aWNlX2FjY291bnQiLAoJInByb2plY3RfaWQiOiAic29tZS10ZXN0IiwKCSJwcml2YXRlX2tleV9pZCI6ICJibGFoYmxhaCIsCgkicHJpdmF0ZV9rZXkiOiAiLS0tLS1CRUdJTiBQUklWQVRFIEtFWS0tLS0tCm15a2V5LS0tLS1FTkQgUFJJVkFURSBLRVktLS0tLQoiLAoJImNsaWVudF9lbWFpbCI6ICJ0ZXN0QHRlc3QuaWFtLmdzZXJ2aWNlYWNjb3VudC5jb20iLAoJImNsaWVudF9pZCI6ICIxMjMiLAoJImF1dGhfdXJpIjogImh0dHBzOi8vYWNjb3VudHMuZ29vZ2xlLmNvbS9vL29hdXRoMi9hdXRoIiwKCSJ0b2tlbl91cmkiOiAiaHR0cHM6Ly9vYXV0aDIuZ29vZ2xlYXBpcy5jb20vdG9rZW4iLAoJImF1dGhfcHJvdmlkZXJfeDUwOV9jZXJ0X3VybCI6ICJodHRwczovL3d3dy5nb29nbGVhcGlzLmNvbS9vYXV0aDIvdjEvY2VydHMiLAoJImNsaWVudF94NTA5X2NlcnRfdXJsIjogImh0dHBzOi8vd3d3Lmdvb2dsZWFwaXMuY29tL3JvYm90L3YxL21ldGFkYXRhL3g1MDkvdGVzdC5pYW0uZ3NlcnZpY2VhY2NvdW50LmNvbSIKfQo=","email":"test@test.iam.gserviceaccount.com"}`)
		authB64 := base64.StdEncoding.EncodeToString(raw)

		client := clitest.DockerClient(t, ctx)
		client.ImagePullFn = func(_ context.Context, ref string, options dockertypes.ImagePullOptions) (io.ReadCloser, error) {
			// Assert that we call the image pull function with the credentials.
			require.Equal(t, authB64, options.RegistryAuth)
			return io.NopCloser(bytes.NewReader(nil)), nil
		}

		err := cmd.ExecuteContext(ctx)
		require.NoError(t, err)
	})

	t.Run("SetsResources", func(t *testing.T) {
		t.Parallel()

		const (
			// 4GB.
			memory = 4 << 30
			cpus   = 6
		)

		ctx, cmd := clitest.New(t, "docker",
			"--image=ubuntu",
			"--username=root",
			"--agent-token=hi",
			fmt.Sprintf("--cpus=%d", cpus),
			fmt.Sprintf("--memory=%d", memory),
		)

		var called bool
		client := clitest.DockerClient(t, ctx)
		client.ContainerCreateFn = func(_ context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, _ *v1.Platform, containerName string) (container.ContainerCreateCreatedBody, error) {
			if containerName == cli.InnerContainerName {
				called = true
				require.Equal(t, int64(memory), hostConfig.Memory)
				require.Equal(t, int64(cpus*dockerutil.DefaultCPUPeriod), hostConfig.CPUQuota)
				require.Equal(t, int64(dockerutil.DefaultCPUPeriod), hostConfig.CPUPeriod)
			}

			return container.ContainerCreateCreatedBody{}, nil
		}

		err := cmd.ExecuteContext(ctx)
		require.NoError(t, err)
		require.True(t, called, "create function was not called for inner container")
	})

	t.Run("GPUNoUsrLibDir", func(t *testing.T) {
		t.Parallel()

		ctx, cmd := clitest.New(t, "docker",
			"--image=ubuntu",
			"--username=root",
			"--agent-token=hi",
			"--add-gpu=true",
		)

		err := cmd.ExecuteContext(ctx)
		require.Error(t, err)
		require.ErrorContains(t, err, fmt.Sprintf("when using GPUs, %q must be specified", cli.EnvUsrLibDir))
	})

	t.Run("GPU", func(t *testing.T) {
		t.Parallel()

		ctx, cmd := clitest.New(t, "docker",
			"--image=ubuntu",
			"--username=root",
			"--agent-token=hi",
			"--add-gpu=true",
			"--usr-lib-dir=/var/coder/usr/lib",
		)

		var (
			mounter = clitest.Mounter(ctx)
			afs     = clitest.FS(ctx)

			procGPUDrivers = []string{
				"/proc/vulkan/foo",
				"/proc/nvidia/bar",
				"/proc/cuda/baz",
			}

			// This path intentionally has a trailing '/' to ensure we are
			// trimming correctly when remapping host-mounted /usr/lib dirs to
			// /usr/lib inside the container.
			usrLibMountpoint = "/var/coder/usr/lib/"
			// expectedUsrLibFiles are files that we expect to be returned as bind mounts.
			expectedUsrLibFiles = []string{
				filepath.Join(usrLibMountpoint, "nvidia", "libglxserver_nvidia.so"),
				filepath.Join(usrLibMountpoint, "libnvidia-ml.so"),
			}
			expectedEnvs = []string{
				"NVIDIA_TEST=1",
				"TEST_NVIDIA=1",
				"nvidia_test=1",
			}
		)

		environ := func() []string {
			return append(
				[]string{
					"LIBGL_TEST=1",
					"VULKAN_TEST=1",
				}, expectedEnvs...)
		}

		ctx = xunix.WithEnvironFn(ctx, environ)

		// Fake all the files.
		for _, file := range append(expectedUsrLibFiles, procGPUDrivers...) {
			_, err := afs.Create(file)
			require.NoError(t, err)
		}

		mounter.MountPoints = []mount.MountPoint{
			{
				Device: "/dev/sda1",
				Path:   "/usr/local/nvidia",
				Opts:   []string{"rw"},
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

		for _, driver := range procGPUDrivers {
			mounter.MountPoints = append(mounter.MountPoints, mount.MountPoint{
				Path: driver,
			})
		}

		_, err := afs.Create("/usr/local/nvidia")
		require.NoError(t, err)

		unmounts := []string{}
		mounter.UnmountFunc = func(path string) error {
			unmounts = append(unmounts, path)
			return nil
		}

		var called bool
		client := clitest.DockerClient(t, ctx)
		client.ContainerCreateFn = func(_ context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, _ *v1.Platform, containerName string) (container.ContainerCreateCreatedBody, error) {
			if containerName == cli.InnerContainerName {
				called = true
				// Test that '/dev' mounts are passed as devices.
				require.Contains(t, hostConfig.Devices, container.DeviceMapping{
					PathOnHost:        "/dev/nvidia0",
					PathInContainer:   "/dev/nvidia0",
					CgroupPermissions: "rwm",
				})
				require.Contains(t, hostConfig.Devices, container.DeviceMapping{
					PathOnHost:        "/dev/nvidia1",
					PathInContainer:   "/dev/nvidia1",
					CgroupPermissions: "rwm",
				})

				// Test that the mountpoint that we provided that is not under
				// '/dev' is passed as a bind mount.
				require.Contains(t, hostConfig.Binds, fmt.Sprintf("%s:%s", "/usr/local/nvidia", "/usr/local/nvidia"))

				// Test that host /usr/lib bind mounts were passed through as read-only.
				for _, file := range expectedUsrLibFiles {
					require.Contains(t, hostConfig.Binds, fmt.Sprintf("%s:%s:ro",
						file,
						strings.Replace(file, usrLibMountpoint, "/usr/lib/", -1),
					))
				}

				// Test that we captured the GPU-related env vars.
				for _, env := range expectedEnvs {
					require.Contains(t, config.Env, env)
				}
			}

			return container.ContainerCreateCreatedBody{}, nil
		}

		err = cmd.ExecuteContext(ctx)
		require.NoError(t, err)
		require.True(t, called, "create function was not called for inner container")
		// Assert that we unmounted /proc GPU drivers.
		for _, driver := range procGPUDrivers {
			require.Contains(t, unmounts, driver)
		}
	})

	t.Run("Hostname", func(t *testing.T) {
		t.Parallel()

		ctx, cmd := clitest.New(t, "docker",
			"--image=ubuntu",
			"--username=root",
			"--agent-token=hi",
			"--hostname=hello-world",
		)

		var called bool
		client := clitest.DockerClient(t, ctx)
		client.ContainerCreateFn = func(_ context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, _ *v1.Platform, containerName string) (container.ContainerCreateCreatedBody, error) {
			if containerName == cli.InnerContainerName {
				called = true
				require.Equal(t, "hello-world", config.Hostname)
			}

			return container.ContainerCreateCreatedBody{}, nil
		}

		err := cmd.ExecuteContext(ctx)
		require.NoError(t, err)
		require.True(t, called, "container create not called")
	})

	t.Run("DisableIDMappedMounts", func(t *testing.T) {
		t.Parallel()

		ctx, cmd := clitest.New(t, "docker",
			"--image=ubuntu",
			"--username=root",
			"--agent-token=hi",
			"--disable-idmapped-mount",
		)

		execer := clitest.Execer(ctx)
		execer.AddCommands(&xunixfake.FakeCmd{
			FakeCmd: &testingexec.FakeCmd{
				Argv: []string{
					"sysbox-mgr",
					"--disable-idmapped-mount",
				},
			},
			WaitFn: func() error { select {} }, //nolint:revive
		})

		err := cmd.ExecuteContext(ctx)
		require.NoError(t, err)
		execer.AssertCommandsCalled(t)
	})
}

// rawDockerAuth is sample input for a kubernetes secret to a gcr.io private
// registry.
const rawDockerAuth = `{"auths":{"us.gcr.io":{"username":"_json_key","password":"{\"type\": \"service_account\", \"project_id\": \"some-test\", \"private_key_id\": \"blahblah\", \"private_key\": \"-----BEGIN PRIVATE KEY-----mykey-----END PRIVATE KEY-----\", \"client_email\": \"test@test.iam.gserviceaccount.com\", \"client_id\": \"123\", \"auth_uri\": \"https://accounts.google.com/o/oauth2/auth\", \"token_uri\": \"https://oauth2.googleapis.com/token\", \"auth_provider_x509_cert_url\": \"https://www.googleapis.com/oauth2/v1/certs\", \"client_x509_cert_url\": \"https://www.googleapis.com/robot/v1/metadata/x509/test.iam.gserviceaccount.com\" }","email":"test@test.iam.gserviceaccount.com","auth":"X2pzb25fa2V5OnsKCgkidHlwZSI6ICJzZXJ2aWNlX2FjY291bnQiLAoJInByb2plY3RfaWQiOiAic29tZS10ZXN0IiwKCSJwcml2YXRlX2tleV9pZCI6ICJibGFoYmxhaCIsCgkicHJpdmF0ZV9rZXkiOiAiLS0tLS1CRUdJTiBQUklWQVRFIEtFWS0tLS0tCm15a2V5LS0tLS1FTkQgUFJJVkFURSBLRVktLS0tLQoiLAoJImNsaWVudF9lbWFpbCI6ICJ0ZXN0QHRlc3QuaWFtLmdzZXJ2aWNlYWNjb3VudC5jb20iLAoJImNsaWVudF9pZCI6ICIxMjMiLAoJImF1dGhfdXJpIjogImh0dHBzOi8vYWNjb3VudHMuZ29vZ2xlLmNvbS9vL29hdXRoMi9hdXRoIiwKCSJ0b2tlbl91cmkiOiAiaHR0cHM6Ly9vYXV0aDIuZ29vZ2xlYXBpcy5jb20vdG9rZW4iLAoJImF1dGhfcHJvdmlkZXJfeDUwOV9jZXJ0X3VybCI6ICJodHRwczovL3d3dy5nb29nbGVhcGlzLmNvbS9vYXV0aDIvdjEvY2VydHMiLAoJImNsaWVudF94NTA5X2NlcnRfdXJsIjogImh0dHBzOi8vd3d3Lmdvb2dsZWFwaXMuY29tL3JvYm90L3YxL21ldGFkYXRhL3g1MDkvdGVzdC5pYW0uZ3NlcnZpY2VhY2NvdW50LmNvbSIKfQo="}}}`
