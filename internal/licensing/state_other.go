//go:build !linux

package licensing

import (
	"errors"
	"os"
)

func openState(path string) (*os.File, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("invalid license state file")
	}
	return os.Open(path)
}
