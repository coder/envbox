package cli

import (
	"context"
	"errors"
	"testing"

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
			ContainerStopFn: func(_ context.Context, name string, options container.StopOptions) error {
				stopped = true
				require.Equal(t, "container-id", name)
				require.NotNil(t, options.Timeout)
				require.Equal(t, 20, *options.Timeout)
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
