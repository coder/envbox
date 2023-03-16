package xunix_test

import (
	"os"
	"testing"

	"github.com/coder/envbox/xunix"
	"github.com/stretchr/testify/require"
)

func TestMustLookupEnv(t *testing.T) {
	t.Parallel()

	t.Run("OK", func(t *testing.T) {
		t.Parallel()

		const (
			key   = "MY_ENV"
			value = "value"
		)
		os.Setenv(key, value)

		val := xunix.MustLookupEnv(key)
		require.Equal(t, value, val)
	})

	t.Run("Panic", func(t *testing.T) {
		t.Parallel()

		defer func() {
			e := recover()
			require.NotNil(t, e, "function should panic")

		}()
		_ = xunix.MustLookupEnv("ASDasdf")
	})
}
