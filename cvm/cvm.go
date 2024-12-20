package cvm

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"cdr.dev/slog"
	"github.com/coder/envbox/buildlog"
	"github.com/coder/envbox/dockerutil"
	"github.com/coder/envbox/xunix"
	"github.com/docker/docker/api/types/container"
	"github.com/google/go-containerregistry/pkg/name"
	"golang.org/x/xerrors"
)

const (
	EnvAgentToken = "CODER_AGENT_TOKEN"

	OuterFUSEPath = "/tmp/coder-fuse"
	InnerFUSEPath = "/dev/fuse"

	OuterTUNPath = "/tmp/coder-tun"
	InnerTUNPath = "/dev/net/tun"

	UserNamespaceOffset = 100000
	InnerContainerName  = "workspace_cvm"
)

type GPUConfig struct {
	HostUsrLibDir string
}

type Config struct {
	// Required fields.
	Tag        name.Tag
	Username   string
	AgentToken string
	OSEnvs     []string

	// Optional fields.
	InnerEnvs            []string
	WorkDir              string
	Hostname             string
	ImagePullSecret      string
	CoderURL             string
	AddTUN               bool
	AddFUSE              bool
	AddGPU               bool
	DockerDBridgeCIDR    string
	BoostrapScript       string
	Mounts               []xunix.Mount
	HostUsrLibDir        string
	DockerConfig         string
	CPUS                 int
	Memory               int
	DisableIDMappedMount bool
	ExtraCertsPath       string

	// Test flags.
	Debug         bool
	noStartupLogs bool
	ethlink       string

	GPUConfig
}

func Run(ctx context.Context, log slog.Logger, blog buildlog.Logger, client dockerutil.Client, fs xunix.FS, cfg Config) error {

	log = log.With(
		slog.F("image", cfg.Tag.String()),
		slog.F("username", cfg.Username),
		slog.F("workdir", cfg.WorkDir),
		slog.F("hostname", cfg.Hostname),
		slog.F("coder_url", cfg.CoderURL),
		slog.F("add_tun", cfg.AddTUN),
		slog.F("add_fuse", cfg.AddFUSE),
		slog.F("add_gpu", cfg.AddGPU),
		slog.F("docker_dbridge_cidr", cfg.DockerDBridgeCIDR),
		slog.F("host_usr_lib_dir", cfg.HostUsrLibDir),
		slog.F("docker_config", cfg.DockerConfig),
		slog.F("cpus", cfg.CPUS),
		slog.F("memory", cfg.Memory),
		slog.F("disable_id_mapped_mount", cfg.DisableIDMappedMount),
		slog.F("extra_certs_path", cfg.ExtraCertsPath),
		slog.F("mounts", cfg.Mounts),
	)

	var dockerAuth dockerutil.AuthConfig
	if cfg.ImagePullSecret != "" {
		var err error
		dockerAuth, err = dockerutil.AuthConfigFromString(cfg.ImagePullSecret, cfg.Tag.RegistryStr())
		if err != nil {
			return xerrors.Errorf("parse auth config: %w", err)
		}
	}

	log.Info(ctx, "checking for docker config file")

	if _, err := fs.Stat(cfg.DockerConfig); err == nil {
		log.Info(ctx, "detected docker config file")
		dockerAuth, err = dockerutil.AuthConfigFromPath(cfg.DockerConfig, cfg.Tag.RegistryStr())
		if err != nil && !xerrors.Is(err, os.ErrNotExist) {
			return xerrors.Errorf("auth config from file: %w", err)
		}
	}

	envs := defaultContainerEnvs(ctx, cfg.OSEnvs, cfg.AgentToken)
	envs = append(envs, filterEnvs(cfg.OSEnvs, cfg.InnerEnvs...)...)

	mounts := append(defaultMounts(), cfg.Mounts...)

	devices, err := ensureDevices(ctx, fs, log, blog, cfg.AddTUN, cfg.AddFUSE)
	if err != nil {
		return xerrors.Errorf("create devices: %w", err)
	}

	log.Debug(ctx, "pulling image")

	err = dockerutil.PullImage(ctx, &dockerutil.PullImageConfig{
		Client:     client,
		Image:      cfg.Tag.String(),
		Auth:       dockerAuth,
		ProgressFn: dockerutil.DefaultLogImagePullFn(blog),
	})
	if err != nil {
		return xerrors.Errorf("pull image: %w", err)
	}

	// After image pull we remount /sys so sysbox can have appropriate perms to create a container.
	err = xunix.MountFS(ctx, "/sys", "/sys", "", "remount", "rw")
	if err != nil {
		return xerrors.Errorf("remount /sys: %w", err)
	}

	if cfg.GPUConfig.HostUsrLibDir != "" {
		// Unmount GPU drivers in /proc as it causes issues when creating any
		// container in some cases (even the image metadata container).
		_, err = xunix.TryUnmountProcGPUDrivers(ctx, log)
		if err != nil {
			return xerrors.Errorf("unmount /proc GPU drivers: %w", err)
		}
	}

	log.Debug(ctx, "fetching image metadata")

	blog.Info("Getting image metadata...")

	// Get metadata about the image. We need to know things like the UID/GID
	// of the user so that we can chown directories to the namespaced UID inside
	// the inner container as well as whether we should be starting the container
	// with /sbin/init or something simple like 'sleep infinity'.
	imgMeta, err := dockerutil.GetImageMetadata(ctx, client, cfg.Tag.String(), cfg.Username)
	if err != nil {
		return xerrors.Errorf("get image metadata: %w", err)
	}

	log.Debug(ctx, "fetched image metadata",
		slog.F("uid", imgMeta.UID),
		slog.F("gid", imgMeta.GID),
		slog.F("has_init", imgMeta.HasInit),
	)

	blog.Infof("Detected entrypoint user '%s:%s' with home directory %q", imgMeta.UID, imgMeta.UID, imgMeta.HomeDir)

	log.Debug(ctx, "fetched image metadata",
		slog.F("uid", imgMeta.UID),
		slog.F("gid", imgMeta.GID),
		slog.F("has_init", imgMeta.HasInit),
	)

	err = idShiftMounts(ctx, log, fs, mounts, imgMeta.UID, imgMeta.GID)
	if err != nil {
		return xerrors.Errorf("id shift mounts: %w", err)
	}

	if cfg.GPUConfig.HostUsrLibDir != "" {
		devs, mounts, envs, err := gpuMappings(ctx, log, cfg.GPUConfig.HostUsrLibDir)
		if err != nil {
			return xerrors.Errorf("gpu mappings: %w", err)
		}

		mounts = append(mounts, mounts...)
		envs = append(envs, envs...)
		devices = append(devices, devs...)
	}

	blog.Info("Creating workspace...")

	// Create the inner container.
	containerID, err := dockerutil.CreateContainer(ctx, client, &dockerutil.ContainerConfig{
		Log:         log,
		Mounts:      mounts,
		Devices:     devices,
		Envs:        envs,
		Name:        InnerContainerName,
		Hostname:    cfg.Hostname,
		WorkingDir:  cfg.WorkDir,
		HasInit:     imgMeta.HasInit,
		Image:       cfg.Tag.String(),
		CPUs:        int64(cfg.CPUS),
		MemoryLimit: int64(cfg.Memory),
	})
	if err != nil {
		return xerrors.Errorf("create container: %w", err)
	}

	blog.Info("Pruning images to free up disk...")
	// Prune images to avoid taking up any unnecessary disk from the user.
	_, err = dockerutil.PruneImages(ctx, client)
	if err != nil {
		return xerrors.Errorf("prune images: %w", err)
	}

	// TODO fix iptables when istio detected.

	blog.Info("Starting up workspace...")
	err = client.ContainerStart(ctx, containerID, container.StartOptions{})
	if err != nil {
		return xerrors.Errorf("start container: %w", err)
	}

	log.Debug(ctx, "creating bootstrap directory", slog.F("directory", imgMeta.HomeDir))

	// Create the directory to which we will download the agent.
	// We create this directory because the default behavior is
	// to download the agent to /tmp/coder.XXXX. This causes a race to happen
	// where we finish downloading the binary but before we can execute
	// systemd remounts /tmp.
	bootDir := filepath.Join(imgMeta.HomeDir, ".coder")

	blog.Infof("Creating %q directory to host Coder assets...", bootDir)
	_, err = dockerutil.ExecContainer(ctx, client, dockerutil.ExecConfig{
		ContainerID: containerID,
		User:        strconv.Itoa(imgMeta.UID),
		Cmd:         "mkdir",
		Args:        []string{"-p", bootDir},
	})
	if err != nil {
		return xerrors.Errorf("make bootstrap dir: %w", err)
	}

	cpuQuota, err := xunix.ReadCPUQuota(ctx, log)
	if err != nil {
		blog.Infof("Unable to read CPU quota: %s", err.Error())
	} else {
		log.Debug(ctx, "setting CPU quota",
			slog.F("quota", cpuQuota.Quota),
			slog.F("period", cpuQuota.Period),
			slog.F("cgroup", cpuQuota.CGroup.String()),
		)

		// We want the inner container to have the same limits as the outer container
		// so that processes inside the container know what they're working with.
		if err := dockerutil.SetContainerQuota(ctx, containerID, cpuQuota); err != nil {
			blog.Infof("Unable to set quota for inner container: %s", err.Error())
			blog.Info("This is not a fatal error, but it may cause cgroup-aware applications to misbehave.")
		}
	}

	blog.Info("Envbox startup complete!")

	// The bootstrap script doesn't return since it execs the agent
	// meaning that it can get pretty noisy if we were to log by default.
	// In order to allow users to discern issues getting the bootstrap script
	// to complete successfully we pipe the output to stdout if
	// CODER_DEBUG=true.
	debugWriter := io.Discard
	if cfg.Debug {
		debugWriter = os.Stdout
	}
	// Bootstrap the container if a script has been provided.
	blog.Infof("Bootstrapping workspace...")
	err = dockerutil.BootstrapContainer(ctx, client, dockerutil.BootstrapConfig{
		ContainerID: containerID,
		User:        strconv.Itoa(imgMeta.UID),
		Script:      cfg.BoostrapScript,
		// We set this because the default behavior is to download the agent
		// to /tmp/coder.XXXX. This causes a race to happen where we finish
		// downloading the binary but before we can execute systemd remounts
		// /tmp.
		Env:       []string{fmt.Sprintf("BINARY_DIR=%s", bootDir)},
		StdOutErr: debugWriter,
	})
	if err != nil {
		return xerrors.Errorf("boostrap container: %w", err)
	}

	return nil
}

func gpuMappings(ctx context.Context, log slog.Logger, urlLibDir string) ([]container.DeviceMapping, []xunix.Mount, []string, error) {
	devs, binds, err := xunix.GPUs(ctx, log, urlLibDir)
	if err != nil {
		return nil, nil, nil, xerrors.Errorf("find gpus: %w", err)
	}

	devices := make([]container.DeviceMapping, 0, len(devs))
	for _, dev := range devs {
		devices = append(devices, container.DeviceMapping{
			PathOnHost:        dev.Path,
			PathInContainer:   dev.Path,
			CgroupPermissions: "rwm",
		})
	}

	for i, bind := range binds {
		// If the bind has a path that points to the host-mounted /usr/lib
		// directory we need to remap it to /usr/lib inside the container.
		bind.Mountpoint = bind.Source
		if strings.HasPrefix(bind.Mountpoint, urlLibDir) {
			bind.Mountpoint = filepath.Join(
				"/usr/lib",
				strings.TrimPrefix(bind.Mountpoint, strings.TrimSuffix(urlLibDir, "/")),
			)
		}
		binds[i] = bind
	}

	envs := xunix.GPUEnvs(ctx)

	return devices, binds, envs, nil
}

// filterEnvs filters a list of environment variables returning a subset that matches
// the provided patterns. Patterns can be exact matches or use * as a wildcard. E.g. 'MY_ENV'
// matches only MY_ENV=value but 'MY_*' matches MY_ENV=value and MY_OTHER_ENV=value.
func filterEnvs(os []string, matches ...string) []string {
	filtered := make([]string, 0, len(os))
	for _, f := range matches {
		f = strings.TrimSpace(f)
		for _, s := range os {
			toks := strings.Split(s, "=")
			if len(toks) < 2 {
				// Malformed environment variable.
				continue
			}

			key := toks[0]

			if strings.HasSuffix(f, "*") {
				filter := strings.TrimSuffix(f, "*")
				if strings.HasPrefix(key, filter) {
					filtered = append(filtered, s)
				}
			} else if key == f {
				filtered = append(filtered, s)
			}
		}
	}

	return filtered
}

func defaultContainerEnvs(ctx context.Context, osEnvs []string, agentToken string) []string {
	const agentSubsystemEnv = "CODER_AGENT_SUBSYSTEM"
	existingSubsystem := ""
	for _, e := range osEnvs {
		if strings.HasPrefix(e, agentSubsystemEnv+"=") {
			existingSubsystem = strings.TrimPrefix(e, agentSubsystemEnv+"=")
			break
		}
	}

	// We should append to the existing agent subsystem if it exists.
	agentSubsystem := "envbox"
	if existingSubsystem != "" {
		split := strings.Split(existingSubsystem, ",")
		split = append(split, "envbox")

		tidy := make([]string, 0, len(split))
		seen := make(map[string]struct{})
		for _, s := range split {
			s := strings.TrimSpace(s)
			if _, ok := seen[s]; s == "" || ok {
				continue
			}
			seen[s] = struct{}{}
			tidy = append(tidy, s)
		}

		sort.Strings(tidy)
		agentSubsystem = strings.Join(tidy, ",")
	}

	return []string{
		fmt.Sprintf("%s=%s", EnvAgentToken, agentToken),
		fmt.Sprintf("%s=%s", "CODER_AGENT_SUBSYSTEM", agentSubsystem),
	}
}

// defaultMounts are bind mounts that are always provided to the inner
// container.
func defaultMounts() []xunix.Mount {
	return []xunix.Mount{
		{
			Source:     "/var/lib/coder/docker",
			Mountpoint: "/var/lib/docker",
		},
		{
			Source:     "/var/lib/coder/containers",
			Mountpoint: "/var/lib/containers",
		},
	}
}

// ensureDevices ensures that the devices are created and returns the resulting mappings. It also
// shifts the id of the owner of the devices to the user namespace offset.
func ensureDevices(ctx context.Context, fs xunix.FS, log slog.Logger, blog buildlog.Logger, tun, fuse bool) ([]container.DeviceMapping, error) {
	devices := make([]container.DeviceMapping, 0, 2)
	if tun {
		log.Debug(ctx, "creating TUN device", slog.F("path", OuterTUNPath))
		blog.Info("Creating TUN device")
		dev, err := xunix.CreateTUNDevice(ctx, OuterTUNPath)
		if err != nil {
			return nil, xerrors.Errorf("create tun device: %w", err)
		}

		devices = append(devices, container.DeviceMapping{
			PathOnHost:        dev.Path,
			PathInContainer:   InnerTUNPath,
			CgroupPermissions: "rwm",
		})
	}

	if fuse {
		log.Debug(ctx, "creating FUSE device", slog.F("path", OuterFUSEPath))
		blog.Info("Creating FUSE device")
		dev, err := xunix.CreateFuseDevice(ctx, OuterFUSEPath)
		if err != nil {
			return nil, xerrors.Errorf("create fuse device: %w", err)
		}

		devices = append(devices, container.DeviceMapping{
			PathOnHost:        dev.Path,
			PathInContainer:   InnerFUSEPath,
			CgroupPermissions: "rwm",
		})
	}

	log.Debug(ctx, "using devices", slog.F("devices", devices))

	for _, device := range devices {
		log.Debug(ctx, "chowning device",
			slog.F("device", device.PathOnHost),
			slog.F("uid", UserNamespaceOffset),
			slog.F("gid", UserNamespaceOffset),
		)
		err := fs.Chown(device.PathOnHost, UserNamespaceOffset, UserNamespaceOffset)
		if err != nil {
			return nil, xerrors.Errorf("chown device %q: %w", device.PathOnHost, err)
		}
	}

	return devices, nil
}

func idShiftMounts(ctx context.Context, log slog.Logger, fs xunix.FS, mounts []xunix.Mount, uid, gid int) error {
	for _, m := range mounts {
		// Don't modify anything private to envbox.
		if isPrivateMount(m) {
			continue
		}

		log.Debug(ctx, "chmod'ing directory",
			slog.F("path", m.Source),
			slog.F("mode", "02755"),
		)

		// If a mount is read-only we have to remount it rw so that we
		// can id shift it correctly. We'll still mount it read-only into
		// the inner container.
		if m.ReadOnly {
			mounter := xunix.Mounter(ctx)
			err := mounter.Mount("", m.Source, "", []string{"remount,rw"})
			if err != nil {
				return xerrors.Errorf("remount: %w", err)
			}
		}

		err := fs.Chmod(m.Source, 0o2755)
		if err != nil {
			return xerrors.Errorf("chmod mountpoint %q: %w", m.Source, err)
		}

		var (
			shiftedUID = shiftedID(0)
			shiftedGID = shiftedID(0)
		)

		if isHomeDir(m.Source) {
			// We want to ensure that the inner directory is ID shifted to
			// the namespaced UID of the user in the inner container otherwise
			// they won't be able to write files.
			shiftedUID = shiftedID(int(uid))
			shiftedGID = shiftedID(int(gid))
		}

		log.Debug(ctx, "chowning mount",
			slog.F("source", m.Source),
			slog.F("target", m.Mountpoint),
			slog.F("uid", shiftedUID),
			slog.F("gid", shiftedGID),
		)

		// Any non-home directory we assume should be owned by id-shifted root
		// user.
		err = fs.Chown(m.Source, shiftedUID, shiftedGID)
		if err != nil {
			return xerrors.Errorf("chown mountpoint %q: %w", m.Source, err)
		}
	}

	return nil
}

func isHomeDir(fpath string) bool {
	if fpath == "/root" {
		return true
	}

	dir, _ := path.Split(fpath)
	return dir == "/home/"
}

// isPrivateMount returns true if the provided mount points to a mount
// private to the envbox container itself.
func isPrivateMount(m xunix.Mount) bool {
	_, ok := envboxPrivateMounts[m.Mountpoint]
	return ok
}

var envboxPrivateMounts = map[string]struct{}{
	"/var/lib/containers": {},
	"/var/lib/docker":     {},
	"/var/lib/sysbox":     {},
	"/lib/modules":        {},
	"/usr/src":            {},
	// /var/lib/coder is not technically a mount
	// private to envbox but it is specially handled
	// by sysbox so it does not require any effort
	// on our part.
	"/var/lib/coder": {},
}

// shiftedID returns the ID but shifted to the user namespace offset we
// use for the inner container.
func shiftedID(id int) int {
	return id + UserNamespaceOffset
}
