package background_test

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"cdr.dev/slog/v3"
	"cdr.dev/slog/v3/sloggers/slogtest"
	"github.com/coder/envbox/background"
)

func TestIsCriticalOutput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		line string
		want bool
	}{
		{
			// The line from the original report, emitted before dockerd's
			// logger starts.
			name: "DockerdNoSpace",
			line: "failed to start daemon: Unable to get the TempDir under /var/lib/docker/100000.100000: mkdir /var/lib/docker/100000.100000/tmp: no space left on device",
			want: true,
		},
		{
			name: "SysboxFatal",
			line: `time="2026-11-24 18:00:00" level=fatal msg="failed to start sysbox-mgr: some error"`,
			want: true,
		},
		{
			name: "SysboxFsStartError",
			line: `level=error msg="failed to start sysbox-fs: ipc init"`,
			want: true,
		},
		{
			name: "CaseInsensitive",
			line: "No Space Left On Device",
			want: true,
		},
		{
			name: "DockerdInfo",
			line: `level=info msg="API listen on /var/run/docker.sock"`,
			want: false,
		},
		{
			name: "DockerdError",
			line: `level=error msg="Handler for GET /v1.47/containers/foo/json returned error: No such container: foo"`,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, background.IsCriticalOutput(tt.line))
		})
	}
}

// testLogger ignores error logs, since critical child output is logged at the
// error level.
func testLogger(t *testing.T) slog.Logger {
	return slogtest.Make(t, &slogtest.Options{IgnoreErrors: true})
}

// lineRecorder is a thread-safe OnCriticalOutput handler.
type lineRecorder struct {
	mu    sync.Mutex
	lines []string
}

func (r *lineRecorder) record(line string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines = append(r.lines, line)
}

func (r *lineRecorder) get() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.lines...)
}

func TestProcessOnCriticalOutput(t *testing.T) {
	t.Parallel()

	t.Run("ForwardsCriticalLinesBeforeExit", func(t *testing.T) {
		t.Parallel()

		var rec lineRecorder
		script := `
echo 'level=info msg="starting up"'
echo 'failed to start daemon: mkdir /var/lib/docker/tmp: no space left on device' >&2
echo 'regular output'
exit 1`
		err := <-background.New(context.Background(), testLogger(t), "sh", "sh", "-c", script).
			OnCriticalOutput(rec.record).
			Run()
		require.Error(t, err)

		// Checked without waiting: the exit is only reported after the
		// handler has received every forwarded line.
		require.Equal(t, []string{
			"failed to start daemon: mkdir /var/lib/docker/tmp: no space left on device",
		}, rec.get())
	})

	t.Run("DedupesAndCaps", func(t *testing.T) {
		t.Parallel()

		var rec lineRecorder
		script := `
for i in 1 2 3; do echo 'level=fatal msg="repeated"'; done
for i in $(seq 1 20); do echo "level=fatal msg=\"line $i\""; done`
		err := <-background.New(context.Background(), testLogger(t), "sh", "sh", "-c", script).
			OnCriticalOutput(rec.record).
			Run()
		require.NoError(t, err)

		want := []string{`level=fatal msg="repeated"`}
		for i := 1; len(want) < 10; i++ {
			want = append(want, `level=fatal msg="line `+strconv.Itoa(i)+`"`)
		}
		require.Equal(t, want, rec.get())
	})

	t.Run("SlowHandlerDoesNotBlockProcess", func(t *testing.T) {
		t.Parallel()

		marker := filepath.Join(t.TempDir(), "done")
		release := make(chan struct{})
		var rec lineRecorder

		// The process writes far more than a pipe buffer after the
		// critical line, then creates the marker file. If the handler
		// blocked output, the marker would never appear.
		script := `
echo 'level=fatal msg="boom"'
head -c 1048576 /dev/zero | tr '\0' 'a' | fold -w 1024
touch "$1"`
		p := background.New(context.Background(), testLogger(t), "sh", "sh", "-c", script, "sh", marker).
			OnCriticalOutput(func(line string) {
				<-release
				rec.record(line)
			})
		require.NoError(t, p.Start())

		require.Eventually(t, func() bool {
			_, err := os.Stat(marker)
			return err == nil
		}, 10*time.Second, 10*time.Millisecond)

		close(release)
		require.NoError(t, <-p.Wait())
		require.Equal(t, []string{`level=fatal msg="boom"`}, rec.get())
	})
}
