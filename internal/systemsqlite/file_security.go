package systemsqlite

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type managedSource struct {
	path       string
	file       *os.File
	sqlitePath string
	info       os.FileInfo
}

func openManagedSource(path string, writable bool) (*managedSource, error) {
	clean := filepath.Clean(strings.TrimSpace(path))
	if clean == "." || !filepath.IsAbs(clean) {
		return nil, errors.New("managed database path is not absolute")
	}
	if err := rejectSymlinkComponents(clean); err != nil {
		return nil, err
	}

	entry, err := os.Lstat(clean)
	if err != nil {
		return nil, err
	}
	if entry.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("managed database is a symbolic link")
	}
	if !entry.Mode().IsRegular() {
		return nil, errors.New("managed database is not a regular file")
	}

	file, err := openPinnedManagedFile(clean, writable)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() || !os.SameFile(entry, info) {
		_ = file.Close()
		return nil, errors.New("managed database identity changed while opening")
	}
	if err := validateSingleLink(file); err != nil {
		_ = file.Close()
		return nil, err
	}
	sqlitePath := pinnedManagedFilePath(file, clean)
	return &managedSource{
		path: clean, file: file, sqlitePath: sqlitePath, info: info,
	}, nil
}

func openManagedSourceInCurrentDirectory(name string, writable bool) (*managedSource, error) {
	clean := filepath.Clean(strings.TrimSpace(name))
	if clean == "." || filepath.IsAbs(clean) || filepath.Base(clean) != clean {
		return nil, errors.New("managed workspace database name is unsafe")
	}
	entry, err := os.Lstat(clean)
	if err != nil {
		return nil, err
	}
	if entry.Mode()&os.ModeSymlink != 0 || !entry.Mode().IsRegular() {
		return nil, errors.New("managed workspace database is not a regular file")
	}
	file, err := openPinnedManagedFile(clean, writable)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() || !os.SameFile(entry, info) {
		_ = file.Close()
		return nil, errors.New("managed workspace database identity changed while opening")
	}
	if err := validateSingleLink(file); err != nil {
		_ = file.Close()
		return nil, err
	}
	return &managedSource{
		path: clean, file: file, sqlitePath: pinnedManagedFilePath(file, clean), info: info,
	}, nil
}

func (source *managedSource) close() {
	if source != nil && source.file != nil {
		_ = source.file.Close()
		source.file = nil
	}
}

func (source *managedSource) databasePath() string {
	if source == nil {
		return ""
	}
	return source.sqlitePath
}

func (source *managedSource) verifyIdentity() error {
	if source == nil || source.file == nil {
		return errors.New("managed database is not pinned")
	}
	current, err := os.Lstat(source.path)
	if err != nil {
		return err
	}
	if current.Mode()&os.ModeSymlink != 0 || !current.Mode().IsRegular() {
		return errors.New("managed database identity changed")
	}
	pinned, err := source.file.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(source.info, pinned) || !os.SameFile(current, pinned) {
		return errors.New("managed database identity changed")
	}
	return validateSingleLink(source.file)
}

func rejectSymlinkComponents(path string) error {
	volume := filepath.VolumeName(path)
	remainder := strings.TrimPrefix(path, volume)
	remainder = strings.TrimLeft(remainder, string(filepath.Separator)+"/\\")

	current := volume + string(filepath.Separator)
	if volume == "" {
		current = string(filepath.Separator)
	}
	for _, part := range strings.FieldsFunc(remainder, func(r rune) bool {
		return r == '/' || r == '\\'
	}) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("managed database path contains a symbolic link")
		}
	}
	return nil
}
