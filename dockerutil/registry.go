package dockerutil

import (
	"context"
	"io"
	"path/filepath"

	"github.com/spf13/afero"
	"golang.org/x/xerrors"

	"github.com/coder/envbox/xunix"
)

// WriteCertsForRegistry writes the certificates found in the provided directory
// to the correct subdirectory that the Docker daemon uses when pulling images
// from the specified private registry.
func WriteCertsForRegistry(ctx context.Context, registryName, certsDir string) error {
	fs := xunix.GetFS(ctx)

	// Docker certs directory.
	registryCertsDir := filepath.Join("/etc/docker/certs.d", registryName)

	// If the directory already exists it means someone
	// has either wrapped the image or has mounted in certs
	// manually. We should assume the user knows what they're
	// doing and avoid mucking with their solution.
	if _, err := fs.Stat(registryCertsDir); err == nil {
		return nil
	}

	// Ensure the registry certs directory exists.
	err := fs.MkdirAll(registryCertsDir, 0755)
	if err != nil {
		return xerrors.Errorf("create registry certs directory: %w", err)
	}

	// Check if certsDir is a file.
	fileInfo, err := fs.Stat(certsDir)
	if err != nil {
		return xerrors.Errorf("stat certs directory/file: %w", err)
	}

	if !fileInfo.IsDir() {
		// If it's a file, copy it directly
		err = copyCertFile(fs, certsDir, filepath.Join(registryCertsDir, "ca.crt"))
		if err != nil {
			return xerrors.Errorf("copy cert file: %w", err)
		}
		return nil
	}

	// If it's a directory, copy all cert files in the root of the directory
	entries, err := afero.ReadDir(fs, certsDir)
	if err != nil {
		return xerrors.Errorf("read certs directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		srcPath := filepath.Join(certsDir, entry.Name())
		dstPath := filepath.Join(registryCertsDir, entry.Name())
		err = copyCertFile(fs, srcPath, dstPath)
		if err != nil {
			return xerrors.Errorf("copy cert file %s: %w", entry.Name(), err)
		}
	}

	return nil
}

func copyCertFile(fs xunix.FS, src, dst string) error {
	srcFile, err := fs.Open(src)
	if err != nil {
		return xerrors.Errorf("open source file: %w", err)
	}
	defer srcFile.Close()

	dstFile, err := fs.Create(dst)
	if err != nil {
		return xerrors.Errorf("create destination file: %w", err)
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	if err != nil {
		return xerrors.Errorf("copy file contents: %w", err)
	}

	return nil
}
