//go:build !linux

package main

import (
	"fmt"
	"os"
)

func secureMailFileExists(path string) (bool, error) {
	return false, fmt.Errorf("secure mail-map access is unavailable on this operating system: %s", path)
}

func secureChmodMailFile(path string, _ os.FileMode) error {
	return fmt.Errorf("secure mail-map access is unavailable on this operating system: %s", path)
}

func secureSetMailFileMetadata(path string, _ os.FileMode, _, _ int) error {
	return fmt.Errorf("secure mail-map access is unavailable on this operating system: %s", path)
}

func secureSetMailDirectoryMetadata(path string, _ os.FileMode, _, _ int) error {
	return fmt.Errorf("secure mail-directory access is unavailable on this operating system: %s", path)
}

func secureSnapshotMailFile(path string) ([]byte, os.FileMode, int, int, error) {
	return nil, 0, 0, 0, fmt.Errorf("secure mail-map access is unavailable on this operating system: %s", path)
}
