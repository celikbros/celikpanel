//go:build linux

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

const defaultCpmoveArchiveRoot = "/var/lib/celikpanel-imports"

var (
	cpmoveArchiveRoot     = defaultCpmoveArchiveRoot
	cpmoveArchiveOwnerUID = uint32(0)
	cpmoveOpenat2         = unix.Openat2
)

func trustedCpmoveRelativePath(archivePath string) (string, error) {
	if archivePath == "" || !filepath.IsAbs(archivePath) ||
		filepath.Clean(archivePath) != archivePath {
		return "", os.ErrPermission
	}
	root := filepath.Clean(cpmoveArchiveRoot)
	if !filepath.IsAbs(root) {
		return "", os.ErrPermission
	}
	relative, err := filepath.Rel(root, archivePath)
	if err != nil || relative == "." || relative == ".." ||
		strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return "", os.ErrPermission
	}
	return filepath.ToSlash(relative), nil
}

func openTrustedCpmoveArchive(archivePath string) (*os.File, error) {
	relative, err := trustedCpmoveRelativePath(archivePath)
	if err != nil {
		return nil, fmt.Errorf("archive must be a file below %s: %w", cpmoveArchiveRoot, err)
	}

	slashFD, err := unix.Open(
		"/",
		unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		0,
	)
	if err != nil {
		return nil, err
	}
	defer unix.Close(slashFD)

	rootRelative := strings.TrimPrefix(filepath.ToSlash(filepath.Clean(cpmoveArchiveRoot)), "/")
	rootFD, err := cpmoveOpenat2(slashFD, rootRelative, &unix.OpenHow{
		Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW,
		Resolve: unix.RESOLVE_BENEATH |
			unix.RESOLVE_NO_SYMLINKS |
			unix.RESOLVE_NO_MAGICLINKS,
	})
	if err != nil {
		if errors.Is(err, unix.ENOSYS) {
			return nil, fmt.Errorf("openat2 is required for cpmove imports: %w", err)
		}
		return nil, err
	}
	defer unix.Close(rootFD)

	var rootStat unix.Stat_t
	if err := unix.Fstat(rootFD, &rootStat); err != nil {
		return nil, err
	}
	if rootStat.Mode&unix.S_IFMT != unix.S_IFDIR || rootStat.Uid != cpmoveArchiveOwnerUID ||
		rootStat.Mode&0o022 != 0 {
		return nil, fmt.Errorf("trusted import root must be a root-owned non-writable directory")
	}

	fd, err := cpmoveOpenat2(rootFD, relative, &unix.OpenHow{
		Flags: unix.O_RDONLY |
			unix.O_CLOEXEC |
			unix.O_NOFOLLOW |
			unix.O_NONBLOCK,
		Resolve: unix.RESOLVE_BENEATH |
			unix.RESOLVE_NO_SYMLINKS |
			unix.RESOLVE_NO_MAGICLINKS |
			unix.RESOLVE_NO_XDEV,
	})
	if err != nil {
		if errors.Is(err, unix.ENOSYS) {
			return nil, fmt.Errorf("openat2 is required for cpmove imports: %w", err)
		}
		return nil, err
	}

	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		unix.Close(fd)
		return nil, err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Nlink != 1 ||
		stat.Uid != cpmoveArchiveOwnerUID || stat.Mode&0o077 != 0 || stat.Size <= 0 {
		unix.Close(fd)
		return nil, fmt.Errorf("archive must be an owner-only, single-link regular file")
	}

	file := os.NewFile(uintptr(fd), filepath.Base(archivePath))
	if file == nil {
		unix.Close(fd)
		return nil, os.ErrInvalid
	}
	return file, nil
}
