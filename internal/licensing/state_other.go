//go:build !linux

package licensing

import (
	"os"
)

func openState(path string) (*os.File, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errInvalidState
	}
	return os.Open(path)
}
