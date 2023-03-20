package dockerutil

import (
	"bytes"
	"context"
	"io"
	"os"

	dockertypes "github.com/docker/docker/api/types"
	"golang.org/x/xerrors"
)

type execConfig struct {
	ContainerID string
	User        string
	Cmd         string
	Args        []string
	Stdin       io.Reader
	Env         []string
}

// execContainer runs a command in a container. It returns the output and any error.
// If an error occurs during the execution of the command, the output is appended to the error.
func execContainer(ctx context.Context, client DockerClient, config execConfig) ([]byte, error) {
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

	var buf bytes.Buffer
	mwr := io.MultiWriter(os.Stderr, os.Stdout, &buf)
	_, err = io.Copy(mwr, resp.Reader)
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
