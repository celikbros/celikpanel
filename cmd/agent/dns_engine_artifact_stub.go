//go:build !linux

package main

import (
	"errors"
	"os"
)

func removePDNSSwitchArtifact(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("PowerDNS switch artifact is not a safe regular file")
	}
	return os.Remove(path)
}
