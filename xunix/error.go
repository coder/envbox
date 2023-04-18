package xunix

import "strings"

func IsNoSpaceErr(err error) bool {
	if err == nil {
		return false
	}

	return strings.Contains(strings.ToLower(err.Error()), "no space left on device")
}
