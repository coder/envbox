package integration

import (
	"context"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/coder/envbox/integration/integrationtest"
)

func TestDocker_Nvidia(t *testing.T) {
	// Only run this test if the nvidia container runtime is detected.
	// Check if the nvidia runtime is available using `docker info`.
	// The docker client doesn't expose this information so we need to fetch it ourselves.
	if !slices.Contains(dockerRuntimes(t), "nvidia") {
		t.Skip("this test requires nvidia runtime to be available")
	}

	t.Run("Ubuntu", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)

		// Start the envbox container.
		ctID := startEnvboxCmd(ctx, t, integrationtest.UbuntuImage, "root",
			"--env", "CODER_ADD_GPU=true",
			"--env", "CODER_USR_LIB_DIR=/usr/lib/x86_64-linux-gnu",
			"--runtime=nvidia",
			"--gpus=all",
		)

		// Assert that we can run nvidia-smi in the inner container.
		_, err := execContainerCmd(ctx, t, ctID, "docker", "exec", "workspace_cvm", "nvidia-smi")
		require.NoError(t, err, "failed to run nvidia-smi in the inner container")
	})

	t.Run("Redhat", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)

		// Start the envbox container.
		ctID := startEnvboxCmd(ctx, t, integrationtest.UbuntuImage, "root",
			"--env", "CODER_ADD_GPU=true",
			"--env", "CODER_USR_LIB_DIR=/usr/lib/x86_64-linux-gnu",
			"--runtime=nvidia",
			"--gpus=all",
		)

		// Assert that we can run nvidia-smi in the inner container.
		_, err := execContainerCmd(ctx, t, ctID, "docker", "exec", "workspace_cvm", "nvidia-smi")
		require.NoError(t, err, "failed to run nvidia-smi in the inner container")
	})
}

// dockerRuntimes returns the list of container runtimes available on the host.
// It does this by running `docker info` and parsing the output.
func dockerRuntimes(t *testing.T) []string {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", "info", "--format", "{{ range $k, $v := .Runtimes}}{{ println $k }}{{ end }}")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "failed to get docker runtimes: %s", out)
	raw := strings.TrimSpace(string(out))
	return strings.Split(raw, "\n")
}

func startEnvboxCmd(ctx context.Context, t *testing.T, innerImage, innerUser string, addlArgs ...string) (containerID string) {
	t.Helper()

	var (
		tmpDir            = integrationtest.TmpDir(t)
		binds             = integrationtest.DefaultBinds(t, tmpDir)
		cancelCtx, cancel = context.WithCancel(ctx)
	)
	t.Cleanup(cancel)

	// Unfortunately ory/dockertest does not allow us to specify runtime.
	// We're instead going to just run the container directly via the docker cli.
	startEnvboxArgs := []string{
		"run",
		"--detach",
		"--rm",
		"--privileged",
		"--env", "CODER_INNER_IMAGE=" + innerImage,
		"--env", "CODER_INNER_USERNAME=" + innerUser,
	}
	for _, bind := range binds {
		bindParts := []string{bind.Source, bind.Target}
		if bind.ReadOnly {
			bindParts = append(bindParts, "ro")
		}
		startEnvboxArgs = append(startEnvboxArgs, []string{"-v", strings.Join(bindParts, ":")}...)
	}
	startEnvboxArgs = append(startEnvboxArgs, addlArgs...)
	startEnvboxArgs = append(startEnvboxArgs, "envbox:latest", "/envbox", "docker")
	t.Logf("envbox docker cmd: docker %s", strings.Join(startEnvboxArgs, " "))

	// Start the envbox container without attaching.
	startEnvboxCmd := exec.CommandContext(cancelCtx, "docker", startEnvboxArgs...)
	out, err := startEnvboxCmd.CombinedOutput()
	require.NoError(t, err, "failed to start envbox container")
	containerID = strings.TrimSpace(string(out))
	t.Logf("envbox container ID: %s", containerID)
	t.Cleanup(func() {
		if t.Failed() {
			// Dump the logs if the test failed.
			logsCmd := exec.Command("docker", "logs", containerID)
			out, err := logsCmd.CombinedOutput()
			if err != nil {
				t.Logf("failed to read logs: %s", err)
			}
			t.Logf("envbox logs:\n%s", string(out))
		}
		// Stop the envbox container.
		stopEnvboxCmd := exec.Command("docker", "rm", "-f", containerID)
		out, err := stopEnvboxCmd.CombinedOutput()
		if err != nil {
			t.Errorf("failed to stop envbox container: %s", out)
		}
	})

	// Wait for the Docker CVM to come up.
	waitCtx, waitCancel := context.WithTimeout(cancelCtx, 5*time.Minute)
	defer waitCancel()
WAITLOOP:
	for {
		select {
		case <-waitCtx.Done():
			t.Fatal("timed out waiting for inner container to come up")
		default:
			execCmd := exec.CommandContext(cancelCtx, "docker", "exec", containerID, "docker", "inspect", "workspace_cvm")
			out, err := execCmd.CombinedOutput()
			if err != nil {
				t.Logf("waiting for inner container to come up:\n%s", string(out))
				<-time.After(time.Second)
				continue WAITLOOP
			}
			t.Logf("inner container is up")
			break WAITLOOP
		}
	}

	return containerID
}

func execContainerCmd(ctx context.Context, t *testing.T, containerID string, cmdArgs ...string) (string, error) {
	t.Helper()

	execArgs := []string{"exec", containerID}
	execArgs = append(execArgs, cmdArgs...)
	t.Logf("exec cmd: docker %s", strings.Join(execArgs, " "))
	execCmd := exec.CommandContext(ctx, "docker", execArgs...)
	out, err := execCmd.CombinedOutput()
	if err != nil {
		t.Logf("exec cmd failed: %s\n%s", err.Error(), string(out))
	} else {
		t.Logf("exec cmd success: %s", out)
	}
	return strings.TrimSpace(string(out)), err
}
