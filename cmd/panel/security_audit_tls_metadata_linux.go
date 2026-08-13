//go:build linux

package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func readPinnedPanelTLSFiles(certPath, keyPath string, certMaximum, keyMaximum int64) ([]byte, []byte, error) {
	if certPath == "" || keyPath == "" || certMaximum < 1 || keyMaximum < 1 ||
		!filepath.IsAbs(certPath) || !filepath.IsAbs(keyPath) ||
		filepath.Clean(certPath) != certPath || filepath.Clean(keyPath) != keyPath {
		return nil, nil, fmt.Errorf("%w: TLS paths are not canonical and absolute", errPanelTLSMetadataUnsafe)
	}
	directory := filepath.Dir(certPath)
	if filepath.Dir(keyPath) != directory {
		return nil, nil, fmt.Errorf("%w: TLS certificate and key must share one pinned directory", errPanelTLSMetadataUnsafe)
	}
	certBase, keyBase := filepath.Base(certPath), filepath.Base(keyPath)
	if certBase == "." || keyBase == "." || certBase == string(filepath.Separator) ||
		keyBase == string(filepath.Separator) || certBase == keyBase {
		return nil, nil, fmt.Errorf("%w: TLS file names are invalid", errPanelTLSMetadataUnsafe)
	}
	directoryFD, err := unix.Open(directory, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		if errors.Is(err, unix.ELOOP) {
			return nil, nil, fmt.Errorf("%w: TLS directory is a symbolic link", errPanelTLSMetadataUnsafe)
		}
		return nil, nil, err
	}
	defer unix.Close(directoryFD)
	var directoryBefore, directoryLinked unix.Stat_t
	if err := unix.Fstat(directoryFD, &directoryBefore); err != nil {
		return nil, nil, err
	}
	if err := unix.Lstat(directory, &directoryLinked); err != nil {
		return nil, nil, err
	}
	if !safePanelTLSDirectoryMetadata(&directoryBefore) ||
		!samePanelTLSDirectoryStat(&directoryBefore, &directoryLinked) {
		return nil, nil, fmt.Errorf("%w: TLS directory is replaceable or has unsafe metadata", errPanelTLSMetadataUnsafe)
	}
	certRaw, err := readPinnedPanelTLSFileAt(directoryFD, certPath, certBase, certMaximum, false)
	if err != nil {
		return nil, nil, err
	}
	keyRaw, err := readPinnedPanelTLSFileAt(directoryFD, keyPath, keyBase, keyMaximum, true)
	if err != nil {
		return nil, nil, err
	}
	var directoryAfter, directoryRelinked unix.Stat_t
	if err := unix.Fstat(directoryFD, &directoryAfter); err != nil {
		return nil, nil, err
	}
	if err := unix.Lstat(directory, &directoryRelinked); err != nil {
		return nil, nil, err
	}
	if !samePanelTLSDirectoryStat(&directoryBefore, &directoryAfter) ||
		!samePanelTLSDirectoryStat(&directoryBefore, &directoryRelinked) {
		return nil, nil, fmt.Errorf("%w: TLS directory binding changed while reading", errPanelTLSMetadataUnsafe)
	}
	return certRaw, keyRaw, nil
}

func readPinnedPanelTLSFileAt(directoryFD int, path, base string, maximum int64, private bool) ([]byte, error) {
	fd, err := unix.Openat(directoryFD, base, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		if errors.Is(err, unix.ELOOP) {
			return nil, fmt.Errorf("%w: TLS file is a symbolic link", errPanelTLSMetadataUnsafe)
		}
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return nil, errors.New("open pinned panel TLS file")
	}
	defer file.Close()

	var before unix.Stat_t
	if err := unix.Fstat(fd, &before); err != nil {
		return nil, err
	}
	if !safePanelTLSFileMetadata(&before, maximum, private) {
		return nil, fmt.Errorf("%w: TLS file ownership, mode, links, type, or size is unsafe", errPanelTLSMetadataUnsafe)
	}
	raw, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) != before.Size || int64(len(raw)) > maximum {
		return nil, errors.New("panel TLS file changed while reading")
	}

	var after, linked unix.Stat_t
	if err := unix.Fstat(fd, &after); err != nil {
		return nil, err
	}
	if err := unix.Fstatat(directoryFD, base, &linked, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return nil, err
	}
	if !samePanelTLSFileStat(&before, &after) || !samePanelTLSFileStat(&before, &linked) {
		return nil, fmt.Errorf("%w: TLS file binding changed while reading", errPanelTLSMetadataUnsafe)
	}
	return raw, nil
}

func safePanelTLSFileMetadata(stat *unix.Stat_t, maximum int64, private bool) bool {
	permissionsSafe := stat.Mode&0o022 == 0
	if private {
		permissionsSafe = stat.Mode&0o037 == 0 &&
			(stat.Mode&0o040 == 0 || stat.Gid == uint32(os.Getegid()))
	}
	return stat.Mode&unix.S_IFMT == unix.S_IFREG && stat.Uid == 0 && stat.Nlink == 1 &&
		permissionsSafe && stat.Size >= 1 && stat.Size <= maximum
}

func safePanelTLSDirectoryMetadata(stat *unix.Stat_t) bool {
	return stat.Mode&unix.S_IFMT == unix.S_IFDIR && stat.Uid == 0 && stat.Nlink >= 1 && stat.Mode&0o022 == 0
}

func samePanelTLSDirectoryStat(left, right *unix.Stat_t) bool {
	return left.Dev == right.Dev && left.Ino == right.Ino && left.Mode == right.Mode &&
		left.Nlink == right.Nlink && left.Uid == right.Uid && left.Gid == right.Gid
}

func samePanelTLSFileStat(left, right *unix.Stat_t) bool {
	return samePanelTLSDirectoryStat(left, right) && left.Size == right.Size &&
		left.Mtim == right.Mtim && left.Ctim == right.Ctim
}
