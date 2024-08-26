//go:build integration
// +build integration

package integration_test

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	dockertest "github.com/ory/dockertest/v3"
	"github.com/stretchr/testify/require"

	"github.com/coder/coder/v2/codersdk"
	"github.com/coder/coder/v2/codersdk/agentsdk"
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
			bindMount(homeDir, "/home/coder", false),
			bindMount(secretDir, "/var/secrets", true),
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
			Binds:           binds,
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
			dir         = integrationtest.TmpDir(t)
			binds       = integrationtest.DefaultBinds(t, dir)
			ctx, cancel = context.WithTimeout(context.Background(), time.Minute*5)
		)
		t.Cleanup(cancel)

		pool, err := dockertest.NewPool("")
		require.NoError(t, err)

		bridgeIP := integrationtest.DockerBridgeIP(t)
		l, err := net.Listen("tcp", fmt.Sprintf("%s:0", bridgeIP))
		require.NoError(t, err)
		defer l.Close()

		host, _, err := net.SplitHostPort(l.Addr().String())
		require.NoError(t, err)

		registryListener, err := net.Listen("tcp", fmt.Sprintf("%s:0", bridgeIP))
		require.NoError(t, err)
		err = registryListener.Close()
		require.NoError(t, err)

		registryHost, registryPort, err := net.SplitHostPort(l.Addr().String())
		require.NoError(t, err)

		coderCert := integrationtest.GenerateTLSCertificate(t, "host.docker.internal", host)
		dockerCert := integrationtest.GenerateTLSCertificate(t, "host.docker.internal", registryHost)

		fakeServer, buildLogCh := fakeCoder(t)
		s := httptest.NewUnstartedServer(fakeServer)
		s.Listener = l
		s.TLS = &tls.Config{
			Certificates: []tls.Certificate{coderCert},
		}
		s.StartTLS()

		certDir := filepath.Join(dir, "certs")
		err = os.MkdirAll(certDir, 0777)
		require.NoError(t, err)
		coderCertPath := filepath.Join(certDir, "coder_cert.pem")
		coderKeyPath := filepath.Join(certDir, "coder_key.pem")
		integrationtest.WriteCertificate(t, coderCert, coderCertPath, coderKeyPath)
		bind := integrationtest.BindMount(certDir, "/tmp/certs", true)

		regCertPath := filepath.Join(certDir, "registry_cert.pem")
		regKeyPath := filepath.Join(certDir, "registry_key.pem")
		integrationtest.WriteCertificate(t, dockerCert, regCertPath, regKeyPath)

		image := integrationtest.RunLocalDockerRegistry(t, pool, integrationtest.RegistryConfig{
			HostCertPath: regCertPath,
			HostKeyPath:  regKeyPath,
			Image:        integrationtest.UbuntuImage,
			TLSPort:      registryPort,
		})

		envs := []string{
			integrationtest.EnvVar(cli.EnvAgentToken, "faketoken"),
			integrationtest.EnvVar(cli.EnvAgentURL, s.URL),
			integrationtest.EnvVar(cli.EnvExtraCertsPath, "/tmp/certs"),
		}

		buildLogDone := waitForBuildLog(t, ctx, buildLogCh)

		// Run the envbox container.
		_ = integrationtest.RunEnvbox(t, pool, &integrationtest.CreateDockerCVMConfig{
			Image:    image,
			Username: "coder",
			Envs:     envs,
			Binds:    append(binds, bind),
		})

		<-buildLogDone
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

func fakeCoder(t testing.TB) (http.Handler, <-chan string) {
	t.Helper()

	logCh := make(chan string)
	t.Cleanup(func() { close(logCh) })

	mux := http.NewServeMux()
	mux.Handle("/api/v2/buildinfo", http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusOK)

			enc := json.NewEncoder(w)
			enc.SetEscapeHTML(true)

			// We can't really do much about these errors, it's probably due to a
			// dropped connection.
			_ = enc.Encode(&codersdk.BuildInfoResponse{
				Version: "v1.0.0",
			})
		}))

	mux.Handle("/api/v2/workspaceagents/me/logs", http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			var logs agentsdk.PatchLogs
			err := json.NewDecoder(r.Body).Decode(&logs)
			require.NoError(t, err)
			w.WriteHeader(http.StatusOK)
			for _, log := range logs.Logs {
				logCh <- log.Output
			}
		}))

	mux.Handle("/", http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			t.Fatalf("unexpected route %v", r.URL.Path)
		}))

	return mux, logCh
}

// todo this sucks refactor it.
func waitForBuildLog(t testing.TB, ctx context.Context, buildLogCh <-chan string) <-chan struct{} {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-ctx.Done():
				t.Fatalf("timed out waiting for final build log")
			case log := <-buildLogCh:
				if log == "Bootstrapping workspace..." {
					return
				}
			}
		}
	}()
	return done
}
