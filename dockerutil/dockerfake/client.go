package dockerfake

import (
	"context"
	"io"
	"strings"

	dockertypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/common"
	containertypes "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	networktypes "github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/registry"
	"github.com/docker/docker/api/types/system"
	dockerclient "github.com/docker/docker/client"
	specs "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/coder/envbox/dockerutil"
)

var _ dockerutil.Client = MockClient{}

// MockClient provides overrides for functions that are called in envbox.
type MockClient struct {
	ImagePullFn            func(_ context.Context, ref string, options image.PullOptions) (io.ReadCloser, error)
	ContainerCreateFn      func(_ context.Context, config *containertypes.Config, hostConfig *containertypes.HostConfig, networkingConfig *networktypes.NetworkingConfig, _ *specs.Platform, containerName string) (containertypes.CreateResponse, error)
	ImagePruneFn           func(_ context.Context, pruneFilter filters.Args) (image.PruneReport, error)
	ContainerStartFn       func(_ context.Context, container string, options containertypes.StartOptions) error
	ContainerExecAttachFn  func(_ context.Context, execID string, config containertypes.ExecAttachOptions) (dockertypes.HijackedResponse, error)
	ContainerExecCreateFn  func(_ context.Context, container string, config containertypes.ExecOptions) (common.IDResponse, error)
	ContainerExecStartFn   func(_ context.Context, execID string, config containertypes.ExecAttachOptions) error
	ContainerExecInspectFn func(_ context.Context, execID string) (containertypes.ExecInspect, error)
	ContainerInspectFn     func(_ context.Context, container string) (dockertypes.ContainerJSON, error)
	ContainerRemoveFn      func(_ context.Context, container string, options containertypes.RemoveOptions) error
	PingFn                 func(_ context.Context) (dockertypes.Ping, error)
}

func (MockClient) ImageBuild(_ context.Context, _ io.Reader, _ dockertypes.ImageBuildOptions) (dockertypes.ImageBuildResponse, error) {
	panic("not implemented")
}

func (MockClient) BuildCachePrune(_ context.Context, _ dockertypes.BuildCachePruneOptions) (*dockertypes.BuildCachePruneReport, error) {
	panic("not implemented")
}

func (MockClient) BuildCancel(_ context.Context, _ string) error {
	panic("not implemented")
}

func (MockClient) ImageCreate(_ context.Context, _ string, _ image.CreateOptions) (io.ReadCloser, error) {
	panic("not implemented")
}

func (MockClient) ImageHistory(_ context.Context, _ string, _ ...dockerclient.ImageHistoryOption) ([]image.HistoryResponseItem, error) {
	panic("not implemented")
}

func (MockClient) ImageImport(_ context.Context, _ image.ImportSource, _ string, _ image.ImportOptions) (io.ReadCloser, error) {
	panic("not implemented")
}

func (MockClient) ImageInspect(_ context.Context, _ string, _ ...dockerclient.ImageInspectOption) (image.InspectResponse, error) {
	panic("not implemented")
}

func (MockClient) ImageInspectWithRaw(_ context.Context, _ string) (image.InspectResponse, []byte, error) {
	panic("not implemented")
}

func (MockClient) ImageList(_ context.Context, _ image.ListOptions) ([]image.Summary, error) {
	panic("not implemented")
}

func (MockClient) ImageLoad(_ context.Context, _ io.Reader, _ ...dockerclient.ImageLoadOption) (image.LoadResponse, error) {
	panic("not implemented")
}

func (m MockClient) ImagePull(ctx context.Context, ref string, options image.PullOptions) (io.ReadCloser, error) {
	if m.ImagePullFn == nil {
		return io.NopCloser(strings.NewReader("")), nil
	}
	return m.ImagePullFn(ctx, ref, options)
}

func (MockClient) ImagePush(_ context.Context, _ string, _ image.PushOptions) (io.ReadCloser, error) {
	panic("not implemented")
}

func (MockClient) ImageRemove(_ context.Context, _ string, _ image.RemoveOptions) ([]image.DeleteResponse, error) {
	panic("not implemented")
}

func (MockClient) ImageSearch(_ context.Context, _ string, _ registry.SearchOptions) ([]registry.SearchResult, error) {
	panic("not implemented")
}

func (MockClient) ImageSave(_ context.Context, _ []string, _ ...dockerclient.ImageSaveOption) (io.ReadCloser, error) {
	panic("not implemented")
}

func (MockClient) ImageTag(_ context.Context, _ string, _ string) error {
	panic("not implemented")
}

func (m MockClient) ImagesPrune(ctx context.Context, pruneFilter filters.Args) (image.PruneReport, error) {
	if m.ImagePruneFn == nil {
		return image.PruneReport{}, nil
	}
	return m.ImagePruneFn(ctx, pruneFilter)
}

func (MockClient) Events(_ context.Context, _ events.ListOptions) (<-chan events.Message, <-chan error) {
	panic("not implemented")
}

func (MockClient) Info(_ context.Context) (system.Info, error) {
	panic("not implemented")
}

func (MockClient) RegistryLogin(_ context.Context, _ registry.AuthConfig) (registry.AuthenticateOKBody, error) {
	panic("not implemented")
}

func (MockClient) DiskUsage(_ context.Context, _ dockertypes.DiskUsageOptions) (dockertypes.DiskUsage, error) {
	panic("not implemented")
}

func (m MockClient) Ping(ctx context.Context) (dockertypes.Ping, error) {
	if m.PingFn == nil {
		return dockertypes.Ping{}, nil
	}
	return m.PingFn(ctx)
}

func (MockClient) ContainerAttach(_ context.Context, _ string, _ containertypes.AttachOptions) (dockertypes.HijackedResponse, error) {
	panic("not implemented")
}

func (MockClient) ContainerCommit(_ context.Context, _ string, _ containertypes.CommitOptions) (dockertypes.IDResponse, error) {
	panic("not implemented")
}

func (m MockClient) ContainerCreate(ctx context.Context, config *containertypes.Config, hostConfig *containertypes.HostConfig, networkingConfig *networktypes.NetworkingConfig, pspecs *specs.Platform, containerName string) (containertypes.CreateResponse, error) {
	if m.ContainerCreateFn == nil {
		return containertypes.CreateResponse{}, nil
	}
	return m.ContainerCreateFn(ctx, config, hostConfig, networkingConfig, pspecs, containerName)
}

func (MockClient) ContainerDiff(_ context.Context, _ string) ([]containertypes.FilesystemChange, error) {
	panic("not implemented")
}

func (m MockClient) ContainerExecAttach(ctx context.Context, execID string, config containertypes.ExecAttachOptions) (dockertypes.HijackedResponse, error) {
	if m.ContainerExecAttachFn == nil {
		return dockertypes.HijackedResponse{}, nil
	}
	return m.ContainerExecAttachFn(ctx, execID, config)
}

func (m MockClient) ContainerExecCreate(ctx context.Context, name string, config containertypes.ExecOptions) (common.IDResponse, error) {
	if m.ContainerExecCreateFn == nil {
		return common.IDResponse{}, nil
	}
	return m.ContainerExecCreateFn(ctx, name, config)
}

func (m MockClient) ContainerExecInspect(ctx context.Context, id string) (containertypes.ExecInspect, error) {
	if m.ContainerExecInspectFn == nil {
		return containertypes.ExecInspect{
			Pid: 123,
		}, nil
	}

	return m.ContainerExecInspectFn(ctx, id)
}

func (MockClient) ContainerExecResize(_ context.Context, _ string, _ containertypes.ResizeOptions) error {
	panic("not implemented")
}

func (m MockClient) ContainerExecStart(ctx context.Context, execID string, config containertypes.ExecAttachOptions) error {
	if m.ContainerExecStartFn == nil {
		return nil
	}
	return m.ContainerExecStartFn(ctx, execID, config)
}

func (MockClient) ContainerExport(_ context.Context, _ string) (io.ReadCloser, error) {
	panic("not implemented")
}

func (m MockClient) ContainerInspect(ctx context.Context, name string) (dockertypes.ContainerJSON, error) {
	if m.ContainerInspectFn == nil {
		return dockertypes.ContainerJSON{}, nil
	}
	return m.ContainerInspectFn(ctx, name)
}

func (MockClient) ContainerInspectWithRaw(_ context.Context, _ string, _ bool) (dockertypes.ContainerJSON, []byte, error) {
	panic("not implemented")
}

func (MockClient) ContainerKill(_ context.Context, _ string, _ string) error {
	panic("not implemented")
}

func (MockClient) ContainerList(_ context.Context, _ containertypes.ListOptions) ([]containertypes.Summary, error) {
	panic("not implemented")
}

func (MockClient) ContainerLogs(_ context.Context, _ string, _ containertypes.LogsOptions) (io.ReadCloser, error) {
	panic("not implemented")
}

func (MockClient) ContainerPause(_ context.Context, _ string) error {
	panic("not implemented")
}

func (m MockClient) ContainerRemove(ctx context.Context, name string, options containertypes.RemoveOptions) error {
	if m.ContainerRemoveFn == nil {
		return nil
	}
	return m.ContainerRemoveFn(ctx, name, options)
}

func (MockClient) ContainerRename(_ context.Context, _ string, _ string) error {
	panic("not implemented")
}

func (MockClient) ContainerResize(_ context.Context, _ string, _ containertypes.ResizeOptions) error {
	panic("not implemented")
}

func (MockClient) ContainerRestart(_ context.Context, _ string, _ containertypes.StopOptions) error {
	panic("not implemented")
}

func (MockClient) ContainerStatPath(_ context.Context, _ string, _ string) (containertypes.PathStat, error) {
	panic("not implemented")
}

func (MockClient) ContainerStats(_ context.Context, _ string, _ bool) (containertypes.StatsResponseReader, error) {
	panic("not implemented")
}

func (m MockClient) ContainerStart(ctx context.Context, name string, options containertypes.StartOptions) error {
	if m.ContainerStartFn == nil {
		return nil
	}
	return m.ContainerStartFn(ctx, name, options)
}

func (MockClient) ContainerStop(_ context.Context, _ string, _ containertypes.StopOptions) error {
	panic("not implemented")
}

func (MockClient) ContainerTop(_ context.Context, _ string, _ []string) (containertypes.ContainerTopOKBody, error) {
	panic("not implemented")
}

func (MockClient) ContainerUnpause(_ context.Context, _ string) error {
	panic("not implemented")
}

func (MockClient) ContainerUpdate(_ context.Context, _ string, _ containertypes.UpdateConfig) (containertypes.ContainerUpdateOKBody, error) {
	panic("not implemented")
}

func (MockClient) ContainerWait(_ context.Context, _ string, _ containertypes.WaitCondition) (<-chan containertypes.WaitResponse, <-chan error) {
	panic("not implemented")
}

func (MockClient) CopyFromContainer(_ context.Context, _ string, _ string) (io.ReadCloser, containertypes.PathStat, error) {
	panic("not implemented")
}

func (MockClient) CopyToContainer(_ context.Context, _ string, _ string, _ io.Reader, _ containertypes.CopyToContainerOptions) error {
	panic("not implemented")
}

func (MockClient) ContainersPrune(_ context.Context, _ filters.Args) (containertypes.PruneReport, error) {
	panic("not implemented")
}

func (MockClient) ContainerStatsOneShot(_ context.Context, _ string) (containertypes.StatsResponseReader, error) {
	panic("not implemented")
}
