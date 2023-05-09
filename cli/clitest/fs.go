package clitest

import (
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/coder/envbox/sysboxutil"
	"github.com/coder/envbox/xunix"
)

func FakeSysboxManagerReady(t *testing.T, fs afero.Fs) {
	err := afero.WriteFile(fs, sysboxutil.ManagerSocketPath, []byte(""), 0o644)
	require.NoError(t, err)
}

func FakeCPUGroups(t *testing.T, fs afero.Fs, quota, period string) {
	err := afero.WriteFile(fs, xunix.CPUPeriodPathCGroupV1, []byte(period), 0o600)
	require.NoError(t, err)

	err = afero.WriteFile(fs, xunix.CPUQuotaPathCGroupV1, []byte(quota), 0o600)
	require.NoError(t, err)
}
