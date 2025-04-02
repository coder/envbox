package sysboxutil

import (
	"context"
	"os"
	"time"

	"golang.org/x/xerrors"

	"github.com/coder/envbox/xunix"
)

const ManagerSocketPath = "/run/sysbox/sysmgr.sock"

// WaitForManager waits for the sysbox-mgr to startup.
func WaitForManager(ctx context.Context) error {
	fs := xunix.GetFS(ctx)

	_, err := fs.Stat(ManagerSocketPath)
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
			_, err := fs.Stat(ManagerSocketPath)
			if err != nil {
				if !xerrors.Is(err, os.ErrNotExist) {
					return xerrors.Errorf("unexpected stat err %s: %w", ManagerSocketPath, err)
				}
				continue
			}
			return nil
		}
	}
}
