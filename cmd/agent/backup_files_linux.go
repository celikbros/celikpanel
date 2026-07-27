//go:build linux

package main

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
)

var backupRename = os.Rename

// The outer package contains only a small, fixed number of payloads, but a
// legitimate website can contain far more than 10,000 files. Keep a separate
// high ceiling for the inner files archive while retaining the total-byte
// limit enforced below.
const maxFilesArchiveEntries = 1_000_000

func secureOpenRegular(filePath string) (*os.File, os.FileInfo, error) {
	if err := rejectSymlinkPath(filepath.Dir(filePath)); err != nil {
		return nil, nil, err
	}
	fd, err := syscall.Open(filePath, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, nil, err
	}
	file := os.NewFile(uintptr(fd), filePath)
	if file == nil {
		_ = syscall.Close(fd)
		return nil, nil, errors.New("could not wrap file descriptor")
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, nil, err
	}
	lstat, err := os.Lstat(filePath)
	if err != nil {
		_ = file.Close()
		return nil, nil, err
	}
	if !info.Mode().IsRegular() || lstat.Mode()&os.ModeSymlink != 0 || !os.SameFile(info, lstat) {
		_ = file.Close()
		return nil, nil, errors.New("path is not a stable regular file")
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); !ok || stat.Nlink != 1 {
		_ = file.Close()
		return nil, nil, errors.New("hard-linked files are not allowed")
	}
	return file, info, nil
}

func openPrivateExclusive(filePath string) (*os.File, error) {
	if err := rejectSymlinkPath(filepath.Dir(filePath)); err != nil {
		return nil, err
	}
	fd, err := syscall.Open(
		filePath,
		syscall.O_WRONLY|syscall.O_CREAT|syscall.O_EXCL|syscall.O_CLOEXEC|syscall.O_NOFOLLOW,
		0o600,
	)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), filePath)
	if file == nil {
		_ = syscall.Close(fd)
		return nil, errors.New("could not wrap file descriptor")
	}
	return file, nil
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
	if current.Mode()&os.ModeSymlink != 0 || !os.SameFile(opened, current) {
		return errors.New("backup changed before deletion")
	}
	return os.Remove(filePath)
}

func rejectSymlinkPath(candidate string) error {
	clean, err := filepath.Abs(candidate)
	if err != nil {
		return err
	}
	clean = filepath.Clean(clean)
	current := string(filepath.Separator)
	parts := strings.Split(strings.TrimPrefix(clean, current), string(filepath.Separator))
	for _, part := range parts {
		if part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink path component: %s", current)
		}
	}
	return nil
}

func secureMkdirAll(candidate string, mode os.FileMode) error {
	clean, err := filepath.Abs(candidate)
	if err != nil {
		return err
	}
	clean = filepath.Clean(clean)
	current := string(filepath.Separator)
	parts := strings.Split(strings.TrimPrefix(clean, current), string(filepath.Separator))
	for _, part := range parts {
		if part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			if err := os.Mkdir(current, mode); err != nil && !errors.Is(err, os.ErrExist) {
				return err
			}
			info, err = os.Lstat(current)
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("unsafe directory component: %s", current)
		}
	}
	return nil
}

func atomicPublishFile(tmpPath, finalPath string) error {
	if err := rejectSymlinkPath(filepath.Dir(finalPath)); err != nil {
		return err
	}
	if err := os.Link(tmpPath, finalPath); err != nil {
		return err
	}
	if err := os.Remove(tmpPath); err != nil {
		_ = os.Remove(finalPath)
		return err
	}
	return nil
}

func safeCreateFilesArchive(sourceDir, destination string) (returnErr error) {
	if err := rejectSymlinkPath(sourceDir); err != nil {
		return err
	}
	rootInfo, err := os.Lstat(sourceDir)
	if err != nil {
		return err
	}
	if !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return errors.New("document root is not a safe directory")
	}
	out, err := openPrivateExclusive(destination)
	if err != nil {
		return err
	}
	defer func() {
		if returnErr != nil {
			_ = out.Close()
			_ = os.Remove(destination)
		}
	}()
	gz := gzip.NewWriter(out)
	tw := tar.NewWriter(gz)
	err = filepath.WalkDir(sourceDir, func(filePath string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if filePath == sourceDir {
			return nil
		}
		info, err := os.Lstat(filePath)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink in document root: %s", filePath)
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported file type in document root: %s", filePath)
		}
		relative, err := filepath.Rel(sourceDir, filePath)
		if err != nil {
			return err
		}
		archiveName := filepath.ToSlash(relative)
		if !validPackageEntryName(archiveName) {
			return errors.New("unsafe document root path")
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = archiveName
		header.Linkname = ""
		header.Uid, header.Gid = 0, 0
		header.Uname, header.Gname = "", ""
		header.Mode = int64(info.Mode().Perm())
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		file, opened, err := secureOpenRegular(filePath)
		if err != nil {
			return err
		}
		written, copyErr := io.Copy(tw, file)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if written != opened.Size() || written != info.Size() {
			return errors.New("file changed while archiving")
		}
		return nil
	})
	if err != nil {
		_ = tw.Close()
		_ = gz.Close()
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}
	if err := out.Sync(); err != nil {
		return err
	}
	return out.Close()
}

type extractedDirectoryMode struct {
	path string
	mode os.FileMode
}

func safeExtractFilesArchive(source, destination string) error {
	if err := secureMkdirAll(destination, 0o700); err != nil {
		return err
	}
	entries, err := os.ReadDir(destination)
	if err != nil {
		return err
	}
	if len(entries) != 0 {
		return errors.New("restore staging directory is not empty")
	}
	file, _, err := secureOpenRegular(source)
	if err != nil {
		return err
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	seen := make(map[string]bool)
	directoryModes := make([]extractedDirectoryMode, 0)
	entryCount := 0
	var totalSize int64
	for {
		header, nextErr := tr.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			return nextErr
		}
		entryCount++
		if entryCount > maxFilesArchiveEntries {
			return errors.New("files archive entry limit exceeded")
		}
		name, rootEntry, err := cleanFilesArchiveName(header.Name, header.Typeflag)
		if err != nil {
			return err
		}
		if rootEntry {
			continue
		}
		if seen[name] {
			return errors.New("duplicate files archive entry")
		}
		seen[name] = true
		target := filepath.Join(destination, filepath.FromSlash(name))
		relative, err := filepath.Rel(destination, target)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return errors.New("files archive escapes staging directory")
		}
		mode := os.FileMode(header.Mode) & os.ModePerm
		switch header.Typeflag {
		case tar.TypeDir:
			if mode == 0 {
				mode = 0o700
			}
			if err := secureMkdirAll(target, 0o700); err != nil {
				return err
			}
			directoryModes = append(directoryModes, extractedDirectoryMode{target, mode})
		case tar.TypeReg, tar.TypeRegA:
			if header.Size < 0 || totalSize > maxPackagePayloadBytes-header.Size {
				return errors.New("files archive size limit exceeded")
			}
			totalSize += header.Size
			if err := secureMkdirAll(filepath.Dir(target), 0o700); err != nil {
				return err
			}
			out, err := openPrivateExclusive(target)
			if err != nil {
				return err
			}
			written, copyErr := io.Copy(out, tr)
			syncErr := out.Sync()
			closeErr := out.Close()
			if copyErr != nil {
				return copyErr
			}
			if syncErr != nil {
				return syncErr
			}
			if closeErr != nil {
				return closeErr
			}
			if written != header.Size {
				return errors.New("truncated files archive entry")
			}
			if mode == 0 {
				mode = 0o600
			}
			if err := os.Chmod(target, mode); err != nil {
				return err
			}
		default:
			return errors.New("links and special files are not allowed in backups")
		}
	}
	sort.Slice(directoryModes, func(i, j int) bool {
		return strings.Count(directoryModes[i].path, string(filepath.Separator)) > strings.Count(directoryModes[j].path, string(filepath.Separator))
	})
	for _, directory := range directoryModes {
		if err := os.Chmod(directory.path, directory.mode); err != nil {
			return err
		}
	}
	return nil
}

func cleanFilesArchiveName(name string, typeflag byte) (string, bool, error) {
	if strings.Contains(name, `\`) || path.IsAbs(name) {
		return "", false, errors.New("unsafe files archive path")
	}
	for strings.HasPrefix(name, "./") {
		name = strings.TrimPrefix(name, "./")
	}
	if typeflag == tar.TypeDir {
		name = strings.TrimRight(name, "/")
	}
	if name == "" || name == "." {
		if typeflag == tar.TypeDir {
			return "", true, nil
		}
		return "", false, errors.New("unsafe files archive root entry")
	}
	clean := path.Clean(name)
	if clean != name || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", false, errors.New("unsafe files archive path")
	}
	return clean, false, nil
}

type stagedFilesRestore struct {
	stage     string
	target    string
	old       string
	published bool
}

func prepareFilesRestore(archivePath, target string) (*stagedFilesRestore, error) {
	if err := rejectSymlinkPath(target); err != nil {
		return nil, err
	}
	targetInfo, err := os.Lstat(target)
	if err != nil {
		return nil, err
	}
	if !targetInfo.IsDir() || targetInfo.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("restore target is not a safe directory")
	}
	parent := filepath.Dir(target)
	stage, err := os.MkdirTemp(parent, ".restore-stage-")
	if err != nil {
		return nil, err
	}
	result := &stagedFilesRestore{stage: stage, target: target}
	if err := os.Chmod(stage, 0o700); err != nil {
		result.Cleanup()
		return nil, err
	}
	if err := safeExtractFilesArchive(archivePath, stage); err != nil {
		result.Cleanup()
		return nil, err
	}
	stat, ok := targetInfo.Sys().(*syscall.Stat_t)
	if !ok {
		result.Cleanup()
		return nil, errors.New("could not read restore target ownership")
	}
	if err := chownRestoreTree(stage, int(stat.Uid), int(stat.Gid)); err != nil {
		result.Cleanup()
		return nil, err
	}
	if err := os.Chmod(stage, targetInfo.Mode().Perm()); err != nil {
		result.Cleanup()
		return nil, err
	}
	return result, nil
}

func chownRestoreTree(root string, uid, gid int) error {
	return filepath.WalkDir(root, func(filePath string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := os.Lstat(filePath)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("symlink appeared in restore staging")
		}
		return os.Chown(filePath, uid, gid)
	})
}

func (s *stagedFilesRestore) Publish() error {
	if s == nil || s.published {
		return errors.New("invalid staged restore state")
	}
	parent := filepath.Dir(s.target)
	placeholder, err := os.MkdirTemp(parent, ".restore-old-")
	if err != nil {
		return err
	}
	if err := os.Remove(placeholder); err != nil {
		return err
	}
	s.old = placeholder
	if err := backupRename(s.target, s.old); err != nil {
		return err
	}
	if err := backupRename(s.stage, s.target); err != nil {
		rollbackErr := backupRename(s.old, s.target)
		if rollbackErr != nil {
			return fmt.Errorf("publish failed: %v; target rollback failed: %w", err, rollbackErr)
		}
		return err
	}
	s.published = true
	if err := syncDirectory(parent); err != nil {
		rollbackErr := s.Rollback()
		if rollbackErr != nil {
			return fmt.Errorf("publish sync failed: %v; target rollback failed: %w", err, rollbackErr)
		}
		return err
	}
	return nil
}

func (s *stagedFilesRestore) Commit() error {
	if s == nil || !s.published {
		return errors.New("staged restore was not published")
	}
	if err := os.RemoveAll(s.old); err != nil {
		return err
	}
	s.old = ""
	return syncDirectory(filepath.Dir(s.target))
}

func (s *stagedFilesRestore) Rollback() error {
	if s == nil || !s.published || s.old == "" {
		return nil
	}
	parent := filepath.Dir(s.target)
	failed, err := os.MkdirTemp(parent, ".restore-failed-")
	if err != nil {
		return err
	}
	if err := os.Remove(failed); err != nil {
		return err
	}
	if err := backupRename(s.target, failed); err != nil {
		return err
	}
	if err := backupRename(s.old, s.target); err != nil {
		_ = backupRename(failed, s.target)
		return err
	}
	s.published = false
	s.old = ""
	_ = os.RemoveAll(failed)
	return syncDirectory(parent)
}

func (s *stagedFilesRestore) Cleanup() {
	if s == nil {
		return
	}
	if !s.published && s.stage != "" {
		_ = os.RemoveAll(s.stage)
	}
}
