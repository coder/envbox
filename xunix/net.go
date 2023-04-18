package xunix

import (
	"github.com/vishvananda/netlink"
	"golang.org/x/xerrors"
)

func NetlinkMTU(name string) (int, error) {
	defaultLink, err := netlink.LinkByName(name)
	if err != nil {
		return 0, xerrors.Errorf("get %s: %w", name, err)
	}

	return defaultLink.Attrs().MTU, nil
}
