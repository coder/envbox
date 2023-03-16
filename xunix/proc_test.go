package xunix_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/coder/envbox/xunix"
	"github.com/coder/envbox/xunix/xunixfake"
)

func TestSetOOMScore(t *testing.T) {
	t.Parallel()

	t.Run("OK", func(t *testing.T) {
		t.Parallel()

		var (
			fs  = &xunixfake.MemFS{MemMapFs: &afero.MemMapFs{}}
			ctx = xunix.WithFS(context.Background(), fs)
		)

		const (
			pid   = "123"
			score = "-1000"
		)

		err := xunix.SetOOMScore(ctx, pid, score)
		require.NoError(t, err)

		actualScore, err := afero.ReadFile(fs, fmt.Sprintf("/proc/%s/oom_score_adj", pid))
		require.NoError(t, err)
		require.Equal(t, score, string(actualScore))
	})
}
