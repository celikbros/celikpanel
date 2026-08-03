//go:build linux

package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"

	"golang.org/x/sys/unix"
)

type wordpressOwnership struct {
	uid int
	gid int
}

func wordpressInstalledMode(path string, directory bool) os.FileMode {
	if directory {
		return 0o750 | os.ModeSetgid
	}
	if filepath.Base(path) == "wp-config.php" {
		return 0o600
	}
	return 0o640
}

func resolveWordPressOwnership(username string) (wordpressOwnership, error) {
	account, err := user.Lookup(username)
	if err != nil {
		return wordpressOwnership{}, fmt.Errorf("lookup site user: %w", err)
	}
	uid, err := strconv.Atoi(account.Uid)
	if err != nil || uid < 1000 {
		return wordpressOwnership{}, fmt.Errorf("site user must have a non-system uid")
	}
	groupName := webServerGroup()
	group, err := user.LookupGroup(groupName)
	if err != nil {
		return wordpressOwnership{}, fmt.Errorf("lookup web-server group %q: %w", groupName, err)
	}
	gid, err := strconv.Atoi(group.Gid)
	if err != nil || gid < 0 {
		return wordpressOwnership{}, fmt.Errorf("web-server group has an invalid gid")
	}
	return wordpressOwnership{uid: uid, gid: gid}, nil
}

func requireWordPressStagingParent(directory string) error {
	info, err := os.Lstat(directory)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("staging parent is not a real directory")
	}
	var stat unix.Stat_t
	if err := unix.Lstat(directory, &stat); err != nil {
		return err
	}
	if stat.Uid != 0 {
		return fmt.Errorf("staging parent is not owned by root")
	}
	if info.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("staging parent is group or world writable")
	}
	return nil
}

func applyWordPressOwnership(root, username string) error {
	ownership, err := resolveWordPressOwnership(username)
	if err != nil {
		return err
	}

	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || (!info.Mode().IsRegular() && !info.IsDir()) {
			return fmt.Errorf("unsafe staged path %s", path)
		}
		if path == root {
			var stat unix.Stat_t
			if err := unix.Lstat(path, &stat); err != nil {
				return err
			}
			if stat.Uid != 0 || info.Mode().Perm()&0o077 != 0 {
				return fmt.Errorf("staging root is not root-owned and private")
			}
			return nil
		}
		mode := wordpressInstalledMode(path, info.IsDir())
		if err := os.Chown(path, ownership.uid, ownership.gid); err != nil {
			return err
		}
		if err := os.Chmod(path, mode); err != nil {
			return err
		}
		var stat unix.Stat_t
		if err := unix.Lstat(path, &stat); err != nil {
			return err
		}
		if int(stat.Uid) != ownership.uid || int(stat.Gid) != ownership.gid {
			return fmt.Errorf("ownership verification failed for %s", path)
		}
		return nil
	})
}

type linuxWordPressPathExchange struct {
	sitesFD      int
	siteFD       int
	stageFD      int
	originalFD   int
	stageName    string
	siteName     string
	documentName string
	siteMode     uint32
	locked       bool
}

func wordPressSameInode(left, right *unix.Stat_t) bool {
	return left.Dev == right.Dev && left.Ino == right.Ino
}

func wordPressFileInfoMatchesStat(info os.FileInfo, stat *unix.Stat_t) bool {
	source, ok := info.Sys().(*syscall.Stat_t)
	return ok && uint64(source.Dev) == uint64(stat.Dev) && source.Ino == stat.Ino
}

func wordPressDirectoryStat(fd int) (unix.Stat_t, error) {
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return stat, err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR {
		return stat, fmt.Errorf("held WordPress path is not a directory")
	}
	return stat, nil
}

func wordPressDirectoryStatAt(fd int, name string) (unix.Stat_t, error) {
	var stat unix.Stat_t
	if err := unix.Fstatat(fd, name, &stat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return stat, err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR {
		return stat, fmt.Errorf("WordPress path binding %q is not a directory", name)
	}
	return stat, nil
}

func requireWordPressDirectoryBinding(parentFD int, name string, heldFD int) error {
	held, err := wordPressDirectoryStat(heldFD)
	if err != nil {
		return err
	}
	bound, err := wordPressDirectoryStatAt(parentFD, name)
	if err != nil {
		return err
	}
	if !wordPressSameInode(&held, &bound) {
		return fmt.Errorf("WordPress path binding %q changed", name)
	}
	return nil
}

func prepareWordPressPathExchange(stageDir, documentRoot string) (wordpressPathExchange, error) {
	return prepareWordPressPathExchangeOwnedBy(stageDir, documentRoot, 0)
}

// prepareWordPressPathExchangeOwnedBy exists so the descriptor-exchange tests
// can exercise the real Linux implementation with directories owned by the
// unprivileged test runner. Production always enters through
// prepareWordPressPathExchange, which requires the staged tree to be owned by
// root (UID 0).
func prepareWordPressPathExchangeOwnedBy(
	stageDir, documentRoot string,
	expectedStageOwnerUID uint32,
) (wordpressPathExchange, error) {
	stageDir = filepath.Clean(stageDir)
	documentRoot = filepath.Clean(documentRoot)
	sitesDir := filepath.Dir(stageDir)
	siteDir := filepath.Dir(documentRoot)
	if filepath.Dir(siteDir) != sitesDir {
		return nil, fmt.Errorf("staging and document root are not in the same trusted sites tree")
	}
	stageName := filepath.Base(stageDir)
	siteName := filepath.Base(siteDir)
	documentName := filepath.Base(documentRoot)
	for _, name := range []string{stageName, siteName, documentName} {
		if name == "." || name == ".." || filepath.Base(name) != name {
			return nil, fmt.Errorf("invalid WordPress exchange path component")
		}
	}

	sitesFD, err := unix.Open(
		sitesDir,
		unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		0,
	)
	if err != nil {
		return nil, fmt.Errorf("open trusted sites directory: %w", err)
	}
	siteFD, err := unix.Openat(
		sitesFD,
		siteName,
		unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		0,
	)
	if err != nil {
		_ = unix.Close(sitesFD)
		return nil, fmt.Errorf("open immutable site directory: %w", err)
	}
	stageFD, err := unix.Openat(
		sitesFD,
		stageName,
		unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		0,
	)
	if err != nil {
		_ = unix.Close(siteFD)
		_ = unix.Close(sitesFD)
		return nil, fmt.Errorf("open staged WordPress root: %w", err)
	}
	originalFD, err := unix.Openat(
		siteFD,
		documentName,
		unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		0,
	)
	if err != nil {
		_ = unix.Close(stageFD)
		_ = unix.Close(siteFD)
		_ = unix.Close(sitesFD)
		return nil, fmt.Errorf("open original document root: %w", err)
	}
	siteStat, err := wordPressDirectoryStat(siteFD)
	if err != nil {
		_ = unix.Close(originalFD)
		_ = unix.Close(stageFD)
		_ = unix.Close(siteFD)
		_ = unix.Close(sitesFD)
		return nil, fmt.Errorf("inspect immutable site directory: %w", err)
	}
	stageStat, err := wordPressDirectoryStat(stageFD)
	if err != nil {
		_ = unix.Close(originalFD)
		_ = unix.Close(stageFD)
		_ = unix.Close(siteFD)
		_ = unix.Close(sitesFD)
		return nil, fmt.Errorf("inspect staged WordPress root: %w", err)
	}
	if stageStat.Uid != expectedStageOwnerUID || stageStat.Mode&0o077 != 0 {
		_ = unix.Close(originalFD)
		_ = unix.Close(stageFD)
		_ = unix.Close(siteFD)
		_ = unix.Close(sitesFD)
		return nil, fmt.Errorf("staged WordPress root is not root-owned and private")
	}
	exchange := &linuxWordPressPathExchange{
		sitesFD:      sitesFD,
		siteFD:       siteFD,
		stageFD:      stageFD,
		originalFD:   originalFD,
		stageName:    stageName,
		siteName:     siteName,
		documentName: documentName,
		siteMode:     siteStat.Mode & 0o7777,
	}
	if err := requireWordPressDirectoryBinding(sitesFD, siteName, siteFD); err != nil {
		_ = exchange.Close()
		return nil, fmt.Errorf("validate site-directory binding: %w", err)
	}
	if err := requireWordPressDirectoryBinding(sitesFD, stageName, stageFD); err != nil {
		_ = exchange.Close()
		return nil, fmt.Errorf("validate staged-root binding: %w", err)
	}
	if err := requireWordPressDirectoryBinding(siteFD, documentName, originalFD); err != nil {
		_ = exchange.Close()
		return nil, fmt.Errorf("validate document-root binding: %w", err)
	}
	return exchange, nil
}

func (exchange *linuxWordPressPathExchange) LockPaths() error {
	if exchange.locked {
		return fmt.Errorf("WordPress publication paths are already locked")
	}
	if err := unix.Fchmod(exchange.siteFD, exchange.siteMode&^0o222); err != nil {
		return fmt.Errorf("lock site-home directory: %w", err)
	}
	exchange.locked = true
	fail := func(cause error) error {
		return errors.Join(cause, exchange.UnlockPaths())
	}
	if err := unix.Fsync(exchange.siteFD); err != nil {
		return fail(fmt.Errorf("sync locked site-home directory: %w", err))
	}
	if err := requireWordPressDirectoryBinding(
		exchange.sitesFD, exchange.siteName, exchange.siteFD,
	); err != nil {
		return fail(fmt.Errorf("validate locked site-directory binding: %w", err))
	}
	if err := requireWordPressDirectoryBinding(
		exchange.sitesFD, exchange.stageName, exchange.stageFD,
	); err != nil {
		return fail(fmt.Errorf("validate locked staged-root binding: %w", err))
	}
	if err := requireWordPressDirectoryBinding(
		exchange.siteFD, exchange.documentName, exchange.originalFD,
	); err != nil {
		return fail(fmt.Errorf("validate locked document-root binding: %w", err))
	}
	return nil
}

func (exchange *linuxWordPressPathExchange) Exchange() error {
	if !exchange.locked {
		return fmt.Errorf("WordPress publication paths are not locked")
	}
	return unix.Renameat2(
		exchange.sitesFD,
		exchange.stageName,
		exchange.siteFD,
		exchange.documentName,
		unix.RENAME_EXCHANGE,
	)
}

func (exchange *linuxWordPressPathExchange) PublishedRootMatches(expected os.FileInfo) (bool, error) {
	if err := requireWordPressDirectoryBinding(
		exchange.sitesFD, exchange.siteName, exchange.siteFD,
	); err != nil {
		return false, err
	}
	held, err := wordPressDirectoryStat(exchange.stageFD)
	if err != nil {
		return false, err
	}
	if !wordPressFileInfoMatchesStat(expected, &held) {
		return false, nil
	}
	published, err := wordPressDirectoryStatAt(
		exchange.siteFD, exchange.documentName,
	)
	if err != nil {
		return false, err
	}
	return wordPressSameInode(&held, &published), nil
}

func (exchange *linuxWordPressPathExchange) SealOriginalRoot(expected os.FileInfo) error {
	return exchange.sealOriginalRootOwnedBy(expected, 0, 0)
}

// sealOriginalRootOwnedBy keeps the production root/root policy explicit while
// allowing unprivileged tests to exercise the complete descriptor-bound seal
// sequence using their own temporary-directory ownership.
func (exchange *linuxWordPressPathExchange) sealOriginalRootOwnedBy(
	expected os.FileInfo,
	expectedUID, expectedGID uint32,
) error {
	held, err := wordPressDirectoryStat(exchange.originalFD)
	if err != nil {
		return err
	}
	if !wordPressFileInfoMatchesStat(expected, &held) {
		return fmt.Errorf("original document-root identity changed")
	}
	bound, err := wordPressDirectoryStatAt(exchange.sitesFD, exchange.stageName)
	if err != nil {
		return err
	}
	if !wordPressSameInode(&held, &bound) {
		return fmt.Errorf("original document root is no longer in its recovery path")
	}
	if err := unix.Fchown(exchange.originalFD, int(expectedUID), int(expectedGID)); err != nil {
		return fmt.Errorf("seal original document-root ownership: %w", err)
	}
	if err := unix.Fchmod(exchange.originalFD, 0o700); err != nil {
		return fmt.Errorf("seal original document-root permissions: %w", err)
	}
	if err := unix.Fsync(exchange.originalFD); err != nil {
		return fmt.Errorf("sync sealed original document root: %w", err)
	}
	sealed, err := wordPressDirectoryStat(exchange.originalFD)
	if err != nil {
		return err
	}
	if sealed.Uid != expectedUID || sealed.Gid != expectedGID || sealed.Mode&0o7777 != 0o700 {
		return fmt.Errorf("sealed original document-root verification failed")
	}
	bound, err = wordPressDirectoryStatAt(exchange.sitesFD, exchange.stageName)
	if err != nil {
		return err
	}
	if !wordPressSameInode(&sealed, &bound) {
		return fmt.Errorf("sealed original document-root binding changed")
	}
	return nil
}

func (exchange *linuxWordPressPathExchange) FinalizePublishedRoot(username string) error {
	ownership, err := resolveWordPressOwnership(username)
	if err != nil {
		return err
	}
	if err := unix.Fchown(exchange.stageFD, ownership.uid, ownership.gid); err != nil {
		return err
	}
	if err := unix.Fchmod(exchange.stageFD, 0o2750); err != nil {
		return err
	}
	stat, err := wordPressDirectoryStat(exchange.stageFD)
	if err != nil {
		return err
	}
	if int(stat.Uid) != ownership.uid || int(stat.Gid) != ownership.gid ||
		stat.Mode&0o7777 != 0o2750 {
		return fmt.Errorf("published root ownership verification failed")
	}
	return nil
}

func (exchange *linuxWordPressPathExchange) SyncPublishedRoot() error {
	return unix.Fsync(exchange.stageFD)
}

func (exchange *linuxWordPressPathExchange) SyncParents() error {
	if err := unix.Fsync(exchange.siteFD); err != nil {
		return err
	}
	return unix.Fsync(exchange.sitesFD)
}

func (exchange *linuxWordPressPathExchange) UnlockPaths() error {
	if !exchange.locked {
		return nil
	}
	if err := unix.Fchmod(exchange.siteFD, exchange.siteMode); err != nil {
		return fmt.Errorf("restore site-home permissions: %w", err)
	}
	exchange.locked = false
	if err := unix.Fsync(exchange.siteFD); err != nil {
		return fmt.Errorf("sync restored site-home permissions: %w", err)
	}
	return unix.Fsync(exchange.sitesFD)
}

func (exchange *linuxWordPressPathExchange) Close() error {
	return errors.Join(
		exchange.UnlockPaths(),
		unix.Close(exchange.originalFD),
		unix.Close(exchange.stageFD),
		unix.Close(exchange.siteFD),
		unix.Close(exchange.sitesFD),
	)
}

func readWordPressPlaceholder(
	path string,
	maximum int64,
) ([]byte, os.FileInfo, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return nil, nil, fmt.Errorf("open placeholder file")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, nil, err
	}
	if !info.Mode().IsRegular() || info.Size() < 0 || info.Size() > maximum {
		return nil, nil, fmt.Errorf("placeholder is not a bounded regular file")
	}
	content, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil {
		return nil, nil, err
	}
	if int64(len(content)) > maximum {
		return nil, nil, fmt.Errorf("placeholder exceeds its canonical size")
	}
	return content, info, nil
}

func syncWordPressTree(root string) error {
	var directories []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refuse to sync symbolic link %s", path)
		}
		if info.IsDir() {
			directories = append(directories, path)
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("refuse to sync special file %s", path)
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		syncErr := file.Sync()
		closeErr := file.Close()
		if syncErr != nil {
			return syncErr
		}
		return closeErr
	})
	if err != nil {
		return err
	}
	for index := len(directories) - 1; index >= 0; index-- {
		if err := syncWordPressDirectories(directories[index]); err != nil {
			return err
		}
	}
	return nil
}

func syncWordPressDirectories(paths ...string) error {
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		path = filepath.Clean(path)
		if _, exists := seen[path]; exists {
			continue
		}
		seen[path] = struct{}{}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("sync target is not a real directory: %s", path)
		}
		directory, err := os.Open(path)
		if err != nil {
			return err
		}
		syncErr := directory.Sync()
		closeErr := directory.Close()
		if syncErr != nil {
			return syncErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}
