package buildlog_test

import (
	"sync"
	"testing"
	"time"

	"golang.org/x/exp/slices"
	"golang.org/x/net/context"

	"cdr.dev/slog/sloggers/slogtest"

	"github.com/stretchr/testify/require"

	"github.com/coder/coder/codersdk/agentsdk"
	"github.com/coder/coder/cryptorand"
	"github.com/coder/envbox/buildlog"
)

func TestCoderLog(t *testing.T) {
	t.Parallel()

	t.Run("OK", func(t *testing.T) {
		t.Parallel()

		var (
			client  = &fakeCoderClient{}
			ctx     = context.Background()
			slogger = slogtest.Make(t, nil)
			logMu   sync.Mutex
		)

		expectedLog, err := cryptorand.String(10)
		require.NoError(t, err)
		var actualLog string
		client.PatchStartupLogsFn = func(ctx context.Context, logs agentsdk.PatchStartupLogs) error {
			logMu.Lock()
			defer logMu.Unlock()

			require.Len(t, logs.Logs, 1)
			require.NotZero(t, logs.Logs[0].CreatedAt)
			require.Equal(t, expectedLog, logs.Logs[0].Output)
			actualLog = logs.Logs[0].Output
			return nil
		}

		log := buildlog.OpenCoderLogger(ctx, client, slogger)

		log.Info(expectedLog)

		require.Eventually(t, func() bool {
			logMu.Lock()
			defer logMu.Unlock()
			equal := actualLog == expectedLog
			if !equal {
				t.Logf("actual log %q does not equal expected log %q", actualLog, expectedLog)
			}
			return equal
		}, time.Millisecond*5, time.Millisecond)
	})

	// Try sending a large line that exceeds the maximum Coder accepts (1KiB).
	// Assert that it is sent as two logs instead.
	t.Run("OutputNotDropped", func(t *testing.T) {
		t.Parallel()

		var (
			maxLogs    = 2
			client     = fakeCoderClient{}
			ctx        = context.Background()
			slogger    = slogtest.Make(t, nil)
			actualLogs = make([]string, 0, maxLogs)
			logMu      sync.Mutex
		)
		client.PatchStartupLogsFn = func(ctx context.Context, logs agentsdk.PatchStartupLogs) error {
			logMu.Lock()
			defer logMu.Unlock()

			require.Len(t, logs.Logs, maxLogs)
			for _, l := range logs.Logs {
				require.NotZero(t, l.CreatedAt)
				actualLogs = append(actualLogs, l.Output)
			}
			return nil
		}

		log := buildlog.OpenCoderLogger(ctx, client, slogger)

		bigLine, err := cryptorand.String(buildlog.MaxCoderLogSize + buildlog.MaxCoderLogSize/2)
		require.NoError(t, err)
		// The line should be chopped up into smaller logs so that we don't
		// drop output.
		expectedLogs := []string{
			bigLine[:buildlog.MaxCoderLogSize],
			bigLine[buildlog.MaxCoderLogSize:],
		}
		log.Info(bigLine)
		// Close the logger to flush the logs.
		log.Close()
		require.Eventually(t, func() bool {
			logMu.Lock()
			defer logMu.Unlock()
			return slices.Equal(expectedLogs, actualLogs)
		}, time.Millisecond*5, time.Millisecond)
	})
}

type fakeCoderClient struct {
	PatchStartupLogsFn func(context.Context, agentsdk.PatchStartupLogs) error
}

func (f fakeCoderClient) PatchStartupLogs(ctx context.Context, req agentsdk.PatchStartupLogs) error {
	if f.PatchStartupLogsFn != nil {
		return f.PatchStartupLogsFn(ctx, req)
	}
	return nil
}
