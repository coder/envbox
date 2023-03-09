package cliflag_test

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
	"golang.org/x/xerrors"

	"github.com/coder/envbox/cli/cliflag"
)

// Testcliflag cannot run in parallel because it uses t.Setenv.
//
//nolint:paralleltest
func TestCliflag(t *testing.T) {
	t.Run("StringDefault", func(t *testing.T) {
		flagset, name, shorthand, env, usage := randomFlag()
		def, _ := String(10)
		cliflag.String(flagset, name, shorthand, env, def, usage)
		got, err := flagset.GetString(name)
		require.NoError(t, err)
		require.Equal(t, def, got)
		require.Contains(t, flagset.FlagUsages(), usage)
		require.Contains(t, flagset.FlagUsages(), fmt.Sprintf("Consumes $%s", env))
	})

	t.Run("StringEnvVar", func(t *testing.T) {
		flagset, name, shorthand, env, usage := randomFlag()
		envValue, _ := String(10)
		t.Setenv(env, envValue)
		def, _ := String(10)
		cliflag.String(flagset, name, shorthand, env, def, usage)
		got, err := flagset.GetString(name)
		require.NoError(t, err)
		require.Equal(t, envValue, got)
	})

	t.Run("StringVarPDefault", func(t *testing.T) {
		var ptr string
		flagset, name, shorthand, env, usage := randomFlag()
		def, _ := String(10)

		cliflag.StringVarP(flagset, &ptr, name, shorthand, env, def, usage)
		got, err := flagset.GetString(name)
		require.NoError(t, err)
		require.Equal(t, def, got)
		require.Contains(t, flagset.FlagUsages(), usage)
		require.Contains(t, flagset.FlagUsages(), fmt.Sprintf("Consumes $%s", env))
	})

	t.Run("StringVarPEnvVar", func(t *testing.T) {
		var ptr string
		flagset, name, shorthand, env, usage := randomFlag()
		envValue, _ := String(10)
		t.Setenv(env, envValue)
		def, _ := String(10)

		cliflag.StringVarP(flagset, &ptr, name, shorthand, env, def, usage)
		got, err := flagset.GetString(name)
		require.NoError(t, err)
		require.Equal(t, envValue, got)
	})

	t.Run("EmptyEnvVar", func(t *testing.T) {
		var ptr string
		flagset, name, shorthand, _, usage := randomFlag()
		def, _ := String(10)

		cliflag.StringVarP(flagset, &ptr, name, shorthand, "", def, usage)
		got, err := flagset.GetString(name)
		require.NoError(t, err)
		require.Equal(t, def, got)
		require.Contains(t, flagset.FlagUsages(), usage)
		require.NotContains(t, flagset.FlagUsages(), "Consumes")
	})

	t.Run("StringArrayDefault", func(t *testing.T) {
		var ptr []string
		flagset, name, shorthand, env, usage := randomFlag()
		def := []string{"hello"}
		cliflag.StringArrayVarP(flagset, &ptr, name, shorthand, env, def, usage)
		got, err := flagset.GetStringArray(name)
		require.NoError(t, err)
		require.Equal(t, def, got)
	})

	t.Run("StringArrayEnvVar", func(t *testing.T) {
		var ptr []string
		flagset, name, shorthand, env, usage := randomFlag()
		t.Setenv(env, "wow,test")
		cliflag.StringArrayVarP(flagset, &ptr, name, shorthand, env, nil, usage)
		got, err := flagset.GetStringArray(name)
		require.NoError(t, err)
		require.Equal(t, []string{"wow", "test"}, got)
	})

	t.Run("StringArrayEnvVarEmpty", func(t *testing.T) {
		var ptr []string
		flagset, name, shorthand, env, usage := randomFlag()
		t.Setenv(env, "")
		cliflag.StringArrayVarP(flagset, &ptr, name, shorthand, env, nil, usage)
		got, err := flagset.GetStringArray(name)
		require.NoError(t, err)
		require.Equal(t, []string{}, got)
	})

	t.Run("UInt8Default", func(t *testing.T) {
		var ptr uint8
		flagset, name, shorthand, env, usage := randomFlag()
		def, _ := Int63n(10)

		cliflag.Uint8VarP(flagset, &ptr, name, shorthand, env, uint8(def), usage)
		got, err := flagset.GetUint8(name)
		require.NoError(t, err)
		require.Equal(t, uint8(def), got)
		require.Contains(t, flagset.FlagUsages(), usage)
		require.Contains(t, flagset.FlagUsages(), fmt.Sprintf("Consumes $%s", env))
	})

	t.Run("UInt8EnvVar", func(t *testing.T) {
		var ptr uint8
		flagset, name, shorthand, env, usage := randomFlag()
		envValue, _ := Int63n(10)
		t.Setenv(env, strconv.FormatUint(uint64(envValue), 10))
		def, _ := Int()

		cliflag.Uint8VarP(flagset, &ptr, name, shorthand, env, uint8(def), usage)
		got, err := flagset.GetUint8(name)
		require.NoError(t, err)
		require.Equal(t, uint8(envValue), got)
	})

	t.Run("UInt8FailParse", func(t *testing.T) {
		var ptr uint8
		flagset, name, shorthand, env, usage := randomFlag()
		envValue, _ := String(10)
		t.Setenv(env, envValue)
		def, _ := Int63n(10)

		cliflag.Uint8VarP(flagset, &ptr, name, shorthand, env, uint8(def), usage)
		got, err := flagset.GetUint8(name)
		require.NoError(t, err)
		require.Equal(t, uint8(def), got)
	})

	t.Run("IntDefault", func(t *testing.T) {
		var ptr int
		flagset, name, shorthand, env, usage := randomFlag()
		def, _ := Int63n(10)

		cliflag.IntVarP(flagset, &ptr, name, shorthand, env, int(def), usage)
		got, err := flagset.GetInt(name)
		require.NoError(t, err)
		require.Equal(t, int(def), got)
		require.Contains(t, flagset.FlagUsages(), usage)
		require.Contains(t, flagset.FlagUsages(), fmt.Sprintf("Consumes $%s", env))
	})

	t.Run("IntEnvVar", func(t *testing.T) {
		var ptr int
		flagset, name, shorthand, env, usage := randomFlag()
		envValue, _ := Int63n(10)
		t.Setenv(env, strconv.FormatUint(uint64(envValue), 10))
		def, _ := Int()

		cliflag.IntVarP(flagset, &ptr, name, shorthand, env, def, usage)
		got, err := flagset.GetInt(name)
		require.NoError(t, err)
		require.Equal(t, int(envValue), got)
	})

	t.Run("IntFailParse", func(t *testing.T) {
		var ptr int
		flagset, name, shorthand, env, usage := randomFlag()
		envValue, _ := String(10)
		t.Setenv(env, envValue)
		def, _ := Int63n(10)

		cliflag.IntVarP(flagset, &ptr, name, shorthand, env, int(def), usage)
		got, err := flagset.GetInt(name)
		require.NoError(t, err)
		require.Equal(t, int(def), got)
	})

	t.Run("BoolDefault", func(t *testing.T) {
		var ptr bool
		flagset, name, shorthand, env, usage := randomFlag()
		def, _ := Bool()

		cliflag.BoolVarP(flagset, &ptr, name, shorthand, env, def, usage)
		got, err := flagset.GetBool(name)
		require.NoError(t, err)
		require.Equal(t, def, got)
		require.Contains(t, flagset.FlagUsages(), usage)
		require.Contains(t, flagset.FlagUsages(), fmt.Sprintf("Consumes $%s", env))
	})

	t.Run("BoolEnvVar", func(t *testing.T) {
		var ptr bool
		flagset, name, shorthand, env, usage := randomFlag()
		envValue, _ := Bool()
		t.Setenv(env, strconv.FormatBool(envValue))
		def, _ := Bool()

		cliflag.BoolVarP(flagset, &ptr, name, shorthand, env, def, usage)
		got, err := flagset.GetBool(name)
		require.NoError(t, err)
		require.Equal(t, envValue, got)
	})

	t.Run("BoolFailParse", func(t *testing.T) {
		var ptr bool
		flagset, name, shorthand, env, usage := randomFlag()
		envValue, _ := String(10)
		t.Setenv(env, envValue)
		def, _ := Bool()

		cliflag.BoolVarP(flagset, &ptr, name, shorthand, env, def, usage)
		got, err := flagset.GetBool(name)
		require.NoError(t, err)
		require.Equal(t, def, got)
	})

	t.Run("DurationDefault", func(t *testing.T) {
		var ptr time.Duration
		flagset, name, shorthand, env, usage := randomFlag()
		def, _ := Duration()

		cliflag.DurationVarP(flagset, &ptr, name, shorthand, env, def, usage)
		got, err := flagset.GetDuration(name)
		require.NoError(t, err)
		require.Equal(t, def, got)
		require.Contains(t, flagset.FlagUsages(), usage)
		require.Contains(t, flagset.FlagUsages(), fmt.Sprintf("Consumes $%s", env))
	})

	t.Run("DurationEnvVar", func(t *testing.T) {
		var ptr time.Duration
		flagset, name, shorthand, env, usage := randomFlag()
		envValue, _ := Duration()
		t.Setenv(env, envValue.String())
		def, _ := Duration()

		cliflag.DurationVarP(flagset, &ptr, name, shorthand, env, def, usage)
		got, err := flagset.GetDuration(name)
		require.NoError(t, err)
		require.Equal(t, envValue, got)
	})

	t.Run("DurationFailParse", func(t *testing.T) {
		var ptr time.Duration
		flagset, name, shorthand, env, usage := randomFlag()
		envValue, _ := String(10)
		t.Setenv(env, envValue)
		def, _ := Duration()

		cliflag.DurationVarP(flagset, &ptr, name, shorthand, env, def, usage)
		got, err := flagset.GetDuration(name)
		require.NoError(t, err)
		require.Equal(t, def, got)
	})
}

func randomFlag() (*pflag.FlagSet, string, string, string, string) {
	fsname, _ := String(10)
	flagset := pflag.NewFlagSet(fsname, pflag.PanicOnError)
	name, _ := String(10)
	shorthand, _ := String(1)
	env, _ := String(10)
	usage, _ := String(10)

	return flagset, name, shorthand, env, usage
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

// Bool returns a random true/false value as a bool.
func Bool() (bool, error) {
	i, err := Uint64()
	if err != nil {
		return false, err
	}

	// True if the least significant bit is 1
	return i&1 == 1, nil
}

// Uint64 returns a random 64-bit integer as a uint64.
func Uint64() (uint64, error) {
	upper, err := Int63()
	if err != nil {
		return 0, xerrors.Errorf("read upper: %w", err)
	}

	lower, err := Int63()
	if err != nil {
		return 0, xerrors.Errorf("read lower: %w", err)
	}

	return uint64(lower)>>31 | uint64(upper)<<32, nil
}

// Duration returns a random time.Duration value
func Duration() (time.Duration, error) {
	i, err := Int63()
	if err != nil {
		return time.Duration(0), err
	}

	return time.Duration(i), nil
}

// Int returns a non-negative random integer as a int.
func Int() (int, error) {
	i, err := Int63()
	if err != nil {
		return 0, err
	}

	if i < 0 {
		return int(-i), nil
	}
	return int(i), nil
}

// Int63n returns a non-negative random integer in [0,max) as a int64.
func Int63n(max int64) (int64, error) {
	if max <= 0 {
		panic("invalid argument to Int63n")
	}

	trueMax := int64((1 << 63) - 1 - (1<<63)%uint64(max))
	i, err := Int63()
	if err != nil {
		return 0, err
	}

	for i > trueMax {
		i, err = Int63()
		if err != nil {
			return 0, err
		}
	}

	return i % max, nil
}
