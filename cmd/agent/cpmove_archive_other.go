//go:build !linux

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const defaultCpmoveArchiveRoot = "/var/lib/celikpanel-imports"

var (
	cpmoveArchiveRoot     = defaultCpmoveArchiveRoot
	cpmoveArchiveOwnerUID = uint32(0)
)

func trustedCpmoveRelativePath(archivePath string) (string, error) {
	if archivePath == "" || !filepath.IsAbs(archivePath) ||
		filepath.Clean(archivePath) != archivePath {
		return "", os.ErrPermission
	}
	root := filepath.Clean(cpmoveArchiveRoot)
	relative, err := filepath.Rel(root, archivePath)
	if err != nil || relative == "." || relative == ".." ||
		strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return "", os.ErrPermission
	}
	return relative, nil
}

func openTrustedCpmoveArchive(archivePath string) (*os.File, error) {
	if _, err := trustedCpmoveRelativePath(archivePath); err != nil {
		return nil, fmt.Errorf("archive must be a file below %s: %w", cpmoveArchiveRoot, err)
	}
	info, err := os.Lstat(archivePath)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 ||
		info.Mode().Perm()&0o077 != 0 || info.Size() <= 0 {
		return nil, fmt.Errorf("archive must be an owner-only regular file")
	}
	file, err := os.Open(archivePath)
	if err != nil {
		return nil, err
	}
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		file.Close()
		if err != nil {
			return nil, err
		}
		return nil, os.ErrPermission
	}
	return file, nil
}
