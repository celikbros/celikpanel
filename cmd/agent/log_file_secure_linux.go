//go:build linux

package main

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

const rootLogDirectoryOwner = uint32(0)

func secureOpenLogFile(root, relative string, write bool) (*os.File, error) {
	return openLogFileBeneath(root, relative, write, rootLogDirectoryOwner)
}

// openLogFileBeneath anchors resolution at an already-open trusted directory.
// openat2 rejects symlinks and magic links in every component and prevents the
// resolved path from escaping above that directory. expectedOwner is fixed to
// root in production; the parameter exists so unprivileged Linux tests can
// exercise the same resolver against a private temporary directory.
func openLogFileBeneath(root, relative string, write bool, expectedOwner uint32) (*os.File, error) {
	if relative == "" || relative == "." || filepath.IsAbs(relative) {
		return nil, fmt.Errorf("invalid root-relative log path")
	}

	rootFD, err := unix.Open(root, unix.O_PATH|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("open trusted log directory: %w", err)
	}
	defer unix.Close(rootFD)

	var rootStat unix.Stat_t
	if err := unix.Fstat(rootFD, &rootStat); err != nil {
		return nil, fmt.Errorf("inspect trusted log directory: %w", err)
	}
	if rootStat.Mode&unix.S_IFMT != unix.S_IFDIR {
		return nil, fmt.Errorf("trusted log root is not a directory")
	}
	if rootStat.Uid != expectedOwner {
		return nil, fmt.Errorf("trusted log root has unexpected owner uid %d", rootStat.Uid)
	}
	if rootStat.Mode&0o022 != 0 {
		return nil, fmt.Errorf("trusted log root must not be group- or world-writable")
	}

	flags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW
	if write {
		flags = unix.O_WRONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW
	}
	fd, err := unix.Openat2(rootFD, relative, &unix.OpenHow{
		Flags: uint64(flags),
		Resolve: unix.RESOLVE_BENEATH |
			unix.RESOLVE_NO_SYMLINKS |
			unix.RESOLVE_NO_MAGICLINKS,
	})
	if err != nil {
		return nil, fmt.Errorf("resolve log file beneath trusted root: %w", err)
	}

	var fileStat unix.Stat_t
	if err := unix.Fstat(fd, &fileStat); err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("inspect open log file: %w", err)
	}
	if fileStat.Mode&unix.S_IFMT != unix.S_IFREG {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("log path must refer to a regular file")
	}

	file := os.NewFile(uintptr(fd), filepath.Join(root, relative))
	if file == nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("create log file handle")
	}
	return file, nil
}
