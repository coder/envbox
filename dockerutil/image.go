package dockerutil

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	goruntime "runtime"
	"strings"
	"time"

	"cdr.dev/slog"
	dockertypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"golang.org/x/xerrors"

	"github.com/coder/envbox/buildlog"
	"github.com/coder/envbox/xunix"
	"github.com/coder/retry"
)

const diskFullStorageDriver = "vfs"

var usrLibMultiarchDir = map[string]string{
	"arm64": "/usr/lib/aarch64-linux-gnu",
	"amd64": "/usr/lib/x86_64-linux-gnu",
}

// Adapted from https://github.com/NVIDIA/libnvidia-container/blob/v1.15.0/src/nvc_container.c#L152-L165
var UsrLibDirs = map[string]string{
	// Debian-based distros use a multi-arch directory.
	"debian": usrLibMultiarchDir[goruntime.GOARCH],
	"ubuntu": usrLibMultiarchDir[goruntime.GOARCH],
	// Fedora and Redhat use the standard /usr/lib64.
	"fedora": "/usr/lib64",
	"rhel":   "/usr/lib64",
	// Fall back to the standard /usr/lib.
	"linux": "/usr/lib",
}

// /etc/os-release is the standard location for system identification data on
// Linux systems running systemd.
// Ref: https://www.freedesktop.org/software/systemd/man/latest/os-release.html
var etcOsRelease = "/etc/os-release"

type PullImageConfig struct {
	Client     Client
	Image      string
	Auth       AuthConfig
	ProgressFn ImagePullProgressFn
}

type ImagePullEvent struct {
	Status         string `json:"status"`
	Error          string `json:"error"`
	Progress       string `json:"progress"`
	ProgressDetail struct {
		Current int `json:"current"`
		Total   int `json:"total"`
	} `json:"progressDetail"`
}

// ImagePullProgressFn provides a way for a consumer to process
// image pull progress.
type ImagePullProgressFn func(e ImagePullEvent) error

// PullImage pulls the provided image.
func PullImage(ctx context.Context, config *PullImageConfig) error {
	authStr, err := config.Auth.Base64()
	if err != nil {
		return xerrors.Errorf("base64 encode auth: %w", err)
	}

	pullImageFn := func() error {
		var rd io.ReadCloser
		rd, err = config.Client.ImagePull(ctx, config.Image, image.PullOptions{
			RegistryAuth: authStr,
		})
		if err != nil {
			return xerrors.Errorf("pull image: %w", err)
		}

		err = processImagePullEvents(rd, config.ProgressFn)
		if err != nil {
			return xerrors.Errorf("process image pull events: %w", err)
		}
		return nil
	}

	err = pullImageFn()
	// We should bail early if we're going to fail due to a
	// certificate error. We can't xerrors.As here since this is
	// returned from the daemon so the client is reporting
	// essentially an unwrapped error.
	if isTLSVerificationErr(err) {
		return err
	}

	if err == nil {
		return nil
	}

	var pruned bool
	for r, n := retry.New(time.Second, time.Second*3), 0; r.Wait(ctx) && n < 10; n++ {
		err = pullImageFn()
		if err != nil {
			// If we failed to pull the image, try to prune existing images
			// to free up space.
			if xunix.IsNoSpaceErr(err) && !pruned {
				pruned = true
				// Pruning is best effort.
				_, _ = PruneImages(ctx, config.Client)
			}
			// If we've already pruned and we still can't pull the image we
			// should just exit.
			if xunix.IsNoSpaceErr(err) && pruned {
				return xerrors.Errorf("insufficient disk to pull image: %w", err)
			}
			continue
		}
		return nil
	}
	if err != nil {
		return xerrors.Errorf("pull image: %w", err)
	}

	return nil
}

// PruneImage runs a simple 'docker prune'.
func PruneImages(ctx context.Context, client Client) (dockertypes.ImagesPruneReport, error) {
	report, err := client.ImagesPrune(ctx,
		filters.NewArgs(filters.Arg("dangling", "false")),
	)
	if err != nil {
		return dockertypes.ImagesPruneReport{}, xerrors.Errorf("images prune: %w", err)
	}

	return report, nil
}

func processImagePullEvents(r io.Reader, fn ImagePullProgressFn) error {
	if fn == nil {
		// This effectively waits until the image is pulled before returning,
		// reporting no progress.
		_, _ = io.Copy(io.Discard, r)
		return nil
	}

	decoder := json.NewDecoder(r)

	var event ImagePullEvent
	for {
		if err := decoder.Decode(&event); err != nil {
			if xerrors.Is(err, io.EOF) {
				break
			}

			return xerrors.Errorf("decode image pull output: %w", err)
		}

		err := fn(event)
		if err != nil {
			return xerrors.Errorf("process image pull event: %w", err)
		}
	}

	return nil
}

type ImageMetadata struct {
	UID         string
	GID         string
	HomeDir     string
	HasInit     bool
	OsReleaseID string
}

// GetImageMetadata returns metadata about an image such as the UID/GID of the
// provided username and whether it contains an /sbin/init that we should run.
func GetImageMetadata(ctx context.Context, log slog.Logger, client Client, img, username string) (ImageMetadata, error) {
	// Creating a dummy container to inspect the filesystem.
	created, err := client.ContainerCreate(ctx,
		&container.Config{
			Image: img,
			Entrypoint: []string{
				"sleep",
			},
			Cmd: []string{
				"infinity",
			},
		},
		&container.HostConfig{
			Runtime: "sysbox-runc",
		}, nil, nil, "")
	if err != nil {
		return ImageMetadata{}, xerrors.Errorf("create container: %w", err)
	}

	defer func() {
		// We wanna remove this, but it's not a huge deal if it fails.
		_ = client.ContainerRemove(ctx, created.ID, container.RemoveOptions{
			Force: true,
		})
	}()

	err = client.ContainerStart(ctx, created.ID, container.StartOptions{})
	if err != nil {
		return ImageMetadata{}, xerrors.Errorf("start container: %w", err)
	}

	inspect, err := client.ContainerInspect(ctx, created.ID)
	if err != nil {
		return ImageMetadata{}, xerrors.Errorf("inspect: %w", err)
	}

	mergedDir := inspect.GraphDriver.Data["MergedDir"]
	// The mergedDir might be empty if we're running dockerd in recovery
	// mode.
	if mergedDir == "" && inspect.GraphDriver.Name != diskFullStorageDriver {
		// The MergedDir is empty when the underlying filesystem does not support
		// OverlayFS as an extension. A customer ran into this when using NFS as
		// a provider for a PVC.
		return ImageMetadata{}, xerrors.Errorf("CVMs do not support NFS volumes")
	}

	_, err = ExecContainer(ctx, client, ExecConfig{
		ContainerID: inspect.ID,
		Cmd:         "stat",
		Args:        []string{"/sbin/init"},
	})
	initExists := err == nil

	out, err := ExecContainer(ctx, client, ExecConfig{
		ContainerID: inspect.ID,
		Cmd:         "getent",
		Args:        []string{"passwd", username},
	})
	if err != nil {
		return ImageMetadata{}, xerrors.Errorf("get /etc/passwd entry for %s: %w", username, err)
	}

	users, err := xunix.ParsePasswd(bytes.NewReader(out))
	if err != nil {
		return ImageMetadata{}, xerrors.Errorf("parse passwd entry for (%s): %w", out, err)
	}
	if len(users) == 0 {
		return ImageMetadata{}, xerrors.Errorf("no users returned for username %s", username)
	}

	// Read the /etc/os-release file to get the ID of the OS.
	// We only care about the ID field.
	var osReleaseID string
	out, err = ExecContainer(ctx, client, ExecConfig{
		ContainerID: inspect.ID,
		Cmd:         "cat",
		Args:        []string{etcOsRelease},
	})
	if err != nil {
		log.Error(ctx, "read os-release", slog.Error(err))
		log.Error(ctx, "falling back to linux for os-release ID")
		osReleaseID = "linux"
	} else {
		osReleaseID = GetOSReleaseID(out)
	}

	return ImageMetadata{
		UID:         users[0].Uid,
		GID:         users[0].Gid,
		HomeDir:     users[0].HomeDir,
		HasInit:     initExists,
		OsReleaseID: osReleaseID,
	}, nil
}

// UsrLibDir returns the path to the /usr/lib directory for the given
// operating system determined by the /etc/os-release file.
func (im ImageMetadata) UsrLibDir() string {
	if val, ok := UsrLibDirs[im.OsReleaseID]; ok && val != "" {
		return val
	}
	return UsrLibDirs["linux"] // fallback
}

// GetOSReleaseID returns the ID of the operating system from the
// raw contents of /etc/os-release.
func GetOSReleaseID(raw []byte) string {
	var osReleaseID string
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "ID=") {
			osReleaseID = strings.TrimPrefix(line, "ID=")
			// The value may be quoted.
			osReleaseID = strings.Trim(osReleaseID, "\"")
			break
		}
	}
	if osReleaseID == "" {
		return "linux"
	}
	return osReleaseID
}

func DefaultLogImagePullFn(log buildlog.Logger) func(ImagePullEvent) error {
	var (
		// Avoid spamming too frequently, the messages can come quickly
		delayDur = time.Second * 2
		// We use a zero-value time.Time to start since we want to log
		// the first event we get.
		lastLog time.Time
	)
	return func(e ImagePullEvent) error {
		if e.Error != "" {
			log.Errorf(e.Error)
			return xerrors.Errorf("pull image: %s", e.Error)
		}

		// Not enough time has transpired, return without logging.
		if time.Since(lastLog) < delayDur {
			return nil
		}

		msg := e.Status
		if e.Progress != "" {
			msg = fmt.Sprintf("%s: %s", e.Status, e.Progress)
		}
		log.Info(msg)
		lastLog = time.Now()

		return nil
	}
}

func isTLSVerificationErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "tls: failed to verify certificate: x509: certificate signed by unknown authority")
}
