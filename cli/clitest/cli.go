package clitest

import (
	"context"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"k8s.io/mount-utils"

	"github.com/coder/envbox/cli"
	"github.com/coder/envbox/dockerutil"
	"github.com/coder/envbox/dockerutil/dockerfake"
	"github.com/coder/envbox/xunix"
	"github.com/coder/envbox/xunix/xunixfake"
)

func Execer(ctx context.Context) *xunixfake.FakeExec {
	//nolint we should panic if this isn't the case.
	return xunix.GetExecer(ctx).(*xunixfake.FakeExec)
}

func FS(ctx context.Context) *xunixfake.MemFS {
	//nolint we should panic if this isn't the case.
	return xunix.GetFS(ctx).(*xunixfake.MemFS)
}

func Mounter(ctx context.Context) *mount.FakeMounter {
	//nolint we should panic if this isn't the case.
	return xunix.Mounter(ctx).(*mount.FakeMounter)
}

// nolint
func DockerClient(t *testing.T, ctx context.Context) *dockerfake.MockClient {
	t.Helper()

	client, err := dockerutil.Client(ctx)
	require.NoError(t, err)
	//nolint we should panic if this isn't the case.
	return client.(*dockerfake.MockClient)
}

// New returns an instantiated Command as well as a context populated with mocked
// values for the command. All mock/fakes have been minimally configured to
// induce a successful call to the command.
func New(t *testing.T, cmd string, args ...string) (context.Context, *cobra.Command) {
	t.Helper()

	var (
		execer = NewFakeExecer()
		fs     = NewMemFS()
		mnt    = &mount.FakeMounter{}
		client = NewFakeDockerClient()
		iface  = GetNetLink(t)
		ctx    = ctx(t, fs, execer, mnt, client)
	)

	root := cli.Root(nil)
	// This is the one thing that isn't really mocked for the tests.
	// I cringe at the thought of introducing yet another mock so
	// let's avoid it for now.
	// If a consumer sets the ethlink arg it should overwrite our
	// default we set here.
	args = append([]string{cmd, "--ethlink=" + iface.Attrs().Name, "--no-startup-log"}, args...)
	root.SetArgs(args)

	FakeSysboxManagerReady(t, fs)
	FakeCPUGroups(t, fs, "1234", "5678")

	return ctx, root
}

func ctx(t *testing.T, fs xunix.FS, ex xunix.Execer, mnt mount.Interface, client dockerutil.DockerClient) context.Context {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	t.Cleanup(cancel)

	ctx = xunix.WithFS(ctx, fs)
	ctx = xunix.WithExecer(ctx, ex)
	ctx = xunix.WithMounter(ctx, mnt)
	ctx = dockerutil.WithClient(ctx, client)

	return ctx
}
