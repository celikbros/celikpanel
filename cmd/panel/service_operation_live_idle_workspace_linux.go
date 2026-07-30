//go:build linux

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

const liveIdleTemporaryRoot = "/tmp"

func createSecureLiveIdleTemporaryDirectory() (directoryPath string, returnErr error) {
	rootInfo, err := validateLiveIdleTemporaryRoot(liveIdleTemporaryRoot)
	if err != nil {
		return "", err
	}
	root, err := os.Open(liveIdleTemporaryRoot)
	if err != nil {
		return "", fmt.Errorf("open fixed temporary root: %w", err)
	}
	defer func() {
		if closeErr := root.Close(); returnErr == nil && closeErr != nil {
			returnErr = closeErr
		}
	}()
	pinnedRootInfo, err := root.Stat()
	if err != nil {
		return "", fmt.Errorf("stat pinned temporary root: %w", err)
	}
	if !os.SameFile(rootInfo, pinnedRootInfo) {
		return "", fmt.Errorf("fixed temporary root changed while it was opened")
	}

	directoryPath, err = os.MkdirTemp(liveIdleTemporaryRoot, "celikpanel-live-idle-")
	if err != nil {
		return "", fmt.Errorf("create temporary directory below fixed root: %w", err)
	}
	defer func() {
		if returnErr != nil {
			_ = os.RemoveAll(directoryPath)
			directoryPath = ""
		}
	}()
	if err := os.Chmod(directoryPath, 0o700); err != nil {
		return "", fmt.Errorf("protect temporary directory: %w", err)
	}

	currentRootInfo, err := validateLiveIdleTemporaryRoot(liveIdleTemporaryRoot)
	if err != nil {
		return "", err
	}
	if !os.SameFile(pinnedRootInfo, currentRootInfo) {
		return "", fmt.Errorf("fixed temporary root changed during directory creation")
	}
	childInfo, err := os.Lstat(directoryPath)
	if err != nil {
		return "", fmt.Errorf("inspect temporary directory: %w", err)
	}
	if childInfo.Mode()&os.ModeSymlink != 0 || !childInfo.IsDir() {
		return "", fmt.Errorf("temporary workspace is not a real directory")
	}
	if childInfo.Mode().Perm() != 0o700 {
		return "", fmt.Errorf("temporary workspace mode is %04o, expected 0700", childInfo.Mode().Perm())
	}
	childStat, ok := childInfo.Sys().(*syscall.Stat_t)
	if !ok {
		return "", fmt.Errorf("temporary workspace ownership metadata is unavailable")
	}
	if int(childStat.Uid) != os.Geteuid() || int(childStat.Gid) != os.Getegid() {
		return "", fmt.Errorf(
			"temporary workspace owner is %d:%d, expected %d:%d",
			childStat.Uid,
			childStat.Gid,
			os.Geteuid(),
			os.Getegid(),
		)
	}
	if filepath.Dir(directoryPath) != liveIdleTemporaryRoot {
		return "", fmt.Errorf("temporary workspace escaped the fixed root")
	}
	return directoryPath, nil
}

func validateLiveIdleTemporaryRoot(path string) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect fixed temporary root: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, fmt.Errorf("fixed temporary root is not a real directory")
	}
	if info.Mode().Perm() != 0o777 || info.Mode()&os.ModeSticky == 0 {
		return nil, fmt.Errorf(
			"fixed temporary root mode is %04o, expected sticky 01777",
			info.Mode().Perm(),
		)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil, fmt.Errorf("fixed temporary root ownership metadata is unavailable")
	}
	if stat.Uid != 0 || stat.Gid != 0 {
		return nil, fmt.Errorf(
			"fixed temporary root owner is %d:%d, expected 0:0",
			stat.Uid,
			stat.Gid,
		)
	}
	return info, nil
}
