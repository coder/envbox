package integrationtest

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
	"github.com/stretchr/testify/require"
	"golang.org/x/xerrors"

	"github.com/coder/envbox/buildlog"
	"github.com/coder/envbox/cli"
	"github.com/coder/envbox/dockerutil"
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

	// RegistryImage is used to assert that we add certs
	// correctly to the docker daemon when pulling an image
	// from a registry with a self signed cert.
	registryImage = "gcr.io/coder-dev-1/sreya/registry"
	registryTag   = "2.8.3"
)

// TODO use df to determine if an environment is running in a docker container or not.

type CreateDockerCVMConfig struct {
	Image           string
	Username        string
	BootstrapScript string
	InnerEnvFilter  []string
	Envs            []string

	OuterMounts []docker.HostMount
	InnerMounts []string
	AddFUSE     bool
	AddTUN      bool
	CPUs        int
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

type CoderdOptions struct {
	TLSEnable    bool
	TLSCert      string
	TLSKey       string
	DefaultImage string
}

// RunEnvbox runs envbox, it returns once the inner container has finished
// spinning up.
func RunEnvbox(t *testing.T, pool *dockertest.Pool, conf *CreateDockerCVMConfig) *dockertest.Resource {
	t.Helper()

	conf.validate(t)

	// If binds aren't passed then we'll just create the minimum amount.
	// If someone is passing them we'll assume they know what they're doing.
	if conf.OuterMounts == nil {
		tmpdir := TmpDir(t)
		conf.OuterMounts = DefaultBinds(t, tmpdir)
	}

	conf.Envs = append(conf.Envs, cmdLineEnvs(conf)...)

	resource, err := pool.RunWithOptions(&dockertest.RunOptions{
		Repository: "envbox",
		Tag:        "latest",
		Entrypoint: []string{"/envbox", "docker"},
		Env:        conf.Envs,
	}, func(host *docker.HostConfig) {
		host.Mounts = conf.OuterMounts
		host.Privileged = true
		host.CPUPeriod = int64(dockerutil.DefaultCPUPeriod)
		host.CPUQuota = int64(conf.CPUs) * int64(dockerutil.DefaultCPUPeriod)
		host.ExtraHosts = []string{"host.docker.internal:host-gateway"}
	})
	require.NoError(t, err)
	// t.Cleanup(func() { _ = pool.Purge(resource) })

	waitForCVM(t, pool, resource)

	return resource
}

// DefaultBinds returns the minimum amount of mounts necessary to spawn
// envbox successfully. Since envbox will chown some of these directories
// to root, they cannot be cleaned up post-test, meaning that it may be
// necesssary to manually clear /tmp from time to time.
func DefaultBinds(t *testing.T, rootDir string) []docker.HostMount {
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

	return []docker.HostMount{
		{
			Source: cntDockerDir,
			Target: "/var/lib/coder/docker",
			Type:   "bind",
		},
		{
			Source: cntDir,
			Target: "/var/lib/coder/containers",
			Type:   "bind",
		},
		{
			Source:   "/usr/src",
			Target:   "/usr/src",
			Type:     "bind",
			ReadOnly: true,
		},
		{
			Source:   "/lib/modules",
			Target:   "/lib/modules",
			Type:     "bind",
			ReadOnly: true,
		},
		{
			Source: sysbox,
			Target: "/var/lib/sysbox",
			Type:   "bind",
		},
		{
			Source: dockerDir,
			Target: "/var/lib/docker",
			Type:   "bind",
		},
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
		var blog buildlog.JSONLog
		if err := json.Unmarshal([]byte(log), &blog); err != nil {
			continue
		}

		if blog.Type == buildlog.JSONLogTypeDone {
			finished = true
			break
		}

		if blog.Type == buildlog.JSONLogTypeError {
			t.Fatalf("envbox failed (%s)", blog.Output)
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

	cmd, err := pool.Client.CreateExec(docker.CreateExecOptions{
		Cmd:          conf.Cmd,
		AttachStdout: true,
		AttachStderr: true,
		User:         conf.User,
		Container:    conf.ContainerID,
	})
	require.NoError(t, err)

	var buf bytes.Buffer
	err = pool.Client.StartExec(cmd.ID, docker.StartExecOptions{
		OutputStream: &buf,
		ErrorStream:  &buf,
	})
	require.NoError(t, err)

	insp, err := pool.Client.InspectExec(cmd.ID)
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
		EnvVar(cli.EnvInnerImage, c.Image),
		EnvVar(cli.EnvInnerUsername, c.Username),
	}

	if len(c.InnerEnvFilter) > 0 {
		envs = append(envs, EnvVar(cli.EnvInnerEnvs, strings.Join(c.InnerEnvFilter, ",")))
	}

	if len(c.InnerMounts) > 0 {
		envs = append(envs, EnvVar(cli.EnvMounts, strings.Join(c.InnerMounts, ",")))
	}

	if c.AddFUSE {
		envs = append(envs, EnvVar(cli.EnvAddFuse, "true"))
	}

	if c.AddTUN {
		envs = append(envs, EnvVar(cli.EnvAddTun, "true"))
	}

	if c.BootstrapScript != "" {
		envs = append(envs, EnvVar(cli.EnvBootstrap, c.BootstrapScript))
	}

	return envs
}

func EnvVar(k, v string) string {
	return fmt.Sprintf("%s=%s", k, v)
}

func DockerBridgeIP(t testing.TB) string {
	t.Helper()

	ifaces, err := net.Interfaces()
	require.NoError(t, err)

	for _, iface := range ifaces {
		if iface.Name != "docker0" {
			continue
		}
		addrs, err := iface.Addrs()
		require.NoError(t, err)

		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
				if ipnet.IP.To4() != nil {
					return ipnet.IP.String()
				}
			}
		}
	}

	t.Fatalf("failed to find docker bridge interface")
	return ""
}

type RegistryConfig struct {
	HostCertPath string
	HostKeyPath  string
	TLSPort      string
	Image        string
}

type RegistryImage string

func (r RegistryImage) Registry() string {
	return strings.Split(string(r), "/")[0]
}

func (r RegistryImage) String() string {
	return string(r)
}

func RunLocalDockerRegistry(t testing.TB, pool *dockertest.Pool, conf RegistryConfig) RegistryImage {
	t.Helper()

	const (
		certPath = "/certs/cert.pem"
		keyPath  = "/certs/key.pem"
	)

	resource, err := pool.RunWithOptions(&dockertest.RunOptions{
		Repository: registryImage,
		Tag:        registryTag,
		Env: []string{
			EnvVar("REGISTRY_HTTP_TLS_CERTIFICATE", certPath),
			EnvVar("REGISTRY_HTTP_TLS_KEY", keyPath),
			EnvVar("REGISTRY_HTTP_ADDR", "0.0.0.0:443"),
		},
		ExposedPorts: []string{"443/tcp"},
	}, func(host *docker.HostConfig) {
		host.Binds = []string{
			mountBinding(conf.HostCertPath, certPath),
			mountBinding(conf.HostKeyPath, keyPath),
		}
		host.ExtraHosts = []string{"host.docker.internal:host-gateway"}
		host.PortBindings = map[docker.Port][]docker.PortBinding{
			"443/tcp": {{
				HostIP:   "0.0.0.0",
				HostPort: conf.TLSPort,
			}},
		}
	})
	require.NoError(t, err)

	host := net.JoinHostPort("0.0.0.0", conf.TLSPort)
	url := fmt.Sprintf("https://%s/v2/_catalog", host)

	waitForRegistry(t, pool, resource, url)
	return pushLocalImage(t, pool, host, conf.Image)
}

func waitForRegistry(t testing.TB, pool *dockertest.Pool, resource *dockertest.Resource, url string) {
	t.Helper()

	//nolint:forcetypeassert
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{
		// We're not interested in asserting the validity
		// of the certificate when pushing the image
		// since this is setup.
		//nolint:gosec
		InsecureSkipVerify: true,
	}
	client := &http.Client{
		Transport: transport,
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	for r := retry.New(time.Second, time.Second); r.Wait(ctx); {
		container, err := pool.Client.InspectContainer(resource.Container.ID)
		require.NoError(t, err)
		require.True(t, container.State.Running, "%v unexpectedly exited", container.ID)

		//nolint:noctx
		res, err := client.Get(url)
		if err != nil {
			continue
		}
		_ = res.Body.Close()
		if res.StatusCode == http.StatusOK {
			return
		}
	}
	require.NoError(t, ctx.Err())
}

func pushLocalImage(t testing.TB, pool *dockertest.Pool, host, remoteImage string) RegistryImage {
	t.Helper()

	const registryHost = "127.0.0.1"
	name := filepath.Base(remoteImage)
	repoTag := strings.Split(name, ":")
	tag := "latest"
	if len(repoTag) == 2 {
		tag = repoTag[1]
	}

	tw := &testWriter{
		t: t,
	}
	err := pool.Client.PullImage(docker.PullImageOptions{
		Repository:   strings.Split(remoteImage, ":")[0],
		Tag:          tag,
		OutputStream: tw,
	}, docker.AuthConfiguration{})
	require.NoError(t, err)

	_, port, err := net.SplitHostPort(host)
	require.NoError(t, err)

	err = pool.Client.TagImage(remoteImage, docker.TagImageOptions{
		Repo: fmt.Sprintf("%s:%s/%s", registryHost, port, name),
		Tag:  tag,
	})
	require.NoError(t, err)

	// Idk what to tell you but the pool.Client.PushImage
	// function is bugged or I'm just dumb...
	image := fmt.Sprintf("%s:%s/%s:%s", registryHost, port, name, tag)
	cmd := exec.Command("docker", "push", image)
	cmd.Stderr = tw
	cmd.Stdout = tw
	err = cmd.Run()
	require.NoError(t, err)
	return RegistryImage(fmt.Sprintf("host.docker.internal:%s/%s:%s", port, name, tag))
}

func mountBinding(src, dst string) string {
	return fmt.Sprintf("%s:%s", src, dst)
}

type testWriter struct {
	t testing.TB
}

func (t *testWriter) Write(b []byte) (int, error) {
	t.t.Logf("%s", b)
	return len(b), nil
}

func BindMount(src, dst string, ro bool) docker.HostMount {
	return docker.HostMount{
		Source:   src,
		Target:   dst,
		ReadOnly: ro,
		Type:     "bind",
	}
}
