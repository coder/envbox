package fake

import (
	"context"
	"io"
	"strings"

	"github.com/coder/envbox/dockerutil"
	dockertypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	containertypes "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	networktypes "github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/registry"
	specs "github.com/opencontainers/image-spec/specs-go/v1"
)

var _ dockerutil.DockerClient = MockClient{}

// MockClient provides overrides for functions that are called in envbox.
type MockClient struct {
	ImagePullFn            func(_ context.Context, ref string, options dockertypes.ImagePullOptions) (io.ReadCloser, error)
	ContainerCreateFn      func(_ context.Context, config *containertypes.Config, hostConfig *containertypes.HostConfig, networkingConfig *networktypes.NetworkingConfig, _ *specs.Platform, containerName string) (containertypes.ContainerCreateCreatedBody, error)
	ImagePruneFn           func(_ context.Context, pruneFilter filters.Args) (dockertypes.ImagesPruneReport, error)
	ContainerStartFn       func(_ context.Context, container string, options dockertypes.ContainerStartOptions) error
	ContainerExecAttachFn  func(_ context.Context, execID string, config dockertypes.ExecStartCheck) (dockertypes.HijackedResponse, error)
	ContainerExecCreateFn  func(_ context.Context, container string, config dockertypes.ExecConfig) (dockertypes.IDResponse, error)
	ContainerExecStartFn   func(_ context.Context, execID string, config dockertypes.ExecStartCheck) error
	ContainerExecInspectFn func(_ context.Context, execID string) (dockertypes.ContainerExecInspect, error)
	ContainerInspectFn     func(_ context.Context, container string) (dockertypes.ContainerJSON, error)
	ContainerRemoveFn      func(_ context.Context, container string, options dockertypes.ContainerRemoveOptions) error
	PingFn                 func(_ context.Context) (dockertypes.Ping, error)
}

func (m MockClient) ImageBuild(_ context.Context, _ io.Reader, _ dockertypes.ImageBuildOptions) (dockertypes.ImageBuildResponse, error) {
	panic("not implemented")
}

func (m MockClient) BuildCachePrune(_ context.Context, _ dockertypes.BuildCachePruneOptions) (*dockertypes.BuildCachePruneReport, error) {
	panic("not implemented")
}

func (m MockClient) BuildCancel(_ context.Context, _ string) error {
	panic("not implemented")
}

func (m MockClient) ImageCreate(_ context.Context, _ string, _ dockertypes.ImageCreateOptions) (io.ReadCloser, error) {
	panic("not implemented")
}

func (m MockClient) ImageHistory(_ context.Context, _ string) ([]image.HistoryResponseItem, error) {
	panic("not implemented")
}

func (m MockClient) ImageImport(_ context.Context, _ dockertypes.ImageImportSource, _ string, _ dockertypes.ImageImportOptions) (io.ReadCloser, error) {
	panic("not implemented")
}

func (m MockClient) ImageInspectWithRaw(_ context.Context, _ string) (dockertypes.ImageInspect, []byte, error) {
	panic("not implemented")
}

func (m MockClient) ImageList(_ context.Context, _ dockertypes.ImageListOptions) ([]dockertypes.ImageSummary, error) {
	panic("not implemented")
}

func (m MockClient) ImageLoad(_ context.Context, _ io.Reader, _ bool) (dockertypes.ImageLoadResponse, error) {
	panic("not implemented")
}

func (m MockClient) ImagePull(ctx context.Context, ref string, options dockertypes.ImagePullOptions) (io.ReadCloser, error) {
	if m.ImagePullFn == nil {
		return io.NopCloser(strings.NewReader("")), nil
	}
	return m.ImagePullFn(ctx, ref, options)
}

func (m MockClient) ImagePush(_ context.Context, _ string, _ dockertypes.ImagePushOptions) (io.ReadCloser, error) {
	panic("not implemented")
}

func (m MockClient) ImageRemove(_ context.Context, _ string, _ dockertypes.ImageRemoveOptions) ([]dockertypes.ImageDeleteResponseItem, error) {
	panic("not implemented")
}

func (m MockClient) ImageSearch(_ context.Context, _ string, _ dockertypes.ImageSearchOptions) ([]registry.SearchResult, error) {
	panic("not implemented")
}

func (m MockClient) ImageSave(_ context.Context, _ []string) (io.ReadCloser, error) {
	panic("not implemented")
}

func (m MockClient) ImageTag(_ context.Context, _ string, _ string) error {
	panic("not implemented")
}

func (m MockClient) ImagesPrune(ctx context.Context, pruneFilter filters.Args) (dockertypes.ImagesPruneReport, error) {
	if m.ImagePruneFn == nil {
		return dockertypes.ImagesPruneReport{}, nil
	}
	return m.ImagePruneFn(ctx, pruneFilter)
}

func (m MockClient) Events(_ context.Context, _ dockertypes.EventsOptions) (<-chan events.Message, <-chan error) {
	panic("not implemented")
}

func (m MockClient) Info(_ context.Context) (dockertypes.Info, error) {
	panic("not implemented")
}

func (m MockClient) RegistryLogin(_ context.Context, _ dockertypes.AuthConfig) (registry.AuthenticateOKBody, error) {
	panic("not implemented")
}

func (m MockClient) DiskUsage(_ context.Context, _ dockertypes.DiskUsageOptions) (dockertypes.DiskUsage, error) {
	panic("not implemented")
}

func (m MockClient) Ping(ctx context.Context) (dockertypes.Ping, error) {
	if m.PingFn == nil {
		return dockertypes.Ping{}, nil
	}
	return m.PingFn(ctx)
}

func (m MockClient) ContainerAttach(_ context.Context, _ string, _ dockertypes.ContainerAttachOptions) (dockertypes.HijackedResponse, error) {
	panic("not implemented")
}

func (m MockClient) ContainerCommit(_ context.Context, _ string, _ dockertypes.ContainerCommitOptions) (dockertypes.IDResponse, error) {
	panic("not implemented")
}

func (m MockClient) ContainerCreate(ctx context.Context, config *containertypes.Config, hostConfig *containertypes.HostConfig, networkingConfig *networktypes.NetworkingConfig, specs *specs.Platform, containerName string) (containertypes.ContainerCreateCreatedBody, error) {
	if m.ContainerCreateFn == nil {
		return containertypes.ContainerCreateCreatedBody{}, nil
	}
	return m.ContainerCreateFn(ctx, config, hostConfig, networkingConfig, specs, containerName)
}

func (m MockClient) ContainerDiff(_ context.Context, _ string) ([]containertypes.ContainerChangeResponseItem, error) {
	panic("not implemented")
}

func (m MockClient) ContainerExecAttach(ctx context.Context, execID string, config dockertypes.ExecStartCheck) (dockertypes.HijackedResponse, error) {
	if m.ContainerExecAttachFn == nil {
		return dockertypes.HijackedResponse{}, nil
	}
	return m.ContainerExecAttachFn(ctx, execID, config)
}

func (m MockClient) ContainerExecCreate(ctx context.Context, container string, config dockertypes.ExecConfig) (dockertypes.IDResponse, error) {
	if m.ContainerExecCreateFn == nil {
		return dockertypes.IDResponse{}, nil
	}
	return m.ContainerExecCreateFn(ctx, container, config)
}

func (m MockClient) ContainerExecInspect(ctx context.Context, id string) (dockertypes.ContainerExecInspect, error) {
	if m.ContainerExecInspectFn == nil {
		return dockertypes.ContainerExecInspect{}, nil
	}

	return m.ContainerExecInspectFn(ctx, id)
}

func (m MockClient) ContainerExecResize(_ context.Context, _ string, _ dockertypes.ResizeOptions) error {
	panic("not implemented")
}

func (m MockClient) ContainerExecStart(ctx context.Context, execID string, config dockertypes.ExecStartCheck) error {
	if m.ContainerExecStartFn == nil {
		return nil
	}
	return m.ContainerExecStartFn(ctx, execID, config)
}

func (m MockClient) ContainerExport(_ context.Context, _ string) (io.ReadCloser, error) {
	panic("not implemented")
}

func (m MockClient) ContainerInspect(ctx context.Context, container string) (dockertypes.ContainerJSON, error) {
	if m.ContainerInspectFn == nil {
		return dockertypes.ContainerJSON{}, nil
	}
	return m.ContainerInspectFn(ctx, container)
}

func (m MockClient) ContainerInspectWithRaw(_ context.Context, _ string, _ bool) (dockertypes.ContainerJSON, []byte, error) {
	panic("not implemented")
}

func (m MockClient) ContainerKill(_ context.Context, _ string, _ string) error {
	panic("not implemented")
}

func (m MockClient) ContainerList(_ context.Context, _ dockertypes.ContainerListOptions) ([]dockertypes.Container, error) {
	panic("not implemented")
}

func (m MockClient) ContainerLogs(_ context.Context, _ string, _ dockertypes.ContainerLogsOptions) (io.ReadCloser, error) {
	panic("not implemented")
}

func (m MockClient) ContainerPause(_ context.Context, _ string) error {
	panic("not implemented")
}

func (m MockClient) ContainerRemove(ctx context.Context, container string, options dockertypes.ContainerRemoveOptions) error {
	if m.ContainerRemoveFn == nil {
		return nil
	}
	return m.ContainerRemoveFn(ctx, container, options)
}

func (m MockClient) ContainerRename(_ context.Context, _ string, _ string) error {
	panic("not implemented")
}

func (m MockClient) ContainerResize(_ context.Context, _ string, _ dockertypes.ResizeOptions) error {
	panic("not implemented")
}

func (m MockClient) ContainerRestart(_ context.Context, _ string, _ containertypes.StopOptions) error {
	panic("not implemented")
}

func (m MockClient) ContainerStatPath(_ context.Context, _ string, _ string) (dockertypes.ContainerPathStat, error) {
	panic("not implemented")
}

func (m MockClient) ContainerStats(_ context.Context, _ string, _ bool) (dockertypes.ContainerStats, error) {
	panic("not implemented")
}

func (m MockClient) ContainerStart(ctx context.Context, container string, options dockertypes.ContainerStartOptions) error {
	if m.ContainerStartFn == nil {
		return nil
	}
	return m.ContainerStartFn(ctx, container, options)
}

func (m MockClient) ContainerStop(_ context.Context, _ string, _ container.StopOptions) error {
	panic("not implemented")
}

func (m MockClient) ContainerTop(_ context.Context, _ string, _ []string) (containertypes.ContainerTopOKBody, error) {
	panic("not implemented")
}

func (m MockClient) ContainerUnpause(_ context.Context, _ string) error {
	panic("not implemented")
}

func (m MockClient) ContainerUpdate(_ context.Context, _ string, _ containertypes.UpdateConfig) (containertypes.ContainerUpdateOKBody, error) {
	panic("not implemented")
}

func (m MockClient) ContainerWait(_ context.Context, _ string, _ containertypes.WaitCondition) (<-chan containertypes.ContainerWaitOKBody, <-chan error) {
	panic("not implemented")
}

func (m MockClient) CopyFromContainer(_ context.Context, _ string, _ string) (io.ReadCloser, dockertypes.ContainerPathStat, error) {
	panic("not implemented")
}

func (m MockClient) CopyToContainer(_ context.Context, _ string, _ string, _ io.Reader, _ dockertypes.CopyToContainerOptions) error {
	panic("not implemented")
}

func (m MockClient) ContainersPrune(_ context.Context, _ filters.Args) (dockertypes.ContainersPruneReport, error) {
	panic("not implemented")
}

func (m MockClient) ContainerStatsOneShot(_ context.Context, _ string) (dockertypes.ContainerStats, error) {
	panic("not implemented")
}
