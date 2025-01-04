package dockerutil

import (
	"context"
	"fmt"
	"time"

	"cdr.dev/slog"
	"github.com/coder/envbox/background"
	"github.com/coder/envbox/xunix"
	dockerclient "github.com/docker/docker/client"
	"golang.org/x/xerrors"
)

const noSpaceDataDir = "/var/lib/docker.bak"

type DaemonOptions struct {
	Link   string
	CIDR   string
	Driver string
}

func StartDaemon(ctx context.Context, log slog.Logger, opts *DaemonOptions) (*background.Process, error) {
	// We need to adjust the MTU for the host otherwise packets will fail delivery.
	// 1500 is the standard, but certain deployments (like GKE) use custom MTU values.
	// See: https://www.atlantis-press.com/journals/ijndc/125936177/view#sec-s3.1

	mtu, err := xunix.NetlinkMTU(opts.Link)
	if err != nil {
		return nil, xerrors.Errorf("custom mtu: %w", err)
	}

	// We set the Docker Bridge IP explicitly here for a number of reasons:
	// 1) It sometimes picks the 172.17.x.x address which conflicts with that of the Docker daemon in the inner container.
	// 2) It defaults to a /16 network which is way more than we need for envbox.
	// 3) The default may conflict with existing internal network resources, and an operator may wish to override it.
	dockerBip, prefixLen := BridgeIPFromCIDR(opts.CIDR)

	args := []string{
		"--debug",
		"--log-level=debug",
		fmt.Sprintf("--mtu=%d", mtu),
		"--userns-remap=coder",
		"--storage-driver=overlay2",
		fmt.Sprintf("--bip=%s/%d", dockerBip, prefixLen),
	}

	if opts.Driver != "" {
		args = append(args,
			fmt.Sprintf("--storage-driver=%s", opts.Driver),
		)
	}

	if opts.Driver == "vfs" {
		args = append(args,
			fmt.Sprintf("--data-root=%s", noSpaceDataDir),
		)
	}

	p := background.New(ctx, log, "dockerd", args...)
	err = p.Start()
	if err != nil {
		return nil, xerrors.Errorf("start dockerd: %w", err)
	}

	return p, nil
}

// WaitForDaemon waits for a Docker daemon to startup. It waits a max
// of 5m before giving up.
func WaitForDaemon(ctx context.Context, client Client) error {
	ticker := time.NewTicker(time.Millisecond * 250)
	defer ticker.Stop()

	ctx, cancel := context.WithTimeout(ctx, time.Minute*5)
	defer cancel()

	pingCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()

	_, err := client.Ping(pingCtx)
	if err == nil {
		// We pinged successfully!
		return nil
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}

		err := func() error {
			pingCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()

			_, err := client.Ping(pingCtx)
			return err
		}()
		if err == nil {
			// We pinged successfully!
			return nil
		}

		// If its a connection failed error we can ignore and continue polling.
		// It's likely that dockerd just isn't fully setup yet.
		if dockerclient.IsErrConnectionFailed(err) || pingCtx.Err() != nil {
			continue
		}

		// If its something else, we return it.
		return err
	}
}
