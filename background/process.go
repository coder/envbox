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

	"cdr.dev/slog"
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
}

// New returns an instantiated daemon.
func New(ctx context.Context, log slog.Logger, cmd string, args ...string) *Process {
	ctx, cancel := context.WithCancel(ctx)
	return &Process{
		ctx:        ctx,
		cancel:     cancel,
		waitCh:     make(chan error, 1),
		cmd:        xunix.GetExecer(ctx).CommandContext(ctx, cmd, args...),
		log:        log.Named(cmd),
		userKilled: i64ptr(0),
		binName:    cmd,
	}
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
// cmd and args.
func (d *Process) Restart(ctx context.Context, cmd string, args ...string) error {
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
	d.binName = cmd

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

	go scanIntoLog(d.ctx, d.log, bufio.NewScanner(pr), d.binName)

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

func scanIntoLog(ctx context.Context, log slog.Logger, scanner *bufio.Scanner, binaryName string) {
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

		logFn(ctx, "child log",
			slog.F("process", binaryName),
			slog.F("content", line),
		)
	}
}

var ErrUserKilled = xerrors.Errorf("daemon killed by user")

func i64ptr(i int64) *int64 {
	return &i
}
