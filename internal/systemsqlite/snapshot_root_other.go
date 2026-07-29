//go:build !linux

package systemsqlite

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

func prepareSnapshotRoot(path string) error {
	clean := filepath.Clean(strings.TrimSpace(path))
	if clean == "." || !filepath.IsAbs(clean) {
		return errors.New("system SQLite snapshot root is not absolute")
	}
	if err := rejectSymlinkComponents(filepath.Dir(clean)); err != nil {
		return errors.New("system SQLite snapshot storage path is unsafe")
	}
	if err := os.Mkdir(clean, 0o700); err != nil && !os.IsExist(err) {
		return errors.New("could not create private system SQLite snapshot storage")
	}
	info, err := os.Lstat(clean)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("system SQLite snapshot storage is not a safe directory")
	}
	return nil
}
