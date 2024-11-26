package dockerutil

import (
	"net"

	"golang.org/x/xerrors"
)

var DefaultBridgeCIDR = "172.19.0.1/30"

func BridgeIPFromCIDR(cidr string) (net.IP, int) {
	ipNet := mustParseIPv4Net(cidr)
	prefixLen, _ := ipNet.Mask.Size()
	bridgeIP := mustNextIPv4(ipNet.IP, 1)
	return bridgeIP, prefixLen
}

func mustParseIPv4Net(s string) *net.IPNet {
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		panic(err)
	}
	if n.IP.To4() == nil {
		panic(xerrors.New("must specify an IPv4 network"))
	}
	return n
}

// Adapted from https://gist.github.com/udhos/b468fbfd376aa0b655b6b0c539a88c03#file-nextip-go-L31
func mustNextIPv4(ip net.IP, inc int) net.IP {
	ip4 := ip.To4()
	if ip4 == nil {
		panic(xerrors.Errorf("invalid IPv4 addr %s", ip.String()))
	}
	v := uint32(ip4[0]) << 24
	v += uint32(ip4[1]) << 16
	v += uint32(ip4[2]) << 8
	v += uint32(ip4[3])
	//nolint:gosec
	v += uint32(inc)
	v3 := byte(v & 0xFF)
	v2 := byte((v >> 8) & 0xFF)
	v1 := byte((v >> 16) & 0xFF)
	v0 := byte((v >> 24) & 0xFF)
	return net.IPv4(v0, v1, v2, v3)
}
