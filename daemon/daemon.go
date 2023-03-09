package daemon

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

	"cdr.dev/slog"
	"github.com/coder/envbox/xio"
	"github.com/coder/envbox/xunix"
	"github.com/spf13/afero"
	"golang.org/x/xerrors"
)

type Daemon struct {
	ctx     context.Context
	cancel  context.CancelFunc
	log     slog.Logger
	cmd     xunix.Cmd
	binName string
	args    []string

	userKilled *int64
	waitCh     chan error
	mu         sync.Mutex
}

func New(ctx context.Context, log slog.Logger, cmd string, args ...string) *Daemon {
	ctx, cancel := context.WithCancel(ctx)
	return &Daemon{
		ctx:        ctx,
		cancel:     cancel,
		waitCh:     make(chan error, 1),
		cmd:        xunix.GetExecer(ctx).CommandContext(ctx, cmd, args...),
		log:        log.Named(cmd),
		userKilled: i64ptr(0),
		binName:    cmd,
	}
}

func (d *Daemon) Start() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.start()
}

func (d *Daemon) Wait() <-chan error {
	d.mu.Lock()
	waitCh := d.waitCh
	d.mu.Unlock()

	return waitCh
}

func (d *Daemon) Run() <-chan error {
	err := d.Start()
	if err != nil {
		ch := make(chan error, 1)
		ch <- err
		return ch
	}

	return d.Wait()

}

func (d *Daemon) Restart(ctx context.Context, cmd string, args ...string) error {
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

	return d.start()
}

func (d *Daemon) start() error {
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

func (d *Daemon) kill(sig syscall.Signal) error {
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

	matches, err := isProcCmd(fs, pid, d.binName)
	if err != nil {
		return xerrors.Errorf("is proc cmd: %w", err)
	}

	// If the cmd line doesn't match it means another process
	// has claimed the PID (meaning our process has exited).
	if !matches {
		return nil
	}

	for {
		select {
		case <-d.ctx.Done():
			return d.ctx.Err()
		case <-ticker.C:
			matches, err = isProcCmd(fs, pid, d.binName)
			if err != nil {
				return xerrors.Errorf("is proc cmd: %w", err)
			}

			if !matches {
				return nil
			}
		}
	}
}

func isProcCmd(fs afero.Fs, pid int, cmd string) (bool, error) {
	cmdline, err := afero.ReadFile(fs, fmt.Sprintf("/proc/%d/cmdline", pid))
	if xerrors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, xerrors.Errorf("read file: %w", err)
	}

	args := bytes.Split(cmdline, []byte{'0'})
	if len(args) < 1 {
		// Honestly idk.
		return false, xerrors.Errorf("cmdline has no output (%s)?", cmdline)
	}

	return cmd == string(args[0]), nil
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
