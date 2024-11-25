package xunixfake

import (
	"io/fs"
	"os"
	"strconv"

	"github.com/spf13/afero"
	"golang.org/x/xerrors"
)

type FileOwner struct {
	UID int
	GID int
}

func NewMemFS() *MemFS {
	return &MemFS{
		MemMapFs: &afero.MemMapFs{},
		Owner:    map[string]FileOwner{},
	}
}

type MemFS struct {
	*afero.MemMapFs
	Owner map[string]FileOwner
}

func (m *MemFS) Mknod(path string, mode uint32, dev int) error {
	return afero.WriteFile(m.MemMapFs, path, []byte(strconv.Itoa(dev)), os.FileMode(mode))
}

// This is so annoying...
func (m *MemFS) Chown(path string, uid int, gid int) error {
	err := m.MemMapFs.Chown(path, uid, gid)
	if err != nil {
		return xerrors.Errorf("chown: %w", err)
	}
	m.Owner[path] = FileOwner{
		UID: uid,
		GID: gid,
	}
	return nil
}

func (m *MemFS) GetFileOwner(path string) (FileOwner, bool) {
	owner, ok := m.Owner[path]
	return owner, ok
}

// LStat doesn't follow symbolic links since this is a in-mem fake.
func (m *MemFS) LStat(path string) (fs.FileInfo, error) {
	return m.MemMapFs.Stat(path)
}

// Readlink doesn't actually read symbolic links since this is a in-mem
// fake.
func (*MemFS) Readlink(path string) (string, error) {
	return path, nil
}
