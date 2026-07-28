//go:build linux

package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

var serviceMutationRequiredOwnerUID uint32 = 0

func secureServiceMutationStat(path string, info os.FileInfo, wantDirectory bool) error {
	if info == nil {
		return errors.New("missing file information")
	}
	if wantDirectory {
		if !info.IsDir() {
			return fmt.Errorf("%s must be a real directory", path)
		}
	} else if !info.Mode().IsRegular() {
		return fmt.Errorf("%s must be a regular file", path)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("inspect owner of %s", path)
	}
	if stat.Uid != serviceMutationRequiredOwnerUID {
		return fmt.Errorf("%s must be owned by uid %d", path, serviceMutationRequiredOwnerUID)
	}
	if wantDirectory {
		if info.Mode().Perm()&0o022 != 0 {
			return fmt.Errorf("%s must not be writable by group or others", path)
		}
		return nil
	}
	if info.Mode().Perm() != 0o600 || stat.Nlink != 1 {
		return fmt.Errorf("%s must be a single-link 0600 file", path)
	}
	return nil
}

func secureServiceMutationStateDirectoryStat(path string, info os.FileInfo) error {
	if err := secureServiceMutationStat(path, info, true); err != nil {
		return err
	}
	if info.Mode().Perm() != 0o700 {
		return fmt.Errorf("%s must be mode 0700", path)
	}
	return nil
}

func ensureSecureServiceMutationStateDirectory(path string) error {
	path = filepath.Clean(path)
	if !filepath.IsAbs(path) {
		return errors.New("service mutation state directory must be absolute")
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		parent := filepath.Dir(path)
		parentInfo, parentErr := os.Lstat(parent)
		if parentErr != nil {
			return fmt.Errorf("inspect state parent: %w", parentErr)
		}
		if err := secureServiceMutationStat(parent, parentInfo, true); err != nil {
			return err
		}
		if err := os.Mkdir(path, 0o700); err != nil {
			return fmt.Errorf("create state directory: %w", err)
		}
		parentFD, openErr := unix.Open(parent, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_DIRECTORY, 0)
		if openErr != nil {
			return fmt.Errorf("open state parent for sync: %w", openErr)
		}
		syncErr := unix.Fsync(parentFD)
		closeErr := unix.Close(parentFD)
		if syncErr != nil {
			return fmt.Errorf("sync state parent: %w", syncErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close state parent: %w", closeErr)
		}
		info, err = os.Lstat(path)
	}
	if err != nil {
		return fmt.Errorf("inspect state directory: %w", err)
	}
	return secureServiceMutationStateDirectoryStat(path, info)
}

func readSecureServiceMutationLedger(path string, maxSize int64) ([]byte, bool, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if errors.Is(err, unix.ENOENT) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("open service mutation ledger: %w", err)
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return nil, false, errors.New("open service mutation ledger file handle")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, false, fmt.Errorf("inspect service mutation ledger: %w", err)
	}
	if err := secureServiceMutationStat(path, info, false); err != nil {
		return nil, false, err
	}
	if info.Size() > maxSize {
		return nil, false, fmt.Errorf("service mutation ledger exceeds %d bytes", maxSize)
	}
	raw, err := io.ReadAll(io.LimitReader(file, maxSize+1))
	if err != nil {
		return nil, false, fmt.Errorf("read service mutation ledger: %w", err)
	}
	if int64(len(raw)) > maxSize {
		return nil, false, fmt.Errorf("service mutation ledger exceeds %d bytes", maxSize)
	}
	return raw, true, nil
}
