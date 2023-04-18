package dockerutil

import (
	"bytes"
	"context"
	"io"

	dockertypes "github.com/docker/docker/api/types"
	"golang.org/x/xerrors"

	"github.com/coder/envbox/xio"
)

type ExecConfig struct {
	ContainerID string
	User        string
	Cmd         string
	Args        []string
	Stdin       io.Reader
	StdOutErr   io.Writer
	Env         []string
	Detach      bool
}

// ExecContainer runs a command in a container. It returns the output and any error.
// If an error occurs during the execution of the command, the output is appended to the error.
func ExecContainer(ctx context.Context, client DockerClient, config ExecConfig) ([]byte, error) {
	exec, err := client.ContainerExecCreate(ctx, config.ContainerID, dockertypes.ExecConfig{
		Detach:       true,
		Cmd:          append([]string{config.Cmd}, config.Args...),
		User:         config.User,
		AttachStderr: true,
		AttachStdout: true,
		AttachStdin:  config.Stdin != nil,
		Env:          config.Env,
	})
	if err != nil {
		return nil, xerrors.Errorf("exec create: %w", err)
	}

	resp, err := client.ContainerExecAttach(ctx, exec.ID, dockertypes.ExecStartCheck{})
	if err != nil {
		return nil, xerrors.Errorf("attach to exec: %w", err)
	}
	defer resp.Close()

	if config.Stdin != nil {
		_, err = io.Copy(resp.Conn, config.Stdin)
		if err != nil {
			return nil, xerrors.Errorf("copy stdin: %w", err)
		}
		err = resp.CloseWrite()
		if err != nil {
			return nil, xerrors.Errorf("close write: %w", err)
		}
	}

	var (
		buf bytes.Buffer
		// Avoid capturing too much output. We want to prevent
		// a memory leak. This is especially important when
		// we run the bootstrap script since we do not return.
		psw = &xio.PrefixSuffixWriter{
			W: &buf,
			N: 1 << 10,
		}
		wr io.Writer = psw
	)

	if config.StdOutErr != nil {
		wr = io.MultiWriter(psw, config.StdOutErr)
	}

	_, err = io.Copy(wr, resp.Reader)
	if err != nil {
		return nil, xerrors.Errorf("copy cmd output: %w", err)
	}
	resp.Close()

	inspect, err := client.ContainerExecInspect(ctx, exec.ID)
	if err != nil {
		return nil, xerrors.Errorf("exec inspect: %w", err)
	}

	if inspect.Running {
		return nil, xerrors.Errorf("unexpectedly still running")
	}

	if inspect.ExitCode > 0 {
		return nil, xerrors.Errorf("%s: exit code %d", buf.Bytes(), inspect.ExitCode)
	}

	return buf.Bytes(), nil
}
