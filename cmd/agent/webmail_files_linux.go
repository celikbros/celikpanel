//go:build linux

package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func validateRoundcubeTreePath(path string) (string, error) {
	clean, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	clean = filepath.Clean(clean)
	if clean == string(filepath.Separator) || filepath.Dir(clean) == clean {
		return "", fmt.Errorf("unsafe Roundcube tree path: %s", path)
	}
	if err := rejectSymlinkPath(filepath.Dir(clean)); err != nil {
		return "", fmt.Errorf("unsafe Roundcube tree parent: %w", err)
	}
	return clean, nil
}

func publishRoundcubeStage(stage, final string) error {
	stage, err := validateRoundcubeTreePath(stage)
	if err != nil {
		return err
	}
	final, err = validateRoundcubeTreePath(final)
	if err != nil {
		return err
	}
	if filepath.Dir(stage) != filepath.Dir(final) {
		return fmt.Errorf("Roundcube staging and final directories must share a parent")
	}
	stageInfo, err := os.Lstat(stage)
	if err != nil {
		return fmt.Errorf("inspect Roundcube staging tree: %w", err)
	}
	if stageInfo.Mode()&os.ModeSymlink != 0 || !stageInfo.IsDir() {
		return fmt.Errorf("Roundcube staging path is not a real directory")
	}
	if err := unix.Renameat2(unix.AT_FDCWD, stage, unix.AT_FDCWD, final, unix.RENAME_NOREPLACE); err != nil {
		return fmt.Errorf("publish Roundcube tree without replacement: %w", err)
	}
	if err := syncRoundcubeParent(filepath.Dir(final)); err != nil {
		if rollbackErr := retireRoundcubeTree(final); rollbackErr != nil {
			return fmt.Errorf("%w; Roundcube publish rollback failed: %v", err, rollbackErr)
		}
		return err
	}
	return nil
}

func retireRoundcubeTree(path string) error {
	clean, err := validateRoundcubeTreePath(path)
	if err != nil {
		return err
	}
	info, err := os.Lstat(clean)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect Roundcube tree: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("refuse to remove non-directory Roundcube path: %s", clean)
	}

	parent := filepath.Dir(clean)
	base := filepath.Base(clean)
	var retired string
	for attempt := 0; attempt < 16; attempt++ {
		random := make([]byte, 12)
		if _, err := rand.Read(random); err != nil {
			return fmt.Errorf("prepare Roundcube retirement: %w", err)
		}
		retired = filepath.Join(parent, "."+base+".retired-"+hex.EncodeToString(random))
		err = unix.Renameat2(unix.AT_FDCWD, clean, unix.AT_FDCWD, retired, unix.RENAME_NOREPLACE)
		if errors.Is(err, unix.EEXIST) {
			continue
		}
		break
	}
	if err != nil {
		return fmt.Errorf("retire Roundcube tree: %w", err)
	}
	if err := syncRoundcubeParent(parent); err != nil {
		return err
	}
	if err := os.RemoveAll(retired); err != nil {
		return fmt.Errorf("remove retired Roundcube tree %s: %w", retired, err)
	}
	return syncRoundcubeParent(parent)
}

func syncRoundcubeParent(path string) error {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("open Roundcube parent directory: %w", err)
	}
	defer unix.Close(fd)
	if err := unix.Fsync(fd); err != nil {
		return fmt.Errorf("sync Roundcube parent directory: %w", err)
	}
	return nil
}
