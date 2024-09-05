package dockerutil_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/coder/envbox/dockerutil"
	"github.com/coder/envbox/xunix"
	"github.com/coder/envbox/xunix/xunixfake"
)

func TestWriteCertsForRegistry(t *testing.T) {
	t.Parallel()

	t.Run("SingleCertFile", func(t *testing.T) {
		t.Parallel()
		// Test setup
		fs := xunixfake.NewMemFS()
		ctx := xunix.WithFS(context.Background(), fs)

		// Create a test certificate file
		certContent := []byte("test certificate content")
		err := afero.WriteFile(fs, "/certs/ca.crt", certContent, 0o644)
		require.NoError(t, err)

		// Run the function
		err = dockerutil.WriteCertsForRegistry(ctx, "test.registry.com", "/certs/ca.crt")
		require.NoError(t, err)

		// Check the result
		copiedContent, err := afero.ReadFile(fs, "/etc/docker/certs.d/test.registry.com/ca.crt")
		require.NoError(t, err)
		assert.Equal(t, certContent, copiedContent)
	})

	t.Run("MultipleCertFiles", func(t *testing.T) {
		t.Parallel()
		// Test setup
		fs := xunixfake.NewMemFS()
		ctx := xunix.WithFS(context.Background(), fs)

		// Create test certificate files
		certFiles := []string{"ca.crt", "client.cert", "client.key"}
		for _, file := range certFiles {
			err := afero.WriteFile(fs, filepath.Join("/certs", file), []byte("content of "+file), 0o644)
			require.NoError(t, err)
		}

		// Run the function
		err := dockerutil.WriteCertsForRegistry(ctx, "test.registry.com", "/certs")
		require.NoError(t, err)

		// Check the results
		for _, file := range certFiles {
			copiedContent, err := afero.ReadFile(fs, filepath.Join("/etc/docker/certs.d/test.registry.com", file))
			require.NoError(t, err)
			assert.Equal(t, []byte("content of "+file), copiedContent)
		}
	})
	t.Run("ExistingRegistryCertsDir", func(t *testing.T) {
		t.Parallel()
		// Test setup
		fs := xunixfake.NewMemFS()
		ctx := xunix.WithFS(context.Background(), fs)

		// Create an existing registry certs directory
		registryCertsDir := "/etc/docker/certs.d/test.registry.com"
		err := fs.MkdirAll(registryCertsDir, 0o755)
		require.NoError(t, err)

		// Create a file in the existing directory
		existingContent := []byte("existing certificate content")
		err = afero.WriteFile(fs, filepath.Join(registryCertsDir, "existing.crt"), existingContent, 0o644)
		require.NoError(t, err)

		// Create a test certificate file in the source directory
		certContent := []byte("new certificate content")
		err = afero.WriteFile(fs, "/certs/ca.crt", certContent, 0o644)
		require.NoError(t, err)

		// Run the function
		err = dockerutil.WriteCertsForRegistry(ctx, "test.registry.com", "/certs")
		require.NoError(t, err)

		// Check that the existing file was not modified
		existingFileContent, err := afero.ReadFile(fs, filepath.Join(registryCertsDir, "existing.crt"))
		require.NoError(t, err)
		assert.Equal(t, existingContent, existingFileContent)

		// Check that the new file was not copied
		_, err = fs.Stat(filepath.Join(registryCertsDir, "ca.crt"))
		assert.True(t, os.IsNotExist(err), "New certificate file should not have been copied")
	})
}
