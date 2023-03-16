package xunix

import (
	"context"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
	"golang.org/x/xerrors"
)

type DeviceType string

const (
	DeviceTypeChar = "c"
)

const (
	charDevMode = 0o666
	// The file type constant of a character-oriented device file
	charFileType = unix.S_IFCHR
)

type Device struct {
	Path     string
	Type     string
	Major    int64
	Minor    int64
	FileMode os.FileMode
	UID      int32
	GID      int32
}

func CreateTUNDevice(ctx context.Context, path string) (Device, error) {
	const (
		major uint = 10
		// See https://github.com/torvalds/linux/blob/master/Documentation/admin-guide/devices.txt#L336
		minor uint = 200
	)

	// TODO offset (from legacy.go)
	err := createDevice(ctx, deviceConfig{
		path:  path,
		mode:  charDevMode,
		dev:   dev(major, minor),
		major: major,
		minor: minor,
		ftype: charFileType,
	})
	if err != nil {
		return Device{}, xerrors.Errorf("create device: %w", err)
	}

	return Device{
		Path:     path,
		Type:     DeviceTypeChar,
		Major:    int64(major),
		Minor:    int64(minor),
		FileMode: charDevMode,
	}, nil
}

func CreateFuseDevice(ctx context.Context, path string) (Device, error) {
	const (
		major uint = 10

		// See https://github.com/torvalds/linux/blob/master/Documentation/admin-guide/devices.txt#L365
		minor uint = 229
	)

	err := createDevice(ctx, deviceConfig{
		path:  path,
		mode:  charDevMode,
		dev:   dev(major, minor),
		major: major,
		minor: minor,
		ftype: charFileType,
	})

	if err != nil {
		return Device{}, xerrors.Errorf("create device: %w", err)
	}

	return Device{
		Path:     path,
		Type:     DeviceTypeChar,
		Major:    int64(major),
		Minor:    int64(minor),
		FileMode: charDevMode,
	}, nil
}

type deviceConfig struct {
	path  string
	mode  uint32
	dev   uint
	major uint
	minor uint
	ftype uint32
}

func createDevice(ctx context.Context, conf deviceConfig) error {
	var (
		fs  = GetFS(ctx)
		dir = filepath.Dir(conf.path)
	)

	err := fs.MkdirAll(dir, 0700)
	if err != nil {
		return xerrors.Errorf("ensure parent dir: %w", err)
	}

	err = fs.Mknod(conf.path, conf.ftype|conf.mode, int(conf.dev))
	if err != nil {
		return xerrors.Errorf("mknod %s c %d %d: %w", conf.path, conf.major, conf.minor, err)
	}

	err = fs.Chmod(conf.path, os.FileMode(conf.mode))
	if err != nil {
		return xerrors.Errorf("chown %v %q: %w", conf.mode, conf.path, err)
	}

	return nil
}

func dev(major, minor uint) uint {
	// This is lifted from the Linux kernel's makedev function.
	return ((major & 0xfff) << 8) | (minor & 0xff)
}
