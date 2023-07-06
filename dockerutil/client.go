package dockerutil

import (
	"context"
	"encoding/base64"
	"encoding/json"

	dockertypes "github.com/docker/docker/api/types"
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

	//nolint we should panic if this isn't the case.
	return client.(DockerClient), nil
}

type AuthConfig dockertypes.AuthConfig

func (a AuthConfig) Base64() (string, error) {
	authStr, err := json.Marshal(a)
	if err != nil {
		return "", xerrors.Errorf("json marshal: %w", err)
	}
	return base64.URLEncoding.EncodeToString(authStr), nil
}

func ParseAuthConfig(raw string) (AuthConfig, error) {
	type dockerConfig struct {
		AuthConfigs map[string]dockertypes.AuthConfig `json:"auths"`
	}

	var conf dockerConfig
	if err := json.Unmarshal([]byte(raw), &conf); err != nil {
		return AuthConfig{}, xerrors.Errorf("parse docker auth config secret: %w", err)
	}
	if len(conf.AuthConfigs) != 1 {
		return AuthConfig{}, xerrors.Errorf("number of image pull auth configs not equal to 1 (%d)", len(conf.AuthConfigs))
	}
	for _, regConfig := range conf.AuthConfigs {
		return AuthConfig(regConfig), nil
	}

	return AuthConfig{}, xerrors.New("no auth configs parsed.")
}
