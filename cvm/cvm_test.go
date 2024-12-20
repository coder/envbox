package cvm_test

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

	"cdr.dev/slog/sloggers/slogtest"
	"github.com/coder/envbox/cli"
	"github.com/coder/envbox/cvm"
	"github.com/coder/envbox/dockerutil"
	"github.com/coder/envbox/dockerutil/dockerfake"
	"github.com/coder/envbox/xunix"
	"github.com/coder/envbox/xunix/xunixfake"
	dockertypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
	mount "k8s.io/mount-utils"
)

func TestRun(t *testing.T) {
	t.Parallel()

	t.Run("OK", func(t *testing.T) {
	})

	t.Run("Images", func(t *testing.T) {
		t.Parallel()
		type testcase struct {
			name    string
			image   string
			success bool
		}

		testcases := []testcase{
			{
				name:    "Repository",
				image:   "ubuntu",
				success: true,
			},
			{
				name:    "RepositoryPath",
				image:   "ubuntu/ubuntu",
				success: true,
			},

			{
				name:    "RepositoryLatest",
				image:   "ubuntu:latest",
				success: true,
			},
			{
				name:    "RepositoryTag",
				image:   "ubuntu:24.04",
				success: true,
			},
			{
				name:    "RepositoryPathTag",
				image:   "ubuntu/ubuntu:18.04",
				success: true,
			},
			{
				name:    "RegistryRepository",
				image:   "gcr.io/ubuntu",
				success: true,
			},
			{
				name:    "RegistryRepositoryTag",
				image:   "gcr.io/ubuntu:24.04",
				success: true,
			},
		}

		for _, tc := range testcases {
			tc := tc
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				tag, err := name.NewTag(tc.image)
				require.NoError(t, err)

				ctx := context.Background()
				client := NewFakeDockerClient()
				log := slogtest.Make(t, nil)
				os := xunixfake.NewFakeOS()

				var created bool
				client.ContainerCreateFn = func(_ context.Context, conf *container.Config, _ *container.HostConfig, _ *network.NetworkingConfig, _ *v1.Platform, _ string) (container.CreateResponse, error) {
					created = true
					require.Equal(t, tc.image, conf.Image)
					return container.CreateResponse{}, nil
				}

				err = cvm.Run(ctx, log, os, client, cvm.Config{
					Tag: tag,
				})

				require.NoError(t, err)
				require.True(t, created, "container create fn not called")
			})
		}
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
				"CODER_AGENT_SUBSYSTEM=envbox,exectrace", // sorted
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
				// Envbox should add to this.
				"CODER_AGENT_SUBSYSTEM=exectrace",
				// Don't include the wildcards.
			}, expectedEnvs...))
		)

		ctx := context.Background()
		log := slogtest.Make(t, nil)
		xos := xunixfake.NewFakeOS()
		client := NewFakeDockerClient()

		var called bool
		client.ContainerCreateFn = func(_ context.Context, config *container.Config, _ *container.HostConfig, _ *network.NetworkingConfig, _ *v1.Platform, containerName string) (container.CreateResponse, error) {
			if containerName == cli.InnerContainerName {
				called = true
				require.ElementsMatch(t, expectedEnvs, config.Env)
			}
			return container.CreateResponse{}, nil
		}
		err := cvm.Run(ctx, log, xos, client, cvm.Config{
			AgentToken: "hi",
			Tag:        requireParseImage(t, "ubuntu"),
			Username:   "root",
			OSEnvs:     osEnvs,
			InnerEnvs:  cntEnvs,
		})
		require.NoError(t, err)
		require.True(t, called, "create function was not called")
	})

	// Test that we parse mounts correctly.
	t.Run("Mounts", func(t *testing.T) {
		t.Parallel()

		var (
			userMounts = []xunix.Mount{
				{
					Source:     "/home/coder",
					Mountpoint: "/home/coder",
				},
				{
					Source:     "/etc/hosts",
					Mountpoint: "/etc/hosts",
					ReadOnly:   true,
				},
				{
					Source:     "/etc/hostname",
					Mountpoint: "/idc/where",
					ReadOnly:   true,
				},
				{
					Source:     "/usr/src",
					Mountpoint: "/a/b/c",
				},
			}

			expectedMounts = append([]string{"/var/lib/coder/docker:/var/lib/docker", "/var/lib/coder/containers:/var/lib/containers"}, mountsToString(userMounts)...)
		)

		var (
			ctx    = context.Background()
			log    = slogtest.Make(t, nil)
			xos    = xunixfake.NewFakeOS()
			client = NewFakeDockerClient()
		)

		for _, mount := range userMounts {
			err := afero.WriteFile(xos, mount.Source, []byte("hi"), 0o777)
			require.NoError(t, err)
		}

		// Set the exec response from inspecting the image to some ID
		// greater than 0.
		client.ContainerExecAttachFn = func(_ context.Context, _ string, _ dockertypes.ExecStartCheck) (dockertypes.HijackedResponse, error) {
			return dockertypes.HijackedResponse{
				Reader: bufio.NewReader(strings.NewReader("root:x:1001:1001:root:/root:/bin/bash")),
				Conn:   &net.IPConn{},
			}, nil
		}

		var called bool
		client.ContainerCreateFn = func(_ context.Context, _ *container.Config, hostConfig *container.HostConfig, _ *network.NetworkingConfig, _ *v1.Platform, containerName string) (container.CreateResponse, error) {
			if containerName == cli.InnerContainerName {
				called = true
				require.Equal(t, expectedMounts, hostConfig.Binds)
			}

			return container.CreateResponse{}, nil
		}

		err := cvm.Run(ctx, log, xos, client, cvm.Config{
			AgentToken: "hi",
			Tag:        requireParseImage(t, "ubuntu"),
			Username:   "root",
			Mounts:     userMounts,
		})
		require.NoError(t, err)
		require.True(t, called, "container create fn not called")

		fi, err := xos.Stat("/home/coder")
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0o755), fi.Mode().Perm())
		// Check that we're calling chown and shifting the ID.
		owner, ok := xos.GetFileOwner("/home/coder")
		require.True(t, ok)
		require.Equal(t, cli.UserNamespaceOffset+1001, owner.UID)
		require.Equal(t, cli.UserNamespaceOffset+1001, owner.GID)
	})

	t.Run("RemountSysfs", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		log := slogtest.Make(t, nil)
		xos := xunixfake.NewFakeOS()
		client := NewFakeDockerClient()

		err := cvm.Run(ctx, log, xos, client, cvm.Config{
			AgentToken: "hi",
			Tag:        requireParseImage(t, "ubuntu"),
			Username:   "root",
		})
		require.NoError(t, err)

		actions := xos.GetLog()
		require.Len(t, actions, 1)
		action := actions[0]
		require.Equal(t, "mount", action.Action)
		require.Equal(t, "", action.FSType)
		require.Equal(t, "/sys", action.Source)
		require.Equal(t, "/sys", action.Target)
	})

	t.Run("Devices", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		log := slogtest.Make(t, nil)
		xos := xunixfake.NewFakeOS()
		client := NewFakeDockerClient()

		expectedDevices := []container.DeviceMapping{
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

		var called bool
		client.ContainerCreateFn = func(_ context.Context, _ *container.Config, hostConfig *container.HostConfig, _ *network.NetworkingConfig, _ *v1.Platform, containerName string) (container.CreateResponse, error) {
			if containerName == cli.InnerContainerName {
				called = true
				require.Equal(t, expectedDevices, hostConfig.Devices)
			}
			return container.CreateResponse{}, nil
		}

		err := cvm.Run(ctx, log, xos, client, cvm.Config{
			AgentToken: "hi",
			Tag:        requireParseImage(t, "ubuntu"),
			Username:   "root",
			AddTUN:     true,
			AddFUSE:    true,
		})
		require.NoError(t, err)
		require.True(t, called, "container create fn not called")

		owner, ok := xos.GetFileOwner(cli.OuterTUNPath)
		require.True(t, ok)
		require.Equal(t, cli.UserNamespaceOffset, owner.UID)
		require.Equal(t, cli.UserNamespaceOffset, owner.GID)

		owner, ok = xos.GetFileOwner(cli.OuterFUSEPath)
		require.True(t, ok)
		require.Equal(t, cli.UserNamespaceOffset, owner.UID)
		require.Equal(t, cli.UserNamespaceOffset, owner.GID)
	})

	// Tests that 'sleep infinity' is used if /sbin/init
	// isn't detected.
	t.Run("NoInit", func(t *testing.T) {
		t.Parallel()

		var (
			ctx        = context.Background()
			client     = NewFakeDockerClient()
			xos        = xunixfake.NewFakeOS()
			log        = slogtest.Make(t, nil)
			statExecID = "hi"
		)

		client.ContainerExecCreateFn = func(_ context.Context, _ string, config dockertypes.ExecConfig) (dockertypes.IDResponse, error) {
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
		client.ContainerCreateFn = func(_ context.Context, config *container.Config, _ *container.HostConfig, _ *network.NetworkingConfig, _ *v1.Platform, containerName string) (container.CreateResponse, error) {
			if containerName == cli.InnerContainerName {
				called = true
				require.Equal(t, []string{"sleep", "infinity"}, []string(config.Entrypoint))
			}

			return container.CreateResponse{}, nil
		}

		err := cvm.Run(ctx, log, xos, client, cvm.Config{
			AgentToken: "hi",
			Tag:        requireParseImage(t, "ubuntu"),
			Username:   "root",
		})
		require.NoError(t, err)
		require.True(t, called, "container create fn not called")
	})

	t.Run("DockerAuth", func(t *testing.T) {
		t.Parallel()

		var (
			ctx    = context.Background()
			log    = slogtest.Make(t, nil)
			xos    = xunixfake.NewFakeOS()
			client = NewFakeDockerClient()
		)

		raw := []byte(`{"username":"_json_key","password":"{\"type\": \"service_account\", \"project_id\": \"some-test\", \"private_key_id\": \"blahblah\", \"private_key\": \"-----BEGIN PRIVATE KEY-----mykey-----END PRIVATE KEY-----\", \"client_email\": \"test@test.iam.gserviceaccount.com\", \"client_id\": \"123\", \"auth_uri\": \"https://accounts.google.com/o/oauth2/auth\", \"token_uri\": \"https://oauth2.googleapis.com/token\", \"auth_provider_x509_cert_url\": \"https://www.googleapis.com/oauth2/v1/certs\", \"client_x509_cert_url\": \"https://www.googleapis.com/robot/v1/metadata/x509/test.iam.gserviceaccount.com\" }"}`)

		authB64 := base64.URLEncoding.EncodeToString(raw)

		var called bool
		client.ImagePullFn = func(_ context.Context, _ string, options image.PullOptions) (io.ReadCloser, error) {
			called = true
			// Assert that we call the image pull function with the credentials.
			require.Equal(t, authB64, options.RegistryAuth)
			return io.NopCloser(bytes.NewReader(nil)), nil
		}

		err := cvm.Run(ctx, log, xos, client, cvm.Config{
			Tag:             requireParseImage(t, "us.gcr.io/ubuntu"),
			Username:        "root",
			AgentToken:      "hi",
			ImagePullSecret: rawDockerAuth,
			// Really weird but afero.FS doesn't return an erro
			// for calling Stat() on an empty value.
			DockerConfig: "/root/.config/idontexist",
		})
		require.NoError(t, err)
		require.True(t, called, "image pull fn not called")
	})

	t.Run("SetsResources", func(t *testing.T) {
		t.Parallel()

		const (
			// 4GB.
			memory = 4 << 30
			cpus   = 6
		)

		var (
			ctx    = context.Background()
			log    = slogtest.Make(t, nil)
			xos    = xunixfake.NewFakeOS()
			client = NewFakeDockerClient()
		)

		var called bool
		client.ContainerCreateFn = func(_ context.Context, _ *container.Config, hostConfig *container.HostConfig, _ *network.NetworkingConfig, _ *v1.Platform, containerName string) (container.CreateResponse, error) {
			if containerName == cli.InnerContainerName {
				called = true
				require.Equal(t, int64(memory), hostConfig.Memory)
				require.Equal(t, int64(cpus*dockerutil.DefaultCPUPeriod), hostConfig.CPUQuota)
				require.Equal(t, int64(dockerutil.DefaultCPUPeriod), hostConfig.CPUPeriod)
			}
			return container.CreateResponse{}, nil
		}

		err := cvm.Run(ctx, log, xos, client, cvm.Config{
			AgentToken: "hi",
			Tag:        requireParseImage(t, "ubuntu"),
			Username:   "root",
			Memory:     memory,
			CPUS:       cpus,
		})
		require.NoError(t, err)
		require.True(t, called, "container create fn not called")
	})

	t.Run("GPU", func(t *testing.T) {
		t.Parallel()

		var (
			ctx    = context.Background()
			log    = slogtest.Make(t, nil)
			xos    = xunixfake.NewFakeOS()
			client = NewFakeDockerClient()
		)

		var (
			procGPUDrivers = []string{
				"/proc/vulkan/foo",
				"/proc/nvidia/bar",
				"/proc/cuda/baz",
			}

			usrLibMountpoint = "/var/coder/usr/lib/"

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

		environ := append([]string{
			"LIBGL_TEST=1",
			"VULKAN_TEST=1",
		}, expectedEnvs...)

		for _, file := range append(expectedUsrLibFiles, procGPUDrivers...) {
			_, err := xos.Create(file)
			require.NoError(t, err)
		}

		xos.MountPoints = []mount.MountPoint{
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
			xos.MountPoints = append(xos.MountPoints, mount.MountPoint{
				Path: driver,
			})
		}

		_, err := xos.Create("/usr/local/nvidia")
		require.NoError(t, err)

		unmounts := []string{}
		xos.UnmountFunc = func(path string) error {
			unmounts = append(unmounts, path)
			return nil
		}

		var called bool
		client.ContainerCreateFn = func(_ context.Context, config *container.Config, hostConfig *container.HostConfig, _ *network.NetworkingConfig, _ *v1.Platform, containerName string) (container.CreateResponse, error) {
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
						strings.ReplaceAll(file, usrLibMountpoint, "/usr/lib/"),
					))
				}

				// Test that we captured the GPU-related env vars.
				for _, env := range expectedEnvs {
					require.Contains(t, config.Env, env)
				}

			}
			return container.CreateResponse{}, nil
		}

		err = cvm.Run(ctx, log, xos, client, cvm.Config{
			AgentToken: "hi",
			Tag:        requireParseImage(t, "ubuntu"),
			Username:   "root",
			OSEnvs:     environ,
			GPUConfig: cvm.GPUConfig{
				HostUsrLibDir: usrLibMountpoint,
			},
		})
		require.NoError(t, err)
		require.True(t, called, "container create fn not called")
		// Assert that we unmounted /proc GPU drivers.
		for _, driver := range procGPUDrivers {
			require.Contains(t, unmounts, driver)
		}
	})

	t.Run("Hostname", func(t *testing.T) {
		t.Parallel()

		var (
			ctx    = context.Background()
			log    = slogtest.Make(t, nil)
			xos    = xunixfake.NewFakeOS()
			client = NewFakeDockerClient()
		)

		var called bool
		client.ContainerCreateFn = func(_ context.Context, config *container.Config, _ *container.HostConfig, _ *network.NetworkingConfig, _ *v1.Platform, containerName string) (container.CreateResponse, error) {
			if containerName == cli.InnerContainerName {
				called = true
				require.Equal(t, "hello-world", config.Hostname)
			}
			return container.CreateResponse{}, nil
		}

		err := cvm.Run(ctx, log, xos, client, cvm.Config{
			AgentToken: "hi",
			Tag:        requireParseImage(t, "ubuntu"),
			Hostname:   "hello-world",
		})
		require.NoError(t, err)
		require.True(t, called, "container create fn not called")
	})
}

func requireParseImage(t *testing.T, image string) name.Tag {
	t.Helper()

	tag, err := name.NewTag(image)
	require.NoError(t, err)
	return tag
}

func NewFakeDockerClient() *dockerfake.MockClient {
	client := &dockerfake.MockClient{}

	client.ContainerInspectFn = func(_ context.Context, _ string) (dockertypes.ContainerJSON, error) {
		return dockertypes.ContainerJSON{
			ContainerJSONBase: &dockertypes.ContainerJSONBase{
				GraphDriver: dockertypes.GraphDriverData{
					Data: map[string]string{"MergedDir": "blah"},
				},
			},
		}, nil
	}

	client.ContainerExecAttachFn = func(_ context.Context, _ string, _ dockertypes.ExecStartCheck) (dockertypes.HijackedResponse, error) {
		return dockertypes.HijackedResponse{
			Reader: bufio.NewReader(strings.NewReader("root:x:0:0:root:/root:/bin/bash")),
			Conn:   &net.IPConn{},
		}, nil
	}

	return client
}

func mountsToString(mounts []xunix.Mount) []string {
	binds := make([]string, 0, len(mounts))
	for _, mount := range mounts {
		binds = append(binds, mount.String())
	}
	return binds
}

// rawDockerAuth is sample input for a kubernetes secret to a gcr.io private
// registry.
const rawDockerAuth = `{"auths":{"us.gcr.io":{"username":"_json_key","password":"{\"type\": \"service_account\", \"project_id\": \"some-test\", \"private_key_id\": \"blahblah\", \"private_key\": \"-----BEGIN PRIVATE KEY-----mykey-----END PRIVATE KEY-----\", \"client_email\": \"test@test.iam.gserviceaccount.com\", \"client_id\": \"123\", \"auth_uri\": \"https://accounts.google.com/o/oauth2/auth\", \"token_uri\": \"https://oauth2.googleapis.com/token\", \"auth_provider_x509_cert_url\": \"https://www.googleapis.com/oauth2/v1/certs\", \"client_x509_cert_url\": \"https://www.googleapis.com/robot/v1/metadata/x509/test.iam.gserviceaccount.com\" }","email":"test@test.iam.gserviceaccount.com","auth":"X2pzb25fa2V5OnsKCgkidHlwZSI6ICJzZXJ2aWNlX2FjY291bnQiLAoJInByb2plY3RfaWQiOiAic29tZS10ZXN0IiwKCSJwcml2YXRlX2tleV9pZCI6ICJibGFoYmxhaCIsCgkicHJpdmF0ZV9rZXkiOiAiLS0tLS1CRUdJTiBQUklWQVRFIEtFWS0tLS0tCm15a2V5LS0tLS1FTkQgUFJJVkFURSBLRVktLS0tLQoiLAoJImNsaWVudF9lbWFpbCI6ICJ0ZXN0QHRlc3QuaWFtLmdzZXJ2aWNlYWNjb3VudC5jb20iLAoJImNsaWVudF9pZCI6ICIxMjMiLAoJImF1dGhfdXJpIjogImh0dHBzOi8vYWNjb3VudHMuZ29vZ2xlLmNvbS9vL29hdXRoMi9hdXRoIiwKCSJ0b2tlbl91cmkiOiAiaHR0cHM6Ly9vYXV0aDIuZ29vZ2xlYXBpcy5jb20vdG9rZW4iLAoJImF1dGhfcHJvdmlkZXJfeDUwOV9jZXJ0X3VybCI6ICJodHRwczovL3d3dy5nb29nbGVhcGlzLmNvbS9vYXV0aDIvdjEvY2VydHMiLAoJImNsaWVudF94NTA5X2NlcnRfdXJsIjogImh0dHBzOi8vd3d3Lmdvb2dsZWFwaXMuY29tL3JvYm90L3YxL21ldGFkYXRhL3g1MDkvdGVzdC5pYW0uZ3NlcnZpY2VhY2NvdW50LmNvbSIKfQo="}}}`
