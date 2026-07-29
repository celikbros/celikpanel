//go:build linux

package systemsqlite

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func openPinnedManagedFile(path string, writable bool) (*os.File, error) {
	flags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW
	if writable {
		flags = unix.O_RDWR | unix.O_CLOEXEC | unix.O_NOFOLLOW
	}
	fd, err := unix.Open(path, flags, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), filepath.Base(path))
	if file == nil {
		_ = unix.Close(fd)
		return nil, errors.New("could not wrap managed database descriptor")
	}
	return file, nil
}

func pinnedManagedFilePath(file *os.File, _ string) string {
	return fmt.Sprintf("/proc/self/fd/%d", file.Fd())
}

func pinnedManagedDirectoryEntryPath(directory *os.File, name string) (string, error) {
	if directory == nil || int(directory.Fd()) != OwnerWorkerWorkspaceFD ||
		name == "" || filepath.Base(name) != name {
		return "", errors.New("managed workspace entry is unsafe")
	}
	return fmt.Sprintf("/proc/self/fd/%d/%s", directory.Fd(), name), nil
}

func validateSingleLink(file *os.File) error {
	var stat unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &stat); err != nil {
		return err
	}
	if stat.Nlink != 1 {
		return errors.New("managed database has hard links")
	}
	return nil
}
