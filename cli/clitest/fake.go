package clitest

import (
	"bufio"
	"context"
	"net"
	"os"
	"strings"

	dockertypes "github.com/docker/docker/api/types"
	testingexec "k8s.io/utils/exec/testing"

	"github.com/coder/envbox/dockerutil"
	"github.com/coder/envbox/dockerutil/dockerfake"
	"github.com/coder/envbox/xunix/xunixfake"
)

func NewFakeExecer() *xunixfake.FakeExec {
	return &xunixfake.FakeExec{
		Commands: map[string]*xunixfake.FakeCmd{},
		DefaultFakeCmd: &xunixfake.FakeCmd{
			FakeCmd:     &testingexec.FakeCmd{},
			FakeProcess: &os.Process{Pid: 1111},
			// The main use of exec commands in this repo
			// are to spawn daemon processes so ideally the
			// default behavior is that they do not exist.
			// nolint
			WaitFn: func() error { select {} },
		},
	}
}

func NewFakeDockerClient() dockerutil.DockerClient {
	client := &dockerfake.MockClient{}

	client.ContainerInspectFn = func(_ context.Context, _ string) (dockertypes.ContainerJSON, error) {
		return dockertypes.ContainerJSON{
			ContainerJSONBase: &dockertypes.ContainerJSONBase{
				GraphDriver: dockertypes.GraphDriverData{
					Data: map[string]string{"MergedDir": "blah"},
				},
			},
		}, nil
	}

	client.ContainerExecAttachFn = func(_ context.Context, _ string, _ dockertypes.ExecStartCheck) (dockertypes.HijackedResponse, error) {
		return dockertypes.HijackedResponse{
			Reader: bufio.NewReader(strings.NewReader("root:x:0:0:root:/root:/bin/bash")),
			Conn:   &net.IPConn{},
		}, nil
	}

	return client
}
