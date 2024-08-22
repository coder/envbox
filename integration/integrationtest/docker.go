package integrationtest

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
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
	CPUs            int
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
	TLSEnable bool
	TLSCert   string
	TLSKey    string
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
		host.CPUPeriod = int64(dockerutil.DefaultCPUPeriod)
		host.CPUQuota = int64(conf.CPUs) * int64(dockerutil.DefaultCPUPeriod)
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
	tmpdir, err := os.MkdirTemp("", strings.ReplaceAll(t.Name(), "/", "_"))
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

func UnsafeTLSCert(t *testing.T) *tls.Certificate {
	t.Helper()

	certBlock, _ := pem.Decode([]byte(SelfSignedCert))
	require.NotNil(t, certBlock)

	cert, err := x509.ParseCertificate(certBlock.Bytes)
	require.NoError(t, err)

	keyBlock, _ := pem.Decode([]byte(SelfSignedKey))
	require.NotNil(t, keyBlock)

	key, err := x509.ParsePKCS8PrivateKey(keyBlock.Bytes)
	require.NoError(t, err)

	return &tls.Certificate{
		Certificate: [][]byte{cert.Raw},
		PrivateKey:  key,
	}
}

func WriteFile(t *testing.T, path, contents string, perms os.FileMode) {
	t.Helper()

	err := os.WriteFile(path, []byte(contents), 0644)
	require.NoError(t, err)
}

const SelfSignedKey = `-----BEGIN PRIVATE KEY-----
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
xD5tMvzZR71+Fyi0l5QFgR0kKksfcQ==
-----END PRIVATE KEY-----`

const SelfSignedCert = `-----BEGIN CERTIFICATE-----
MIIFDzCCAvegAwIBAgIUBbwBOjaGxuQRwFH/xyWH96Q5qRQwDQYJKoZIhvcNAQEL
BQAwITELMAkGA1UEBhMCVVMxEjAQBgNVBAMMCWxvY2FsaG9zdDAgFw0yNDA4MjIw
MTMwMzJaGA8yMTI0MDcyOTAxMzAzMlowITELMAkGA1UEBhMCVVMxEjAQBgNVBAMM
CWxvY2FsaG9zdDCCAiIwDQYJKoZIhvcNAQEBBQADggIPADCCAgoCggIBAKZARm9L
ntzqvB9WIJZ2sySu3L2qs543KkK2oLa4r/sy2KcdmSxPkKlC0CZqg8aLGbJn3dnz
0MDsdOfS57+0D2X7/GADqSojrShjysvLY5N7eF9lyFdNxkn5nje4V3WhAkIcWhmi
UgcB3lPAr/AlI6rQPP8o4PekRVVUFfM9k6OBnlSyW3Sad4+jZuoqvmHEWWNYYoBf
C0JKbnFkmj86vE7z4++StqYbEg93zscp2kY88FqA1ZfRPJJSUeSqPU8Vlt/Dim69
9uO+0Z1QVwClphGpsBaoe8TM82GsQcbAGb+mQnofLe2dgW6+BJjT4ufO0iCykiuo
EnFgN9bL5MylRTw/cK1bKByinAWYWbpMzfA9wRCIsOuo/wUdvOlqhH8BbIZSsQd1
wPok/CUrA6YXjT/kqiBp8xFbCahVnu/sjVyyZ86ZlWxZGS6HUklISzuqCbqGMGiV
biW1ZhiQBds8CVc3+u0C4WI6MT2sJA5F1EOw16dVYie05Xrj+/Ejvlup6PgcBoF/
0Z8jbl8MHZaNtSSzIDWjDOoK6DsSkWmaxi0Jzu7AvWpttWz1ud0DHQFYLec7qnV7
pMIy81W13DIp471Et1mDnu6QhPS7lTtjXwsrVUrqh2HiULqCmZW2Zy4oI7HPRUWc
IKEIFQl+3AAZWGITxh0K4JBfm14OeDOtxfrFAgMBAAGjPTA7MBoGA1UdEQQTMBGC
CWxvY2FsaG9zdIcEfwAAATAdBgNVHQ4EFgQUv86/KL5Qi8xVEnEMXlumAO8FI/Ew
DQYJKoZIhvcNAQELBQADggIBAD2P/O3KcWlY81DHOr3AA1ZUAi81Y/V/vviakM/p
sc97iTSmC/xfux0M2HhaW6qJBIESTcWrvSAsKjiOqY/p/1O13bwDJ493psyfmE2W
CTNUdPPusvQ/LF5HCg9B3glRz2on8fPVyhfI2xhTliQ6jpT0m1C9nFFtki3xsZf8
0WkMo13wsCrqZ79IJ9GeNXqAM+gFP/skHJQ5JI3hbOlFlCrB9LiMlRiVgW+uoOi+
wNmioHMGmDBG8TgldJ9hmP/Ed1JmioyBK/wh+SM/c9LS6lBT6d0iRNffYxS36dh0
Xr5lYn/8gClm33mkVzh4J73byqR0jsuCQD1LgpMR/tbd4n8E/25UFBiO/FGW19NG
52IRytBLJcTuNN9e3o2YQs0N+tfQxCBIfUep15cUNbAYlNsNPErX6zGoCkhYiMYw
w/SB6336cErL+7kMp3H9FXaiDvlldJ3+mAbROa2Sz9Re5q0zepynBSflJ8kHDNFi
CeQi+PSR5stOuz13RTWgygtFXE9gUKCvk2mid/JA/Q8BfD0rcuFr5N5B0AKBrtUu
RAfefTglzhqADFY9lLfqjsE58i/uhf4FdvxEYWO6/SvDo7WzJe9KGihMMSr9q/ux
NDEfyem1ELLynf8J7BxqDn6GvKZYaBkZDBskaBovwv5dGWE+rjuekor0mYt64NCa
tCAG
-----END CERTIFICATE-----`

func EnvVar(key, value string) string {
	return fmt.Sprintf("%s=%s", key, value)

}

func BindMount(src, dest string, ro bool) string {
	if ro {
		return fmt.Sprintf("%s:%s:%s", src, dest, "ro")
	}
	return fmt.Sprintf("%s:%s", src, dest)
}
