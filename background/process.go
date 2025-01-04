package background

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"strings"
	"sync"
	"sync/atomic"

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
	waitCh     chan struct{}
	mu         sync.Mutex
	err        error
}

func RunCh(ctx context.Context, log slog.Logger, cmd string, args ...string) <-chan error {
	proc := New(ctx, log, cmd, args...)
	errCh := make(chan error, 1)
	go func() {
		errCh <- proc.Run()
	}()
	return errCh
}

// New returns an instantiated daemon.
func New(ctx context.Context, log slog.Logger, cmd string, args ...string) *Process {
	ctx, cancel := context.WithCancel(ctx)
	return &Process{
		ctx:        ctx,
		cancel:     cancel,
		waitCh:     make(chan struct{}, 1),
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
func (d *Process) Wait() error {
	<-d.waitCh
	return d.err
}

// Run runs the command and waits for it to exit. It is a convenience
// function that combines both Start() and Wait().
func (d *Process) Run() error {
	err := d.Start()
	if err != nil {
		return err
	}

	return d.Wait()
}

func (d *Process) KillAndWait() error {
	if atomic.CompareAndSwapInt64(d.userKilled, 0, 1) {
		err := d.kill()
		if err != nil {
			return xerrors.Errorf("kill: %w", err)
		}
	}

	return d.Wait()
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
			d.err = ErrUserKilled
			return
		}

		if err == nil {
			d.err = nil
		} else {
			d.err = xerrors.Errorf("%s: %w", buf.Bytes(), err)
		}
	}()
	return nil
}

func (d *Process) kill() error {
	if d.cmd.OSProcess() == nil {
		return xerrors.Errorf("cmd has not been started")
	}

	atomic.StoreInt64(d.userKilled, 1)
	return d.cmd.OSProcess().Kill()
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
