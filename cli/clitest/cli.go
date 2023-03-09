package clitest

import (
	"context"
	"testing"
	"time"

	"github.com/coder/envbox/cli"
	"github.com/coder/envbox/dockerutil"
	"github.com/coder/envbox/dockerutil/fake"
	"github.com/coder/envbox/xunix"
	"github.com/coder/envbox/xunix/xunixfake"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"k8s.io/mount-utils"
)

func Execer(ctx context.Context) *xunixfake.FakeExec {
	return xunix.GetExecer(ctx).(*xunixfake.FakeExec)
}

func FS(ctx context.Context) *xunixfake.MemFS {
	return xunix.GetFS(ctx).(*xunixfake.MemFS)
}

func Mounter(ctx context.Context) *mount.FakeMounter {
	return xunix.Mounter(ctx).(*mount.FakeMounter)
}

func DockerClient(t *testing.T, ctx context.Context) *fake.MockClient {
	t.Helper()

	client, err := dockerutil.Client(ctx)
	require.NoError(t, err)
	return client.(*fake.MockClient)
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

	root := cli.Root()
	// This is the one thing that isn't really mocked for the tests.
	// I cringe at the thought of introducing yet another mock so
	// let's avoid it for now.
	// If a consumer sets the ethlink arg it should overwrite our
	// default we set here.
	args = append([]string{cmd, "--ethlink=" + iface.Attrs().Name}, args...)
	root.SetArgs(args)

	MockSysboxManagerReady(t, fs)
	MockCPUCGroups(t, fs, "1234", "5678")

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
