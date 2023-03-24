package buildlog_test

import (
	"crypto/rand"
	"encoding/binary"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/exp/slices"
	"golang.org/x/net/context"
	"golang.org/x/xerrors"

	"cdr.dev/slog/sloggers/slogtest"

	"github.com/stretchr/testify/require"

	"github.com/coder/coder/codersdk/agentsdk"
	"github.com/coder/envbox/buildlog"
)

func TestCoderLog(t *testing.T) {
	t.Parallel()

	t.Run("MaxLogsCausesSend", func(t *testing.T) {
		t.Parallel()

		var (
			maxLogs      = buildlog.CoderLoggerMaxLogs
			client       = fakeCoderClient{}
			ctx          = context.Background()
			slogger      = slogtest.Make(t, nil)
			expectedLogs = make([]string, 20)
			actualLogs   = make([]string, 0, 20)
			logMu        sync.Mutex
		)
		client.PatchStartupLogsFn = func(ctx context.Context, logs agentsdk.PatchStartupLogs) error {
			logMu.Lock()
			defer logMu.Unlock()

			require.Len(t, logs.Logs, maxLogs)
			for _, l := range logs.Logs {
				require.NotZero(t, l.CreatedAt)
				actualLogs = append(actualLogs, l.Output)
			}
			return nil
		}

		log := buildlog.OpenCoderLogger(ctx, client, slogger)

		for i := 0; i < maxLogs; i++ {
			expectedLogs[i] = MustString(10)
			log.Info(expectedLogs[i])
		}

		require.Eventually(t, func() bool {
			logMu.Lock()
			defer logMu.Unlock()
			return slices.Equal(expectedLogs, actualLogs)
		}, time.Millisecond*5, time.Millisecond)
	})

	t.Run("OutputNotDropped", func(t *testing.T) {
		t.Parallel()

		var (
			maxLogs    = 2
			client     = fakeCoderClient{}
			ctx        = context.Background()
			slogger    = slogtest.Make(t, nil)
			actualLogs = make([]string, 0, maxLogs)
			logMu      sync.Mutex
		)
		client.PatchStartupLogsFn = func(ctx context.Context, logs agentsdk.PatchStartupLogs) error {
			logMu.Lock()
			defer logMu.Unlock()

			require.Len(t, logs.Logs, maxLogs)
			for _, l := range logs.Logs {
				require.NotZero(t, l.CreatedAt)
				actualLogs = append(actualLogs, l.Output)
			}
			return nil
		}

		log := buildlog.OpenCoderLogger(ctx, client, slogger)

		bigLine := MustString(buildlog.MaxCoderLogSize + buildlog.MaxCoderLogSize/2)
		// The line should be chopped up into smaller logs so that we don't
		// drop output.
		expectedLogs := []string{
			bigLine[:buildlog.MaxCoderLogSize],
			bigLine[buildlog.MaxCoderLogSize:],
		}
		log.Info(bigLine)
		// Close the logger to flush the logs.
		log.Close()
		require.Eventually(t, func() bool {
			logMu.Lock()
			defer logMu.Unlock()
			return slices.Equal(expectedLogs, actualLogs)
		}, time.Millisecond*5, time.Millisecond)
	})

	t.Run("CloseFlushes", func(t *testing.T) {
		t.Parallel()

		var (
			maxLogs      = 20
			numLogs      = maxLogs / 2
			client       = fakeCoderClient{}
			ctx          = context.Background()
			slogger      = slogtest.Make(t, nil)
			expectedLogs = make([]string, numLogs)
			logMu        sync.Mutex
			actualLogs   = make([]string, 0, numLogs)
		)
		client.PatchStartupLogsFn = func(ctx context.Context, logs agentsdk.PatchStartupLogs) error {
			logMu.Lock()
			defer logMu.Unlock()
			require.Len(t, logs.Logs, numLogs)
			for _, l := range logs.Logs {
				require.NotZero(t, l.CreatedAt)
				actualLogs = append(actualLogs, l.Output)
			}
			return nil
		}

		log := buildlog.OpenCoderLogger(ctx, client, slogger)

		for i := 0; i < numLogs; i++ {
			expectedLogs[i] = MustString(10)
			log.Info(expectedLogs[i])
		}

		log.Close()

		require.Eventually(t, func() bool {
			logMu.Lock()
			defer logMu.Unlock()

			return slices.Equal(expectedLogs, actualLogs)
		}, time.Millisecond*100, time.Millisecond*10)
	})
}

type fakeCoderClient struct {
	PatchStartupLogsFn func(context.Context, agentsdk.PatchStartupLogs) error
}

func (f fakeCoderClient) PatchStartupLogs(ctx context.Context, req agentsdk.PatchStartupLogs) error {
	if f.PatchStartupLogsFn != nil {
		return f.PatchStartupLogsFn(ctx, req)
	}
	return nil
}

// StringCharset generates a random string using the provided charset and size
func StringCharset(charSetStr string, size int) (string, error) {
	charSet := []rune(charSetStr)

	if len(charSet) == 0 || size == 0 {
		return "", nil
	}

	// This buffer facilitates pre-emptively creation of random uint32s
	// to reduce syscall overhead.
	ibuf := make([]byte, 4*size)

	_, err := rand.Read(ibuf)
	if err != nil {
		return "", err
	}

	var buf strings.Builder
	buf.Grow(size)

	for i := 0; i < size; i++ {
		count, err := UnbiasedModulo32(
			binary.BigEndian.Uint32(ibuf[i*4:(i+1)*4]),
			int32(len(charSet)),
		)
		if err != nil {
			return "", err
		}

		_, _ = buf.WriteRune(charSet[count])
	}

	return buf.String(), nil
}

func MustString(size int) string {
	s, err := String(size)
	if err != nil {
		panic(err)
	}
	return s
}

// String returns a random string using Default.
func String(size int) (string, error) {
	return StringCharset(Default, size)
}

// UnbiasedModulo32 uniformly modulos v by n over a sufficiently large data
// set, regenerating v if necessary. n must be > 0. All input bits in v must be
// fully random, you cannot cast a random uint8/uint16 for input into this
// function.
//
//nolint:varnamelen
func UnbiasedModulo32(v uint32, n int32) (int32, error) {
	prod := uint64(v) * uint64(n)
	low := uint32(prod)
	if low < uint32(n) {
		thresh := uint32(-n) % uint32(n)
		for low < thresh {
			var err error
			v, err = Uint32()
			if err != nil {
				return 0, err
			}
			prod = uint64(v) * uint64(n)
			low = uint32(prod)
		}
	}
	return int32(prod >> 32), nil
}

// Uint32 returns a 32-bit value as a uint32.
func Uint32() (uint32, error) {
	i, err := Int63()
	if err != nil {
		return 0, err
	}

	return uint32(i >> 31), nil
}

// Int64 returns a non-negative random 63-bit integer as a int64.
func Int63() (int64, error) {
	var i int64
	err := binary.Read(rand.Reader, binary.BigEndian, &i)
	if err != nil {
		return 0, xerrors.Errorf("read binary: %w", err)
	}

	if i < 0 {
		return -i, nil
	}
	return i, nil
}

// Charsets
const (
	// Numeric includes decimal numbers (0-9)
	Numeric = "0123456789"

	// Upper is uppercase characters in the Latin alphabet
	Upper = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"

	// Lower is lowercase characters in the Latin alphabet
	Lower = "abcdefghijklmnopqrstuvwxyz"

	// Alpha is upper or lowercase alphabetic characters
	Alpha = Upper + Lower

	// Default is uppercase, lowercase, or numeric characters
	Default = Numeric + Alpha

	// Hex is hexadecimal lowercase characters
	Hex = "0123456789abcdef"

	// Human creates strings which are easily distinguishable from
	// others created with the same charset. It contains most lowercase
	// alphanumeric characters without 0,o,i,1,l.
	Human = "23456789abcdefghjkmnpqrstuvwxyz"
)
