package cli

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/stretchr/testify/require"

	"cdr.dev/slog/v3/sloggers/slogtest"
	"github.com/coder/envbox/dockerutil/dockerfake"
)

func TestShutdownInnerContainer(t *testing.T) {
	t.Parallel()

	t.Run("Stop", func(t *testing.T) {
		t.Parallel()

		var stopped bool
		client := dockerfake.MockClient{
			ContainerStopFn: func(ctx context.Context, name string, options container.StopOptions) error {
				stopped = true
				requireContextDeadlineWithin(t, ctx, time.Duration(innerContainerStopTimeout)*time.Second+innerContainerStopContextSlack)
				require.Equal(t, "container-id", name)
				require.NotNil(t, options.Timeout)
				require.Equal(t, innerContainerStopTimeout, *options.Timeout)
				return nil
			},
			ContainerKillFn: func(context.Context, string, string) error {
				t.Fatal("container should not be killed after clean stop")
				return nil
			},
			ContainerRemoveFn: func(context.Context, string, container.RemoveOptions) error {
				t.Fatal("container should not be force removed after clean stop")
				return nil
			},
		}

		shutdownInnerContainer(context.Background(), slogtest.Make(t, nil), client, "container-id")
		require.True(t, stopped)
	})

	t.Run("KillAndRemove", func(t *testing.T) {
		t.Parallel()

		var killed, removed bool
		client := dockerfake.MockClient{
			ContainerStopFn: func(context.Context, string, container.StopOptions) error {
				return errors.New("stop failed")
			},
			ContainerKillFn: func(_ context.Context, name string, signal string) error {
				killed = true
				require.Equal(t, "container-id", name)
				require.Equal(t, "SIGKILL", signal)
				return nil
			},
			ContainerRemoveFn: func(_ context.Context, name string, options container.RemoveOptions) error {
				removed = true
				require.Equal(t, "container-id", name)
				require.True(t, options.Force)
				require.False(t, options.RemoveVolumes)
				return nil
			},
		}

		log := slogtest.Make(t, &slogtest.Options{IgnoreErrors: true})

		shutdownInnerContainer(context.Background(), log, client, "container-id")
		require.True(t, killed)
		require.True(t, removed)
	})
}

func TestShutdownBootstrapExecUsesStepDeadline(t *testing.T) {
	t.Parallel()

	var inspected bool
	client := dockerfake.MockClient{
		ContainerExecInspectFn: func(ctx context.Context, execID string) (container.ExecInspect, error) {
			inspected = true
			require.Equal(t, "exec-id", execID)
			requireContextDeadlineWithin(t, ctx, bootstrapExecShutdownTimeout)
			return container.ExecInspect{}, errors.New("inspect failed")
		},
	}

	log := slogtest.Make(t, &slogtest.Options{IgnoreErrors: true})

	shutdownBootstrapExec(context.Background(), log, client, "exec-id")
	require.True(t, inspected)
}

func TestShutdownDockerCVMUsesBoundedContext(t *testing.T) {
	t.Parallel()

	var stopped bool
	client := dockerfake.MockClient{
		ContainerStopFn: func(ctx context.Context, name string, options container.StopOptions) error {
			stopped = true
			require.Equal(t, "container-id", name)
			require.NotNil(t, options.Timeout)
			require.Equal(t, innerContainerStopTimeout, *options.Timeout)
			requireContextDeadlineWithin(t, ctx, time.Duration(innerContainerStopTimeout)*time.Second+innerContainerStopContextSlack)
			return nil
		},
	}

	shutdownDockerCVM(slogtest.Make(t, nil), client, dockerCVMResult{containerID: "container-id"})
	require.True(t, stopped)
}

func requireContextDeadlineWithin(t *testing.T, ctx context.Context, max time.Duration) {
	t.Helper()

	deadline, ok := ctx.Deadline()
	require.True(t, ok, "context must have a deadline")
	remaining := time.Until(deadline)
	require.Positive(t, remaining)
	require.LessOrEqual(t, remaining, max)
}
