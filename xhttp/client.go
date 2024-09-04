package xhttp

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"os"
	"path/filepath"

	"golang.org/x/xerrors"

	"cdr.dev/slog"
)

func Client(log slog.Logger, extraCertsPath string) (*http.Client, error) {
	if extraCertsPath == "" {
		return &http.Client{}, nil
	}

	log = log.With(slog.F("root_path", extraCertsPath))
	log.Debug(context.Background(), "adding certs to default pool")
	pool, err := certPool(log, extraCertsPath)
	if err != nil {
		return nil, xerrors.Errorf("cert pool: %w", err)
	}

	//nolint:forcetypeassert
	transport := (http.DefaultTransport.(*http.Transport)).Clone()

	//nolint:gosec
	transport.TLSClientConfig = &tls.Config{
		RootCAs: pool,
	}

	return &http.Client{
		Transport: transport,
	}, nil
}

func certPool(log slog.Logger, certsPath string) (*x509.CertPool, error) {
	pool, err := x509.SystemCertPool()
	if err != nil {
		return nil, xerrors.Errorf("system cert pool: %w", err)
	}

	fi, err := os.Stat(certsPath)
	if err != nil {
		return nil, xerrors.Errorf("stat %v: %w", certsPath, err)
	}

	if !fi.IsDir() {
		err = addCert(log, pool, certsPath)
		if err != nil {
			return nil, xerrors.Errorf("add cert: %w", err)
		}
		return pool, nil
	}

	entries, err := os.ReadDir(certsPath)
	if err != nil {
		return nil, xerrors.Errorf("read dir %v: %w", certsPath, err)
	}

	for _, entry := range entries {
		path := filepath.Join(certsPath, entry.Name())
		err = addCert(log, pool, path)
		if err != nil {
			return nil, xerrors.Errorf("add cert: %w", err)
		}
	}

	return pool, nil
}

func addCert(log slog.Logger, pool *x509.CertPool, path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return xerrors.Errorf("read file: %w", err)
	}

	if !pool.AppendCertsFromPEM(b) {
		log.Error(context.Background(), "failed to append cert",
			slog.F("filepath", path))
	} else {
		log.Debug(context.Background(), "added cert", slog.F("path", path))
	}
	return nil
}
