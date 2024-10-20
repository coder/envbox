package dockerutil_test

import (
	"testing"

	"github.com/coder/envbox/dockerutil"
	"github.com/stretchr/testify/require"
)

func TestAuthConfigFromString(t *testing.T) {
	t.Parallel()

	creds := `{ "auths": { "docker.registry.test": { "auth": "Zm9vQGJhci5jb206YWJjMTIzCg==" } } }`
	expectedUsername := "foo@bar.com"
	expectedPassword := "abc123"

	cfg, err := dockerutil.AuthConfigFromString(creds, "docker.registry.test")
	require.NoError(t, err)
	require.Equal(t, expectedUsername, cfg.Username)
	require.Equal(t, expectedPassword, cfg.Password)
}
