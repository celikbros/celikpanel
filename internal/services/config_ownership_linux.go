//go:build linux

package services

import (
	"fmt"
	"os"
	"syscall"
)

type managedFileOwnership struct {
	present bool
	uid     int
	gid     int
}

func managedOwnershipFromInfo(info os.FileInfo) (managedFileOwnership, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return managedFileOwnership{}, fmt.Errorf("unsupported file ownership metadata")
	}
	return managedFileOwnership{present: true, uid: int(stat.Uid), gid: int(stat.Gid)}, nil
}

func applyManagedOwnership(file *os.File, ownership managedFileOwnership) error {
	if !ownership.present {
		return nil
	}
	return file.Chown(ownership.uid, ownership.gid)
}

func syncManagedConfigDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	if err := directory.Sync(); err != nil {
		_ = directory.Close()
		return err
	}
	return directory.Close()
}
