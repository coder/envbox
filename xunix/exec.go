package xunix

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	osexec "os/exec"
	"syscall"
	"time"

	utilexec "k8s.io/utils/exec"
)

type execerKey struct{}

func WithExecer(ctx context.Context, execer Execer) context.Context {
	return context.WithValue(ctx, execerKey{}, execer)
}

func GetExecer(ctx context.Context) Execer {
	execer := ctx.Value(execerKey{})
	if execer == nil {
		// This is a typical os/exec implementation.
		return &executor{}
	}

	//nolint we should panic if this isn't the case.
	return execer.(Execer)
}

// The code henceforth is copied straight and modified slightly from
// "k8s.io/utils/exec".  Their interface doesn't allow for a reference to
// exec.Cmd.Process which we use to kill and wait for a process to exit.

type Execer interface {
	// CommandContext returns a Cmd instance which can be used to run a single command.
	//
	// The provided context is used to kill the process if the context becomes done
	// before the command completes on its own. For example, a timeout can be set in
	// the context.
	CommandContext(ctx context.Context, cmd string, args ...string) Cmd
}

type Cmd interface {
	utilexec.Cmd
	OSProcess() *os.Process
}

type executor struct{}

// CommandContext is part of the Interface interface.
func (*executor) CommandContext(ctx context.Context, cmd string, args ...string) Cmd {
	return (*cmdWrapper)(maskErrDotCmd(osexec.CommandContext(ctx, cmd, args...)))
}

// Wraps exec.Cmd so we can capture errors.
type cmdWrapper osexec.Cmd

var _ Cmd = &cmdWrapper{}

func (cmd *cmdWrapper) SetDir(dir string) {
	cmd.Dir = dir
}

func (cmd *cmdWrapper) SetStdin(in io.Reader) {
	cmd.Stdin = in
}

func (cmd *cmdWrapper) SetStdout(out io.Writer) {
	cmd.Stdout = out
}

func (cmd *cmdWrapper) SetStderr(out io.Writer) {
	cmd.Stderr = out
}

func (cmd *cmdWrapper) SetEnv(env []string) {
	cmd.Env = env
}

func (cmd *cmdWrapper) StdoutPipe() (io.ReadCloser, error) {
	r, err := (*osexec.Cmd)(cmd).StdoutPipe()
	return r, handleError(err)
}

func (cmd *cmdWrapper) StderrPipe() (io.ReadCloser, error) {
	r, err := (*osexec.Cmd)(cmd).StderrPipe()
	return r, handleError(err)
}

func (cmd *cmdWrapper) Start() error {
	err := (*osexec.Cmd)(cmd).Start()
	return handleError(err)
}

func (cmd *cmdWrapper) Wait() error {
	err := (*osexec.Cmd)(cmd).Wait()
	return handleError(err)
}

// Run is part of the Cmd interface.
func (cmd *cmdWrapper) Run() error {
	err := (*osexec.Cmd)(cmd).Run()
	return handleError(err)
}

// CombinedOutput is part of the Cmd interface.
func (cmd *cmdWrapper) CombinedOutput() ([]byte, error) {
	out, err := (*osexec.Cmd)(cmd).CombinedOutput()
	return out, handleError(err)
}

func (cmd *cmdWrapper) Output() ([]byte, error) {
	out, err := (*osexec.Cmd)(cmd).Output()
	return out, handleError(err)
}

func (cmd *cmdWrapper) OSProcess() *os.Process {
	return (*osexec.Cmd)(cmd).Process
}

// Stop is part of the Cmd interface.
func (cmd *cmdWrapper) Stop() {
	c := (*osexec.Cmd)(cmd)

	if c.Process == nil {
		return
	}

	_ = c.Process.Signal(syscall.SIGTERM)

	time.AfterFunc(10*time.Second, func() {
		if !c.ProcessState.Exited() {
			_ = c.Process.Signal(syscall.SIGKILL)
		}
	})
}

func handleError(err error) error {
	if err == nil {
		return nil
	}

	//nolint copied code from k8s.
	switch e := err.(type) {
	case *osexec.ExitError:
		return &utilexec.ExitErrorWrapper{ExitError: e}
	case *fs.PathError:
		return utilexec.ErrExecutableNotFound
		// nolint copied code from k8s
	case *osexec.Error:
		if e.Err == osexec.ErrNotFound {
			return utilexec.ErrExecutableNotFound
		}
	}

	return err
}

// maskErrDotCmd reverts the behavior of osexec.Cmd to what it was before go1.19
// specifically set the Err field to nil (LookPath returns a new error when the file
// is resolved to the current directory.
func maskErrDotCmd(cmd *osexec.Cmd) *osexec.Cmd {
	cmd.Err = maskErrDot(cmd.Err)
	return cmd
}

func maskErrDot(err error) error {
	if err != nil && errors.Is(err, osexec.ErrDot) {
		return nil
	}
	return err
}
