package xunixfake

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/coder/envbox/xunix"
	"github.com/stretchr/testify/require"
	testexec "k8s.io/utils/exec/testing"
)

var _ xunix.Execer = &FakeExec{}

type FakeExec struct {
	Commands       map[string]*FakeCmd
	DefaultFakeCmd *FakeCmd
}

func cmdKey(cmd string, args ...string) string {
	return cmd + " " + strings.Join(args, " ")
}

func (f *FakeExec) CommandContext(_ context.Context, cmd string, args ...string) xunix.Cmd {
	// TODO: This isn't a great key because you may have multiple of the same commands
	// but desire different output.
	if c, ok := f.Commands[cmdKey(cmd, args...)]; ok {
		c.Called = true
		return c
	}
	return f.DefaultFakeCmd
}

func (f *FakeExec) AddCommands(cmds ...*FakeCmd) {
	for _, cmd := range cmds {
		key := cmdKey(cmd.Argv[0], cmd.Argv[1:]...)
		f.Commands[key] = cmd
	}
}

func (f *FakeExec) AssertCommandsCalled(t *testing.T) {
	t.Helper()
	for k, cmd := range f.Commands {
		require.True(t, cmd.Called, "%q not called", k)
	}
}

var _ xunix.Cmd = &FakeCmd{}

type FakeCmd struct {
	*testexec.FakeCmd
	FakeProcess *os.Process
	WaitFn      func() error
	Called      bool
}

func (f *FakeCmd) Wait() error {
	if f.WaitFn == nil {
		return nil
	}
	return f.WaitFn()
}

func (f *FakeCmd) OSProcess() *os.Process {
	return f.FakeProcess
}
