package dockerutil

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	dockertypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"golang.org/x/xerrors"

	"github.com/coder/envbox/buildlog"
	"github.com/coder/envbox/xunix"
	"github.com/coder/retry"
)

const diskFullStorageDriver = "vfs"

type PullImageConfig struct {
	Client     DockerClient
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
		rd, err = config.Client.ImagePull(ctx, config.Image, dockertypes.ImagePullOptions{
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
func PruneImages(ctx context.Context, client DockerClient) (dockertypes.ImagesPruneReport, error) {
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
	UID     string
	GID     string
	HomeDir string
	HasInit bool
}

// GetImageMetadata returns metadata about an image such as the UID/GID of the
// provided username and whether it contains an /sbin/init that we should run.
func GetImageMetadata(ctx context.Context, client DockerClient, image, username string) (ImageMetadata, error) {
	// Creating a dummy container to inspect the filesystem.
	created, err := client.ContainerCreate(ctx,
		&container.Config{
			Image: image,
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
		_ = client.ContainerRemove(ctx, created.ID, dockertypes.ContainerRemoveOptions{
			Force: true,
		})
	}()

	err = client.ContainerStart(ctx, created.ID, dockertypes.ContainerStartOptions{})
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

	return ImageMetadata{
		UID:     users[0].Uid,
		GID:     users[0].Gid,
		HomeDir: users[0].HomeDir,
		HasInit: initExists,
	}, nil
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
