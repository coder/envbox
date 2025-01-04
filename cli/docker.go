package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/spf13/cobra"
	"golang.org/x/xerrors"

	"cdr.dev/slog"
	"cdr.dev/slog/sloggers/slogjson"
	"github.com/coder/envbox/background"
	"github.com/coder/envbox/buildlog"
	"github.com/coder/envbox/cli/cliflag"
	"github.com/coder/envbox/cvm"
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
				ctx = cmd.Context()
				log = slog.Make(
					slogjson.Sink(cmd.ErrOrStderr()),
					slogkubeterminate.Make(),
				).Leveled(slog.LevelDebug)
				cfg = cvm.Config{
					Username:   flags.innerUsername,
					AgentToken: flags.agentToken,
					OSEnvs:     os.Environ(),

					BuildLog: buildlog.JSONLogger{
						Encoder: json.NewEncoder(os.Stderr),
					},
					InnerEnvs:       strings.Split(flags.innerEnvs, ","),
					WorkDir:         flags.innerWorkDir,
					Hostname:        flags.innerHostname,
					ImagePullSecret: flags.imagePullSecret,
					CoderURL:        flags.coderURL,
					AddTUN:          flags.addTUN,
					AddFUSE:         flags.addFUSE,
					BoostrapScript:  flags.boostrapScript,
					DockerConfig:    flags.dockerConfig,
					CPUS:            flags.cpus,
					Memory:          flags.memory,
					GPUConfig: cvm.GPUConfig{
						HostUsrLibDir: flags.hostUsrLibDir,
					},
				}
			)

			cfg.Mounts, err = parseMounts(flags.containerMounts)
			if err != nil {
				return xerrors.Errorf("parse mounts: %w", err)
			}

			if flags.addGPU && flags.hostUsrLibDir == "" {
				return xerrors.Errorf("when using GPUs, %q must be specified", EnvUsrLibDir)
			}

			if flags.noStartupLogs {
				log = slog.Make(slogjson.Sink(io.Discard))
				cfg.BuildLog = buildlog.NopLogger{}
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
					cfg.BuildLog = buildlog.MultiLogger(
						buildlog.OpenCoderLogger(ctx, agent, log),
						cfg.BuildLog,
					)
				}
			}
			defer cfg.BuildLog.Close()

			defer func(err *error) {
				if *err != nil {
					cfg.BuildLog.Errorf("Failed to run envbox: %v", *err)
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
				case err := <-background.RunCh(ctx, log, "sysbox-mgr", sysboxArgs...):
					cfg.BuildLog.Info(sysboxErrMsg)
					//nolint
					log.Fatal(ctx, "sysbox-mgr exited", slog.Error(err))
				case err := <-background.RunCh(ctx, log, "sysbox-fs"):
					cfg.BuildLog.Info(sysboxErrMsg)
					//nolint
					log.Fatal(ctx, "sysbox-fs exited", slog.Error(err))
				}
			}()

			cidr := dockerutil.DefaultBridgeCIDR
			if flags.dockerdBridgeCIDR != "" {
				cidr = flags.dockerdBridgeCIDR
				log.Debug(ctx, "using custom docker bridge CIDR", slog.F("cidr", cidr))
			}

			cfg.BuildLog.Info("Waiting for sysbox processes to startup...")

			err = sysboxutil.WaitForManager(ctx)
			if err != nil {
				return xerrors.Errorf("wait for sysbox-mgr: %w", err)
			}

			client, err := dockerutil.ExtractClient(ctx)
			if err != nil {
				return xerrors.Errorf("new docker client: %w", err)
			}

			dockerd, err := dockerutil.StartDaemon(ctx, log, &dockerutil.DaemonOptions{
				Link:   flags.ethlink,
				CIDR:   cidr,
				Driver: "vfs",
			})
			if err != nil {
				return xerrors.Errorf("start dockerd: %w", err)
			}

			mustRestartDockerd := mustRestartDockerd(ctx, log, cfg.BuildLog, dockerd, &dockerutil.DaemonOptions{
				Link:   flags.ethlink,
				CIDR:   cidr,
				Driver: "vfs",
			})

			go func() {
				// It's possible the for the docker daemon to run out of disk
				// while trying to startup, in such cases we should restart
				// it and point it to an ephemeral directory. Since this
				// directory is going to be on top of an overlayfs filesystem
				// we have to use the vfs storage driver.
				if !xunix.IsNoSpaceErr(err) {
					cfg.BuildLog.Error("Failed to create Container-based Virtual Machine: " + err.Error())
					//nolint
					log.Fatal(ctx, "dockerd exited", slog.Error(err))
				}
				cfg.BuildLog.Info("Insufficient space to start inner container. Restarting dockerd using the vfs driver. Your performance will be degraded. Clean up your home volume and then restart the workspace to improve performance.")
				log.Debug(ctx, "encountered 'no space left on device' error while starting workspace", slog.Error(err))

				mustRestartDockerd()
			}()

			log.Debug(ctx, "waiting for dockerd")

			// We wait for the daemon after spawning the goroutine in case
			// startup causes the daemon to encounter encounter a 'no space left
			// on device' error.
			cfg.BuildLog.Info("Waiting for dockerd to startup...")
			err = dockerutil.WaitForDaemon(ctx, client)
			if err != nil {
				return xerrors.Errorf("wait for dockerd: %w", err)
			}

			tag, err := name.NewTag(flags.innerImage)
			if err != nil {
				return xerrors.Errorf("parse image: %w", err)
			}

			if flags.extraCertsPath != "" {
				registryName := tag.RegistryStr()
				// Write certificates for the registry
				err = dockerutil.WriteCertsForRegistry(ctx, registryName, flags.extraCertsPath)
				if err != nil {
					return xerrors.Errorf("write certs for registry: %w", err)
				}

				cfg.BuildLog.Infof("Successfully copied certificates from %q to %q", flags.extraCertsPath, filepath.Join("/etc/docker/certs.d", registryName))
				log.Debug(ctx, "wrote certificates for registry", slog.F("registry", registryName),
					slog.F("extra_certs_path", flags.extraCertsPath),
				)
			}

			// Set our OOM score to something really unfavorable to avoid getting killed
			// in memory-scarce scenarios.
			err = xunix.SetOOMScore(ctx, "self", "-1000")
			if err != nil {
				return xerrors.Errorf("set oom score: %w", err)
			}

			err = cvm.Run(ctx, log, xunix.NewLinuxOS(), client, cfg)
			if err != nil {
				// It's possible we failed because we ran out of disk while
				// pulling the image. We should restart the daemon and use
				// the vfs storage driver to try to get the container up so that
				// a user can access their workspace and try to delete whatever
				// is causing their disk to fill up.
				if xunix.IsNoSpaceErr(err) {
					cfg.BuildLog.Info("Insufficient space to start inner container. Restarting dockerd using the vfs driver. Your performance will be degraded. Clean up your home volume and then restart the workspace to improve performance.")
					log.Debug(ctx, "encountered 'no space left on device' error while starting workspace", slog.Error(err))

					mustRestartDockerd()

					log.Debug(ctx, "reattempting container creation")
					err = cvm.Run(ctx, log, xunix.NewLinuxOS(), client, cfg)
				}
			}
			if err != nil {
				cfg.BuildLog.Errorf("Failed to run envbox: %v", err)
				return xerrors.Errorf("run: %w", err)
			}

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

func mustRestartDockerd(ctx context.Context, log slog.Logger, blog buildlog.Logger, dockerd *background.Process, options *dockerutil.DaemonOptions) func() {
	return sync.OnceFunc(func() {
		err := dockerd.KillAndWait()
		if err != nil {
			log.Error(ctx, "failed to kill dockerd", slog.Error(err))
		}

		dockerd, err = dockerutil.StartDaemon(ctx, log, options)
		if err != nil {
			log.Fatal(ctx, "failed to start dockerd", slog.Error(err))
		}

		go func() {
			err := dockerd.Wait()
			blog.Errorf("restarted dockerd exited: %v", err)
			//nolint
			log.Fatal(ctx, "restarted dockerd exited", slog.Error(err))
		}()
	})
}
