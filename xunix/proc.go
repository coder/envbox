package xunix

import (
	"context"
	"fmt"

	"github.com/spf13/afero"
	"golang.org/x/xerrors"
)

func SetOOMScore(ctx context.Context, pid, score string) error {
	var (
		fs   = GetFS(ctx)
		file = fmt.Sprintf("/proc/%v/oom_score_adj", pid)
	)

	err := afero.WriteFile(fs, file, []byte(score), 0o644)
	if err != nil {
		return xerrors.Errorf("write file: %w", err)
	}

	return nil
}
