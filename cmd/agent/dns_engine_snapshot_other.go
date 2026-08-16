//go:build !linux

package main

import (
	"errors"
	"os"
)

type dnsSnapshotMetadata struct {
	Mode       os.FileMode
	UID        uint32
	GID        uint32
	OwnerKnown bool
}

func dnsSnapshotOwnerRequired() bool { return false }

func readDNSFileForSnapshot(path string) ([]byte, dnsSnapshotMetadata, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, dnsSnapshotMetadata{}, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, dnsSnapshotMetadata{}, errors.New("managed configuration snapshot is not a regular file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, dnsSnapshotMetadata{}, err
	}
	return data, dnsSnapshotMetadata{Mode: info.Mode().Perm()}, nil
}

func verifyDNSRootDirectory(path string, mode os.FileMode) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != mode.Perm() {
		return errors.New("managed DNS directory does not have the required metadata")
	}
	return nil
}
