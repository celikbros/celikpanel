//go:build !linux

package systemsqlite

import (
	"errors"
	"os"
)

func openPinnedManagedFile(path string, writable bool) (*os.File, error) {
	flags := os.O_RDONLY
	if writable {
		flags = os.O_RDWR
	}
	return os.OpenFile(path, flags, 0)
}

func pinnedManagedFilePath(_ *os.File, path string) string {
	return path
}

func pinnedManagedDirectoryEntryPath(*os.File, string) (string, error) {
	return "", errors.New("managed workspace descriptor paths require Linux")
}

func validateSingleLink(file *os.File) error {
	return nil
}
