package cliflag_test

import (
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"

	"github.com/coder/coder/cryptorand"
	"github.com/coder/envbox/cli/cliflag"
)

// Testcliflag cannot run in parallel because it uses t.Setenv.
//
//nolint:paralleltest
func TestCliflag(t *testing.T) {
	t.Run("StringDefault", func(t *testing.T) {
		flagset, name, shorthand, env, usage := randomFlag()
		def, _ := cryptorand.String(10)
		cliflag.String(flagset, name, shorthand, env, def, usage)
		got, err := flagset.GetString(name)
		require.NoError(t, err)
		require.Equal(t, def, got)
		require.Contains(t, flagset.FlagUsages(), usage)
		require.Contains(t, flagset.FlagUsages(), fmt.Sprintf("Consumes $%s", env))
	})

	t.Run("StringEnvVar", func(t *testing.T) {
		flagset, name, shorthand, env, usage := randomFlag()
		envValue, _ := cryptorand.String(10)
		t.Setenv(env, envValue)
		def, _ := cryptorand.String(10)
		cliflag.String(flagset, name, shorthand, env, def, usage)
		got, err := flagset.GetString(name)
		require.NoError(t, err)
		require.Equal(t, envValue, got)
	})

	t.Run("StringVarPDefault", func(t *testing.T) {
		var ptr string
		flagset, name, shorthand, env, usage := randomFlag()
		def, _ := cryptorand.String(10)

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
		envValue, _ := cryptorand.String(10)
		t.Setenv(env, envValue)
		def, _ := cryptorand.String(10)

		cliflag.StringVarP(flagset, &ptr, name, shorthand, env, def, usage)
		got, err := flagset.GetString(name)
		require.NoError(t, err)
		require.Equal(t, envValue, got)
	})

	t.Run("EmptyEnvVar", func(t *testing.T) {
		var ptr string
		flagset, name, shorthand, _, usage := randomFlag()
		def, _ := cryptorand.String(10)

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
		def, _ := cryptorand.Intn(10)

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
		envValue, _ := cryptorand.Intn(10)
		t.Setenv(env, strconv.FormatUint(uint64(envValue), 10))
		def, _ := cryptorand.Intn(10)

		cliflag.Uint8VarP(flagset, &ptr, name, shorthand, env, uint8(def), usage)
		got, err := flagset.GetUint8(name)
		require.NoError(t, err)
		require.Equal(t, uint8(envValue), got)
	})

	t.Run("UInt8FailParse", func(t *testing.T) {
		var ptr uint8
		flagset, name, shorthand, env, usage := randomFlag()
		envValue, _ := cryptorand.String(10)
		t.Setenv(env, envValue)
		def, _ := cryptorand.Intn(10)

		cliflag.Uint8VarP(flagset, &ptr, name, shorthand, env, uint8(def), usage)
		got, err := flagset.GetUint8(name)
		require.NoError(t, err)
		require.Equal(t, uint8(def), got)
	})

	t.Run("IntDefault", func(t *testing.T) {
		var ptr int
		flagset, name, shorthand, env, usage := randomFlag()
		def, _ := cryptorand.Intn(10)

		cliflag.IntVarP(flagset, &ptr, name, shorthand, env, def, usage)
		got, err := flagset.GetInt(name)
		require.NoError(t, err)
		require.Equal(t, def, got)
		require.Contains(t, flagset.FlagUsages(), usage)
		require.Contains(t, flagset.FlagUsages(), fmt.Sprintf("Consumes $%s", env))
	})

	t.Run("IntEnvVar", func(t *testing.T) {
		var ptr int
		flagset, name, shorthand, env, usage := randomFlag()
		envValue, _ := cryptorand.Intn(10)
		t.Setenv(env, strconv.FormatUint(uint64(envValue), 10))
		def, _ := cryptorand.Intn(10)

		cliflag.IntVarP(flagset, &ptr, name, shorthand, env, def, usage)
		got, err := flagset.GetInt(name)
		require.NoError(t, err)
		require.Equal(t, envValue, got)
	})

	t.Run("IntFailParse", func(t *testing.T) {
		var ptr int
		flagset, name, shorthand, env, usage := randomFlag()
		envValue, _ := cryptorand.String(10)
		t.Setenv(env, envValue)
		def, _ := cryptorand.Intn(10)

		cliflag.IntVarP(flagset, &ptr, name, shorthand, env, def, usage)
		got, err := flagset.GetInt(name)
		require.NoError(t, err)
		require.Equal(t, def, got)
	})

	t.Run("BoolDefault", func(t *testing.T) {
		var ptr bool
		flagset, name, shorthand, env, usage := randomFlag()
		def, _ := cryptorand.Intn(1)
		boolDef := def == 1

		cliflag.BoolVarP(flagset, &ptr, name, shorthand, env, boolDef, usage)
		got, err := flagset.GetBool(name)
		require.NoError(t, err)
		require.Equal(t, boolDef, got)
		require.Contains(t, flagset.FlagUsages(), usage)
		require.Contains(t, flagset.FlagUsages(), fmt.Sprintf("Consumes $%s", env))
	})

	t.Run("BoolEnvVar", func(t *testing.T) {
		var ptr bool
		flagset, name, shorthand, env, usage := randomFlag()
		num, _ := cryptorand.Intn(1)
		envValue := num == 1
		t.Setenv(env, strconv.FormatBool(envValue))
		defNum, _ := cryptorand.Intn(1)

		def := defNum == 1

		cliflag.BoolVarP(flagset, &ptr, name, shorthand, env, def, usage)
		got, err := flagset.GetBool(name)
		require.NoError(t, err)
		require.Equal(t, envValue, got)
	})

	t.Run("BoolFailParse", func(t *testing.T) {
		var ptr bool
		flagset, name, shorthand, env, usage := randomFlag()
		envValue, _ := cryptorand.String(10)
		t.Setenv(env, envValue)
		defNum, _ := cryptorand.Intn(1)
		def := defNum == 1
		cliflag.BoolVarP(flagset, &ptr, name, shorthand, env, def, usage)
		got, err := flagset.GetBool(name)
		require.NoError(t, err)
		require.Equal(t, def, got)
	})

	t.Run("DurationDefault", func(t *testing.T) {
		var ptr time.Duration
		flagset, name, shorthand, env, usage := randomFlag()
		def := 10 * time.Second

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
		envValue := 10 * time.Second
		t.Setenv(env, envValue.String())
		def := 10 * time.Minute

		cliflag.DurationVarP(flagset, &ptr, name, shorthand, env, def, usage)
		got, err := flagset.GetDuration(name)
		require.NoError(t, err)
		require.Equal(t, envValue, got)
	})

	t.Run("DurationFailParse", func(t *testing.T) {
		var ptr time.Duration
		flagset, name, shorthand, env, usage := randomFlag()
		envValue, _ := cryptorand.String(10)
		t.Setenv(env, envValue)
		def := 10 * time.Minute

		cliflag.DurationVarP(flagset, &ptr, name, shorthand, env, def, usage)
		got, err := flagset.GetDuration(name)
		require.NoError(t, err)
		require.Equal(t, def, got)
	})
}

func randomFlag() (*pflag.FlagSet, string, string, string, string) {
	fsname, _ := cryptorand.String(10)
	flagset := pflag.NewFlagSet(fsname, pflag.PanicOnError)
	name, _ := cryptorand.String(10)
	shorthand, _ := cryptorand.String(1)
	env, _ := cryptorand.String(10)
	usage, _ := cryptorand.String(10)

	return flagset, name, shorthand, env, usage
}
