package background

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/spf13/afero"
	"golang.org/x/xerrors"

	"cdr.dev/slog/v3"
	"github.com/coder/envbox/xio"
	"github.com/coder/envbox/xunix"
)

// Process is an abstraction for running a command as a background process.
type Process struct {
	ctx     context.Context
	cancel  context.CancelFunc
	log     slog.Logger
	cmd     xunix.Cmd
	binName string

	userKilled *int64
	waitCh     chan error
	mu         sync.Mutex

	onCriticalOutput func(line string)
}

const (
	// maxCriticalLines caps how many critical lines a single run of a
	// process forwards to its OnCriticalOutput handler.
	maxCriticalLines = 10
	// criticalFlushTimeout bounds how long reporting a process exit waits for
	// the OnCriticalOutput handler to finish with already forwarded lines.
	criticalFlushTimeout = 5 * time.Second
)

// criticalOutputPatterns are lowercase substrings of child output that point
// at an actionable startup failure. Keep this list short: every match is
// shown to the user in the workspace startup logs.
var criticalOutputPatterns = []string{
	// Disk exhaustion, from dockerd or sysbox.
	"no space left on device",
	// dockerd prints this to stderr before its logger is set up, so the
	// line has no level= field.
	"failed to start daemon",
	// sysbox-mgr and sysbox-fs startup errors.
	"failed to create sysbox-mgr",
	"failed to start sysbox-mgr",
	"failed to start sysbox-fs",
	// logrus fatal level, used by dockerd, sysbox-mgr, and sysbox-fs right
	// before they exit.
	"level=fatal",
}

// IsCriticalOutput reports whether a line of child process output describes
// a failure that should be shown to the user, rather than only written to the
// envbox container's logs.
func IsCriticalOutput(line string) bool {
	line = strings.ToLower(line)
	for _, pattern := range criticalOutputPatterns {
		if strings.Contains(line, pattern) {
			return true
		}
	}
	return false
}

// New returns an instantiated daemon. binName is the expected
// /proc/<pid>/cmdline value used for exit detection; pass cmd for plain
// invocations, or the post-exec binary name for exec wrappers (e.g.
// "dockerd" when cmd is "unshare ... -- sh -c 'exec dockerd ...'").
func New(ctx context.Context, log slog.Logger, binName, cmd string, args ...string) *Process {
	ctx, cancel := context.WithCancel(ctx)
	return &Process{
		ctx:        ctx,
		cancel:     cancel,
		waitCh:     make(chan error, 1),
		cmd:        xunix.GetExecer(ctx).CommandContext(ctx, cmd, args...),
		log:        log.Named(cmd),
		userKilled: i64ptr(0),
		binName:    binName,
	}
}

// OnCriticalOutput registers fn to receive lines of the process's output for
// which IsCriticalOutput is true. Each run of the process forwards a line at
// most once and at most maxCriticalLines lines in total. fn runs on its own
// goroutine so a slow fn never blocks the process. The channel returned by
// Wait does not report an exit until fn has returned for every forwarded
// line, or until criticalFlushTimeout passes. Call it before Start or Run;
// the handler also applies after Restart.
func (d *Process) OnCriticalOutput(fn func(line string)) *Process {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.onCriticalOutput = fn
	return d
}

// Start starts the daemon. It functions akin to ox/exec.Command.Start().
func (d *Process) Start() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.startProcess()
}

// Wait waits for the process to exit, returning the error on the provided
// channel.
func (d *Process) Wait() <-chan error {
	d.mu.Lock()
	waitCh := d.waitCh
	d.mu.Unlock()

	return waitCh
}

// Run runs the command and waits for it to exit. It is a convenience
// function that combines both Start() and Wait().
func (d *Process) Run() <-chan error {
	err := d.Start()
	if err != nil {
		ch := make(chan error, 1)
		ch <- err
		return ch
	}

	return d.Wait()
}

// Restart kill the running process and reruns the command with the updated
// binName, cmd and args. See New for the meaning of binName.
func (d *Process) Restart(ctx context.Context, binName, cmd string, args ...string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	err := d.kill(syscall.SIGTERM)
	if err != nil {
		return xerrors.Errorf("kill cmd: %w", err)
	}

	ctx, cancel := context.WithCancel(ctx)
	d.ctx = ctx
	d.cancel = cancel
	d.cmd = xunix.GetExecer(ctx).CommandContext(ctx, cmd, args...)
	d.waitCh = make(chan error, 1)
	d.userKilled = i64ptr(0)
	d.binName = binName

	return d.startProcess()
}

func (d *Process) startProcess() error {
	var (
		buf bytes.Buffer

		pr, pw = io.Pipe()
		// Wrap our buffer in a limiter to
		// avoid ballooning our memory use over time.
		psw = &xio.PrefixSuffixWriter{
			N: 1 << 10,
			W: &buf,
		}

		w   = &xio.SyncWriter{W: pw}
		out = &xio.SyncWriter{W: psw}

		mw  = io.MultiWriter(w, out)
		cmd = d.cmd
	)

	cmd.SetStdout(mw)
	cmd.SetStderr(mw)

	scanDone := make(chan struct{})
	go func() {
		defer close(scanDone)
		scanIntoLog(d.ctx, d.log, bufio.NewScanner(pr), d.binName, d.onCriticalOutput)
	}()

	err := d.cmd.Start()
	if err != nil {
		return xerrors.Errorf("start: %w", err)
	}

	userKilled := d.userKilled

	go func() {
		defer d.cancel()
		defer close(d.waitCh)

		err := cmd.Wait()
		_ = psw.Flush()

		// Wait returns only after all output has been copied into the
		// pipe. Closing it lets scanIntoLog finish, so critical lines
		// reach the handler before the exit is reported to callers.
		_ = pw.Close()
		select {
		case <-scanDone:
		case <-time.After(criticalFlushTimeout):
			d.log.Warn(d.ctx, "timed out forwarding critical output", slog.F("process", d.binName))
		}

		// If the user killed the application the actual error returned
		// from wait doesn't really matter.
		if atomic.LoadInt64(userKilled) == 1 {
			d.waitCh <- ErrUserKilled
			return
		}

		if err == nil {
			d.waitCh <- nil
		} else {
			d.waitCh <- xerrors.Errorf("%s: %w", buf.Bytes(), err)
		}
	}()
	return nil
}

func (d *Process) kill(sig syscall.Signal) error {
	if d.cmd.OSProcess() == nil {
		return xerrors.Errorf("cmd has not been started")
	}

	atomic.StoreInt64(d.userKilled, 1)

	pid := d.cmd.OSProcess().Pid
	err := d.cmd.OSProcess().Signal(sig)
	if err != nil {
		return xerrors.Errorf("kill proc: %w", err)
	}

	ticker := time.NewTicker(time.Millisecond * 10)
	defer ticker.Stop()

	fs := xunix.GetFS(d.ctx)

	for {
		// Try to find the process in the procfs. If we can't find
		// it, it means the process has exited. It's also possible that
		// we find the same PID but the cmd is different indicating the PID
		// has been reused.
		exited, err := isProcExited(fs, pid, d.binName)
		if err != nil {
			return xerrors.Errorf("is proc cmd: %w", err)
		}

		if exited {
			return nil
		}

		select {
		case <-d.ctx.Done():
			return d.ctx.Err()
		case <-ticker.C:
		}
	}
}

// isProcExited checks if the provided PID has exited. It does this
// by attempting to read its entry in /proc/<pid>. If it can't find the
// entry then the process has exited. If the entry exists we check to see
// if the cmd is the same since it is possible (even if extremely unlikely)
// that the PID may be reclaimed and reused for a separate process.
func isProcExited(fs afero.Fs, pid int, cmd string) (bool, error) {
	cmdline, err := afero.ReadFile(fs, fmt.Sprintf("/proc/%d/cmdline", pid))
	if xerrors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, xerrors.Errorf("read file: %w", err)
	}

	args := bytes.Split(cmdline, []byte{'0'})
	if len(args) < 1 {
		// Honestly idk.
		return false, xerrors.Errorf("cmdline has no output (%s)?", cmdline)
	}

	// If the cmd doesn't match then the PID has been reused for a different
	// process indicating the proc we're looking for has successfully exited.
	return cmd != string(args[0]), nil
}

func scanIntoLog(ctx context.Context, log slog.Logger, scanner *bufio.Scanner, binaryName string, onCritical func(line string)) {
	fwd := newCriticalForwarder(onCritical)
	defer fwd.close()

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}

		var (
			line  = scanner.Text()
			logFn = log.Info
		)

		//nolint:gocritic
		if strings.Contains(line, "level=debug") {
			logFn = log.Debug
		} else if strings.Contains(line, "level=info") {
			logFn = log.Info
		} else if strings.Contains(line, "level=warning") {
			logFn = log.Warn
		} else if strings.Contains(line, "level=error") {
			logFn = log.Error
		} else if strings.Contains(line, "level=fatal") {
			logFn = log.Error
		}

		if IsCriticalOutput(line) {
			logFn = log.Error
			fwd.forward(line)
		}

		logFn(ctx, "child log",
			slog.F("process", binaryName),
			slog.F("content", line),
		)
	}
}

// criticalForwarder delivers critical lines to a handler on a separate
// goroutine. forward and close must be called from a single goroutine.
type criticalForwarder struct {
	lines chan string
	seen  map[string]struct{}
	done  chan struct{}
}

// newCriticalForwarder returns nil when fn is nil. The nil forwarder's
// methods are no-ops.
func newCriticalForwarder(fn func(line string)) *criticalForwarder {
	if fn == nil {
		return nil
	}

	f := &criticalForwarder{
		lines: make(chan string, maxCriticalLines),
		seen:  make(map[string]struct{}),
		done:  make(chan struct{}),
	}
	go func() {
		defer close(f.done)
		for line := range f.lines {
			fn(line)
		}
	}()
	return f
}

func (f *criticalForwarder) forward(line string) {
	if f == nil {
		return
	}
	if _, ok := f.seen[line]; ok || len(f.seen) >= maxCriticalLines {
		return
	}
	f.seen[line] = struct{}{}
	// Never blocks: the buffer holds maxCriticalLines and no more than that
	// are ever sent.
	f.lines <- line
}

// close waits for the handler to finish with every forwarded line.
func (f *criticalForwarder) close() {
	if f == nil {
		return
	}
	close(f.lines)
	<-f.done
}

var ErrUserKilled = xerrors.Errorf("daemon killed by user")

func i64ptr(i int64) *int64 {
	return &i
}
