//go:build linux

package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

const secureConfigResolve = unix.RESOLVE_BENEATH |
	unix.RESOLVE_NO_SYMLINKS |
	unix.RESOLVE_NO_MAGICLINKS

func secureConfigRelativePath(path string) (string, error) {
	clean := filepath.Clean(path)
	if !filepath.IsAbs(clean) || clean == string(os.PathSeparator) {
		return "", configPathRefusal("managed configuration path must be an absolute file path: %s", path)
	}
	relative := strings.TrimPrefix(clean, string(os.PathSeparator))
	if relative == "" || relative == "." {
		return "", configPathRefusal("managed configuration path must name a file: %s", path)
	}
	return relative, nil
}

func openSecureConfigRoot() (int, error) {
	fd, err := unix.Open("/", unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, fmt.Errorf("open managed configuration root: %w", err)
	}
	return fd, nil
}

func secureConfigOpenError(operation, path string, err error) error {
	if errors.Is(err, unix.ELOOP) || errors.Is(err, unix.EXDEV) {
		return fmt.Errorf("%w: %s refused because the path contains a symbolic link or escapes the managed root (%s): %v", errConfigPathRefused, operation, path, err)
	}
	if errors.Is(err, unix.ENOSYS) {
		return fmt.Errorf("%s refused because secure openat2 path resolution is unavailable: %w", operation, err)
	}
	return fmt.Errorf("%s %s: %w", operation, path, err)
}

func secureReadConfig(path string) ([]byte, error) {
	relative, err := secureConfigRelativePath(path)
	if err != nil {
		return nil, err
	}
	rootFD, err := openSecureConfigRoot()
	if err != nil {
		return nil, err
	}
	defer unix.Close(rootFD)

	fd, err := unix.Openat2(rootFD, relative, &unix.OpenHow{
		Flags:   uint64(unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK),
		Resolve: secureConfigResolve,
	})
	if err != nil {
		return nil, secureConfigOpenError("read managed configuration", path, err)
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		unix.Close(fd)
		return nil, fmt.Errorf("read managed configuration %s: invalid file descriptor", path)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat managed configuration %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, configPathRefusal("read managed configuration refused for non-regular file: %s", path)
	}
	content, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("read managed configuration %s: %w", path, err)
	}
	return content, nil
}

func secureWriteConfig(path string, content []byte, mode os.FileMode) error {
	relative, err := secureConfigRelativePath(path)
	if err != nil {
		return err
	}
	rootFD, err := openSecureConfigRoot()
	if err != nil {
		return err
	}
	defer unix.Close(rootFD)

	parent := filepath.Dir(relative)
	base := filepath.Base(relative)
	parentFD, err := unix.Openat2(rootFD, parent, &unix.OpenHow{
		Flags:   uint64(unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW),
		Resolve: secureConfigResolve,
	})
	if err != nil {
		return secureConfigOpenError("write managed configuration", path, err)
	}
	defer unix.Close(parentFD)

	fileMode := uint32(mode.Perm())
	ownerUID, ownerGID := -1, -1
	existingFD, err := unix.Openat2(parentFD, base, &unix.OpenHow{
		Flags:   uint64(unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK),
		Resolve: secureConfigResolve,
	})
	if err == nil {
		var stat unix.Stat_t
		if err := unix.Fstat(existingFD, &stat); err != nil {
			unix.Close(existingFD)
			return fmt.Errorf("stat managed configuration %s: %w", path, err)
		}
		unix.Close(existingFD)
		if stat.Mode&unix.S_IFMT != unix.S_IFREG {
			return configPathRefusal("write managed configuration refused for non-regular file: %s", path)
		}
		fileMode = stat.Mode & 0o777
		ownerUID, ownerGID = int(stat.Uid), int(stat.Gid)
	} else if !errors.Is(err, unix.ENOENT) {
		return secureConfigOpenError("inspect managed configuration", path, err)
	}

	var tempName string
	var fd int
	for attempt := 0; attempt < 16; attempt++ {
		random := make([]byte, 12)
		if _, err := rand.Read(random); err != nil {
			return fmt.Errorf("prepare atomic managed configuration write %s: %w", path, err)
		}
		tempName = "." + base + ".celikpanel-" + hex.EncodeToString(random)
		fd, err = unix.Openat(parentFD, tempName,
			unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW,
			fileMode)
		if errors.Is(err, unix.EEXIST) {
			continue
		}
		break
	}
	if err != nil {
		return secureConfigOpenError("create atomic managed configuration", path, err)
	}
	if fd < 0 {
		return fmt.Errorf("create atomic managed configuration %s: no unique temporary name", path)
	}
	published := false
	defer func() {
		if !published {
			_ = unix.Unlinkat(parentFD, tempName, 0)
		}
	}()

	file := os.NewFile(uintptr(fd), path+" (atomic replacement)")
	if file == nil {
		unix.Close(fd)
		return fmt.Errorf("write managed configuration %s: invalid file descriptor", path)
	}
	closed := false
	defer func() {
		if !closed {
			_ = file.Close()
		}
	}()
	if ownerUID >= 0 {
		if err := unix.Fchown(fd, ownerUID, ownerGID); err != nil {
			return fmt.Errorf("preserve managed configuration ownership %s: %w", path, err)
		}
	}
	if err := unix.Fchmod(fd, fileMode); err != nil {
		return fmt.Errorf("preserve managed configuration mode %s: %w", path, err)
	}
	if _, err := io.Copy(file, bytes.NewReader(content)); err != nil {
		return fmt.Errorf("write managed configuration %s: %w", path, err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync managed configuration %s: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close managed configuration %s: %w", path, err)
	}
	closed = true
	if err := unix.Renameat(parentFD, tempName, parentFD, base); err != nil {
		return secureConfigOpenError("publish atomic managed configuration", path, err)
	}
	published = true
	if err := unix.Fsync(parentFD); err != nil {
		return fmt.Errorf("sync managed configuration directory %s: %w", filepath.Dir(path), err)
	}
	return nil
}

func secureRemoveConfig(path string) error {
	relative, err := secureConfigRelativePath(path)
	if err != nil {
		return err
	}
	parent := filepath.Dir(relative)
	base := filepath.Base(relative)

	rootFD, err := openSecureConfigRoot()
	if err != nil {
		return err
	}
	defer unix.Close(rootFD)

	parentFD, err := unix.Openat2(rootFD, parent, &unix.OpenHow{
		// A readable directory descriptor is required because the successful
		// unlink is followed by fsync(2). O_PATH descriptors cannot be synced
		// and would turn an otherwise successful removal into EBADF.
		Flags:   uint64(unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW),
		Resolve: secureConfigResolve,
	})
	if err != nil {
		return secureConfigOpenError("remove managed configuration", path, err)
	}
	defer unix.Close(parentFD)

	targetFD, err := unix.Openat2(parentFD, base, &unix.OpenHow{
		Flags:   uint64(unix.O_PATH | unix.O_CLOEXEC | unix.O_NOFOLLOW),
		Resolve: secureConfigResolve,
	})
	if err != nil {
		return secureConfigOpenError("remove managed configuration", path, err)
	}
	target := os.NewFile(uintptr(targetFD), path)
	if target == nil {
		unix.Close(targetFD)
		return fmt.Errorf("remove managed configuration %s: invalid file descriptor", path)
	}
	info, err := target.Stat()
	target.Close()
	if err != nil {
		return fmt.Errorf("stat managed configuration %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return configPathRefusal("remove managed configuration refused for non-regular file: %s", path)
	}

	if err := unix.Unlinkat(parentFD, base, 0); err != nil {
		return secureConfigOpenError("remove managed configuration", path, err)
	}
	if err := unix.Fsync(parentFD); err != nil {
		return fmt.Errorf("sync managed configuration directory %s: %w", filepath.Dir(path), err)
	}
	return nil
}
