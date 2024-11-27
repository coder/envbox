package integrationtest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TmpDir returns a subdirectory in /tmp that can be used for test files.
func TmpDir(t *testing.T) string {
	// We use os.MkdirTemp as oposed to t.TempDir since the envbox container will
	// chown some of the created directories here to root:root causing the cleanup
	// function to fail once the test exits.
	tmpdir, err := os.MkdirTemp(os.TempDir(), strings.ReplaceAll(t.Name(), "/", "_"))
	require.NoError(t, err)
	t.Logf("using tmpdir %s", tmpdir)
	t.Cleanup(func() {
		if !t.Failed() {
			// Could be useful in case of test failure.
			_ = os.RemoveAll(tmpdir)
		}
	})
	return tmpdir
}

func MkdirAll(t testing.TB, elem ...string) string {
	t.Helper()

	path := filepath.Join(elem...)
	err := os.MkdirAll(path, 0o777)
	require.NoError(t, err)
	return path
}

func WriteFile(t *testing.T, path, contents string) {
	t.Helper()

	//nolint:gosec
	err := os.WriteFile(path, []byte(contents), 0o644)
	require.NoError(t, err)
}
