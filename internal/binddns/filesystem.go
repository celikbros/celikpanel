package binddns

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"sort"
	"strings"
)

// Entry is the security-relevant subset of lstat metadata used by Publisher.
// OwnerKnown is false only on platforms that cannot report POSIX ownership;
// Linux production implementations always report it.
type Entry struct {
	Mode       fs.FileMode
	Size       int64
	UID        int
	GID        int
	OwnerKnown bool
}

// FileSystem exposes the exact operations needed to publish an immutable
// generation. Injecting it keeps renderer and crash-safety tests independent
// of the host OS while OSFileSystem is used by the Linux agent.
type FileSystem interface {
	Lstat(name string) (Entry, error)
	Mkdir(name string, mode fs.FileMode) error
	WriteFileExclusive(name string, data []byte, mode fs.FileMode) error
	ReadFile(name string) ([]byte, error)
	ReadDirNames(name string) ([]string, error)
	Chmod(name string, mode fs.FileMode) error
	Chown(name string, uid, gid int) error
	Lchown(name string, uid, gid int) error
	RenameNoReplace(oldName, newName string) error
	RenameReplace(oldName, newName string) error
	Symlink(target, linkName string) error
	Readlink(name string) (string, error)
	Remove(name string) error
	RemoveAll(name string) error
	Sync(name string) error
}

// CommandRunner runs BIND's native syntax validators against a staged tree.
type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

// ExecRunner is the production CommandRunner.
type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, name, args...)
	output, err := command.CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(output))
		if len(detail) > 4096 {
			detail = detail[:4096] + "..."
		}
		if detail == "" {
			return output, fmt.Errorf("%s failed: %w", name, err)
		}
		return output, fmt.Errorf("%s failed: %w: %s", name, err, detail)
	}
	return output, nil
}

// OSFileSystem is the production filesystem implementation.
type OSFileSystem struct{}

func (OSFileSystem) Lstat(name string) (Entry, error) {
	info, err := os.Lstat(name)
	if err != nil {
		return Entry{}, err
	}
	uid, gid, known := platformOwnership(info)
	return Entry{Mode: info.Mode(), Size: info.Size(), UID: uid, GID: gid, OwnerKnown: known}, nil
}

func (OSFileSystem) Mkdir(name string, mode fs.FileMode) error {
	return os.Mkdir(name, mode)
}

func (OSFileSystem) WriteFileExclusive(name string, data []byte, mode fs.FileMode) (returnErr error) {
	file, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	defer func() {
		returnErr = errors.Join(returnErr, file.Close())
	}()
	if _, err := file.Write(data); err != nil {
		return err
	}
	return file.Sync()
}

func (OSFileSystem) ReadFile(name string) ([]byte, error) { return os.ReadFile(name) }

func (OSFileSystem) ReadDirNames(name string) ([]string, error) {
	entries, err := os.ReadDir(name)
	if err != nil {
		return nil, err
	}
	names := make([]string, len(entries))
	for index, entry := range entries {
		names[index] = entry.Name()
	}
	sort.Strings(names)
	return names, nil
}

func (OSFileSystem) Chmod(name string, mode fs.FileMode) error { return os.Chmod(name, mode) }
func (OSFileSystem) Chown(name string, uid, gid int) error     { return os.Chown(name, uid, gid) }
func (OSFileSystem) Lchown(name string, uid, gid int) error    { return os.Lchown(name, uid, gid) }
func (OSFileSystem) RenameNoReplace(oldName, newName string) error {
	return renameNoReplace(oldName, newName)
}
func (OSFileSystem) RenameReplace(oldName, newName string) error { return os.Rename(oldName, newName) }
func (OSFileSystem) Symlink(target, linkName string) error       { return os.Symlink(target, linkName) }
func (OSFileSystem) Readlink(name string) (string, error)        { return os.Readlink(name) }
func (OSFileSystem) Remove(name string) error                    { return os.Remove(name) }
func (OSFileSystem) RemoveAll(name string) error                 { return os.RemoveAll(name) }

func (OSFileSystem) Sync(name string) error {
	file, err := os.Open(name)
	if err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}
