package xunix

import "strings"

func IsNoSpaceErr(err error) bool {
	return errStringContains(err, "no space left on device")
}

func IsInputOutputErr(err error) bool {
	return errStringContains(err, "input/output error")
}

func errStringContains(err error, substr string) bool {
	if err == nil {
		return false
	}

	return strings.Contains(strings.ToLower(err.Error()), substr)
}
