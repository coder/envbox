package dockerutil_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/coder/envbox/dockerutil"
)

func TestAuthConfigFromString(t *testing.T) {
	t.Parallel()

	//nolint:gosec // this is a test
	creds := `{ "auths": { "docker.registry.test": { "auth": "Zm9vQGJhci5jb206YWJjMTIz" } } }`
	expectedUsername := "foo@bar.com"
	expectedPassword := "abc123"

	cfg, err := dockerutil.AuthConfigFromString(creds, "docker.registry.test")
	require.NoError(t, err)
	require.Equal(t, expectedUsername, cfg.Username)
	require.Equal(t, expectedPassword, cfg.Password)
}