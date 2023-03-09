package clitest

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/vishvananda/netlink"
)

func GetNetLink(t *testing.T) netlink.Link {
	t.Helper()

	addrs, err := netlink.AddrList(nil, netlink.FAMILY_V4)
	require.NoError(t, err)

	for _, addr := range addrs {
		if !addr.IP.IsGlobalUnicast() || addr.IP.To4() == nil || addr.Label == "docker0" {
			continue
		}

		nl, err := netlink.LinkByName(addr.Label)
		require.NoError(t, err)
		return nl
	}

	t.Fatalf("failed to find a valid network interface")
	return nil
}
