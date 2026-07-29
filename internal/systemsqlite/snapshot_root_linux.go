//go:build linux

package systemsqlite

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

func validateSnapshotDirectory(fd int, private bool) error {
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR || stat.Uid != uint32(os.Geteuid()) {
		return errors.New("system SQLite snapshot storage has an untrusted owner")
	}
	if stat.Mode&0o022 != 0 || private && stat.Mode&0o077 != 0 {
		return errors.New("system SQLite snapshot storage permissions are not private")
	}
	return nil
}

func prepareSnapshotRoot(path string) error {
	clean := filepath.Clean(strings.TrimSpace(path))
	if clean == "." || !filepath.IsAbs(clean) || filepath.Dir(clean) == clean {
		return errors.New("system SQLite snapshot root is not absolute")
	}
	parent := filepath.Dir(clean)
	if err := rejectSymlinkComponents(parent); err != nil {
		return errors.New("system SQLite snapshot storage path is unsafe")
	}
	parentFD, err := unix.Open(parent, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return errors.New("system SQLite snapshot storage parent is not available")
	}
	defer unix.Close(parentFD)
	if err := validateSnapshotDirectory(parentFD, false); err != nil {
		return err
	}
	if err := unix.Mkdirat(parentFD, filepath.Base(clean), 0o700); err != nil && !errors.Is(err, unix.EEXIST) {
		return errors.New("could not create private system SQLite snapshot storage")
	}
	return validateSnapshotRootAt(parentFD, filepath.Base(clean))
}

func validateSnapshotRootAt(parentFD int, name string) error {
	fd, err := unix.Openat(
		parentFD,
		name,
		unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		0,
	)
	if err != nil {
		return errors.New("system SQLite snapshot storage is not a safe directory")
	}
	defer unix.Close(fd)
	if err := validateSnapshotDirectory(fd, true); err != nil {
		return err
	}
	return nil
}
