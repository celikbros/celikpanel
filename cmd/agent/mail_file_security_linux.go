//go:build linux

package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"golang.org/x/sys/unix"
)

func secureMailFileExists(path string) (bool, error) {
	fd, err := openSecureMailFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	_ = unix.Close(fd)
	return true, nil
}

func secureChmodMailFile(path string, mode os.FileMode) error {
	fd, err := openSecureMailFile(path)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	if err := unix.Fchmod(fd, uint32(mode.Perm())); err != nil {
		return fmt.Errorf("set mail map mode %s: %w", path, err)
	}
	return nil
}

func secureSetMailFileMetadata(path string, mode os.FileMode, uid, gid int) error {
	fd, err := openSecureMailFile(path)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	if err := unix.Fchown(fd, uid, gid); err != nil {
		return fmt.Errorf("set mail file ownership %s: %w", path, err)
	}
	if err := unix.Fchmod(fd, uint32(mode.Perm())); err != nil {
		return fmt.Errorf("set mail file mode %s: %w", path, err)
	}
	return nil
}

func secureSetMailDirectoryMetadata(path string, mode os.FileMode, uid, gid int) error {
	relative, err := secureConfigRelativePath(path)
	if err != nil {
		return err
	}
	rootFD, err := openSecureConfigRoot()
	if err != nil {
		return err
	}
	defer unix.Close(rootFD)

	fd, err := unix.Openat2(rootFD, relative, &unix.OpenHow{
		Flags:   uint64(unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW),
		Resolve: secureConfigResolve,
	})
	if err != nil {
		return secureConfigOpenError("open mail directory", path, err)
	}
	defer unix.Close(fd)
	if err := unix.Fchown(fd, uid, gid); err != nil {
		return fmt.Errorf("set mail directory ownership %s: %w", path, err)
	}
	if err := unix.Fchmod(fd, uint32(mode.Perm())); err != nil {
		return fmt.Errorf("set mail directory mode %s: %w", path, err)
	}
	return nil
}

func secureSnapshotMailFile(path string) ([]byte, os.FileMode, int, int, error) {
	fd, err := openSecureMailFile(path)
	if err != nil {
		return nil, 0, 0, 0, err
	}
	file := os.NewFile(uintptr(fd), path+" (mail snapshot)")
	if file == nil {
		_ = unix.Close(fd)
		return nil, 0, 0, 0, fmt.Errorf("snapshot mail file %s: invalid file descriptor", path)
	}
	defer file.Close()

	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return nil, 0, 0, 0, fmt.Errorf("stat mail file %s: %w", path, err)
	}
	content, err := io.ReadAll(file)
	if err != nil {
		return nil, 0, 0, 0, fmt.Errorf("read mail file %s: %w", path, err)
	}
	return content, os.FileMode(stat.Mode & 0o7777), int(stat.Uid), int(stat.Gid), nil
}

func openSecureMailFile(path string) (int, error) {
	relative, err := secureConfigRelativePath(path)
	if err != nil {
		return -1, err
	}
	rootFD, err := openSecureConfigRoot()
	if err != nil {
		return -1, err
	}
	defer unix.Close(rootFD)

	fd, err := unix.Openat2(rootFD, relative, &unix.OpenHow{
		Flags:   uint64(unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK),
		Resolve: secureConfigResolve,
	})
	if err != nil {
		return -1, secureConfigOpenError("open mail map", path, err)
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		_ = unix.Close(fd)
		return -1, fmt.Errorf("stat mail map %s: %w", path, err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG {
		_ = unix.Close(fd)
		return -1, fmt.Errorf("mail map is not a regular file: %s", path)
	}
	return fd, nil
}
