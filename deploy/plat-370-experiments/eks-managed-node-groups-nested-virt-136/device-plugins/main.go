package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	deviceplugin "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

const (
	pluginDir   = "/var/lib/kubelet/device-plugins"
	kubeletSock = "kubelet.sock"
	endpoint    = "coder-kvm.sock"
	resource    = "devices.coder.com/kvm"
	devicePath  = "/dev/kvm"
)

type plugin struct {
	deviceplugin.UnimplementedDevicePluginServer
	devicePath  string
	deviceCount int
}

func (p *plugin) GetDevicePluginOptions(context.Context, *deviceplugin.Empty) (*deviceplugin.DevicePluginOptions, error) {
	return &deviceplugin.DevicePluginOptions{}, nil
}

func (p *plugin) ListAndWatch(_ *deviceplugin.Empty, stream deviceplugin.DevicePlugin_ListAndWatchServer) error {
	count := p.deviceCount
	if value := os.Getenv("KVM_DEVICE_COUNT"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 {
			return fmt.Errorf("invalid KVM_DEVICE_COUNT %q", value)
		}
		count = parsed
	}
	devices := make([]*deviceplugin.Device, 0, count)
	for i := 0; i < count; i++ {
		devices = append(devices, &deviceplugin.Device{ID: fmt.Sprintf("kvm%d", i), Health: deviceplugin.Healthy})
	}
	if err := stream.Send(&deviceplugin.ListAndWatchResponse{Devices: devices}); err != nil {
		return err
	}
	select {}
}

func (p *plugin) Allocate(_ context.Context, request *deviceplugin.AllocateRequest) (*deviceplugin.AllocateResponse, error) {
	response := &deviceplugin.AllocateResponse{}
	for range request.ContainerRequests {
		response.ContainerResponses = append(response.ContainerResponses, &deviceplugin.ContainerAllocateResponse{
			Devices: []*deviceplugin.DeviceSpec{{HostPath: p.devicePath, ContainerPath: p.devicePath, Permissions: "rwm"}},
		})
	}
	return response, nil
}

func main() {
	configuredDevicePath := os.Getenv("DEVICE_PLUGIN_DEVICE_PATH")
	if configuredDevicePath == "" {
		configuredDevicePath = devicePath
	}
	configuredResource := os.Getenv("DEVICE_PLUGIN_RESOURCE")
	if configuredResource == "" {
		configuredResource = resource
	}
	configuredEndpoint := os.Getenv("DEVICE_PLUGIN_ENDPOINT")
	if configuredEndpoint == "" {
		configuredEndpoint = endpoint
	}
	deviceCount := 1
	if value := os.Getenv("KVM_DEVICE_COUNT"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 {
			log.Fatalf("invalid KVM_DEVICE_COUNT %q", value)
		}
		deviceCount = parsed
	}
	if _, err := os.Stat(configuredDevicePath); err != nil {
		log.Fatalf("%s is unavailable: %v", configuredDevicePath, err)
	}

	socketPath := filepath.Join(pluginDir, configuredEndpoint)
	_ = os.Remove(socketPath)
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		log.Fatalf("listen on %s: %v", socketPath, err)
	}
	server := grpc.NewServer()
	deviceplugin.RegisterDevicePluginServer(server, &plugin{devicePath: configuredDevicePath, deviceCount: deviceCount})
	go func() {
		if err := server.Serve(listener); err != nil {
			log.Fatalf("serve device plugin: %v", err)
		}
	}()
	defer server.Stop()

	kubeletPath := filepath.Join(pluginDir, kubeletSock)
	var conn *grpc.ClientConn
	for deadline := time.Now().Add(2 * time.Minute); time.Now().Before(deadline); {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		conn, err = grpc.DialContext(ctx, "unix://"+kubeletPath, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
		cancel()
		if err == nil {
			break
		}
		log.Printf("waiting for kubelet device-plugin socket: %v", err)
		time.Sleep(2 * time.Second)
	}
	if err != nil {
		log.Fatalf("connect to kubelet device-plugin socket: %v", err)
	}
	defer conn.Close()

	client := deviceplugin.NewRegistrationClient(conn)
	if _, err := client.Register(context.Background(), &deviceplugin.RegisterRequest{
		Version:      deviceplugin.Version,
		Endpoint:     configuredEndpoint,
		ResourceName: configuredResource,
	}); err != nil {
		log.Fatalf("register %s: %v", resource, err)
	}

	log.Printf("registered %s using %s", configuredResource, configuredDevicePath)
	select {}
}
