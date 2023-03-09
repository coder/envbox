package clitest

import (
	"bufio"
	"context"
	"net"
	"os"
	"strings"

	"github.com/coder/envbox/dockerutil"
	"github.com/coder/envbox/dockerutil/fake"
	"github.com/coder/envbox/xunix/xunixfake"
	dockertypes "github.com/docker/docker/api/types"
	"github.com/spf13/afero"
	testingexec "k8s.io/utils/exec/testing"
)

func NewMemFS() *xunixfake.MemFS {
	return &xunixfake.MemFS{
		MemMapFs: &afero.MemMapFs{},
		Owner:    map[string]xunixfake.FileOwner{},
	}
}

func NewFakeExecer() *xunixfake.FakeExec {
	return &xunixfake.FakeExec{
		Commands: map[string]*xunixfake.FakeCmd{},
		DefaultFakeCmd: &xunixfake.FakeCmd{
			FakeCmd:     &testingexec.FakeCmd{},
			FakeProcess: &os.Process{Pid: 1111},
			// The main use of exec commands in this repo
			// are to spawn daemon processes so ideally the
			// default behavior is that they do not exist.
			WaitFn: func() error { select {} },
		},
	}
}

func NewFakeDockerClient() dockerutil.DockerClient {
	client := &fake.MockClient{}

	client.ContainerInspectFn = func(_ context.Context, container string) (dockertypes.ContainerJSON, error) {
		return dockertypes.ContainerJSON{
			ContainerJSONBase: &dockertypes.ContainerJSONBase{
				GraphDriver: dockertypes.GraphDriverData{
					Data: map[string]string{"MergedDir": "blah"},
				},
			},
		}, nil
	}

	client.ContainerExecAttachFn = func(_ context.Context, execID string, config dockertypes.ExecStartCheck) (dockertypes.HijackedResponse, error) {
		return dockertypes.HijackedResponse{
			Reader: bufio.NewReader(strings.NewReader("root:x:0:0:root:/root:/bin/bash")),
			Conn:   &net.IPConn{},
		}, nil
	}

	return client
}
