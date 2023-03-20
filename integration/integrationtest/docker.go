package integrationtest

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
	"github.com/stretchr/testify/require"
	"golang.org/x/xerrors"

	"github.com/coder/envbox/cli"
	"github.com/coder/envbox/envboxlog"
	"github.com/coder/retry"
)

const (
	// DockerdImage is a large image (~1GB) and should only be used to test
	// dockerd.
	DockerdImage = "gcr.io/coder-dev-1/sreya/enterprise-base:ubuntu"
	// HelloWorldImage is useful for testing a CVM's dockerd is functioning
	// correctly
	HelloWorldImage = "gcr.io/coder-dev-1/sreya/hello-world"
	// UbuntuImage is just vanilla ubuntu (80MB) but the user is set to a non-root
	// user .
	UbuntuImage = "gcr.io/coder-dev-1/sreya/ubuntu-coder"
)

// TODO use df to determine if an environment is running in a docker container or not.

type CreateDockerCVMConfig struct {
	Image           string
	Username        string
	BootstrapScript string
	InnerEnvFilter  []string
	Envs            []string
	Binds           []string
	Mounts          []string
	AddFUSE         bool
	AddTUN          bool
}

func (c CreateDockerCVMConfig) validate(t *testing.T) {
	t.Helper()

	if c.Image == "" {
		t.Fatalf("an image must be provided")
	}

	if c.Username == "" {
		t.Fatalf("a username must be provided")
	}
}

// RunEnvbox runs envbox, it returns once the inner container has finished
// spinning up.
func RunEnvbox(t *testing.T, pool *dockertest.Pool, conf *CreateDockerCVMConfig) *dockertest.Resource {
	t.Helper()

	conf.validate(t)

	// If binds aren't passed then we'll just create the minimum amount.
	// If someone is passing them we'll assume they know what they're doing.
	if conf.Binds == nil {
		tmpdir := TmpDir(t)
		conf.Binds = DefaultBinds(t, tmpdir)
	}

	conf.Envs = append(conf.Envs, cmdLineEnvs(conf)...)

	resource, err := pool.RunWithOptions(&dockertest.RunOptions{
		Repository: "envbox",
		Tag:        "latest",
		Entrypoint: []string{"/envbox", "docker"},
		Env:        conf.Envs,
	}, func(host *docker.HostConfig) {
		host.Binds = conf.Binds
		host.Privileged = true
	})
	require.NoError(t, err)
	// t.Cleanup(func() { _ = pool.Purge(resource) })

	waitForCVM(t, pool, resource)

	return resource
}

// TmpDir returns a subdirectory in /tmp that can be used for test files.
func TmpDir(t *testing.T) string {
	// We use os.MkdirTemp as oposed to t.TempDir since the envbox container will
	// chown some of the created directories here to root:root causing the cleanup
	// function to fail once the test exits.
	tmpdir, err := os.MkdirTemp("", strings.Replace(t.Name(), "/", "_", -1))
	require.NoError(t, err)
	t.Logf("using tmpdir %s", tmpdir)
	return tmpdir
}

// DefaultBinds returns the minimum amount of mounts necessary to spawn
// envbox successfully. Since envbox will chown some of these directories
// to root, they cannot be cleaned up post-test, meaning that it may be
// necesssary to manually clear /tmp from time to time.
func DefaultBinds(t *testing.T, rootDir string) []string {
	t.Helper()

	// Create a bunch of mounts for the envbox container. Some proceses
	// cannot run ontop of overlayfs because they also use overlayfs
	// and so we need to pass vanilla ext4 filesystems for these processes
	// to use.

	// Create a mount for the inner docker directory.
	cntDockerDir := filepath.Join(rootDir, "coder", "docker")
	err := os.MkdirAll(cntDockerDir, 0o777)
	require.NoError(t, err)

	// Create a mount for the inner docker directory.
	cntDir := filepath.Join(rootDir, "coder", "containers")
	err = os.MkdirAll(cntDir, 0o777)
	require.NoError(t, err)

	// Create a mount for envbox's docker directory.
	dockerDir := filepath.Join(rootDir, "docker")
	err = os.MkdirAll(dockerDir, 0o777)
	require.NoError(t, err)

	// Create a mount for sysbox.
	sysbox := filepath.Join(rootDir, "sysbox")
	err = os.MkdirAll(sysbox, 0o777)
	require.NoError(t, err)

	return []string{
		fmt.Sprintf("%s:%s", cntDockerDir, "/var/lib/coder/docker"),
		fmt.Sprintf("%s:%s", cntDir, "/var/lib/coder/containers"),
		"/usr/src:/usr/src",
		"/lib/modules:/lib/modules",
		fmt.Sprintf("%s:/var/lib/sysbox", sysbox),
		fmt.Sprintf("%s:/var/lib/docker", dockerDir),
	}
}

// WaitForCVMDocker waits for the inner container docker daemon to spin up.
func WaitForCVMDocker(t *testing.T, pool *dockertest.Pool, resource *dockertest.Resource, timeout time.Duration) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	for r := retry.New(time.Second, time.Second); r.Wait(ctx); {
		_, err := ExecInnerContainer(t, pool, ExecConfig{
			ContainerID: resource.Container.ID,
			Cmd:         []string{"docker", "info"},
		})
		if err == nil {
			break
		}
	}
}

// waitForCVM waits for the inner container to spin up.
func waitForCVM(t *testing.T, pool *dockertest.Pool, resource *dockertest.Resource) {
	t.Helper()

	rd, wr := io.Pipe()
	defer rd.Close()
	defer wr.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		defer wr.Close()
		err := pool.Client.Logs(docker.LogsOptions{
			Context:      ctx,
			Container:    resource.Container.ID,
			OutputStream: wr,
			ErrorStream:  wr,
			Follow:       true,
			Stdout:       true,
			Stderr:       true,
		})
		if ctx.Err() == nil {
			// Only check if error is nil if we didn't cancel the context.
			require.NoError(t, err)
		}
	}()

	scanner := bufio.NewScanner(rd)
	var finished bool
	for scanner.Scan() {
		log := scanner.Text()

		t.Log(log)
		var blog envboxlog.BuildLog
		if err := json.Unmarshal([]byte(log), &blog); err != nil {
			continue
		}

		if blog.Type == envboxlog.BuildLogTypeYield {
			finished = true
			break
		}

		if blog.Type == envboxlog.BuildLogTypeYieldFail {
			t.Fatalf("envbox failed (%s)", blog.Msg)
		}
	}
	require.NoError(t, scanner.Err())
	require.True(t, finished, "unexpected logger exit")
}

type ExecConfig struct {
	ContainerID string
	Cmd         []string
	User        string
}

// ExecInnerContainer runs a command in the inner container.
func ExecInnerContainer(t *testing.T, pool *dockertest.Pool, conf ExecConfig) ([]byte, error) {
	t.Helper()

	conf.Cmd = append([]string{"docker", "exec", "workspace_cvm"}, conf.Cmd...)
	return ExecEnvbox(t, pool, conf)
}

// ExecEnvbox runs a command in the outer container.
func ExecEnvbox(t *testing.T, pool *dockertest.Pool, conf ExecConfig) ([]byte, error) {
	t.Helper()

	exec, err := pool.Client.CreateExec(docker.CreateExecOptions{
		Cmd:          conf.Cmd,
		AttachStdout: true,
		AttachStderr: true,
		User:         conf.User,
		Container:    conf.ContainerID,
	})
	require.NoError(t, err)

	var buf bytes.Buffer
	err = pool.Client.StartExec(exec.ID, docker.StartExecOptions{
		OutputStream: &buf,
		ErrorStream:  &buf,
	})
	require.NoError(t, err)

	insp, err := pool.Client.InspectExec(exec.ID)
	require.NoError(t, err)
	require.Equal(t, false, insp.Running)

	if insp.ExitCode > 0 {
		return nil, xerrors.Errorf("output(%s): exit code %d", buf.Bytes(), insp.ExitCode)
	}

	return buf.Bytes(), nil
}

// cmdLineEnvs returns args passed to the /envbox command
// but using their env var alias.
func cmdLineEnvs(c *CreateDockerCVMConfig) []string {
	envs := []string{
		envVar(cli.EnvInnerImage, c.Image),
		envVar(cli.EnvInnerUsername, c.Username),
	}

	if len(c.InnerEnvFilter) > 0 {
		envs = append(envs, envVar(cli.EnvInnerEnvs, strings.Join(c.InnerEnvFilter, ",")))
	}

	if len(c.Mounts) > 0 {
		envs = append(envs, envVar(cli.EnvMounts, strings.Join(c.Mounts, ",")))
	}

	if c.AddFUSE {
		envs = append(envs, envVar(cli.EnvAddFuse, "true"))
	}

	if c.AddTUN {
		envs = append(envs, envVar(cli.EnvAddTun, "true"))
	}

	if c.BootstrapScript != "" {
		envs = append(envs, envVar(cli.EnvBootstrap, c.BootstrapScript))
	}

	return envs
}

func envVar(k, v string) string {
	return fmt.Sprintf("%s=%s", k, v)
}
