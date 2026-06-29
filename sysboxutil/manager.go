package sysboxutil

import (
	"context"
	"os"
	"time"

	"golang.org/x/xerrors"

	"github.com/coder/envbox/xunix"
)

const (
	ManagerSocketPath = "/run/sysbox/sysmgr.sock"
	FSSocketPath      = "/run/sysbox/sysfs.sock"
)

// WaitForManager waits for the sysbox-mgr to startup.
func WaitForManager(ctx context.Context) error {
	return waitForSocket(ctx, ManagerSocketPath)
}

// WaitForFS waits for sysbox-fs to startup.
func WaitForFS(ctx context.Context) error {
	return waitForSocket(ctx, FSSocketPath)
}

func waitForSocket(ctx context.Context, path string) error {
	fs := xunix.GetFS(ctx)

	_, err := fs.Stat(path)
	if err == nil {
		return nil
	}

	const (
		period = time.Second
	)

	t := time.NewTicker(period)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			_, err := fs.Stat(path)
			if err != nil {
				if !xerrors.Is(err, os.ErrNotExist) {
					return xerrors.Errorf("unexpected stat err %s: %w", path, err)
				}
				continue
			}
			return nil
		}
	}
}
