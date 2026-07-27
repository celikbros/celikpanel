//go:build linux

package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

const sslConfinedResolveFlags = unix.RESOLVE_BENEATH |
	unix.RESOLVE_NO_SYMLINKS |
	unix.RESOLVE_NO_MAGICLINKS

// openSSLConfinedAt is the small descriptor-relative primitive shared only by
// ACME webroot preparation and immutable certificate snapshot cleanup.
func openSSLConfinedAt(dirFD int, relativePath string, flags int, mode uint32) (int, error) {
	how := &unix.OpenHow{
		Flags:   uint64(flags | unix.O_CLOEXEC),
		Mode:    uint64(mode),
		Resolve: sslConfinedResolveFlags,
	}
	fd, err := unix.Openat2(dirFD, relativePath, how)
	if errors.Is(err, syscall.ENOSYS) {
		return -1, fmt.Errorf("openat2 is required for confined SSL filesystem access: %w", err)
	}
	return fd, err
}

func duplicateSSLFD(fd int) (int, error) {
	duplicate, err := unix.Dup(fd)
	if err == nil {
		unix.CloseOnExec(duplicate)
	}
	return duplicate, err
}

func splitSSLRelativePath(relativePath string) []string {
	if relativePath == "." || relativePath == "" {
		return nil
	}
	return strings.Split(relativePath, "/")
}

func openSSLConfinedRoot(root string) (int, error) {
	if !path.IsAbs(root) || path.Clean(root) != root || root == "/" {
		return -1, fmt.Errorf("managed certificate root must be an absolute canonical directory")
	}
	slashFD, err := unix.Open(
		"/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0,
	)
	if err != nil {
		return -1, err
	}
	defer unix.Close(slashFD)
	return openSSLConfinedAt(
		slashFD,
		strings.TrimPrefix(root, "/"),
		unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW,
		0,
	)
}

func openSSLConfinedParent(rootFD int, relativePath string) (int, string, error) {
	if relativePath == "" || relativePath == "." || path.IsAbs(relativePath) ||
		path.Clean(relativePath) != relativePath {
		return -1, "", fmt.Errorf("managed snapshot path must be canonical and relative")
	}
	parts := splitSSLRelativePath(relativePath)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" ||
		parts[0] == "." || parts[0] == ".." ||
		parts[1] == "." || parts[1] == ".." {
		return -1, "", fmt.Errorf("managed snapshot path must identify one exact domain version")
	}
	parentFD, err := openSSLConfinedAt(
		rootFD, parts[0],
		unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW,
		0,
	)
	if err != nil {
		return -1, "", err
	}
	return parentFD, parts[1], nil
}

// secureDeleteManagedCertificateSnapshot removes one exact, already verified
// immutable version directory. It never follows a symlink and cannot traverse
// outside the caller-provided managed certificate root.
func secureDeleteManagedCertificateSnapshot(root, relativePath string) error {
	rootFD, err := openSSLConfinedRoot(root)
	if err != nil {
		return err
	}
	defer unix.Close(rootFD)

	parentFD, leaf, err := openSSLConfinedParent(rootFD, relativePath)
	if err != nil {
		return err
	}
	defer unix.Close(parentFD)
	return secureDeleteSSLAt(parentFD, leaf, true)
}

func secureDeleteSSLAt(parentFD int, leaf string, rejectSymlink bool) error {
	var stat unix.Stat_t
	if err := unix.Fstatat(parentFD, leaf, &stat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return err
	}
	if stat.Mode&unix.S_IFMT == unix.S_IFLNK {
		if rejectSymlink {
			return os.ErrPermission
		}
		return unix.Unlinkat(parentFD, leaf, 0)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR {
		return unix.Unlinkat(parentFD, leaf, 0)
	}

	dirFD, err := openSSLConfinedAt(
		parentFD, leaf,
		unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW,
		0,
	)
	if err != nil {
		return err
	}
	dir := os.NewFile(uintptr(dirFD), leaf)
	if dir == nil {
		unix.Close(dirFD)
		return os.ErrInvalid
	}
	names, readErr := dir.Readdirnames(-1)
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		_ = dir.Close()
		return readErr
	}
	for _, name := range names {
		if err := secureDeleteSSLAt(dirFD, name, false); err != nil {
			_ = dir.Close()
			return err
		}
	}
	if err := dir.Close(); err != nil {
		return err
	}
	return unix.Unlinkat(parentFD, leaf, unix.AT_REMOVEDIR)
}
