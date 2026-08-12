//go:build !linux

package main

import (
	"fmt"
	"os"
	"path/filepath"
)

type serviceMutationFileLock struct {
	path string
	file *os.File
}

func acquireServiceMutationFileLock(path string) (*serviceMutationFileLock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if os.IsExist(err) {
		return nil, errServiceMutationHostBusy
	}
	if err != nil {
		return nil, fmt.Errorf("create service mutation lock: %w", err)
	}
	return &serviceMutationFileLock{path: path, file: file}, nil
}

func (l *serviceMutationFileLock) Close() error {
	if l == nil {
		return nil
	}
	if l.file != nil {
		_ = l.file.Close()
		l.file = nil
	}
	if l.path != "" {
		err := os.Remove(l.path)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		l.path = ""
	}
	return nil
}

func probeServiceMutationFileLockIdle(path string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect service mutation lock: %w", err)
	}
	if err := secureServiceMutationStat(path, info, false); err != nil {
		return err
	}
	return errServiceMutationHostBusy
}

func verifyInheritedServiceMutationFileLock(string) error {
	return fmt.Errorf("inherited service mutation flock proof is supported only on Linux")
}

func syncServiceMutationDirectory(string) error {
	return nil
}

func validateDNSClusterConfigDirectory(string) error {
	return nil
}

func syncDNSClusterConfigDirectory(string) error {
	return nil
}

func realPackageManagerMutationBusy() (bool, error) {
	return false, nil
}
