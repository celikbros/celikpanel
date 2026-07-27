//go:build !linux

package main

import (
	"errors"
	"os"
	"path/filepath"
)

var backupRename = os.Rename

var errBackupPlatformUnsupported = errors.New("secure backup filesystem operations require Linux")

func secureOpenRegular(filePath string) (*os.File, os.FileInfo, error) {
	info, err := os.Lstat(filePath)
	if err != nil {
		return nil, nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, nil, errors.New("path is not a regular file")
	}
	file, err := os.Open(filePath)
	if err != nil {
		return nil, nil, err
	}
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		_ = file.Close()
		if err != nil {
			return nil, nil, err
		}
		return nil, nil, errors.New("file changed while opening")
	}
	return file, opened, nil
}

func openPrivateExclusive(filePath string) (*os.File, error) {
	return os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
}

func secureRemoveRegular(filePath string) error {
	file, opened, err := secureOpenRegular(filePath)
	if err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	current, err := os.Lstat(filePath)
	if err != nil {
		return err
	}
	if !os.SameFile(opened, current) {
		return errors.New("backup changed before deletion")
	}
	return os.Remove(filePath)
}

func rejectSymlinkPath(candidate string) error {
	clean, err := filepath.Abs(candidate)
	if err != nil {
		return err
	}
	for current := clean; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("symlink path component")
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}
	return nil
}

func secureMkdirAll(candidate string, mode os.FileMode) error {
	if err := os.MkdirAll(candidate, mode); err != nil {
		return err
	}
	return rejectSymlinkPath(candidate)
}

func atomicPublishFile(tmpPath, finalPath string) error {
	if err := os.Link(tmpPath, finalPath); err != nil {
		return err
	}
	if err := os.Remove(tmpPath); err != nil {
		_ = os.Remove(finalPath)
		return err
	}
	return nil
}

func safeCreateFilesArchive(string, string) error {
	return errBackupPlatformUnsupported
}

func safeExtractFilesArchive(string, string) error {
	return errBackupPlatformUnsupported
}

type stagedFilesRestore struct{}

func prepareFilesRestore(string, string) (*stagedFilesRestore, error) {
	return nil, errBackupPlatformUnsupported
}

func (s *stagedFilesRestore) Publish() error {
	return errBackupPlatformUnsupported
}

func (s *stagedFilesRestore) Commit() error {
	return errBackupPlatformUnsupported
}

func (s *stagedFilesRestore) Rollback() error {
	return errBackupPlatformUnsupported
}

func (s *stagedFilesRestore) Cleanup() {}
