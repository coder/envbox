package xunixfake

import (
	"os"
	"strconv"

	"github.com/spf13/afero"
	"golang.org/x/xerrors"
)

type FileOwner struct {
	UID int
	GID int
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
