package cli

import (
	"context"
	"fmt"
	"os"
	"path"
	"strconv"
	"strings"

	dockertypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/spf13/cobra"
	"golang.org/x/xerrors"

	"cdr.dev/slog"
	"github.com/coder/envbox/background"
	"github.com/coder/envbox/cli/cliflag"
	"github.com/coder/envbox/dockerutil"
	"github.com/coder/envbox/envboxlog"
	"github.com/coder/envbox/slogkubeterminate"
	"github.com/coder/envbox/sysboxutil"
	"github.com/coder/envbox/xunix"
)

const (
	// EnvBoxPullImageSecretEnvVar defines the environment variable at which the
	// pull image secret is mounted for envbox.
	// Suppresses warning: G101: Potential hardcoded credentials
	// EnvBoxContainerName is the name of the inner user container.
	EnvBoxPullImageSecretEnvVar = "CODER_IMAGE_PULL_SECRET" //nolint:gosec
	EnvBoxContainerName         = "CODER_CVM_CONTAINER_NAME"
)

const (
	defaultNetLink      = "eth0"
	defaultDockerBridge = "docker0"
	// From https://docs.aws.amazon.com/eks/latest/userguide/iam-roles-for-service-accounts-technical-overview.html
	awsWebIdentityTokenFilePath = "/var/run/secrets/eks.amazonaws.com/serviceaccount/token" //nolint
	sysboxErrMsg                = "Sysbox exited, possibly because of an unsupported kernel version. Please contact an infrastructure administrator and request a node kernel with seccomp API level >= 5."

	// noSpaceDataDir is the directory to use for the data directory
	// for dockerd when the default directory (/var/lib/docker pointing
	// to the user's pvc) is at capacity. This directory points to
	// ephemeral storage allocated by the node and should be more likely
	// to have capacity.
	noSpaceDataDir = "/var/lib/docker.bak"
	// noSpaceDockerDriver is the storage driver to use in cases where
	// the default data dir (residing in the user's PVC) is at capacity.
	// In such cases we must use the vfs storage driver because overlay2
	// does not work on top of overlay.
	noSpaceDockerDriver = "vfs"

	OuterFUSEPath = "/tmp/coder-fuse"
	InnerFUSEPath = "/dev/fuse"

	OuterTUNPath = "/tmp/coder-tun"
	InnerTUNPath = "/dev/net/tun"

	// Required for userns mapping.
	// This is the ID of the user we apply in `envbox/Dockerfile`.
	//
	// There should be caution changing this value.
	// Source directory permissions on the host are offset by this
	// value. For example, folder `/home/coder` inside the container
	// with UID/GID 1000 will be mapped to `UserNamespaceOffset` + 1000
	// on the host. Changing this value will result in improper mappings
	// on existing containers.
	UserNamespaceOffset = 100000
	devDir              = "/dev"
)

var (
	EnvInnerImage         = "CODER_INNER_IMAGE"
	EnvInnerUsername      = "CODER_INNER_USERNAME"
	EnvInnerContainerName = "CODER_INNER_CONTAINER_NAME"
	EnvInnerEnvs          = "CODER_INNER_ENVS"
	EnvInnerWorkDir       = "CODER_INNER_WORK_DIR"
	EnvInnerHostname      = "CODER_INNER_HOSTNAME"
	EnvAddTun             = "CODER_ADD_TUN"
	EnvAddFuse            = "CODER_ADD_FUSE"
	EnvBridgeCIDR         = "CODER_ENVBOX_BRIDGE_CIDR"
	EnvAgentToken         = "CODER_AGENT_TOKEN"
	EnvBootstrap          = "CODER_BOOTSTRAP_SCRIPT"
	EnvMounts             = "CODER_MOUNTS"
)

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

func dockerCmd() *cobra.Command {
	var (
		// Required flags.
		innerImage         string
		innerUsername      string
		innerContainerName string
		agentToken         string

		// Optional flags.
		innerEnvs         string
		innerWorkDir      string
		innerHostname     string
		addTUN            bool
		addFUSE           bool
		dockerdBridgeCIDR string
		boostrapScript    string
		ethlink           string
		containerMounts   string
	)

	cmd := &cobra.Command{
		Use:   "docker",
		Short: "Create a docker-based CVM",
		RunE: func(cmd *cobra.Command, args []string) error {
			var (
				ctx = cmd.Context()
				log = slog.Make(envboxlog.NewSink(os.Stderr), slogkubeterminate.Make()).Leveled(slog.LevelDebug)
			)

			go func() {
				select {
				// Start sysbox-mgr and sysbox-fs in order to run
				// sysbox containers.
				case err := <-background.New(ctx, log, "sysbox-mgr").Run():
					_ = envboxlog.YieldAndFailBuild(sysboxErrMsg)
					log.Fatal(ctx, "sysbox-mgr exited", slog.Error(err))
				case err := <-background.New(ctx, log, "sysbox-fs").Run():
					_ = envboxlog.YieldAndFailBuild(sysboxErrMsg)
					log.Fatal(ctx, "sysbox-fs exited", slog.Error(err))
				}
			}()

			cidr := dockerutil.DefaultBridgeCIDR
			if dockerdBridgeCIDR != "" {
				cidr = dockerdBridgeCIDR
				log.Debug(ctx, "using custom docker bridge CIDR", slog.F("cidr", cidr))
			}

			dargs, err := dockerdArgs(ctx, log, ethlink, cidr, false)
			if err != nil {
				return xerrors.Errorf("dockerd args: %w", err)
			}

			log.Debug(ctx, "starting dockerd", slog.F("args", args))

			dockerd := background.New(ctx, log, "dockerd", dargs...)
			err = dockerd.Start()
			if err != nil {
				return xerrors.Errorf("start dockerd: %w", err)
			}

			log.Debug(ctx, "waiting for manager")

			err = sysboxutil.WaitForManager(ctx)
			if err != nil {
				return xerrors.Errorf("wait for sysbox-mgr: %w", err)
			}

			client, err := dockerutil.Client(ctx)
			if err != nil {
				return xerrors.Errorf("new docker client: %w", err)
			}

			go func() {
				err := <-dockerd.Wait()
				// It's possible the for the docker daemon to run out of disk
				// while trying to startup, in such cases we should restart
				// it and point it to an ephemeral directory. Since this
				// directory is going to be on top of an overlayfs filesystem
				// we have to use the vfs storage driver.
				if xunix.IsNoSpaceErr(err) {
					args, err = dockerdArgs(ctx, log, ethlink, cidr, true)
					if err != nil {
						_ = envboxlog.YieldAndFailBuild("Failed to create Container-based Virtual Machine: " + err.Error())
						log.Fatal(ctx, "dockerd exited, failed getting args for restart", slog.Error(err))
					}

					err = dockerd.Restart(ctx, "dockerd", args...)
					if err != nil {
						_ = envboxlog.YieldAndFailBuild("Failed to create Container-based Virtual Machine: " + err.Error())
						log.Fatal(ctx, "restart dockerd", slog.Error(err))
					}

					err = <-dockerd.Wait()
				}

				// It's possible lower down in the call stack to restart
				// the docker daemon if we run out of disk while starting the
				// container.
				if err != nil && !xerrors.Is(err, background.ErrUserKilled) {
					_ = envboxlog.YieldAndFailBuild("Failed to create Container-based Virtual Machine: " + err.Error())
					log.Fatal(ctx, "dockerd exited", slog.Error(err))
				}
			}()

			log.Debug(ctx, "waiting for dockerd")

			// We wait for the daemon after spawning the goroutine in case
			// startup causes the daemon to encounter encounter a 'no space left
			// on device' error.
			err = dockerutil.WaitForDaemon(ctx, client)
			if err != nil {
				return xerrors.Errorf("wait for dockerd: %w", err)
			}

			flags := flags{
				// Required flags.
				innerImage:         innerImage,
				innerUsername:      innerUsername,
				innerContainerName: innerContainerName,
				agentToken:         agentToken,

				// Optional flags.
				innerEnvs:         innerEnvs,
				innerWorkDir:      innerWorkDir,
				innerHostname:     innerHostname,
				addTUN:            addTUN,
				addFUSE:           addFUSE,
				dockerdBridgeCIDR: dockerdBridgeCIDR,
				boostrapScript:    boostrapScript,
				ethlink:           ethlink,
				containerMounts:   containerMounts,
			}

			err = runDockerCVM(ctx, log, client, flags)
			if err != nil {
				// It's possible we failed because we ran out of disk while
				// pulling the image. We should restart the daemon and use
				// the vfs storage driver to try to get the container up so that
				// a user can access their workspace and try to delete whatever
				// is causing their disk to fill up.
				if xunix.IsNoSpaceErr(err) {
					log.Debug(ctx, "encountered 'no space left on device' error while starting workspace", slog.Error(err))
					args, err := dockerdArgs(ctx, log, ethlink, cidr, true)
					if err != nil {
						return xerrors.Errorf("dockerd args for restart: %w", err)
					}

					log.Debug(ctx, "restarting dockerd", slog.F("args", args))

					err = dockerd.Restart(ctx, "dockerd", args...)
					if err != nil {
						return xerrors.Errorf("restart dockerd: %w", err)
					}
					go func() {
						err = <-dockerd.Wait()
						log.Fatal(ctx, "restarted dockerd exited", slog.Error(err))
					}()

					log.Debug(ctx, "reattempting container creation")
					err = runDockerCVM(ctx, log, client, flags)
				}
				if err != nil {
					return xerrors.Errorf("run: %w", err)
				}
			}

			// Allow the remainder of the buildlog to continue
			_ = envboxlog.YieldBuildLog()

			return nil
		},
	}

	// Required flags.
	cliflag.StringVarP(cmd.Flags(), &innerImage, "image", "", EnvInnerImage, "", "The image for the inner container. Required.")
	cliflag.StringVarP(cmd.Flags(), &innerUsername, "username", "", EnvInnerUsername, "", "The username to use for the inner container. Required.")
	cliflag.StringVarP(cmd.Flags(), &innerContainerName, "container-name", "", EnvInnerContainerName, "", "The name of the inner container. Required.")
	cliflag.StringVarP(cmd.Flags(), &agentToken, "agent-token", "", EnvAgentToken, "", "The token to be used by the workspace agent to estabish a connection with the control plane. Required.")

	// Optional flags.
	cliflag.StringVarP(cmd.Flags(), &innerEnvs, "envs", "", EnvInnerEnvs, "", "Comma separated list of envs to add to the inner container.")
	cliflag.StringVarP(cmd.Flags(), &innerWorkDir, "work-dir", "", EnvInnerWorkDir, "", "The working directory of the inner container.")
	cliflag.StringVarP(cmd.Flags(), &innerHostname, "hostname", "", EnvInnerHostname, "", "The hostname to use for the inner container.")
	cliflag.StringVarP(cmd.Flags(), &dockerdBridgeCIDR, "bridge-cidr", "", EnvBridgeCIDR, "", "The CIDR to use for the docker bridge.")
	cliflag.StringVarP(cmd.Flags(), &boostrapScript, "boostrap-script", "", EnvBootstrap, "", "The script to use to bootstrap the container. This should typically install and start the agent.")
	cliflag.StringVarP(cmd.Flags(), &containerMounts, "mounts", "", EnvMounts, "", "Comma separated list of mounts in the form of '<source>:<target>[:options]' (e.g. /var/lib/docker:/var/lib/docker:ro,/usr/src:/usr/src).")
	cliflag.StringVarP(cmd.Flags(), &ethlink, "ethlink", "", "", defaultNetLink, "The ethernet link to query for the MTU that is passed to docerd. Used for tests.")
	cliflag.BoolVarP(cmd.Flags(), &addTUN, "add-tun", "", EnvAddTun, false, "Add a TUN device to the inner container.")
	cliflag.BoolVarP(cmd.Flags(), &addFUSE, "add-fuse", "", EnvAddFuse, false, "Add a FUSE device to the inner container.")

	return cmd
}

type flags struct {
	innerImage         string
	innerUsername      string
	innerContainerName string
	agentToken         string

	// Optional flags.
	innerEnvs         string
	innerWorkDir      string
	innerHostname     string
	addTUN            bool
	addFUSE           bool
	dockerdBridgeCIDR string
	boostrapScript    string
	ethlink           string
	containerMounts   string
}

func runDockerCVM(ctx context.Context, log slog.Logger, client dockerutil.DockerClient, flags flags) error {
	fs := xunix.GetFS(ctx)

	// Set our OOM score to something really unfavorable to avoid getting killed
	// in memory-scarce scenarios.
	err := xunix.SetOOMScore(ctx, "self", "-1000")
	if err != nil {
		return xerrors.Errorf("set oom score: %w", err)
	}

	envs := defaultContainerEnvs(flags.agentToken)

	innerEnvsTokens := strings.Split(flags.innerEnvs, ",")
	envs = append(envs, filterElements(xunix.Environ(ctx), innerEnvsTokens...)...)

	mounts := defaultMounts()
	// Add any user-specified mounts to our mounts list.
	extraMounts, err := parseMounts(ctx, flags.containerMounts)
	if err != nil {
		return xerrors.Errorf("read mounts: %w", err)
	}
	mounts = append(mounts, extraMounts...)

	log.Debug(ctx, "using mounts", slog.F("mounts", mounts))

	devices := make([]container.DeviceMapping, 0, 2)
	if flags.addTUN {
		log.Debug(ctx, "creating TUN device", slog.F("path", OuterTUNPath))
		dev, err := xunix.CreateTUNDevice(ctx, OuterTUNPath)
		if err != nil {
			return xerrors.Errorf("creat tun device: %w", err)
		}

		devices = append(devices, container.DeviceMapping{
			PathOnHost:        dev.Path,
			PathInContainer:   InnerTUNPath,
			CgroupPermissions: "rwm",
		})
	}

	if flags.addFUSE {
		log.Debug(ctx, "creating FUSE device", slog.F("path", OuterFUSEPath))
		dev, err := xunix.CreateFuseDevice(ctx, OuterFUSEPath)
		if err != nil {
			return xerrors.Errorf("create fuse device: %w", err)
		}

		devices = append(devices, container.DeviceMapping{
			PathOnHost:        dev.Path,
			PathInContainer:   InnerFUSEPath,
			CgroupPermissions: "rwm",
		})
	}

	log.Debug(ctx, "using devices", slog.F("devices", devices))

	// ID shift the devices so that they reflect the root user
	// inside the container.
	for _, device := range devices {
		log.Debug(ctx, "chowning device",
			slog.F("device", device.PathOnHost),
			slog.F("uid", UserNamespaceOffset),
			slog.F("gid", UserNamespaceOffset),
		)
		err = fs.Chown(device.PathOnHost, UserNamespaceOffset, UserNamespaceOffset)
		if err != nil {
			return xerrors.Errorf("chown device %q: %w", device.PathOnHost, err)
		}
	}

	log.Debug(ctx, "pulling image", slog.F("image", flags.innerImage))

	err = dockerutil.PullImage(ctx, &dockerutil.PullImageConfig{
		Client:     client,
		Image:      flags.innerImage,
		Auth:       dockerutil.DockerAuth{},                                // TODO
		ProgressFn: func(e dockerutil.ImagePullEvent) error { return nil }, // TODO
	})
	if err != nil {
		return xerrors.Errorf("pull image: %w", err)
	}

	log.Debug(ctx, "remounting /sys")

	// After image pull we remount /sys so sysbox can have appropriate perms to create a container.
	err = xunix.MountFS(ctx, "/sys", "/sys", "", "remount", "rw")
	if err != nil {
		return xerrors.Errorf("remount /sys: %w", err)
	}

	// TODO There's some bespoke logic related to aws service account keys
	// that we handle that we should probably port over.

	log.Debug(ctx, "fetching image metadata",
		slog.F("image", flags.innerImage),
		slog.F("username", flags.innerUsername),
	)

	// Get metadata about the image. We need to know things like the UID/GID
	// of the user so that we can chown directories to the namespaced UID inside
	// the inner container as well as whether we should be starting the container
	// with /sbin/init or something simple like 'sleep infinity'.
	imgMeta, err := dockerutil.GetImageMetadata(ctx, client, flags.innerImage, flags.innerUsername)
	if err != nil {
		return xerrors.Errorf("get image metadata: %w", err)
	}

	log.Debug(ctx, "fetched image metadata",
		slog.F("uid", imgMeta.UID),
		slog.F("gid", imgMeta.GID),
		slog.F("has_init", imgMeta.HasInit),
	)

	uid, err := strconv.ParseInt(imgMeta.UID, 10, 32)
	if err != nil {
		return xerrors.Errorf("parse image uid: %w", err)
	}
	gid, err := strconv.ParseInt(imgMeta.GID, 10, 32)
	if err != nil {
		return xerrors.Errorf("parse image gid: %w", err)
	}

	for _, m := range mounts {
		// Don't modify anything private to envbox.
		if isPrivateMount(m) {
			continue
		}

		log.Debug(ctx, "chmod'ing home directory",
			slog.F("path", m.Source),
			slog.F("mode", "02755"),
		)

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

	// TODO unmount gpus
	// Unmount GPU stuff in /proc as it causes issues when creating any
	// container in some cases (even the parseImageFS container). Ignore any
	// errors (but report to user).
	// if shouldHaveGPUs() {
	// 	err = unmountProblematicGPUDrivers(ctx, log)
	// 	if err != nil {
	// 		return xerrors.Errorf("unmount problematic GPU driver mounts: %w", err)
	// 	}
	// }

	// Create the inner container.
	containerID, err := dockerutil.CreateContainer(ctx, client, &dockerutil.ContainerConfig{
		Log:        log,
		Mounts:     mounts,
		Devices:    devices,
		Envs:       envs,
		Name:       "workspace_cvm",
		WorkingDir: flags.innerWorkDir,
		HasInit:    imgMeta.HasInit,
		Image:      flags.innerImage,
		// TODO set CPUQuota,MemoryLimit
	})
	if err != nil {
		return xerrors.Errorf("create container: %w", err)
	}

	// Prune images to avoid taking up any unnecessary disk from the user.
	_, err = dockerutil.PruneImages(ctx, client)
	if err != nil {
		return xerrors.Errorf("prune images: %w", err)
	}

	// TODO fix iptables when istio detected.

	err = client.ContainerStart(ctx, containerID, dockertypes.ContainerStartOptions{})
	if err != nil {
		if err != nil {
			return xerrors.Errorf("start container: %w", err)
		}
	}

	log.Debug(ctx, "bootstrapping container", slog.F("script", flags.boostrapScript))

	// Bootstrap the container if a script has been provided.
	err = dockerutil.BootstrapContainer(ctx, client, dockerutil.BootstrapConfig{
		ContainerID: containerID,
		User:        imgMeta.UID,
		Script:      flags.boostrapScript,
	})
	if err != nil {
		return xerrors.Errorf("boostrap container: %w", err)
	}

	cpuQuota, err := xunix.ReadCPUQuota(ctx)
	if err != nil {
		return xerrors.Errorf("read CPU quota: %w", err)
	}

	log.Debug(ctx, "setting CPU quota",
		slog.F("quota", cpuQuota.Quota),
		slog.F("period", cpuQuota.Period),
	)

	// We want the inner container to have the same limits as the outer container
	// so that processes inside the container know what they're working with.
	err = dockerutil.SetContainerCPUQuota(ctx, containerID, cpuQuota.Quota, cpuQuota.Period)
	if err != nil {
		return xerrors.Errorf("set inner container CPU quota: %w", err)
	}

	return nil
}

func dockerdArgs(ctx context.Context, log slog.Logger, link, cidr string, isNoSpace bool) ([]string, error) {
	// We need to adjust the MTU for the host otherwise packets will fail delivery.
	// 1500 is the standard, but certain deployments (like GKE) use custom MTU values.
	// See: https://www.atlantis-press.com/journals/ijndc/125936177/view#sec-s3.1

	mtu, err := xunix.NetlinkMTU(link)
	if err != nil {
		return nil, xerrors.Errorf("custom mtu: %w", err)
	}

	// We set the Docker Bridge IP explicitly here for a number of reasons:
	// 1) It sometimes picks the 172.17.x.x address which conflicts with that of the Docker daemon in the inner container.
	// 2) It defaults to a /16 network which is way more than we need for envbox.
	// 3) The default may conflict with existing internal network resources, and an operator may wish to override it.
	dockerBip, prefixLen := dockerutil.BridgeIPFromCIDR(cidr)

	args := []string{
		"--debug",
		"--log-level=debug",
		fmt.Sprintf("--mtu=%d", mtu),
		"--userns-remap=coder",
		"--storage-driver=overlay2",
		fmt.Sprintf("--bip=%s/%d", dockerBip, prefixLen),
	}

	if isNoSpace {
		args = append(args,
			fmt.Sprintf("--data-root=%s", noSpaceDataDir),
			fmt.Sprintf("--storage-driver=%s", noSpaceDockerDriver),
		)
	}

	return args, nil
}

// TODO This is bad code.
func filterElements(ss []string, filters ...string) []string {
	filtered := make([]string, 0, len(ss))
	for _, f := range filters {
		for _, s := range ss {
			filter := f
			if strings.HasSuffix(filter, "*") {
				filter = strings.TrimSuffix(filter, "*")
				if strings.HasPrefix(s, filter) {
					filtered = append(filtered, s)
				}
			} else if s == filter {
				filtered = append(filtered, s)
			}
		}
	}

	return filtered
}

// parseMounts parses a list of mounts from containerMounts. The format should
// be "src:dst[:ro],src:dst[:ro]".
func parseMounts(ctx context.Context, containerMounts string) ([]xunix.Mount, error) {
	if containerMounts == "" {
		return nil, nil
	}

	mountsStr := strings.Split(containerMounts, ",")

	mounts := make([]xunix.Mount, 0, len(mountsStr))
	for _, mount := range mountsStr {
		tokens := strings.Split(mount, ":")
		if len(tokens) < 2 {
			return nil, xerrors.Errorf("malformed mounts value %q", containerMounts)
		}
		m := xunix.Mount{
			Source:     tokens[0],
			Mountpoint: tokens[1],
		}
		if len(tokens) == 3 {
			m.ReadOnly = tokens[2] == "ro"
		}
		mounts = append(mounts, m)
	}

	return mounts, nil
}

// defaultContainerEnvs returns environment variables that should always
// be passed to the inner container.
func defaultContainerEnvs(agentToken string) []string {
	return []string{fmt.Sprintf("%s=%s", EnvAgentToken, agentToken)}
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

// isPrivateMount returns true if the provided mount points to a mount
// private to the envbox container itself.
func isPrivateMount(m xunix.Mount) bool {
	_, ok := envboxPrivateMounts[m.Mountpoint]
	return ok
}

func isHomeDir(filepath string) bool {
	if filepath == "/root" {
		return true
	}

	dir, _ := path.Split(filepath)
	return dir == "/home/"
}

// shiftedID returns the ID but shifted to the user namespace offset we
// use for the inner container.
func shiftedID(id int) int {
	return id + UserNamespaceOffset
}
