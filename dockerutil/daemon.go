package dockerutil

import (
	"context"
	"time"

	dockerclient "github.com/docker/docker/client"
)

func WaitForDaemon(ctx context.Context, client DockerClient) error {
	ticker := time.NewTicker(time.Millisecond * 250)
	defer ticker.Stop()

	ctx, cancel := context.WithTimeout(ctx, time.Second*30)
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

		pingCtx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()

		_, err := client.Ping(pingCtx)
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
