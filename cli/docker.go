package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/spf13/cobra"
	"golang.org/x/exp/slices"
	"golang.org/x/xerrors"

	"cdr.dev/slog"
	"cdr.dev/slog/sloggers/slogjson"
	"github.com/coder/envbox/background"
	"github.com/coder/envbox/buildlog"
	"github.com/coder/envbox/cli/cliflag"
	"github.com/coder/envbox/dockerutil"
	"github.com/coder/envbox/slogkubeterminate"
	"github.com/coder/envbox/sysboxutil"
	"github.com/coder/envbox/xhttp"
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

	InnerContainerName = "workspace_cvm"

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
	EnvInnerImage    = "CODER_INNER_IMAGE"
	EnvInnerUsername = "CODER_INNER_USERNAME"
	EnvInnerEnvs     = "CODER_INNER_ENVS"
	EnvInnerWorkDir  = "CODER_INNER_WORK_DIR"
	EnvInnerHostname = "CODER_INNER_HOSTNAME"
	EnvAddTun        = "CODER_ADD_TUN"
	EnvAddFuse       = "CODER_ADD_FUSE"
	EnvBridgeCIDR    = "CODER_DOCKER_BRIDGE_CIDR"
	//nolint
	EnvAgentToken           = "CODER_AGENT_TOKEN"
	EnvAgentURL             = "CODER_AGENT_URL"
	EnvBootstrap            = "CODER_BOOTSTRAP_SCRIPT"
	EnvMounts               = "CODER_MOUNTS"
	EnvCPUs                 = "CODER_CPUS"
	EnvMemory               = "CODER_MEMORY"
	EnvAddGPU               = "CODER_ADD_GPU"
	EnvUsrLibDir            = "CODER_USR_LIB_DIR"
	EnvInnerUsrLibDir       = "CODER_INNER_USR_LIB_DIR"
	EnvDockerConfig         = "CODER_DOCKER_CONFIG"
	EnvDebug                = "CODER_DEBUG"
	EnvDisableIDMappedMount = "CODER_DISABLE_IDMAPPED_MOUNT"
	EnvExtraCertsPath       = "CODER_EXTRA_CERTS_PATH"
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

type flags struct {
	innerImage    string
	innerUsername string
	agentToken    string

	// Optional flags.
	innerEnvs            string
	innerWorkDir         string
	innerHostname        string
	imagePullSecret      string
	coderURL             string
	addTUN               bool
	addFUSE              bool
	addGPU               bool
	dockerdBridgeCIDR    string
	boostrapScript       string
	containerMounts      string
	hostUsrLibDir        string
	innerUsrLibDir       string
	dockerConfig         string
	cpus                 int
	memory               int
	disableIDMappedMount bool
	extraCertsPath       string

	// Test flags.
	noStartupLogs bool
	debug         bool
	ethlink       string
}

func dockerCmd() *cobra.Command {
	var flags flags

	cmd := &cobra.Command{
		Use:   "docker",
		Short: "Create a docker-based CVM",
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			var (
				ctx, cancel                 = context.WithCancel(cmd.Context()) //nolint
				log                         = slog.Make(slogjson.Sink(cmd.ErrOrStderr()), slogkubeterminate.Make()).Leveled(slog.LevelDebug)
				blog        buildlog.Logger = buildlog.JSONLogger{Encoder: json.NewEncoder(os.Stderr)}
			)

			// We technically leak a context here, but it's impact is negligible.
			signalCtx, signalCancel := context.WithCancel(cmd.Context())
			sigs := make(chan os.Signal, 1)
			signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT, syscall.SIGWINCH)

			// Spawn a goroutine to wait for a signal.
			go func() {
				defer signalCancel()
				log.Info(ctx, "waiting for signal")
				<-sigs
				log.Info(ctx, "got signal, canceling context")
			}()

			cmd.SetContext(ctx)

			if flags.noStartupLogs {
				log = slog.Make(slogjson.Sink(io.Discard))
				blog = buildlog.NopLogger{}
			}

			httpClient, err := xhttp.Client(log, flags.extraCertsPath)
			if err != nil {
				return xerrors.Errorf("http client: %w", err)
			}

			if !flags.noStartupLogs && flags.agentToken != "" && flags.coderURL != "" {
				coderURL, err := url.Parse(flags.coderURL)
				if err != nil {
					return xerrors.Errorf("parse coder URL %q: %w", flags.coderURL, err)
				}

				agent, err := buildlog.OpenCoderClient(ctx, log, coderURL, httpClient, flags.agentToken)
				if err != nil {
					// Don't fail workspace startup on
					// an inability to push build logs.
					log.Error(ctx, "failed to instantiate coder build log client, no logs will be pushed", slog.Error(err))
				} else {
					blog = buildlog.MultiLogger(
						buildlog.OpenCoderLogger(ctx, agent, log),
						blog,
					)
				}
			}
			defer blog.Close()

			defer func(err *error) {
				if *err != nil {
					blog.Errorf("Failed to run envbox: %v", *err)
				}
			}(&err)

			sysboxArgs := []string{}
			if flags.disableIDMappedMount {
				sysboxArgs = append(sysboxArgs, "--disable-idmapped-mount")
			}

			go func() {
				select {
				// Start sysbox-mgr and sysbox-fs in order to run
				// sysbox containers.
				case err := <-background.New(ctx, log, "sysbox-mgr", sysboxArgs...).Run():
					blog.Info(sysboxErrMsg)
					//nolint
					log.Critical(ctx, "sysbox-mgr exited", slog.Error(err))
					panic(err)
				case err := <-background.New(ctx, log, "sysbox-fs").Run():
					blog.Info(sysboxErrMsg)
					//nolint
					log.Critical(ctx, "sysbox-fs exited", slog.Error(err))
					panic(err)
				}
			}()

			cidr := dockerutil.DefaultBridgeCIDR
			if flags.dockerdBridgeCIDR != "" {
				cidr = flags.dockerdBridgeCIDR
				log.Debug(ctx, "using custom docker bridge CIDR", slog.F("cidr", cidr))
			}

			dargs, err := dockerdArgs(flags.ethlink, cidr, false)
			if err != nil {
				return xerrors.Errorf("dockerd args: %w", err)
			}

			log.Debug(ctx, "starting dockerd", slog.F("args", args))

			blog.Info("Waiting for sysbox processes to startup...")
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

			client, err := dockerutil.ExtractClient(ctx)
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
					args, err = dockerdArgs(flags.ethlink, cidr, true)
					if err != nil {
						blog.Info("Failed to create Container-based Virtual Machine: " + err.Error())
						//nolint
						log.Fatal(ctx, "dockerd exited, failed getting args for restart", slog.Error(err))
					}

					err = dockerd.Restart(ctx, "dockerd", args...)
					if err != nil {
						blog.Info("Failed to create Container-based Virtual Machine: " + err.Error())
						//nolint
						log.Fatal(ctx, "restart dockerd", slog.Error(err))
					}

					err = <-dockerd.Wait()
				}

				// It's possible lower down in the call stack to restart
				// the docker daemon if we run out of disk while starting the
				// container.
				if err != nil && !xerrors.Is(err, background.ErrUserKilled) {
					blog.Info("Failed to create Container-based Virtual Machine: " + err.Error())
					//nolint
					log.Fatal(ctx, "dockerd exited", slog.Error(err))
				}
			}()

			log.Debug(ctx, "waiting for dockerd")

			// We wait for the daemon after spawning the goroutine in case
			// startup causes the daemon to encounter encounter a 'no space left
			// on device' error.
			blog.Info("Waiting for dockerd to startup...")
			err = dockerutil.WaitForDaemon(ctx, client)
			if err != nil {
				return xerrors.Errorf("wait for dockerd: %w", err)
			}

			if flags.extraCertsPath != "" {
				// Parse the registry from the inner image
				registry, err := name.ParseReference(flags.innerImage)
				if err != nil {
					return xerrors.Errorf("invalid image: %w", err)
				}
				registryName := registry.Context().RegistryStr()

				// Write certificates for the registry
				err = dockerutil.WriteCertsForRegistry(ctx, registryName, flags.extraCertsPath)
				if err != nil {
					return xerrors.Errorf("write certs for registry: %w", err)
				}

				blog.Infof("Successfully copied certificates from %q to %q", flags.extraCertsPath, filepath.Join("/etc/docker/certs.d", registryName))
				log.Debug(ctx, "wrote certificates for registry", slog.F("registry", registryName),
					slog.F("extra_certs_path", flags.extraCertsPath),
				)
			}

			bootstrapExecID, err := runDockerCVM(ctx, log, client, blog, flags)
			if err != nil {
				// It's possible we failed because we ran out of disk while
				// pulling the image. We should restart the daemon and use
				// the vfs storage driver to try to get the container up so that
				// a user can access their workspace and try to delete whatever
				// is causing their disk to fill up.
				if xunix.IsNoSpaceErr(err) {
					blog.Info("Insufficient space to start inner container. Restarting dockerd using the vfs driver. Your performance will be degraded. Clean up your home volume and then restart the workspace to improve performance.")
					log.Debug(ctx, "encountered 'no space left on device' error while starting workspace", slog.Error(err))
					args, err := dockerdArgs(flags.ethlink, cidr, true)
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
						blog.Errorf("restarted dockerd exited: %v", err)
						//nolint
						log.Fatal(ctx, "restarted dockerd exited", slog.Error(err))
					}()

					log.Debug(ctx, "reattempting container creation")
					bootstrapExecID, err = runDockerCVM(ctx, log, client, blog, flags)
				}
				if err != nil {
					blog.Errorf("Failed to run envbox: %v", err)
					return xerrors.Errorf("run: %w", err)
				}
			}

			go func() {
				defer cancel()

				<-signalCtx.Done()
				log.Debug(ctx, "ctx canceled, forwarding signal to inner container")

				time.Sleep(time.Second * 10)
				if bootstrapExecID == "" {
					log.Debug(ctx, "no bootstrap exec id, skipping")
					return
				}

				ctx, cancel := context.WithTimeout(context.Background(), time.Second*90)
				defer cancel()

				bootstrapPID, err := dockerutil.GetExecPID(ctx, client, bootstrapExecID)
				if err != nil {
					log.Error(ctx, "get exec pid", slog.Error(err))
				}

				log.Debug(ctx, "killing container", slog.F("bootstrap_pid", bootstrapPID))

				// The PID returned is the PID _outside_ the container...
				out, err := exec.Command("kill", "-TERM", strconv.Itoa(bootstrapPID)).CombinedOutput()
				if err != nil {
					log.Error(ctx, "kill bootstrap process", slog.Error(err), slog.F("output", string(out)))
					return
				}

				log.Debug(ctx, "sent kill signal waiting for process to exit")
				err = dockerutil.WaitForExit(ctx, client, bootstrapExecID)
				if err != nil {
					log.Error(ctx, "wait for exit", slog.Error(err))
					return
				}

				log.Debug(ctx, "bootstrap process successfully exited")
			}()

			return nil
		},
	}

	// Required flags.
	cliflag.StringVarP(cmd.Flags(), &flags.innerImage, "image", "", EnvInnerImage, "", "The image for the inner container. Required.")
	cliflag.StringVarP(cmd.Flags(), &flags.innerUsername, "username", "", EnvInnerUsername, "", "The username to use for the inner container. Required.")
	cliflag.StringVarP(cmd.Flags(), &flags.agentToken, "agent-token", "", EnvAgentToken, "", "The token to be used by the workspace agent to estabish a connection with the control plane. Required.")
	cliflag.StringVarP(cmd.Flags(), &flags.coderURL, "coder-url", "", EnvAgentURL, "", "The URL of the Coder deployement.")

	// Optional flags.
	cliflag.StringVarP(cmd.Flags(), &flags.innerEnvs, "envs", "", EnvInnerEnvs, "", "Comma separated list of envs to add to the inner container.")
	cliflag.StringVarP(cmd.Flags(), &flags.innerWorkDir, "work-dir", "", EnvInnerWorkDir, "", "The working directory of the inner container.")
	cliflag.StringVarP(cmd.Flags(), &flags.innerHostname, "hostname", "", EnvInnerHostname, "", "The hostname to use for the inner container.")
	cliflag.StringVarP(cmd.Flags(), &flags.imagePullSecret, "image-secret", "", EnvBoxPullImageSecretEnvVar, "", fmt.Sprintf("The secret to use to pull the image. It is highly encouraged to provide this via the %s environment variable.", EnvBoxPullImageSecretEnvVar))
	cliflag.StringVarP(cmd.Flags(), &flags.dockerdBridgeCIDR, "bridge-cidr", "", EnvBridgeCIDR, "", "The CIDR to use for the docker bridge.")
	cliflag.StringVarP(cmd.Flags(), &flags.boostrapScript, "boostrap-script", "", EnvBootstrap, "", "The script to use to bootstrap the container. This should typically install and start the agent.")
	cliflag.StringVarP(cmd.Flags(), &flags.containerMounts, "mounts", "", EnvMounts, "", "Comma separated list of mounts in the form of '<source>:<target>[:options]' (e.g. /var/lib/docker:/var/lib/docker:ro,/usr/src:/usr/src).")
	cliflag.StringVarP(cmd.Flags(), &flags.hostUsrLibDir, "usr-lib-dir", "", EnvUsrLibDir, "", "The host /usr/lib mountpoint. Used to detect GPU drivers to mount into inner container.")
	cliflag.StringVarP(cmd.Flags(), &flags.innerUsrLibDir, "inner-usr-lib-dir", "", EnvInnerUsrLibDir, "", "The inner /usr/lib mountpoint. This is automatically detected based on /etc/os-release in the inner image, but may optionally be overridden.")
	cliflag.StringVarP(cmd.Flags(), &flags.dockerConfig, "docker-config", "", EnvDockerConfig, "/root/.docker/config.json", "The path to the docker config to consult when pulling an image.")
	cliflag.BoolVarP(cmd.Flags(), &flags.addTUN, "add-tun", "", EnvAddTun, false, "Add a TUN device to the inner container.")
	cliflag.BoolVarP(cmd.Flags(), &flags.addFUSE, "add-fuse", "", EnvAddFuse, false, "Add a FUSE device to the inner container.")
	cliflag.BoolVarP(cmd.Flags(), &flags.addGPU, "add-gpu", "", EnvAddGPU, false, "Add detected GPUs to the inner container.")
	cliflag.IntVarP(cmd.Flags(), &flags.cpus, "cpus", "", EnvCPUs, 0, "Number of CPUs to allocate inner container. e.g. 2")
	cliflag.IntVarP(cmd.Flags(), &flags.memory, "memory", "", EnvMemory, 0, "Max memory to allocate to the inner container in bytes.")
	cliflag.BoolVarP(cmd.Flags(), &flags.disableIDMappedMount, "disable-idmapped-mount", "", EnvDisableIDMappedMount, false, "Disable idmapped mounts in sysbox. Note that you may need an alternative (e.g. shiftfs).")
	cliflag.StringVarP(cmd.Flags(), &flags.extraCertsPath, "extra-certs-path", "", EnvExtraCertsPath, "", "The path to a directory or file containing extra CA certificates.")

	// Test flags.
	cliflag.BoolVarP(cmd.Flags(), &flags.noStartupLogs, "no-startup-log", "", "", false, "Do not log startup logs. Useful for testing.")
	cliflag.BoolVarP(cmd.Flags(), &flags.debug, "debug", "", EnvDebug, false, "Log additional output.")
	cliflag.StringVarP(cmd.Flags(), &flags.ethlink, "ethlink", "", "", defaultNetLink, "The ethernet link to query for the MTU that is passed to docerd. Used for tests.")

	return cmd
}

func runDockerCVM(ctx context.Context, log slog.Logger, client dockerutil.Client, blog buildlog.Logger, flags flags) (string, error) {
	fs := xunix.GetFS(ctx)
	err := xunix.SetOOMScore(ctx, "self", "-1000")
	if err != nil {
		return "", xerrors.Errorf("set oom score: %w", err)
	}
	ref, err := name.ParseReference(flags.innerImage)
	if err != nil {
		return "", xerrors.Errorf("parse ref: %w", err)
	}

	var dockerAuth dockerutil.AuthConfig
	if flags.imagePullSecret != "" {
		dockerAuth, err = dockerutil.AuthConfigFromString(flags.imagePullSecret, ref.Context().RegistryStr())
		if err != nil {
			return "", xerrors.Errorf("parse auth config: %w", err)
		}
	}

	log.Info(ctx, "checking for docker config file", slog.F("path", flags.dockerConfig))
	if _, err := fs.Stat(flags.dockerConfig); err == nil {
		log.Info(ctx, "detected file", slog.F("image", flags.innerImage))
		dockerAuth, err = dockerutil.AuthConfigFromPath(flags.dockerConfig, ref.Context().RegistryStr())
		if err != nil && !xerrors.Is(err, os.ErrNotExist) {
			return "", xerrors.Errorf("auth config from file: %w", err)
		}
	}

	envs := defaultContainerEnvs(ctx, flags.agentToken)

	innerEnvsTokens := strings.Split(flags.innerEnvs, ",")
	envs = append(envs, filterElements(xunix.Environ(ctx), innerEnvsTokens...)...)

	mounts := defaultMounts()
	// Add any user-specified mounts to our mounts list.
	extraMounts, err := parseMounts(flags.containerMounts)
	if err != nil {
		return "", xerrors.Errorf("read mounts: %w", err)
	}
	mounts = append(mounts, extraMounts...)

	log.Debug(ctx, "using mounts", slog.F("mounts", mounts))

	devices := make([]container.DeviceMapping, 0, 2)
	if flags.addTUN {
		log.Debug(ctx, "creating TUN device", slog.F("path", OuterTUNPath))
		blog.Info("Creating TUN device")
		dev, err := xunix.CreateTUNDevice(ctx, OuterTUNPath)
		if err != nil {
			return "", xerrors.Errorf("creat tun device: %w", err)
		}

		devices = append(devices, container.DeviceMapping{
			PathOnHost:        dev.Path,
			PathInContainer:   InnerTUNPath,
			CgroupPermissions: "rwm",
		})
	}

	if flags.addFUSE {
		log.Debug(ctx, "creating FUSE device", slog.F("path", OuterFUSEPath))
		blog.Info("Creating FUSE device")
		dev, err := xunix.CreateFuseDevice(ctx, OuterFUSEPath)
		if err != nil {
			return "", xerrors.Errorf("create fuse device: %w", err)
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
			return "", xerrors.Errorf("chown device %q: %w", device.PathOnHost, err)
		}
	}

	log.Debug(ctx, "pulling image", slog.F("image", flags.innerImage))

	err = dockerutil.PullImage(ctx, &dockerutil.PullImageConfig{
		Client:     client,
		Image:      flags.innerImage,
		Auth:       dockerAuth,
		ProgressFn: dockerutil.DefaultLogImagePullFn(blog),
	})
	if err != nil {
		return "", xerrors.Errorf("pull image: %w", err)
	}

	log.Debug(ctx, "remounting /sys")

	// After image pull we remount /sys so sysbox can have appropriate perms to create a container.
	err = xunix.MountFS(ctx, "/sys", "/sys", "", "remount", "rw")
	if err != nil {
		return "", xerrors.Errorf("remount /sys: %w", err)
	}

	if flags.addGPU {
		if flags.hostUsrLibDir == "" {
			return "", xerrors.Errorf("when using GPUs, %q must be specified", EnvUsrLibDir)
		}

		// Unmount GPU drivers in /proc as it causes issues when creating any
		// container in some cases (even the image metadata container).
		_, err = xunix.TryUnmountProcGPUDrivers(ctx, log)
		if err != nil {
			return "", xerrors.Errorf("unmount /proc GPU drivers: %w", err)
		}
	}

	log.Debug(ctx, "fetching image metadata",
		slog.F("image", flags.innerImage),
		slog.F("username", flags.innerUsername),
	)

	blog.Info("Getting image metadata...")
	// Get metadata about the image. We need to know things like the UID/GID
	// of the user so that we can chown directories to the namespaced UID inside
	// the inner container as well as whether we should be starting the container
	// with /sbin/init or something simple like 'sleep infinity'.
	imgMeta, err := dockerutil.GetImageMetadata(ctx, log, client, flags.innerImage, flags.innerUsername)
	if err != nil {
		return "", xerrors.Errorf("get image metadata: %w", err)
	}

	blog.Infof("Detected entrypoint user '%s:%s' with home directory %q", imgMeta.UID, imgMeta.UID, imgMeta.HomeDir)

	log.Debug(ctx, "fetched image metadata",
		slog.F("uid", imgMeta.UID),
		slog.F("gid", imgMeta.GID),
		slog.F("has_init", imgMeta.HasInit),
		slog.F("os_release", imgMeta.OsReleaseID),
		slog.F("home_dir", imgMeta.HomeDir),
	)

	uid, err := strconv.ParseInt(imgMeta.UID, 10, 32)
	if err != nil {
		return "", xerrors.Errorf("parse image uid: %w", err)
	}
	gid, err := strconv.ParseInt(imgMeta.GID, 10, 32)
	if err != nil {
		return "", xerrors.Errorf("parse image gid: %w", err)
	}

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
				return "", xerrors.Errorf("remount: %w", err)
			}
		}

		err := fs.Chmod(m.Source, 0o2755)
		if err != nil {
			return "", xerrors.Errorf("chmod mountpoint %q: %w", m.Source, err)
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
			return "", xerrors.Errorf("chown mountpoint %q: %w", m.Source, err)
		}
	}

	if flags.addGPU {
		devs, binds, err := xunix.GPUs(ctx, log, flags.hostUsrLibDir)
		if err != nil {
			return "", xerrors.Errorf("find gpus: %w", err)
		}

		for _, dev := range devs {
			devices = append(devices, container.DeviceMapping{
				PathOnHost:        dev.Path,
				PathInContainer:   dev.Path,
				CgroupPermissions: "rwm",
			})
		}

		innerUsrLibDir := imgMeta.UsrLibDir()
		if flags.innerUsrLibDir != "" {
			log.Info(ctx, "overriding auto-detected inner usr lib dir ",
				slog.F("before", innerUsrLibDir),
				slog.F("after", flags.innerUsrLibDir))
			innerUsrLibDir = flags.innerUsrLibDir
		}
		for _, bind := range binds {
			// If the bind has a path that points to the host-mounted /usr/lib
			// directory we need to remap it to /usr/lib inside the container.
			mountpoint := bind.Path
			if strings.HasPrefix(mountpoint, flags.hostUsrLibDir) {
				mountpoint = filepath.Join(
					// Note: we used to mount into /usr/lib, but this can change
					// based on the distro inside the container.
					innerUsrLibDir,
					strings.TrimPrefix(mountpoint, strings.TrimSuffix(flags.hostUsrLibDir, "/")),
				)
			}
			// Even though xunix.GPUs checks for duplicate mounts, we need to check
			// for duplicates again here after remapping the path.
			if slices.ContainsFunc(mounts, func(m xunix.Mount) bool {
				return m.Mountpoint == mountpoint
			}) {
				log.Debug(ctx, "skipping duplicate mount", slog.F("path", mountpoint))
				continue
			}
			mounts = append(mounts, xunix.Mount{
				Source:     bind.Path,
				Mountpoint: mountpoint,
				ReadOnly:   slices.Contains(bind.Opts, "ro"),
			})
		}
		envs = append(envs, xunix.GPUEnvs(ctx)...)
	}

	blog.Info("Creating workspace...")
	// If imgMeta.HasInit is true, we just use flags.boostrapScript as the entrypoint.
	// But if it's false, we need to run /sbin/init as the entrypoint.
	// We need to mount or run some exec command that injects a systemd service for starting
	// the coder agent.

	// We need to check that if PID1 is systemd (or /sbin/init) that systemd propagates SIGTERM
	// to service units. If it doesn't then this solution doesn't help us.

	// Create the inner container.
	containerID, err := dockerutil.CreateContainer(ctx, client, &dockerutil.ContainerConfig{
		Log:         log,
		Mounts:      mounts,
		Devices:     devices,
		Envs:        envs,
		Name:        InnerContainerName,
		Hostname:    flags.innerHostname,
		WorkingDir:  flags.innerWorkDir,
		HasInit:     imgMeta.HasInit,
		Image:       flags.innerImage,
		CPUs:        int64(flags.cpus),
		MemoryLimit: int64(flags.memory),
	})
	if err != nil {
		return "", xerrors.Errorf("create container: %w", err)
	}

	blog.Info("Pruning images to free up disk...")
	// Prune images to avoid taking up any unnecessary disk from the user.
	_, err = dockerutil.PruneImages(ctx, client)
	if err != nil {
		return "", xerrors.Errorf("prune images: %w", err)
	}

	// TODO fix iptables when istio detected.

	blog.Info("Starting up workspace...")
	err = client.ContainerStart(ctx, containerID, container.StartOptions{})
	if err != nil {
		return "", xerrors.Errorf("start container: %w", err)
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
		User:        imgMeta.UID,
		Cmd:         "mkdir",
		Args:        []string{"-p", bootDir},
	})
	if err != nil {
		return "", xerrors.Errorf("make bootstrap dir: %w", err)
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
	if flags.boostrapScript == "" {
		return "", nil
	}
	blog.Infof("Bootstrapping workspace...")

	bootstrapExec, err := client.ContainerExecCreate(ctx, containerID, container.ExecOptions{
		User:         imgMeta.UID,
		Cmd:          []string{"/bin/sh", "-s"},
		Env:          []string{fmt.Sprintf("BINARY_DIR=%s", bootDir)},
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		Detach:       true,
	})
	if err != nil {
		return "", xerrors.Errorf("create exec: %w", err)
	}

	resp, err := client.ContainerExecAttach(ctx, bootstrapExec.ID, container.ExecStartOptions{})
	if err != nil {
		return "", xerrors.Errorf("attach exec: %w", err)
	}

	_, err = io.Copy(resp.Conn, strings.NewReader(flags.boostrapScript))
	if err != nil {
		return "", xerrors.Errorf("copy stdin: %w", err)
	}
	err = resp.CloseWrite()
	if err != nil {
		return "", xerrors.Errorf("close write: %w", err)
	}

	go func() {
		defer resp.Close()
		rd := io.LimitReader(resp.Reader, 1<<10)
		_, err := io.Copy(blog, rd)
		if err != nil {
			log.Error(ctx, "copy bootstrap output", slog.Error(err))
		}
	}()

	// We can't just call ExecInspect because there's a race where the cmd
	// hasn't been assigned a PID yet.
	return bootstrapExec.ID, nil
}

//nolint:revive
func dockerdArgs(link, cidr string, isNoSpace bool) ([]string, error) {
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
		f = strings.TrimSpace(f)
		for _, s := range ss {
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

// parseMounts parses a list of mounts from containerMounts. The format should
// be "src:dst[:ro],src:dst[:ro]".
func parseMounts(containerMounts string) ([]xunix.Mount, error) {
	if containerMounts == "" {
		return nil, nil
	}

	mountsStr := strings.Split(containerMounts, ",")

	mounts := make([]xunix.Mount, 0, len(mountsStr))
	for _, mount := range mountsStr {
		tokens := strings.Split(mount, ":")
		if len(tokens) < 2 || len(tokens) > 3 {
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
func defaultContainerEnvs(ctx context.Context, agentToken string) []string {
	const agentSubsystemEnv = "CODER_AGENT_SUBSYSTEM"
	env := xunix.Environ(ctx)
	existingSubsystem := ""
	for _, e := range env {
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

// isPrivateMount returns true if the provided mount points to a mount
// private to the envbox container itself.
func isPrivateMount(m xunix.Mount) bool {
	_, ok := envboxPrivateMounts[m.Mountpoint]
	return ok
}

func isHomeDir(fpath string) bool {
	if fpath == "/root" {
		return true
	}

	dir, _ := path.Split(fpath)
	return dir == "/home/"
}

// shiftedID returns the ID but shifted to the user namespace offset we
// use for the inner container.
func shiftedID(id int) int {
	return id + UserNamespaceOffset
}
