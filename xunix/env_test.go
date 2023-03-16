package xunix_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/coder/envbox/xunix"
)

func TestMustLookupEnv(t *testing.T) {
	t.Parallel()

	t.Run("OK", func(t *testing.T) {
		t.Parallel()

		const (
			key   = "MY_ENV"
			value = "value"
		)

		//nolint can't use t.SetEnv in parallel tests.
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
