//go:build linux

package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const fileResolveFlags = unix.RESOLVE_BENEATH |
	unix.RESOLVE_NO_SYMLINKS |
	unix.RESOLVE_NO_MAGICLINKS

func openFileManagerRoot(root string) (int, error) {
	if !path.IsAbs(root) || path.Clean(root) != root {
		return -1, fmt.Errorf("invalid document root")
	}
	slashFD, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, err
	}
	defer unix.Close(slashFD)
	relativeRoot := strings.TrimPrefix(root, "/")
	if relativeRoot == "" {
		return -1, fmt.Errorf("filesystem root is not a valid file-manager root")
	}
	return openFileManagerAt(
		slashFD,
		relativeRoot,
		unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW,
		0,
	)
}

func openFileManagerAt(dirFD int, relativePath string, flags int, mode uint32) (int, error) {
	how := &unix.OpenHow{
		Flags:   uint64(flags | unix.O_CLOEXEC),
		Mode:    uint64(mode),
		Resolve: fileResolveFlags,
	}
	fd, err := unix.Openat2(dirFD, relativePath, how)
	if errors.Is(err, syscall.ENOSYS) {
		return -1, fmt.Errorf("openat2 is required for secure file-manager access: %w", err)
	}
	return fd, err
}

func duplicateFD(fd int) (int, error) {
	dup, err := unix.Dup(fd)
	if err == nil {
		unix.CloseOnExec(dup)
	}
	return dup, err
}

func statFileInfo(name, relativePath string, st *unix.Stat_t) FileInfo {
	mode := os.FileMode(st.Mode & 0o777)
	switch st.Mode & unix.S_IFMT {
	case unix.S_IFDIR:
		mode |= os.ModeDir
	case unix.S_IFLNK:
		mode |= os.ModeSymlink
	case unix.S_IFIFO:
		mode |= os.ModeNamedPipe
	case unix.S_IFSOCK:
		mode |= os.ModeSocket
	case unix.S_IFCHR:
		mode |= os.ModeCharDevice | os.ModeDevice
	case unix.S_IFBLK:
		mode |= os.ModeDevice
	}
	return FileInfo{
		Name:        name,
		Path:        relativePath,
		IsDir:       st.Mode&unix.S_IFMT == unix.S_IFDIR,
		Size:        st.Size,
		Permissions: mode.String(),
		Owner:       strconv.FormatUint(uint64(st.Uid), 10),
		Group:       strconv.FormatUint(uint64(st.Gid), 10),
		ModTime:     time.Unix(st.Mtim.Sec, st.Mtim.Nsec),
	}
}

func secureListFiles(root, relativePath string, maxEntries int) ([]FileInfo, error) {
	rootFD, err := openFileManagerRoot(root)
	if err != nil {
		return nil, err
	}
	defer unix.Close(rootFD)

	dirFD, err := openFileManagerAt(rootFD, relativePath, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	dir := os.NewFile(uintptr(dirFD), relativePath)
	if dir == nil {
		unix.Close(dirFD)
		return nil, os.ErrInvalid
	}
	defer dir.Close()

	names, err := dir.Readdirnames(maxEntries + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if len(names) > maxEntries {
		return nil, fmt.Errorf("directory contains more than %d entries", maxEntries)
	}

	result := make([]FileInfo, 0, len(names))
	for _, name := range names {
		var st unix.Stat_t
		if err := unix.Fstatat(dirFD, name, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			return nil, err
		}
		result = append(result, statFileInfo(name, path.Join(relativePath, name), &st))
	}
	return result, nil
}

func secureReadFile(root, relativePath string, maxBytes int64) ([]byte, FileInfo, error) {
	rootFD, err := openFileManagerRoot(root)
	if err != nil {
		return nil, FileInfo{}, err
	}
	defer unix.Close(rootFD)

	fd, err := openFileManagerAt(rootFD, relativePath, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, FileInfo{}, err
	}
	file := os.NewFile(uintptr(fd), relativePath)
	if file == nil {
		unix.Close(fd)
		return nil, FileInfo{}, os.ErrInvalid
	}
	defer file.Close()

	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return nil, FileInfo{}, err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG || st.Size < 0 || st.Size > maxBytes {
		return nil, FileInfo{}, os.ErrInvalid
	}
	content, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, FileInfo{}, err
	}
	if int64(len(content)) > maxBytes {
		return nil, FileInfo{}, os.ErrInvalid
	}
	return content, statFileInfo(path.Base(relativePath), relativePath, &st), nil
}

func parentAndLeaf(relativePath string) (string, string, error) {
	if relativePath == "." {
		return "", "", fmt.Errorf("operation on document root is forbidden")
	}
	parent, leaf := path.Dir(relativePath), path.Base(relativePath)
	if leaf == "." || leaf == ".." || leaf == "" {
		return "", "", os.ErrInvalid
	}
	return parent, leaf, nil
}

func inheritParentOwnership(parentFD, childFD int) {
	var parentStat unix.Stat_t
	if unix.Fstat(parentFD, &parentStat) == nil {
		_ = unix.Fchown(childFD, int(parentStat.Uid), int(parentStat.Gid))
	}
}

// secureMkdirAllAt creates and opens a relative directory one component at a
// time. Every lookup is independently protected by openat2; an attacker that
// swaps a component for a symlink loses the race with an error, never escape.
func secureMkdirAllAt(rootFD int, relativeDir string) (int, error) {
	currentFD, err := duplicateFD(rootFD)
	if err != nil {
		return -1, err
	}
	if relativeDir == "." {
		return currentFD, nil
	}

	for _, component := range splitRelativePath(relativeDir) {
		nextFD, openErr := openFileManagerAt(currentFD, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
		if errors.Is(openErr, syscall.ENOENT) {
			if err := unix.Mkdirat(currentFD, component, 0o755); err != nil && !errors.Is(err, syscall.EEXIST) {
				unix.Close(currentFD)
				return -1, err
			}
			nextFD, openErr = openFileManagerAt(currentFD, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
			if openErr == nil {
				inheritParentOwnership(currentFD, nextFD)
			}
		}
		unix.Close(currentFD)
		if openErr != nil {
			return -1, openErr
		}
		currentFD = nextFD
	}
	return currentFD, nil
}

func splitRelativePath(relativePath string) []string {
	if relativePath == "." || relativePath == "" {
		return nil
	}
	var result []string
	for start := 0; start < len(relativePath); {
		end := start
		for end < len(relativePath) && relativePath[end] != '/' {
			end++
		}
		result = append(result, relativePath[start:end])
		start = end + 1
	}
	return result
}

func openParent(rootFD int, relativePath string, create bool) (int, string, error) {
	parent, leaf, err := parentAndLeaf(relativePath)
	if err != nil {
		return -1, "", err
	}
	if create {
		fd, err := secureMkdirAllAt(rootFD, parent)
		return fd, leaf, err
	}
	fd, err := openFileManagerAt(rootFD, parent, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	return fd, leaf, err
}

type atomicWriteSnapshot struct {
	exists bool
	dev    uint64
	ino    uint64
	uid    int
	gid    int
	mode   uint32
}

func snapshotAtomicWriteTarget(parentFD int, leaf string, defaultMode uint32) (atomicWriteSnapshot, error) {
	var st unix.Stat_t
	err := unix.Fstatat(parentFD, leaf, &st, unix.AT_SYMLINK_NOFOLLOW)
	if errors.Is(err, syscall.ENOENT) {
		var parent unix.Stat_t
		if err := unix.Fstat(parentFD, &parent); err != nil {
			return atomicWriteSnapshot{}, err
		}
		return atomicWriteSnapshot{
			uid: int(parent.Uid), gid: int(parent.Gid), mode: defaultMode & 0o777,
		}, nil
	}
	if err != nil {
		return atomicWriteSnapshot{}, err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG || st.Nlink != 1 {
		return atomicWriteSnapshot{}, os.ErrPermission
	}
	return atomicWriteSnapshot{
		exists: true,
		dev:    uint64(st.Dev),
		ino:    st.Ino,
		uid:    int(st.Uid),
		gid:    int(st.Gid),
		mode:   st.Mode & 0o777,
	}, nil
}

func atomicWriteTargetUnchanged(parentFD int, leaf string, before atomicWriteSnapshot) error {
	var current unix.Stat_t
	err := unix.Fstatat(parentFD, leaf, &current, unix.AT_SYMLINK_NOFOLLOW)
	if !before.exists {
		if errors.Is(err, syscall.ENOENT) {
			return nil
		}
		if err == nil {
			return syscall.EEXIST
		}
		return err
	}
	if err != nil {
		return err
	}
	if current.Mode&unix.S_IFMT != unix.S_IFREG || current.Nlink != 1 ||
		uint64(current.Dev) != before.dev || current.Ino != before.ino {
		return syscall.EBUSY
	}
	return nil
}

// secureAtomicWriteAt never truncates the live inode. Bytes are written and
// fsynced into a fresh inode in the same directory, then renamed over an
// unchanged regular-file target. A short write, ENOSPC or fsync failure leaves
// the previous file intact, and a concurrent symlink/hardlink swap fails closed.
func secureAtomicWriteAt(
	parentFD int,
	leaf string,
	defaultMode uint32,
	write func(*os.File) error,
) error {
	before, err := snapshotAtomicWriteTarget(parentFD, leaf, defaultMode)
	if err != nil {
		return err
	}

	var tempName string
	var fd int
	for attempts := 0; attempts < 32; attempts++ {
		var random [12]byte
		if _, err := rand.Read(random[:]); err != nil {
			return err
		}
		tempName = ".celikpanel-write-" + hex.EncodeToString(random[:])
		fd, err = openFileManagerAt(
			parentFD, tempName,
			unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_NONBLOCK,
			0o600,
		)
		if errors.Is(err, syscall.EEXIST) {
			continue
		}
		break
	}
	if err != nil {
		return err
	}
	if fd < 0 {
		return syscall.EEXIST
	}
	keepTemp := true
	defer func() {
		if keepTemp {
			_ = unix.Unlinkat(parentFD, tempName, 0)
		}
	}()

	file := os.NewFile(uintptr(fd), tempName)
	if file == nil {
		unix.Close(fd)
		return os.ErrInvalid
	}
	closed := false
	defer func() {
		if !closed {
			_ = file.Close()
		}
	}()

	var created unix.Stat_t
	if err := unix.Fstat(fd, &created); err != nil ||
		created.Mode&unix.S_IFMT != unix.S_IFREG || created.Nlink != 1 {
		if err != nil {
			return err
		}
		return os.ErrPermission
	}
	if err := unix.Fchown(fd, before.uid, before.gid); err != nil {
		return err
	}
	if err := unix.Fchmod(fd, before.mode); err != nil {
		return err
	}
	if err := write(file); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		closed = true
		return err
	}
	closed = true

	if err := atomicWriteTargetUnchanged(parentFD, leaf, before); err != nil {
		return err
	}
	if err := unix.Renameat(parentFD, tempName, parentFD, leaf); err != nil {
		return err
	}
	keepTemp = false
	return unix.Fsync(parentFD)
}

func secureWriteFile(root, relativePath string, content []byte) error {
	rootFD, err := openFileManagerRoot(root)
	if err != nil {
		return err
	}
	defer unix.Close(rootFD)

	parentFD, leaf, err := openParent(rootFD, relativePath, false)
	if err != nil {
		return err
	}
	defer unix.Close(parentFD)

	return secureAtomicWriteAt(parentFD, leaf, 0o644, func(file *os.File) error {
		_, err := file.Write(content)
		return err
	})
}

func secureCreateFileOrDir(root, relativePath string, isDir bool) error {
	rootFD, err := openFileManagerRoot(root)
	if err != nil {
		return err
	}
	defer unix.Close(rootFD)

	parentFD, leaf, err := openParent(rootFD, relativePath, true)
	if err != nil {
		return err
	}
	defer unix.Close(parentFD)

	if isDir {
		if err := unix.Mkdirat(parentFD, leaf, 0o755); err != nil {
			return err
		}
		fd, err := openFileManagerAt(parentFD, leaf, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
		if err != nil {
			return err
		}
		defer unix.Close(fd)
		inheritParentOwnership(parentFD, fd)
		return nil
	}

	fd, err := openFileManagerAt(parentFD, leaf, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW, 0o644)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	inheritParentOwnership(parentFD, fd)
	return nil
}

func secureDeleteFileOrDir(root, relativePath string) error {
	rootFD, err := openFileManagerRoot(root)
	if err != nil {
		return err
	}
	defer unix.Close(rootFD)
	parentFD, leaf, err := openParent(rootFD, relativePath, false)
	if err != nil {
		return err
	}
	defer unix.Close(parentFD)
	return secureDeleteAt(parentFD, leaf, true)
}

func secureDeleteAt(parentFD int, leaf string, rejectSymlink bool) error {
	var st unix.Stat_t
	if err := unix.Fstatat(parentFD, leaf, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return err
	}
	if st.Mode&unix.S_IFMT == unix.S_IFLNK {
		if rejectSymlink {
			return os.ErrPermission
		}
		return unix.Unlinkat(parentFD, leaf, 0)
	}
	if st.Mode&unix.S_IFMT != unix.S_IFDIR {
		return unix.Unlinkat(parentFD, leaf, 0)
	}

	dirFD, err := openFileManagerAt(parentFD, leaf, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	dir := os.NewFile(uintptr(dirFD), leaf)
	if dir == nil {
		unix.Close(dirFD)
		return os.ErrInvalid
	}
	names, readErr := dir.Readdirnames(-1)
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		dir.Close()
		return readErr
	}
	for _, name := range names {
		if err := secureDeleteAt(dirFD, name, false); err != nil {
			dir.Close()
			return err
		}
	}
	if err := dir.Close(); err != nil {
		return err
	}
	return unix.Unlinkat(parentFD, leaf, unix.AT_REMOVEDIR)
}

func secureChmodFile(root, relativePath string, mode os.FileMode) error {
	rootFD, err := openFileManagerRoot(root)
	if err != nil {
		return err
	}
	defer unix.Close(rootFD)
	fd, err := openFileManagerAt(rootFD, relativePath, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)

	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return err
	}
	fileType := st.Mode & unix.S_IFMT
	if fileType != unix.S_IFREG && fileType != unix.S_IFDIR {
		return os.ErrPermission
	}
	if fileType == unix.S_IFREG && st.Nlink > 1 {
		return os.ErrPermission
	}
	return unix.Fchmod(fd, uint32(mode.Perm()))
}

func secureRenameFile(root, oldPath, newPath string) error {
	rootFD, err := openFileManagerRoot(root)
	if err != nil {
		return err
	}
	defer unix.Close(rootFD)
	oldParentFD, oldLeaf, err := openParent(rootFD, oldPath, false)
	if err != nil {
		return err
	}
	defer unix.Close(oldParentFD)
	newParentFD, newLeaf, err := openParent(rootFD, newPath, false)
	if err != nil {
		return err
	}
	defer unix.Close(newParentFD)

	var st unix.Stat_t
	if err := unix.Fstatat(oldParentFD, oldLeaf, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return err
	}
	if st.Mode&unix.S_IFMT == unix.S_IFLNK {
		return os.ErrPermission
	}
	return unix.Renameat2(oldParentFD, oldLeaf, newParentFD, newLeaf, unix.RENAME_NOREPLACE)
}

func secureUploadFile(root, relativePath string, content []byte) error {
	return secureWriteFile(root, relativePath, content)
}
