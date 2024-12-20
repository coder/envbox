package cli_test

import (
	"fmt"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
	testingexec "k8s.io/utils/exec/testing"

	"github.com/coder/envbox/cli"
	"github.com/coder/envbox/cli/clitest"
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

		called := make(chan struct{})
		execer := clitest.Execer(ctx)
		execer.AddCommands(&xunixfake.FakeCmd{
			FakeCmd: &testingexec.FakeCmd{
				Argv: []string{
					"sysbox-mgr",
				},
			},
			WaitFn: func() error { close(called); select {} }, //nolint:revive
		})

		err := cmd.ExecuteContext(ctx)
		<-called
		require.NoError(t, err)
		execer.AssertCommandsCalled(t)
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

	t.Run("DisableIDMappedMounts", func(t *testing.T) {
		t.Parallel()

		ctx, cmd := clitest.New(t, "docker",
			"--image=ubuntu",
			"--username=root",
			"--agent-token=hi",
			"--disable-idmapped-mount",
		)

		called := make(chan struct{})
		execer := clitest.Execer(ctx)
		execer.AddCommands(&xunixfake.FakeCmd{
			FakeCmd: &testingexec.FakeCmd{
				Argv: []string{
					"sysbox-mgr",
					"--disable-idmapped-mount",
				},
			},
			WaitFn: func() error { close(called); select {} }, //nolint:revive
		})

		err := cmd.ExecuteContext(ctx)
		<-called
		require.NoError(t, err)
		execer.AssertCommandsCalled(t)
	})
}
