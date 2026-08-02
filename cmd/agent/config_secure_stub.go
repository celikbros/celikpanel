//go:build !linux

package main

import (
	"fmt"
	"os"
)

func secureReadConfig(path string) ([]byte, error) {
	return nil, fmt.Errorf("secure managed configuration access is unavailable on this operating system: %s", path)
}

func secureWriteConfig(path string, _ []byte, _ os.FileMode) error {
	return fmt.Errorf("secure managed configuration access is unavailable on this operating system: %s", path)
}

func secureRemoveConfig(path string) error {
	return fmt.Errorf("secure managed configuration access is unavailable on this operating system: %s", path)
}
