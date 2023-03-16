package dockerutil

import (
	"context"

	dockerclient "github.com/docker/docker/client"
	"golang.org/x/xerrors"
)

type clientKey struct{}

// WithClient sets the provided DockerClient on the context.
// It should only be used for tests.
func WithClient(ctx context.Context, client DockerClient) context.Context {
	return context.WithValue(ctx, clientKey{}, client)
}

// Client returns the DockerClient set on the context. If one can't be
// found a default client is returned.
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
