//go:build !linux

package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

var serviceMutationRequiredOwnerUID uint32
var serviceMutationRequiredOwnerGID uint32

func secureServiceMutationStat(path string, info os.FileInfo, wantDirectory bool) error {
	if info == nil {
		return errors.New("missing file information")
	}
	if wantDirectory {
		if !info.IsDir() {
			return fmt.Errorf("%s must be a real directory", path)
		}
		if info.Mode().Perm()&0o022 != 0 {
			return fmt.Errorf("%s must not be writable by group or others", path)
		}
		return nil
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return fmt.Errorf("%s must be a 0600 regular file", path)
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

func securePreLedgerServiceMutationStateDirectoryStat(path string, info os.FileInfo) error {
	return secureServiceMutationStateDirectoryStat(path, info)
}

func ensureSecureServiceMutationStateDirectory(path string) error {
	path = filepath.Clean(path)
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		if err := os.Mkdir(path, 0o700); err != nil {
			return fmt.Errorf("create state directory: %w", err)
		}
		info, err = os.Lstat(path)
	}
	if err != nil {
		return fmt.Errorf("inspect state directory: %w", err)
	}
	return secureServiceMutationStateDirectoryStat(path, info)
}

func readSecureServiceMutationLedger(path string, maxSize int64) ([]byte, bool, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("inspect service mutation ledger: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || info.Size() > maxSize {
		return nil, false, errors.New("service mutation ledger must be a 0600 regular file within the size limit")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, false, fmt.Errorf("open service mutation ledger: %w", err)
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, maxSize+1))
	if err != nil {
		return nil, false, fmt.Errorf("read service mutation ledger: %w", err)
	}
	if int64(len(raw)) > maxSize {
		return nil, false, errors.New("service mutation ledger exceeds the size limit")
	}
	return raw, true, nil
}
func readRecoverableInitialServiceMutationStage(path string, maxSize int64) ([]byte, bool, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("inspect recoverable initial service mutation stage: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || info.Size() > maxSize {
		return nil, false, errors.New("recoverable initial service mutation stage metadata is unsafe")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, false, fmt.Errorf("open recoverable initial service mutation stage: %w", err)
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, maxSize+1))
	if err != nil {
		return nil, false, fmt.Errorf("read recoverable initial service mutation stage: %w", err)
	}
	if int64(len(raw)) > maxSize {
		return nil, false, errors.New("recoverable initial service mutation stage exceeds the size limit")
	}
	return raw, true, nil
}
