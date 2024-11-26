// Code generated by MockGen. DO NOT EDIT.
// Source: github.com/coder/envbox/dockerutil (interfaces: DockerClient)
//
// Generated by this command:
//
//	mockgen -destination ./mock.go -package dockerfake github.com/coder/envbox/dockerutil DockerClient
//

// Package dockerfake is a generated GoMock package.
package dockerfake

import (
	context "context"
	io "io"
	reflect "reflect"

	types "github.com/docker/docker/api/types"
	container "github.com/docker/docker/api/types/container"
	events "github.com/docker/docker/api/types/events"
	filters "github.com/docker/docker/api/types/filters"
	image "github.com/docker/docker/api/types/image"
	network "github.com/docker/docker/api/types/network"
	registry "github.com/docker/docker/api/types/registry"
	system "github.com/docker/docker/api/types/system"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	gomock "go.uber.org/mock/gomock"
)

// MockDockerClient is a mock of DockerClient interface.
type MockDockerClient struct {
	ctrl     *gomock.Controller
	recorder *MockDockerClientMockRecorder
	isgomock struct{}
}

// MockDockerClientMockRecorder is the mock recorder for MockDockerClient.
type MockDockerClientMockRecorder struct {
	mock *MockDockerClient
}

// NewMockDockerClient creates a new mock instance.
func NewMockDockerClient(ctrl *gomock.Controller) *MockDockerClient {
	mock := &MockDockerClient{ctrl: ctrl}
	mock.recorder = &MockDockerClientMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockDockerClient) EXPECT() *MockDockerClientMockRecorder {
	return m.recorder
}

// BuildCachePrune mocks base method.
func (m *MockDockerClient) BuildCachePrune(ctx context.Context, opts types.BuildCachePruneOptions) (*types.BuildCachePruneReport, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "BuildCachePrune", ctx, opts)
	ret0, _ := ret[0].(*types.BuildCachePruneReport)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// BuildCachePrune indicates an expected call of BuildCachePrune.
func (mr *MockDockerClientMockRecorder) BuildCachePrune(ctx, opts any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "BuildCachePrune", reflect.TypeOf((*MockDockerClient)(nil).BuildCachePrune), ctx, opts)
}

// BuildCancel mocks base method.
func (m *MockDockerClient) BuildCancel(ctx context.Context, id string) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "BuildCancel", ctx, id)
	ret0, _ := ret[0].(error)
	return ret0
}

// BuildCancel indicates an expected call of BuildCancel.
func (mr *MockDockerClientMockRecorder) BuildCancel(ctx, id any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "BuildCancel", reflect.TypeOf((*MockDockerClient)(nil).BuildCancel), ctx, id)
}

// ContainerAttach mocks base method.
func (m *MockDockerClient) ContainerAttach(ctx context.Context, container string, options container.AttachOptions) (types.HijackedResponse, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ContainerAttach", ctx, container, options)
	ret0, _ := ret[0].(types.HijackedResponse)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// ContainerAttach indicates an expected call of ContainerAttach.
func (mr *MockDockerClientMockRecorder) ContainerAttach(ctx, container, options any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ContainerAttach", reflect.TypeOf((*MockDockerClient)(nil).ContainerAttach), ctx, container, options)
}

// ContainerCommit mocks base method.
func (m *MockDockerClient) ContainerCommit(ctx context.Context, container string, options container.CommitOptions) (types.IDResponse, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ContainerCommit", ctx, container, options)
	ret0, _ := ret[0].(types.IDResponse)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// ContainerCommit indicates an expected call of ContainerCommit.
func (mr *MockDockerClientMockRecorder) ContainerCommit(ctx, container, options any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ContainerCommit", reflect.TypeOf((*MockDockerClient)(nil).ContainerCommit), ctx, container, options)
}

// ContainerCreate mocks base method.
func (m *MockDockerClient) ContainerCreate(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, platform *v1.Platform, containerName string) (container.CreateResponse, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ContainerCreate", ctx, config, hostConfig, networkingConfig, platform, containerName)
	ret0, _ := ret[0].(container.CreateResponse)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// ContainerCreate indicates an expected call of ContainerCreate.
func (mr *MockDockerClientMockRecorder) ContainerCreate(ctx, config, hostConfig, networkingConfig, platform, containerName any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ContainerCreate", reflect.TypeOf((*MockDockerClient)(nil).ContainerCreate), ctx, config, hostConfig, networkingConfig, platform, containerName)
}

// ContainerDiff mocks base method.
func (m *MockDockerClient) ContainerDiff(ctx context.Context, container string) ([]container.FilesystemChange, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ContainerDiff", ctx, container)
	ret0, _ := ret[0].([]container.FilesystemChange)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// ContainerDiff indicates an expected call of ContainerDiff.
func (mr *MockDockerClientMockRecorder) ContainerDiff(ctx, container any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ContainerDiff", reflect.TypeOf((*MockDockerClient)(nil).ContainerDiff), ctx, container)
}

// ContainerExecAttach mocks base method.
func (m *MockDockerClient) ContainerExecAttach(ctx context.Context, execID string, options container.ExecStartOptions) (types.HijackedResponse, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ContainerExecAttach", ctx, execID, options)
	ret0, _ := ret[0].(types.HijackedResponse)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// ContainerExecAttach indicates an expected call of ContainerExecAttach.
func (mr *MockDockerClientMockRecorder) ContainerExecAttach(ctx, execID, options any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ContainerExecAttach", reflect.TypeOf((*MockDockerClient)(nil).ContainerExecAttach), ctx, execID, options)
}

// ContainerExecCreate mocks base method.
func (m *MockDockerClient) ContainerExecCreate(ctx context.Context, container string, options container.ExecOptions) (types.IDResponse, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ContainerExecCreate", ctx, container, options)
	ret0, _ := ret[0].(types.IDResponse)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// ContainerExecCreate indicates an expected call of ContainerExecCreate.
func (mr *MockDockerClientMockRecorder) ContainerExecCreate(ctx, container, options any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ContainerExecCreate", reflect.TypeOf((*MockDockerClient)(nil).ContainerExecCreate), ctx, container, options)
}

// ContainerExecInspect mocks base method.
func (m *MockDockerClient) ContainerExecInspect(ctx context.Context, execID string) (container.ExecInspect, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ContainerExecInspect", ctx, execID)
	ret0, _ := ret[0].(container.ExecInspect)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// ContainerExecInspect indicates an expected call of ContainerExecInspect.
func (mr *MockDockerClientMockRecorder) ContainerExecInspect(ctx, execID any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ContainerExecInspect", reflect.TypeOf((*MockDockerClient)(nil).ContainerExecInspect), ctx, execID)
}

// ContainerExecResize mocks base method.
func (m *MockDockerClient) ContainerExecResize(ctx context.Context, execID string, options container.ResizeOptions) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ContainerExecResize", ctx, execID, options)
	ret0, _ := ret[0].(error)
	return ret0
}

// ContainerExecResize indicates an expected call of ContainerExecResize.
func (mr *MockDockerClientMockRecorder) ContainerExecResize(ctx, execID, options any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ContainerExecResize", reflect.TypeOf((*MockDockerClient)(nil).ContainerExecResize), ctx, execID, options)
}

// ContainerExecStart mocks base method.
func (m *MockDockerClient) ContainerExecStart(ctx context.Context, execID string, options container.ExecStartOptions) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ContainerExecStart", ctx, execID, options)
	ret0, _ := ret[0].(error)
	return ret0
}

// ContainerExecStart indicates an expected call of ContainerExecStart.
func (mr *MockDockerClientMockRecorder) ContainerExecStart(ctx, execID, options any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ContainerExecStart", reflect.TypeOf((*MockDockerClient)(nil).ContainerExecStart), ctx, execID, options)
}

// ContainerExport mocks base method.
func (m *MockDockerClient) ContainerExport(ctx context.Context, container string) (io.ReadCloser, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ContainerExport", ctx, container)
	ret0, _ := ret[0].(io.ReadCloser)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// ContainerExport indicates an expected call of ContainerExport.
func (mr *MockDockerClientMockRecorder) ContainerExport(ctx, container any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ContainerExport", reflect.TypeOf((*MockDockerClient)(nil).ContainerExport), ctx, container)
}

// ContainerInspect mocks base method.
func (m *MockDockerClient) ContainerInspect(ctx context.Context, container string) (types.ContainerJSON, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ContainerInspect", ctx, container)
	ret0, _ := ret[0].(types.ContainerJSON)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// ContainerInspect indicates an expected call of ContainerInspect.
func (mr *MockDockerClientMockRecorder) ContainerInspect(ctx, container any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ContainerInspect", reflect.TypeOf((*MockDockerClient)(nil).ContainerInspect), ctx, container)
}

// ContainerInspectWithRaw mocks base method.
func (m *MockDockerClient) ContainerInspectWithRaw(ctx context.Context, container string, getSize bool) (types.ContainerJSON, []byte, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ContainerInspectWithRaw", ctx, container, getSize)
	ret0, _ := ret[0].(types.ContainerJSON)
	ret1, _ := ret[1].([]byte)
	ret2, _ := ret[2].(error)
	return ret0, ret1, ret2
}

// ContainerInspectWithRaw indicates an expected call of ContainerInspectWithRaw.
func (mr *MockDockerClientMockRecorder) ContainerInspectWithRaw(ctx, container, getSize any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ContainerInspectWithRaw", reflect.TypeOf((*MockDockerClient)(nil).ContainerInspectWithRaw), ctx, container, getSize)
}

// ContainerKill mocks base method.
func (m *MockDockerClient) ContainerKill(ctx context.Context, container, signal string) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ContainerKill", ctx, container, signal)
	ret0, _ := ret[0].(error)
	return ret0
}

// ContainerKill indicates an expected call of ContainerKill.
func (mr *MockDockerClientMockRecorder) ContainerKill(ctx, container, signal any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ContainerKill", reflect.TypeOf((*MockDockerClient)(nil).ContainerKill), ctx, container, signal)
}

// ContainerList mocks base method.
func (m *MockDockerClient) ContainerList(ctx context.Context, options container.ListOptions) ([]types.Container, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ContainerList", ctx, options)
	ret0, _ := ret[0].([]types.Container)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// ContainerList indicates an expected call of ContainerList.
func (mr *MockDockerClientMockRecorder) ContainerList(ctx, options any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ContainerList", reflect.TypeOf((*MockDockerClient)(nil).ContainerList), ctx, options)
}

// ContainerLogs mocks base method.
func (m *MockDockerClient) ContainerLogs(ctx context.Context, container string, options container.LogsOptions) (io.ReadCloser, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ContainerLogs", ctx, container, options)
	ret0, _ := ret[0].(io.ReadCloser)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// ContainerLogs indicates an expected call of ContainerLogs.
func (mr *MockDockerClientMockRecorder) ContainerLogs(ctx, container, options any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ContainerLogs", reflect.TypeOf((*MockDockerClient)(nil).ContainerLogs), ctx, container, options)
}

// ContainerPause mocks base method.
func (m *MockDockerClient) ContainerPause(ctx context.Context, container string) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ContainerPause", ctx, container)
	ret0, _ := ret[0].(error)
	return ret0
}

// ContainerPause indicates an expected call of ContainerPause.
func (mr *MockDockerClientMockRecorder) ContainerPause(ctx, container any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ContainerPause", reflect.TypeOf((*MockDockerClient)(nil).ContainerPause), ctx, container)
}

// ContainerRemove mocks base method.
func (m *MockDockerClient) ContainerRemove(ctx context.Context, container string, options container.RemoveOptions) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ContainerRemove", ctx, container, options)
	ret0, _ := ret[0].(error)
	return ret0
}

// ContainerRemove indicates an expected call of ContainerRemove.
func (mr *MockDockerClientMockRecorder) ContainerRemove(ctx, container, options any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ContainerRemove", reflect.TypeOf((*MockDockerClient)(nil).ContainerRemove), ctx, container, options)
}

// ContainerRename mocks base method.
func (m *MockDockerClient) ContainerRename(ctx context.Context, container, newContainerName string) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ContainerRename", ctx, container, newContainerName)
	ret0, _ := ret[0].(error)
	return ret0
}

// ContainerRename indicates an expected call of ContainerRename.
func (mr *MockDockerClientMockRecorder) ContainerRename(ctx, container, newContainerName any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ContainerRename", reflect.TypeOf((*MockDockerClient)(nil).ContainerRename), ctx, container, newContainerName)
}

// ContainerResize mocks base method.
func (m *MockDockerClient) ContainerResize(ctx context.Context, container string, options container.ResizeOptions) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ContainerResize", ctx, container, options)
	ret0, _ := ret[0].(error)
	return ret0
}

// ContainerResize indicates an expected call of ContainerResize.
func (mr *MockDockerClientMockRecorder) ContainerResize(ctx, container, options any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ContainerResize", reflect.TypeOf((*MockDockerClient)(nil).ContainerResize), ctx, container, options)
}

// ContainerRestart mocks base method.
func (m *MockDockerClient) ContainerRestart(ctx context.Context, container string, options container.StopOptions) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ContainerRestart", ctx, container, options)
	ret0, _ := ret[0].(error)
	return ret0
}

// ContainerRestart indicates an expected call of ContainerRestart.
func (mr *MockDockerClientMockRecorder) ContainerRestart(ctx, container, options any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ContainerRestart", reflect.TypeOf((*MockDockerClient)(nil).ContainerRestart), ctx, container, options)
}

// ContainerStart mocks base method.
func (m *MockDockerClient) ContainerStart(ctx context.Context, container string, options container.StartOptions) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ContainerStart", ctx, container, options)
	ret0, _ := ret[0].(error)
	return ret0
}

// ContainerStart indicates an expected call of ContainerStart.
func (mr *MockDockerClientMockRecorder) ContainerStart(ctx, container, options any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ContainerStart", reflect.TypeOf((*MockDockerClient)(nil).ContainerStart), ctx, container, options)
}

// ContainerStatPath mocks base method.
func (m *MockDockerClient) ContainerStatPath(ctx context.Context, container, path string) (container.PathStat, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ContainerStatPath", ctx, container, path)
	ret0, _ := ret[0].(container.PathStat)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// ContainerStatPath indicates an expected call of ContainerStatPath.
func (mr *MockDockerClientMockRecorder) ContainerStatPath(ctx, container, path any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ContainerStatPath", reflect.TypeOf((*MockDockerClient)(nil).ContainerStatPath), ctx, container, path)
}

// ContainerStats mocks base method.
func (m *MockDockerClient) ContainerStats(ctx context.Context, container string, stream bool) (container.StatsResponseReader, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ContainerStats", ctx, container, stream)
	ret0, _ := ret[0].(container.StatsResponseReader)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// ContainerStats indicates an expected call of ContainerStats.
func (mr *MockDockerClientMockRecorder) ContainerStats(ctx, container, stream any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ContainerStats", reflect.TypeOf((*MockDockerClient)(nil).ContainerStats), ctx, container, stream)
}

// ContainerStatsOneShot mocks base method.
func (m *MockDockerClient) ContainerStatsOneShot(ctx context.Context, container string) (container.StatsResponseReader, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ContainerStatsOneShot", ctx, container)
	ret0, _ := ret[0].(container.StatsResponseReader)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// ContainerStatsOneShot indicates an expected call of ContainerStatsOneShot.
func (mr *MockDockerClientMockRecorder) ContainerStatsOneShot(ctx, container any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ContainerStatsOneShot", reflect.TypeOf((*MockDockerClient)(nil).ContainerStatsOneShot), ctx, container)
}

// ContainerStop mocks base method.
func (m *MockDockerClient) ContainerStop(ctx context.Context, container string, options container.StopOptions) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ContainerStop", ctx, container, options)
	ret0, _ := ret[0].(error)
	return ret0
}

// ContainerStop indicates an expected call of ContainerStop.
func (mr *MockDockerClientMockRecorder) ContainerStop(ctx, container, options any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ContainerStop", reflect.TypeOf((*MockDockerClient)(nil).ContainerStop), ctx, container, options)
}

// ContainerTop mocks base method.
func (m *MockDockerClient) ContainerTop(ctx context.Context, container string, arguments []string) (container.ContainerTopOKBody, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ContainerTop", ctx, container, arguments)
	ret0, _ := ret[0].(container.ContainerTopOKBody)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// ContainerTop indicates an expected call of ContainerTop.
func (mr *MockDockerClientMockRecorder) ContainerTop(ctx, container, arguments any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ContainerTop", reflect.TypeOf((*MockDockerClient)(nil).ContainerTop), ctx, container, arguments)
}

// ContainerUnpause mocks base method.
func (m *MockDockerClient) ContainerUnpause(ctx context.Context, container string) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ContainerUnpause", ctx, container)
	ret0, _ := ret[0].(error)
	return ret0
}

// ContainerUnpause indicates an expected call of ContainerUnpause.
func (mr *MockDockerClientMockRecorder) ContainerUnpause(ctx, container any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ContainerUnpause", reflect.TypeOf((*MockDockerClient)(nil).ContainerUnpause), ctx, container)
}

// ContainerUpdate mocks base method.
func (m *MockDockerClient) ContainerUpdate(ctx context.Context, container string, updateConfig container.UpdateConfig) (container.ContainerUpdateOKBody, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ContainerUpdate", ctx, container, updateConfig)
	ret0, _ := ret[0].(container.ContainerUpdateOKBody)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// ContainerUpdate indicates an expected call of ContainerUpdate.
func (mr *MockDockerClientMockRecorder) ContainerUpdate(ctx, container, updateConfig any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ContainerUpdate", reflect.TypeOf((*MockDockerClient)(nil).ContainerUpdate), ctx, container, updateConfig)
}

// ContainerWait mocks base method.
func (m *MockDockerClient) ContainerWait(ctx context.Context, container string, condition container.WaitCondition) (<-chan container.WaitResponse, <-chan error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ContainerWait", ctx, container, condition)
	ret0, _ := ret[0].(<-chan container.WaitResponse)
	ret1, _ := ret[1].(<-chan error)
	return ret0, ret1
}

// ContainerWait indicates an expected call of ContainerWait.
func (mr *MockDockerClientMockRecorder) ContainerWait(ctx, container, condition any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ContainerWait", reflect.TypeOf((*MockDockerClient)(nil).ContainerWait), ctx, container, condition)
}

// ContainersPrune mocks base method.
func (m *MockDockerClient) ContainersPrune(ctx context.Context, pruneFilters filters.Args) (container.PruneReport, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ContainersPrune", ctx, pruneFilters)
	ret0, _ := ret[0].(container.PruneReport)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// ContainersPrune indicates an expected call of ContainersPrune.
func (mr *MockDockerClientMockRecorder) ContainersPrune(ctx, pruneFilters any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ContainersPrune", reflect.TypeOf((*MockDockerClient)(nil).ContainersPrune), ctx, pruneFilters)
}

// CopyFromContainer mocks base method.
func (m *MockDockerClient) CopyFromContainer(ctx context.Context, container, srcPath string) (io.ReadCloser, container.PathStat, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "CopyFromContainer", ctx, container, srcPath)
	ret0, _ := ret[0].(io.ReadCloser)
	ret1, _ := ret[1].(container.PathStat)
	ret2, _ := ret[2].(error)
	return ret0, ret1, ret2
}

// CopyFromContainer indicates an expected call of CopyFromContainer.
func (mr *MockDockerClientMockRecorder) CopyFromContainer(ctx, container, srcPath any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "CopyFromContainer", reflect.TypeOf((*MockDockerClient)(nil).CopyFromContainer), ctx, container, srcPath)
}

// CopyToContainer mocks base method.
func (m *MockDockerClient) CopyToContainer(ctx context.Context, container, path string, content io.Reader, options container.CopyToContainerOptions) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "CopyToContainer", ctx, container, path, content, options)
	ret0, _ := ret[0].(error)
	return ret0
}

// CopyToContainer indicates an expected call of CopyToContainer.
func (mr *MockDockerClientMockRecorder) CopyToContainer(ctx, container, path, content, options any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "CopyToContainer", reflect.TypeOf((*MockDockerClient)(nil).CopyToContainer), ctx, container, path, content, options)
}

// DiskUsage mocks base method.
func (m *MockDockerClient) DiskUsage(ctx context.Context, options types.DiskUsageOptions) (types.DiskUsage, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "DiskUsage", ctx, options)
	ret0, _ := ret[0].(types.DiskUsage)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// DiskUsage indicates an expected call of DiskUsage.
func (mr *MockDockerClientMockRecorder) DiskUsage(ctx, options any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "DiskUsage", reflect.TypeOf((*MockDockerClient)(nil).DiskUsage), ctx, options)
}

// Events mocks base method.
func (m *MockDockerClient) Events(ctx context.Context, options events.ListOptions) (<-chan events.Message, <-chan error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Events", ctx, options)
	ret0, _ := ret[0].(<-chan events.Message)
	ret1, _ := ret[1].(<-chan error)
	return ret0, ret1
}

// Events indicates an expected call of Events.
func (mr *MockDockerClientMockRecorder) Events(ctx, options any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Events", reflect.TypeOf((*MockDockerClient)(nil).Events), ctx, options)
}

// ImageBuild mocks base method.
func (m *MockDockerClient) ImageBuild(ctx context.Context, context io.Reader, options types.ImageBuildOptions) (types.ImageBuildResponse, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ImageBuild", ctx, context, options)
	ret0, _ := ret[0].(types.ImageBuildResponse)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// ImageBuild indicates an expected call of ImageBuild.
func (mr *MockDockerClientMockRecorder) ImageBuild(ctx, context, options any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ImageBuild", reflect.TypeOf((*MockDockerClient)(nil).ImageBuild), ctx, context, options)
}

// ImageCreate mocks base method.
func (m *MockDockerClient) ImageCreate(ctx context.Context, parentReference string, options image.CreateOptions) (io.ReadCloser, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ImageCreate", ctx, parentReference, options)
	ret0, _ := ret[0].(io.ReadCloser)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// ImageCreate indicates an expected call of ImageCreate.
func (mr *MockDockerClientMockRecorder) ImageCreate(ctx, parentReference, options any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ImageCreate", reflect.TypeOf((*MockDockerClient)(nil).ImageCreate), ctx, parentReference, options)
}

// ImageHistory mocks base method.
func (m *MockDockerClient) ImageHistory(ctx context.Context, image string) ([]image.HistoryResponseItem, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ImageHistory", ctx, image)
	ret0, _ := ret[0].([]image.HistoryResponseItem)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// ImageHistory indicates an expected call of ImageHistory.
func (mr *MockDockerClientMockRecorder) ImageHistory(ctx, image any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ImageHistory", reflect.TypeOf((*MockDockerClient)(nil).ImageHistory), ctx, image)
}

// ImageImport mocks base method.
func (m *MockDockerClient) ImageImport(ctx context.Context, source image.ImportSource, ref string, options image.ImportOptions) (io.ReadCloser, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ImageImport", ctx, source, ref, options)
	ret0, _ := ret[0].(io.ReadCloser)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// ImageImport indicates an expected call of ImageImport.
func (mr *MockDockerClientMockRecorder) ImageImport(ctx, source, ref, options any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ImageImport", reflect.TypeOf((*MockDockerClient)(nil).ImageImport), ctx, source, ref, options)
}

// ImageInspectWithRaw mocks base method.
func (m *MockDockerClient) ImageInspectWithRaw(ctx context.Context, image string) (types.ImageInspect, []byte, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ImageInspectWithRaw", ctx, image)
	ret0, _ := ret[0].(types.ImageInspect)
	ret1, _ := ret[1].([]byte)
	ret2, _ := ret[2].(error)
	return ret0, ret1, ret2
}

// ImageInspectWithRaw indicates an expected call of ImageInspectWithRaw.
func (mr *MockDockerClientMockRecorder) ImageInspectWithRaw(ctx, image any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ImageInspectWithRaw", reflect.TypeOf((*MockDockerClient)(nil).ImageInspectWithRaw), ctx, image)
}

// ImageList mocks base method.
func (m *MockDockerClient) ImageList(ctx context.Context, options image.ListOptions) ([]image.Summary, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ImageList", ctx, options)
	ret0, _ := ret[0].([]image.Summary)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// ImageList indicates an expected call of ImageList.
func (mr *MockDockerClientMockRecorder) ImageList(ctx, options any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ImageList", reflect.TypeOf((*MockDockerClient)(nil).ImageList), ctx, options)
}

// ImageLoad mocks base method.
func (m *MockDockerClient) ImageLoad(ctx context.Context, input io.Reader, quiet bool) (image.LoadResponse, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ImageLoad", ctx, input, quiet)
	ret0, _ := ret[0].(image.LoadResponse)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// ImageLoad indicates an expected call of ImageLoad.
func (mr *MockDockerClientMockRecorder) ImageLoad(ctx, input, quiet any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ImageLoad", reflect.TypeOf((*MockDockerClient)(nil).ImageLoad), ctx, input, quiet)
}

// ImagePull mocks base method.
func (m *MockDockerClient) ImagePull(ctx context.Context, ref string, options image.PullOptions) (io.ReadCloser, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ImagePull", ctx, ref, options)
	ret0, _ := ret[0].(io.ReadCloser)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// ImagePull indicates an expected call of ImagePull.
func (mr *MockDockerClientMockRecorder) ImagePull(ctx, ref, options any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ImagePull", reflect.TypeOf((*MockDockerClient)(nil).ImagePull), ctx, ref, options)
}

// ImagePush mocks base method.
func (m *MockDockerClient) ImagePush(ctx context.Context, ref string, options image.PushOptions) (io.ReadCloser, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ImagePush", ctx, ref, options)
	ret0, _ := ret[0].(io.ReadCloser)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// ImagePush indicates an expected call of ImagePush.
func (mr *MockDockerClientMockRecorder) ImagePush(ctx, ref, options any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ImagePush", reflect.TypeOf((*MockDockerClient)(nil).ImagePush), ctx, ref, options)
}

// ImageRemove mocks base method.
func (m *MockDockerClient) ImageRemove(ctx context.Context, image string, options image.RemoveOptions) ([]image.DeleteResponse, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ImageRemove", ctx, image, options)
	ret0, _ := ret[0].([]image.DeleteResponse)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// ImageRemove indicates an expected call of ImageRemove.
func (mr *MockDockerClientMockRecorder) ImageRemove(ctx, image, options any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ImageRemove", reflect.TypeOf((*MockDockerClient)(nil).ImageRemove), ctx, image, options)
}

// ImageSave mocks base method.
func (m *MockDockerClient) ImageSave(ctx context.Context, images []string) (io.ReadCloser, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ImageSave", ctx, images)
	ret0, _ := ret[0].(io.ReadCloser)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// ImageSave indicates an expected call of ImageSave.
func (mr *MockDockerClientMockRecorder) ImageSave(ctx, images any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ImageSave", reflect.TypeOf((*MockDockerClient)(nil).ImageSave), ctx, images)
}

// ImageSearch mocks base method.
func (m *MockDockerClient) ImageSearch(ctx context.Context, term string, options registry.SearchOptions) ([]registry.SearchResult, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ImageSearch", ctx, term, options)
	ret0, _ := ret[0].([]registry.SearchResult)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// ImageSearch indicates an expected call of ImageSearch.
func (mr *MockDockerClientMockRecorder) ImageSearch(ctx, term, options any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ImageSearch", reflect.TypeOf((*MockDockerClient)(nil).ImageSearch), ctx, term, options)
}

// ImageTag mocks base method.
func (m *MockDockerClient) ImageTag(ctx context.Context, image, ref string) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ImageTag", ctx, image, ref)
	ret0, _ := ret[0].(error)
	return ret0
}

// ImageTag indicates an expected call of ImageTag.
func (mr *MockDockerClientMockRecorder) ImageTag(ctx, image, ref any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ImageTag", reflect.TypeOf((*MockDockerClient)(nil).ImageTag), ctx, image, ref)
}

// ImagesPrune mocks base method.
func (m *MockDockerClient) ImagesPrune(ctx context.Context, pruneFilter filters.Args) (image.PruneReport, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ImagesPrune", ctx, pruneFilter)
	ret0, _ := ret[0].(image.PruneReport)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// ImagesPrune indicates an expected call of ImagesPrune.
func (mr *MockDockerClientMockRecorder) ImagesPrune(ctx, pruneFilter any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ImagesPrune", reflect.TypeOf((*MockDockerClient)(nil).ImagesPrune), ctx, pruneFilter)
}

// Info mocks base method.
func (m *MockDockerClient) Info(ctx context.Context) (system.Info, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Info", ctx)
	ret0, _ := ret[0].(system.Info)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Info indicates an expected call of Info.
func (mr *MockDockerClientMockRecorder) Info(ctx any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Info", reflect.TypeOf((*MockDockerClient)(nil).Info), ctx)
}

// Ping mocks base method.
func (m *MockDockerClient) Ping(ctx context.Context) (types.Ping, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Ping", ctx)
	ret0, _ := ret[0].(types.Ping)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Ping indicates an expected call of Ping.
func (mr *MockDockerClientMockRecorder) Ping(ctx any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Ping", reflect.TypeOf((*MockDockerClient)(nil).Ping), ctx)
}

// RegistryLogin mocks base method.
func (m *MockDockerClient) RegistryLogin(ctx context.Context, auth registry.AuthConfig) (registry.AuthenticateOKBody, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "RegistryLogin", ctx, auth)
	ret0, _ := ret[0].(registry.AuthenticateOKBody)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// RegistryLogin indicates an expected call of RegistryLogin.
func (mr *MockDockerClientMockRecorder) RegistryLogin(ctx, auth any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "RegistryLogin", reflect.TypeOf((*MockDockerClient)(nil).RegistryLogin), ctx, auth)
}
