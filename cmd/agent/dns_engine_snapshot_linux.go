//go:build linux

package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"golang.org/x/sys/unix"
)

type dnsSnapshotMetadata struct {
	Mode       os.FileMode
	UID        uint32
	GID        uint32
	OwnerKnown bool
}

func dnsSnapshotOwnerRequired() bool { return true }

func readDNSFileForSnapshot(path string) ([]byte, dnsSnapshotMetadata, error) {
	relative, err := secureConfigRelativePath(path)
	if err != nil {
		return nil, dnsSnapshotMetadata{}, err
	}
	rootFD, err := openSecureConfigRoot()
	if err != nil {
		return nil, dnsSnapshotMetadata{}, err
	}
	defer unix.Close(rootFD)
	fd, err := unix.Openat2(rootFD, relative, &unix.OpenHow{
		Flags:   uint64(unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK),
		Resolve: secureConfigResolve,
	})
	if err != nil {
		return nil, dnsSnapshotMetadata{}, secureConfigOpenError("snapshot managed configuration", path, err)
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		unix.Close(fd)
		return nil, dnsSnapshotMetadata{}, errors.New("snapshot managed configuration has an invalid descriptor")
	}
	defer file.Close()
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return nil, dnsSnapshotMetadata{}, fmt.Errorf("stat managed configuration snapshot %s: %w", path, err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Nlink != 1 {
		return nil, dnsSnapshotMetadata{}, errors.New("managed configuration snapshot is not a single-link regular file")
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, dnsSnapshotMetadata{}, err
	}
	var after unix.Stat_t
	if err := unix.Fstat(fd, &after); err != nil {
		return nil, dnsSnapshotMetadata{}, err
	}
	if stat.Dev != after.Dev || stat.Ino != after.Ino || stat.Size != after.Size ||
		stat.Mtim != after.Mtim || int64(len(data)) != after.Size {
		return nil, dnsSnapshotMetadata{}, errors.New("managed configuration changed while it was snapshotted")
	}
	return data, dnsSnapshotMetadata{
		Mode: os.FileMode(stat.Mode & 0o777), UID: stat.Uid, GID: stat.Gid, OwnerKnown: true,
	}, nil
}

func verifyDNSRootDirectory(path string, mode os.FileMode) error {
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
		return secureConfigOpenError("verify managed DNS directory", path, err)
	}
	defer unix.Close(fd)
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR || os.FileMode(stat.Mode&0o777) != mode.Perm() ||
		stat.Uid != 0 || stat.Gid != 0 {
		return errors.New("managed DNS directory is not root-owned with the required mode")
	}
	return nil
}
