package dockerutil

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"

	"github.com/cpuguy83/dockercfg"
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

func AuthConfigFromPath(path string, reg string) (AuthConfig, error) {
	var config dockercfg.Config
	err := dockercfg.FromFile(path, &config)
	if err != nil {
		return AuthConfig{}, xerrors.Errorf("load config: %w", err)
	}

	return parseConfig(config, reg)
}

func AuthConfigFromString(raw string, reg string) (AuthConfig, error) {
	var cfg dockercfg.Config
	err := json.Unmarshal([]byte(raw), &cfg)
	if err != nil {
		return AuthConfig{}, xerrors.Errorf("parse config: %w", err)
	}
	return parseConfig(cfg, reg)
}

func parseConfig(cfg dockercfg.Config, registry string) (AuthConfig, error) {

	hostname := dockercfg.ResolveRegistryHost(registry)

	username, secret, err := cfg.GetRegistryCredentials(hostname)
	if err != nil {
		return AuthConfig{}, xerrors.Errorf("get credentials from helper: %w", err)
	}

	if secret != "" {
		if username == "" {
			return AuthConfig{
				IdentityToken: secret,
			}, nil
		}
		return AuthConfig{
			Username: username,
			Password: secret,
		}, nil
	}

	return AuthConfig{}, xerrors.Errorf("no auth config found for registry %s: %w", registry, os.ErrNotExist)

}
