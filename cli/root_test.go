package cli

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLifecycle(t *testing.T) {
	t.Parallel()

	t.Run("UnarmedDoesNotWait", func(t *testing.T) {
		t.Parallel()

		lifecycle := newLifecycle()
		returned := make(chan struct{})
		go func() {
			lifecycle.wait()
			close(returned)
		}()

		require.Eventually(t, func() bool {
			select {
			case <-returned:
				return true
			default:
				return false
			}
		}, time.Second, time.Millisecond)
	})

	t.Run("ArmedWaitsForFinish", func(t *testing.T) {
		t.Parallel()

		lifecycle := newLifecycle()
		lifecycle.arm()
		returned := make(chan struct{})
		go func() {
			lifecycle.wait()
			close(returned)
		}()

		select {
		case <-returned:
			t.Fatal("lifecycle returned before shutdown finished")
		case <-time.After(10 * time.Millisecond):
		}

		lifecycle.finish()
		require.Eventually(t, func() bool {
			select {
			case <-returned:
				return true
			default:
				return false
			}
		}, time.Second, time.Millisecond)
	})
}
