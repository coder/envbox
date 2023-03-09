package dockerutil

import (
	"context"

	dockerclient "github.com/docker/docker/client"
	"golang.org/x/xerrors"
)

type clientKey struct{}

func WithClient(ctx context.Context, client DockerClient) context.Context {
	return context.WithValue(ctx, clientKey{}, client)
}

func Client(ctx context.Context) (DockerClient, error) {
	client := ctx.Value(clientKey{})
	if client == nil {
		client, err := dockerclient.NewClientWithOpts(dockerclient.FromEnv)
		if err != nil {
			return nil, xerrors.Errorf("new env client: %w", err)
		}

		return client, nil
	}

	return client.(DockerClient), nil
}
