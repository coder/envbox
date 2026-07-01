package background

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"cdr.dev/slog/v3/sloggers/slogtest"
)

func TestProcessRestartAfterExit(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	process := New(ctx, slogtest.Make(t, nil), "false", "false")
	require.NoError(t, process.Start())

	err := <-process.Wait()
	require.Error(t, err)

	require.NoError(t, process.Restart(ctx, "sleep", "sleep", "30"))

	waitCh := process.Wait()
	cancel()

	select {
	case <-waitCh:
	case <-time.After(5 * time.Second):
		t.Fatal("restarted process did not exit after context cancellation")
	}
}

func TestProcessRestartClosesOldWaitChannel(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	process := New(ctx, slogtest.Make(t, nil), "sleep", "sleep", "30")
	require.NoError(t, process.Start())

	oldWaitCh := process.Wait()
	require.NoError(t, process.Restart(ctx, "sleep", "sleep", "30"))

	select {
	case err := <-oldWaitCh:
		require.ErrorIs(t, err, ErrUserKilled)
	case <-time.After(5 * time.Second):
		t.Fatal("old wait channel did not close after restart")
	}

	newWaitCh := process.Wait()
	cancel()

	select {
	case <-newWaitCh:
	case <-time.After(5 * time.Second):
		t.Fatal("new wait channel did not close after context cancellation")
	}
}
