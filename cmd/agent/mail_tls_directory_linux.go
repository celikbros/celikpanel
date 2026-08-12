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

const mailTLSDirectoryResolve = unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS

var mailTLSDirectoryOpenat2 = unix.Openat2

func prepareDefaultMailTLSDirectory(certPath, keyPath string) (mailTLSDirectoryOwner, error) {
	directory, production, err := validateDefaultMailTLSDirectoryPaths(certPath, keyPath)
	if err != nil {
		return mailTLSDirectoryOwner{}, err
	}
	if production {
		return prepareProductionMailTLSDirectory(directory)
	}
	return prepareCustomMailTLSDirectory(directory)
}

func openMailTLSDirectoryAt(parentFD int, relative, label string) (int, error) {
	fd, err := mailTLSDirectoryOpenat2(parentFD, relative, &unix.OpenHow{
		Flags:   uint64(unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW),
		Resolve: mailTLSDirectoryResolve,
	})
	if errors.Is(err, unix.ENOSYS) {
		return -1, fmt.Errorf("%s requires Linux openat2: %w", label, err)
	}
	if errors.Is(err, unix.ELOOP) || errors.Is(err, unix.EXDEV) {
		return -1, fmt.Errorf("%s refused a symbolic link or path escape: %w", label, err)
	}
	if err != nil {
		return -1, fmt.Errorf("%s: %w", label, err)
	}
	return fd, nil
}

func validateMailTLSDirectoryFD(fd int, expected mailTLSDirectoryOwner, label string) (mailTLSDirectoryOwner, error) {
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return mailTLSDirectoryOwner{}, fmt.Errorf("stat %s: %w", label, err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR {
		return mailTLSDirectoryOwner{}, fmt.Errorf("%s is not a directory", label)
	}
	owner := mailTLSDirectoryOwner{uid: uint64(stat.Uid), gid: uint64(stat.Gid)}
	if owner != expected {
		return mailTLSDirectoryOwner{}, fmt.Errorf("%s must be owned by uid %d gid %d", label, expected.uid, expected.gid)
	}
	if stat.Mode&0o022 != 0 {
		return mailTLSDirectoryOwner{}, fmt.Errorf("%s must not be writable by group or others", label)
	}
	return owner, nil
}

func createOrOpenMailTLSDirectoryAt(parentFD int, name string, owner mailTLSDirectoryOwner, label string) (int, error) {
	if name == "" || name == "." || name == ".." || filepath.Base(name) != name {
		return -1, fmt.Errorf("%s has an invalid path component", label)
	}
	fd, err := openMailTLSDirectoryAt(parentFD, name, label)
	if err == nil {
		if _, err := validateMailTLSDirectoryFD(fd, owner, label); err != nil {
			unix.Close(fd)
			return -1, err
		}
		return fd, nil
	}
	if !errors.Is(err, unix.ENOENT) {
		return -1, err
	}
	created := false
	if mkdirErr := unix.Mkdirat(parentFD, name, 0o755); mkdirErr == nil {
		created = true
	} else if !errors.Is(mkdirErr, unix.EEXIST) {
		return -1, fmt.Errorf("create %s: %w", label, mkdirErr)
	}
	fd, err = openMailTLSDirectoryAt(parentFD, name, label)
	if err != nil {
		return -1, err
	}
	if created {
		if err := unix.Fchown(fd, int(owner.uid), int(owner.gid)); err != nil {
			unix.Close(fd)
			return -1, fmt.Errorf("set %s ownership: %w", label, err)
		}
		if err := unix.Fchmod(fd, 0o755); err != nil {
			unix.Close(fd)
			return -1, fmt.Errorf("set %s permissions: %w", label, err)
		}
		if err := unix.Fsync(fd); err != nil {
			unix.Close(fd)
			return -1, fmt.Errorf("sync %s: %w", label, err)
		}
		if err := unix.Fsync(parentFD); err != nil {
			unix.Close(fd)
			return -1, fmt.Errorf("sync %s parent: %w", label, err)
		}
	}
	if _, err := validateMailTLSDirectoryFD(fd, owner, label); err != nil {
		unix.Close(fd)
		return -1, err
	}
	return fd, nil
}

func prepareProductionMailTLSDirectory(directory string) (mailTLSDirectoryOwner, error) {
	rootOwner := mailTLSDirectoryOwner{uid: 0, gid: 0}
	currentFD, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return mailTLSDirectoryOwner{}, fmt.Errorf("open mail TLS filesystem root: %w", err)
	}
	defer func() {
		if currentFD >= 0 {
			_ = unix.Close(currentFD)
		}
	}()
	if _, err := validateMailTLSDirectoryFD(currentFD, rootOwner, "mail TLS filesystem root"); err != nil {
		return mailTLSDirectoryOwner{}, err
	}
	components := strings.Split(strings.TrimPrefix(filepath.ToSlash(directory), "/"), "/")
	for index, component := range components {
		label := "/" + strings.Join(components[:index+1], "/")
		nextFD, err := createOrOpenMailTLSDirectoryAt(currentFD, component, rootOwner, label)
		if err != nil {
			return mailTLSDirectoryOwner{}, err
		}
		_ = unix.Close(currentFD)
		currentFD = nextFD
	}
	return rootOwner, nil
}

func prepareCustomMailTLSDirectory(directory string) (mailTLSDirectoryOwner, error) {
	currentOwner := mailTLSDirectoryOwner{uid: uint64(os.Geteuid()), gid: uint64(os.Getegid())}
	rootFD, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return mailTLSDirectoryOwner{}, fmt.Errorf("open mail TLS filesystem root: %w", err)
	}
	defer unix.Close(rootFD)
	parent := filepath.Dir(directory)
	relativeParent := strings.TrimPrefix(filepath.ToSlash(parent), "/")
	parentFD, err := openMailTLSDirectoryAt(rootFD, relativeParent, "default mail TLS parent directory")
	if err != nil {
		return mailTLSDirectoryOwner{}, err
	}
	defer unix.Close(parentFD)
	if _, err := validateMailTLSDirectoryFD(parentFD, currentOwner, "default mail TLS parent directory"); err != nil {
		return mailTLSDirectoryOwner{}, err
	}
	directoryFD, err := createOrOpenMailTLSDirectoryAt(parentFD, filepath.Base(directory), currentOwner, "default mail TLS directory")
	if err != nil {
		return mailTLSDirectoryOwner{}, err
	}
	defer unix.Close(directoryFD)
	return currentOwner, nil
}
