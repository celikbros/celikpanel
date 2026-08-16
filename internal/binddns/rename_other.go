//go:build !linux

package binddns

import (
	"errors"
	"os"
)

func renameNoReplace(oldName, newName string) error {
	if _, err := os.Lstat(newName); err == nil {
		return os.ErrExist
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Rename(oldName, newName)
}
