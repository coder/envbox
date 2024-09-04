//go:build integration
// +build integration

package integration_test

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
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
				Image:       integrationtest.DockerdImage,
				Username:    "root",
				OuterMounts: binds,
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
			tmpdir              = integrationtest.TmpDir(t)
			binds               = integrationtest.DefaultBinds(t, tmpdir)
			expectedMemoryLimit = "1073741824"
			expectedCPULimit    = 1
			expectedHostname    = "testmepls"
		)

		homeDir := filepath.Join(tmpdir, "home")
		err = os.MkdirAll(homeDir, 0o777)
		require.NoError(t, err)

		// Emulate someone wanting to mount a secret into envbox.
		secretDir := filepath.Join(tmpdir, "secrets")
		err = os.MkdirAll(secretDir, 0o777)
		require.NoError(t, err)

		binds = append(binds,
			integrationtest.BindMount(homeDir, "/home/coder", false),
			integrationtest.BindMount(secretDir, "/var/secrets", true),
		)

		var (
			envFilter = []string{
				"KUBERNETES_*",
				"HELLO",
				"TEST_ME",
				"TEST_VAR",
			}

			envs = []string{
				"KUBERNETES_PORT=123",
				"KUBERNETES_SERVICE_HOST=10.0.1",
				"HELLO=world",
				"TEST_ME=pls",
				"ENVBOX_ONLY=hi",
				"TEST_ME_PLS=hmm",
				"TEST_VAR=hello=world",
				// Add a mount mapping to the inner container.
				fmt.Sprintf("%s=%s:%s,%s:%s:ro", cli.EnvMounts, "/home/coder", "/home/coder", "/var/secrets", "/var/secrets"),
				fmt.Sprintf("%s=%s", cli.EnvMemory, expectedMemoryLimit),
				fmt.Sprintf("%s=%d", cli.EnvCPUs, expectedCPULimit),
				fmt.Sprintf("%s=%s", cli.EnvInnerHostname, expectedHostname),
			}
		)

		// We touch /home/coder/.coder/foo because it asserts that we're
		// making the directory that ultimately will host the agent for Coder.
		// We set this as the BINARY_DIR that we pass to the startup script
		// so that we can avoid the race that occurs where systemd is remounting
		// /tmp while we are downloading the agent binary /tmp/coder.XXX.
		bootstrapScript := `#!/usr/bin/env bash

			echo "hello" > /home/coder/bootstrap
			mkdir /home/coder/bar
			touch /home/coder/.coder/foo
`

		// Run the envbox container.
		resource := integrationtest.RunEnvbox(t, pool, &integrationtest.CreateDockerCVMConfig{
			Image:           integrationtest.UbuntuImage,
			Username:        "coder",
			InnerEnvFilter:  envFilter,
			Envs:            envs,
			OuterMounts:     binds,
			AddFUSE:         true,
			AddTUN:          true,
			BootstrapScript: bootstrapScript,
			CPUs:            expectedCPULimit,
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
			"TEST_VAR=hello=world",
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

		// Validate that memory limit is being applied to the inner container.
		out, err = integrationtest.ExecInnerContainer(t, pool, integrationtest.ExecConfig{
			ContainerID: resource.Container.ID,
			Cmd:         []string{"cat", "/sys/fs/cgroup/memory/memory.limit_in_bytes"},
		})
		require.NoError(t, err)
		require.Equal(t, expectedMemoryLimit, strings.TrimSpace(string(out)))

		periodStr, err := integrationtest.ExecInnerContainer(t, pool, integrationtest.ExecConfig{
			ContainerID: resource.Container.ID,
			Cmd:         []string{"cat", "/sys/fs/cgroup/cpu,cpuacct/cpu.cfs_period_us"},
		})
		require.NoError(t, err)
		period, err := strconv.ParseInt(strings.TrimSpace(string(periodStr)), 10, 64)
		require.NoError(t, err)

		quotaStr, err := integrationtest.ExecInnerContainer(t, pool, integrationtest.ExecConfig{
			ContainerID: resource.Container.ID,
			Cmd:         []string{"cat", "/sys/fs/cgroup/cpu/cpu.cfs_quota_us"},
		})
		require.NoError(t, err)
		quota, err := strconv.ParseInt(strings.TrimSpace(string(quotaStr)), 10, 64)
		require.NoError(t, err)

		// Validate that the CPU limit is being applied to the inner container.
		actualLimit := float64(quota) / float64(period)
		require.Equal(t, expectedCPULimit, int(actualLimit))

		// Validate that the hostname is being set.
		hostname, err := integrationtest.ExecInnerContainer(t, pool, integrationtest.ExecConfig{
			ContainerID: resource.Container.ID,
			Cmd:         []string{"hostname"},
		})
		require.NoError(t, err)
		require.Equal(t, expectedHostname, strings.TrimSpace(string(hostname)))
	})

	t.Run("SelfSignedCerts", func(t *testing.T) {
		t.Parallel()

		var (
			dir   = integrationtest.TmpDir(t)
			binds = integrationtest.DefaultBinds(t, dir)
		)

		pool, err := dockertest.NewPool("")
		require.NoError(t, err)

		// Create some listeners for the Docker and Coder
		// services we'll be running with self signed certs.
		bridgeIP := integrationtest.DockerBridgeIP(t)
		coderListener, err := net.Listen("tcp", fmt.Sprintf("%s:0", bridgeIP))
		require.NoError(t, err)
		defer coderListener.Close()
		coderAddr := tcpAddr(t, coderListener)

		registryListener, err := net.Listen("tcp", fmt.Sprintf("%s:0", bridgeIP))
		require.NoError(t, err)
		err = registryListener.Close()
		require.NoError(t, err)
		registryAddr := tcpAddr(t, registryListener)

		coderCert := integrationtest.GenerateTLSCertificate(t, "host.docker.internal", coderAddr.IP.String())
		dockerCert := integrationtest.GenerateTLSCertificate(t, "host.docker.internal", registryAddr.IP.String())

		// Startup our fake Coder "control-plane".
		recorder := integrationtest.FakeBuildLogRecorder(t, coderListener, coderCert)

		certDir := integrationtest.MkdirAll(t, dir, "certs")

		// Write the Coder cert disk.
		coderCertPath := filepath.Join(certDir, "coder_cert.pem")
		coderKeyPath := filepath.Join(certDir, "coder_key.pem")
		integrationtest.WriteCertificate(t, coderCert, coderCertPath, coderKeyPath)
		coderCertMount := integrationtest.BindMount(certDir, "/tmp/certs", false)

		// Write the Registry cert to disk.
		regCertPath := filepath.Join(certDir, "registry_cert.crt")
		regKeyPath := filepath.Join(certDir, "registry_key.pem")
		integrationtest.WriteCertificate(t, dockerCert, regCertPath, regKeyPath)

		// Start up the docker registry and push an image
		// to it that we can reference.
		image := integrationtest.RunLocalDockerRegistry(t, pool, integrationtest.RegistryConfig{
			HostCertPath: regCertPath,
			HostKeyPath:  regKeyPath,
			Image:        integrationtest.UbuntuImage,
			TLSPort:      strconv.Itoa(registryAddr.Port),
		})

		envs := []string{
			integrationtest.EnvVar(cli.EnvAgentToken, "faketoken"),
			integrationtest.EnvVar(cli.EnvAgentURL, fmt.Sprintf("https://%s:%d", "host.docker.internal", coderAddr.Port)),
			integrationtest.EnvVar(cli.EnvExtraCertsPath, "/tmp/certs"),
		}

		// Run the envbox container.
		_ = integrationtest.RunEnvbox(t, pool, &integrationtest.CreateDockerCVMConfig{
			Image:       image.String(),
			Username:    "coder",
			Envs:        envs,
			OuterMounts: append(binds, coderCertMount),
		})

		// This indicates we've made it all the way to end
		// of the logs we attempt to push.
		require.True(t, recorder.ContainsLog("Bootstrapping workspace..."))
	})

	// This tests the inverse of SelfSignedCerts. We assert that
	// the container fails to startup since we don't have a valid
	// cert for the registry. It mainly tests that we aren't
	// getting a false positive for SelfSignedCerts.
	t.Run("InvalidCert", func(t *testing.T) {
		t.Parallel()

		var (
			dir   = integrationtest.TmpDir(t)
			binds = integrationtest.DefaultBinds(t, dir)
		)

		pool, err := dockertest.NewPool("")
		require.NoError(t, err)

		// Create some listeners for the Docker and Coder
		// services we'll be running with self signed certs.
		bridgeIP := integrationtest.DockerBridgeIP(t)
		coderListener, err := net.Listen("tcp", fmt.Sprintf("%s:0", bridgeIP))
		require.NoError(t, err)
		defer coderListener.Close()
		coderAddr := tcpAddr(t, coderListener)

		registryListener, err := net.Listen("tcp", fmt.Sprintf("%s:0", bridgeIP))
		require.NoError(t, err)
		err = registryListener.Close()
		require.NoError(t, err)
		registryAddr := tcpAddr(t, registryListener)

		// Generate a random cert so the same codepaths
		// get triggered.
		randomCert := integrationtest.GenerateTLSCertificate(t, "host.docker.internal", coderAddr.IP.String())
		// Generate a cert for the registry. We are intentionally
		// not passing this to envbox.
		registryCert := integrationtest.GenerateTLSCertificate(t, "host.docker.internal", registryAddr.IP.String())

		certDir := integrationtest.MkdirAll(t, dir, "certs")
		registryCertDir := integrationtest.MkdirAll(t, dir, "registry_certs")

		// Write the Coder cert disk.
		coderCertPath := filepath.Join(certDir, "random_cert.pem")
		coderKeyPath := filepath.Join(certDir, "random_key.pem")
		integrationtest.WriteCertificate(t, randomCert, coderCertPath, coderKeyPath)
		coderCertMount := integrationtest.BindMount(certDir, "/tmp/certs", false)

		// Write the Registry cert to disk in a separate directory.
		regCertPath := filepath.Join(registryCertDir, "registry_cert.crt")
		regKeyPath := filepath.Join(registryCertDir, "registry_key.pem")
		integrationtest.WriteCertificate(t, registryCert, regCertPath, regKeyPath)

		// Start up the docker registry and push an image
		// to it that we can reference.
		image := integrationtest.RunLocalDockerRegistry(t, pool, integrationtest.RegistryConfig{
			HostCertPath: regCertPath,
			HostKeyPath:  regKeyPath,
			Image:        integrationtest.UbuntuImage,
			TLSPort:      strconv.Itoa(registryAddr.Port),
		})

		envs := []string{
			integrationtest.EnvVar(cli.EnvExtraCertsPath, "/tmp/certs"),
		}

		// Run the envbox container.
		_ = integrationtest.RunEnvbox(t, pool, &integrationtest.CreateDockerCVMConfig{
			Image:         image.String(),
			Username:      "coder",
			Envs:          envs,
			OuterMounts:   append(binds, coderCertMount),
			ExpectFailure: true,
		})
	})

	// InvalidCoderCert tests that an invalid cert
	// for the Coder control plane does not result in a
	// fatal error. The container should still start up
	// but we won't receive any build logs.
	t.Run("InvalidCoderCert", func(t *testing.T) {
		t.Parallel()

		var (
			dir   = integrationtest.TmpDir(t)
			binds = integrationtest.DefaultBinds(t, dir)
		)

		pool, err := dockertest.NewPool("")
		require.NoError(t, err)

		// Create a listener for the Coder service with a self-signed cert.
		bridgeIP := integrationtest.DockerBridgeIP(t)
		coderListener, err := net.Listen("tcp", fmt.Sprintf("%s:0", bridgeIP))
		require.NoError(t, err)
		defer coderListener.Close()
		coderAddr := tcpAddr(t, coderListener)

		coderCert := integrationtest.GenerateTLSCertificate(t, "host.docker.internal", coderAddr.IP.String())
		fakeCert := integrationtest.GenerateTLSCertificate(t, "host.docker.internal", coderAddr.IP.String())

		// Startup our fake Coder "control-plane".
		recorder := integrationtest.FakeBuildLogRecorder(t, coderListener, coderCert)

		certDir := integrationtest.MkdirAll(t, dir, "certs")

		// This is all a little unnecessary we could just not
		// write one but let's fully simulate someone mounting
		// a bad cert.
		fakeCertPath := filepath.Join(certDir, "fake_cert.pem")
		fakeKeyPath := filepath.Join(certDir, "fake_key.pem")
		integrationtest.WriteCertificate(t, fakeCert, fakeCertPath, fakeKeyPath)
		coderCertMount := integrationtest.BindMount(certDir, "/tmp/certs", false)

		envs := []string{
			integrationtest.EnvVar(cli.EnvAgentToken, "faketoken"),
			integrationtest.EnvVar(cli.EnvAgentURL, fmt.Sprintf("https://%s:%d", "host.docker.internal", coderAddr.Port)),
			integrationtest.EnvVar(cli.EnvExtraCertsPath, "/tmp/certs"),
		}

		// Run the envbox container.
		resource := integrationtest.RunEnvbox(t, pool, &integrationtest.CreateDockerCVMConfig{
			Image:       integrationtest.UbuntuImage,
			Username:    "coder",
			Envs:        envs,
			OuterMounts: append(binds, coderCertMount),
		})

		// This indicates we've made it all the way to end
		// of the logs we attempt to push.
		require.Equal(t, 0, recorder.Len())

		// Sanity check that we're actually running.
		output, err := integrationtest.ExecInnerContainer(t, pool, integrationtest.ExecConfig{
			ContainerID: resource.Container.ID,
			Cmd:         []string{"echo", "hello"},
			User:        "root",
		})
		require.NoError(t, err)
		require.Equal(t, "hello\n", string(output))
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

func tcpAddr(t testing.TB, l net.Listener) *net.TCPAddr {
	t.Helper()

	tcpAddr, ok := l.Addr().(*net.TCPAddr)
	require.True(t, ok)
	return tcpAddr
}
