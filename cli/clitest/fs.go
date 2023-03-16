package clitest

import (
	"testing"

	"github.com/coder/envbox/sysboxutil"
	"github.com/coder/envbox/xunix"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func MockSysboxManagerReady(t *testing.T, fs afero.Fs) {
	err := afero.WriteFile(fs, sysboxutil.ManagerSocketPath, []byte(""), 0o644)
	require.NoError(t, err)
}

func MockCPUCGroups(t *testing.T, fs afero.Fs, quota, period string) {
	err := afero.WriteFile(fs, xunix.CPUPeriodPath, []byte(period), 0o600)
	require.NoError(t, err)

	err = afero.WriteFile(fs, xunix.CPUQuotaPath, []byte(quota), 0o600)
	require.NoError(t, err)
}
