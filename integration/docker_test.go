//go:build integration
// +build integration

package integration_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	dockertest "github.com/ory/dockertest/v3"
	"github.com/stretchr/testify/require"

	"github.com/coder/envbox/cli"
	"github.com/coder/envbox/integration/integrationtest"
)

func TestDocker(t *testing.T) {
	t.Parallel()

	// Dockerd just tests that dockerd can spin up and function correctly.
	t.Run("Dockerd", func(t *testing.T) {
		t.Parallel()

		pool, err := dockertest.NewPool("")
		require.NoError(t, err)

		var (
			tmpdir = integrationtest.TmpDir(t)
			binds  = integrationtest.DefaultBinds(t, tmpdir)
		)

		runEnvbox := func() *dockertest.Resource {
			// Run the envbox container.
			resource := integrationtest.RunEnvbox(t, pool, &integrationtest.CreateDockerCVMConfig{
				Image:    integrationtest.DockerdImage,
				Username: "root",
				Binds:    binds,
			})

			// Wait for the inner container's docker daemon.
			integrationtest.WaitForCVMDocker(t, pool, resource, time.Minute)

			// Assert that we can run docker in the inner container.
			_, err = integrationtest.ExecInnerContainer(t, pool, integrationtest.ExecConfig{
				ContainerID: resource.Container.ID,
				Cmd:         []string{"docker", "run", integrationtest.HelloWorldImage},
			})

			require.NoError(t, err)
			return resource
		}

		// Run envbox initially, this tests the initial creation of a workspace.
		resource := runEnvbox()

		t.Logf("envbox %q started successfully, recreating...", resource.Container.ID)

		// Destroy the container, we're going to recreate it to ensure that when volumes are reused
		// IDs are still mapped correctly.
		err = resource.Close()
		require.NoError(t, err)

		// Run envbox again to test that when we restart a workspace things still
		// work correctly.
		_ = runEnvbox()
	})

	// EnvboxArgs validates that arguments passed to envbox function correctly.
	// Most cases should be covered with unit tests, the intent with this is to
	// test cases that do not garner a high degree of confidence from mocking
	// (such as creating devices e.g. FUSE, TUN).
	t.Run("EnvboxArgs", func(t *testing.T) {
		t.Parallel()

		pool, err := dockertest.NewPool("")
		require.NoError(t, err)

		var (
			tmpdir = integrationtest.TmpDir(t)
			binds  = integrationtest.DefaultBinds(t, tmpdir)
		)

		homeDir := filepath.Join(tmpdir, "home")
		err = os.MkdirAll(homeDir, 0o777)
		require.NoError(t, err)

		// Emulate someone wanting to mount a secret into envbox.
		secretDir := filepath.Join(tmpdir, "secrets")
		err = os.MkdirAll(secretDir, 0o777)
		require.NoError(t, err)

		binds = append(binds,
			bindMount(homeDir, "/home/coder", false),
			bindMount(secretDir, "/var/secrets", false),
		)

		var (
			envFilter = []string{
				"KUBERNETES_*",
				"HELLO=world",
				"TEST_ME=pls",
			}

			envs = []string{
				"KUBERNETES_PORT=123",
				"KUBERNETES_SERVICE_HOST=10.0.1",
				"HELLO=world",
				"TEST_ME=pls",
				"ENVBOX_ONLY=hi",
				"TEST_ME_PLS=hmm",
				// Add a mount mapping to the inner container.
				fmt.Sprintf("%s=%s:%s,%s:%s:ro", cli.EnvMounts, "/home/coder", "/home/coder", "/var/secrets", "/var/secrets"),
			}
		)

		bootstrapScript := `#!/usr/bin/env bash

			echo "hello" > /home/coder/bootstrap
			mkdir /home/coder/bar
`

		// Run the envbox container.
		resource := integrationtest.RunEnvbox(t, pool, &integrationtest.CreateDockerCVMConfig{
			Image:           integrationtest.UbuntuImage,
			Username:        "coder",
			InnerEnvFilter:  envFilter,
			Envs:            envs,
			Binds:           binds,
			AddFUSE:         true,
			AddTUN:          true,
			BootstrapScript: bootstrapScript,
		})

		// Validate that the envs are set correctly.
		vars, err := integrationtest.ExecInnerContainer(t, pool, integrationtest.ExecConfig{
			ContainerID: resource.Container.ID,
			Cmd:         []string{"env"},
		})
		require.NoError(t, err)

		envVars := strings.Split(string(vars), "\n")

		requireSliceContains(t, envVars,
			"KUBERNETES_PORT=123",
			"KUBERNETES_SERVICE_HOST=10.0.1",
			"HELLO=world",
			"TEST_ME=pls",
		)
		requireSliceNoContains(t, envVars,
			"ENVBOX_ONLY=hi",
			"TEST_ME_PLS=hmm",
		)

		// Assert that the FUSE device exists.
		_, err = integrationtest.ExecInnerContainer(t, pool, integrationtest.ExecConfig{
			ContainerID: resource.Container.ID,
			Cmd:         []string{"stat", cli.InnerFUSEPath},
		})
		require.NoError(t, err)

		// Assert that the TUN device exists.
		_, err = integrationtest.ExecInnerContainer(t, pool, integrationtest.ExecConfig{
			ContainerID: resource.Container.ID,
			Cmd:         []string{"stat", cli.InnerTUNPath},
		})
		require.NoError(t, err)

		// Assert that the home directory exists and has the correct shifted permissions.
		homeDirUID, err := integrationtest.ExecEnvbox(t, pool, integrationtest.ExecConfig{
			ContainerID: resource.Container.ID,
			Cmd:         []string{"stat", `--format=%u`, "/home/coder"},
		})
		require.NoError(t, err)
		require.Equal(t, "101000", strings.TrimSpace(string(homeDirUID)))

		// Validate that we can touch files in the home directory.
		_, err = integrationtest.ExecInnerContainer(t, pool, integrationtest.ExecConfig{
			ContainerID: resource.Container.ID,
			Cmd:         []string{"touch", "/home/coder/foo"},
			User:        "coder",
		})
		require.NoError(t, err)

		secretsDirUID, err := integrationtest.ExecEnvbox(t, pool, integrationtest.ExecConfig{
			ContainerID: resource.Container.ID,
			Cmd:         []string{"stat", "--format=%u", "/var/secrets"},
		})
		require.NoError(t, err)
		require.Equal(t, "100000", strings.TrimSpace(string(secretsDirUID)))

		// Validate that we cannot touch files in this case since it should be a
		// read only mount.
		_, err = integrationtest.ExecInnerContainer(t, pool, integrationtest.ExecConfig{
			ContainerID: resource.Container.ID,
			Cmd:         []string{"touch", "/var/secrets/foo"},
		})
		require.Error(t, err)
		// Make sure the error is actually because of a read only filesystem
		// and not some random other error.
		require.Contains(t, err.Error(), "Read-only file system")

		// Validate that the bootstrap script ran.
		out, err := integrationtest.ExecInnerContainer(t, pool, integrationtest.ExecConfig{
			ContainerID: resource.Container.ID,
			Cmd:         []string{"cat", "/home/coder/bootstrap"},
		})
		require.NoError(t, err)
		require.Equal(t, "hello", strings.TrimSpace(string(out)))

		// Validate that the bootstrap script ran.
		out, err = integrationtest.ExecInnerContainer(t, pool, integrationtest.ExecConfig{
			ContainerID: resource.Container.ID,
			Cmd:         []string{"stat", "--format=%u", "/home/coder/bar"},
		})
		require.NoError(t, err)
		require.Equal(t, "1000", strings.TrimSpace(string(out)))
	})
}

func requireSliceNoContains(t *testing.T, ss []string, els ...string) {
	for _, e := range els {
		for _, s := range ss {
			if s == e {
				t.Fatalf("unexpectedly found %q in %+v", e, ss)
			}
		}
	}
}

func requireSliceContains(t *testing.T, ss []string, els ...string) {
	for _, e := range els {
		var found bool
		for _, s := range ss {
			if s == e {
				found = true
				break
			}
		}
		require.True(t, found, "expected to find %s in %+v", e, ss)
	}
}

func bindMount(src, dest string, ro bool) string {
	if ro {
		return fmt.Sprintf("%s:%s:%s", src, dest, "ro")
	}
	return fmt.Sprintf("%s:%s", src, dest)
}
