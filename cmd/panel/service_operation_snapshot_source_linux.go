//go:build linux

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

type exactPinnedSQLiteFile struct {
	file *os.File
	stat unix.Stat_t
}

type quarantinedServiceOperationSnapshotSource struct {
	parentPath string
	baseName   string
	parent     *os.File
	owner      serviceOperationRestoreOwner
	initial    unix.Stat_t
	database   exactPinnedSQLiteFile
	sidecars   map[string]*exactPinnedSQLiteFile
	locked     bool
}

func createReleaseServiceOperationSnapshotWithOwner(
	sourcePath string,
	destinationPath string,
	schema serviceOperationSnapshotSchema,
	owner serviceOperationRestoreOwner,
) (returnErr error) {
	if os.Geteuid() != 0 {
		return fmt.Errorf("service operation snapshot must run as root")
	}
	if owner.uid == 0 || owner.gid == 0 {
		return fmt.Errorf("service operation snapshot source owner must be non-root")
	}
	source, err := openQuarantinedServiceOperationSnapshotSource(sourcePath, owner)
	if err != nil {
		return err
	}
	defer func() {
		returnErr = errors.Join(returnErr, source.close())
	}()
	return createVerifiedQuarantinedServiceOperationSnapshot(
		source.sqlitePath(),
		destinationPath,
		schema,
		source.verify,
	)
}

func openQuarantinedServiceOperationSnapshotSource(
	sourcePath string,
	owner serviceOperationRestoreOwner,
) (*quarantinedServiceOperationSnapshotSource, error) {
	if sourcePath == "" ||
		!filepath.IsAbs(sourcePath) ||
		filepath.Clean(sourcePath) != sourcePath ||
		filepath.Base(sourcePath) != serviceOperationSnapshotBasename {
		return nil, fmt.Errorf("canonical snapshot source must be a clean absolute %s path", serviceOperationSnapshotBasename)
	}
	parentPath := filepath.Dir(sourcePath)
	if err := validateRootOwnedSnapshotDirectoryChain(filepath.Dir(parentPath)); err != nil {
		return nil, fmt.Errorf("validate canonical database ancestor chain: %w", err)
	}
	parentFD, err := unix.Open(
		parentPath,
		unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		0,
	)
	if err != nil {
		return nil, fmt.Errorf("open canonical snapshot source parent: %w", err)
	}
	parent := os.NewFile(uintptr(parentFD), parentPath)
	if parent == nil {
		_ = unix.Close(parentFD)
		return nil, fmt.Errorf("open canonical snapshot source parent handle")
	}
	source := &quarantinedServiceOperationSnapshotSource{
		parentPath: parentPath,
		baseName:   filepath.Base(sourcePath),
		parent:     parent,
		owner:      owner,
		sidecars:   make(map[string]*exactPinnedSQLiteFile, 3),
	}
	fail := func(cause error) (*quarantinedServiceOperationSnapshotSource, error) {
		return nil, errors.Join(cause, source.close())
	}
	if err := source.verifyParent(false); err != nil {
		return fail(err)
	}
	var databasePathStat unix.Stat_t
	if err := unix.Fstatat(parentFD, source.baseName, &databasePathStat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return fail(fmt.Errorf("inspect canonical snapshot source: %w", err))
	}
	if err := validatePanelOwnedDatabaseFile(databasePathStat, owner, "canonical snapshot source"); err != nil {
		return fail(err)
	}
	source.initial = databasePathStat
	if err := rejectDatabaseQuarantineUsersAndHandles("/proc", owner.uid, databasePathStat); err != nil {
		return fail(err)
	}
	if err := source.lockParent(); err != nil {
		return fail(err)
	}
	if err := source.verifyParent(true); err != nil {
		return fail(err)
	}
	if err := rejectDatabaseQuarantineUsersAndHandles("/proc", owner.uid, databasePathStat); err != nil {
		return fail(err)
	}
	if err := source.pinDatabaseAndSidecars(); err != nil {
		return fail(err)
	}
	if err := source.verify(); err != nil {
		return fail(err)
	}
	return source, nil
}

func validatePanelOwnedDatabaseFile(
	stat unix.Stat_t,
	owner serviceOperationRestoreOwner,
	purpose string,
) error {
	if stat.Mode&unix.S_IFMT != unix.S_IFREG ||
		stat.Uid != owner.uid ||
		stat.Gid != owner.gid ||
		stat.Mode&0o777 != 0o600 ||
		stat.Nlink != 1 {
		return fmt.Errorf("%s must be a celikpanel-owned single-link 0600 regular file", purpose)
	}
	return nil
}

func (s *quarantinedServiceOperationSnapshotSource) verifyParent(locked bool) error {
	if s == nil || s.parent == nil {
		return fmt.Errorf("canonical snapshot source parent is not open")
	}
	if err := validateRootOwnedSnapshotDirectoryChain(filepath.Dir(s.parentPath)); err != nil {
		return fmt.Errorf("revalidate canonical database ancestor chain: %w", err)
	}
	pathInfo, err := os.Lstat(s.parentPath)
	if err != nil {
		return fmt.Errorf("inspect canonical snapshot source parent path: %w", err)
	}
	pinnedInfo, err := s.parent.Stat()
	if err != nil {
		return fmt.Errorf("inspect pinned canonical snapshot source parent: %w", err)
	}
	if !os.SameFile(pathInfo, pinnedInfo) {
		return fmt.Errorf("canonical snapshot source parent changed while pinned")
	}
	stat, ok := pinnedInfo.Sys().(*syscall.Stat_t)
	if !ok || !pinnedInfo.IsDir() || pinnedInfo.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("canonical snapshot source parent must be a real directory")
	}
	if locked {
		if pinnedInfo.Mode().Perm() != 0o700 || stat.Uid != 0 || stat.Gid != 0 {
			return fmt.Errorf("canonical snapshot source parent must be root:root mode 0700 while quarantined")
		}
		return nil
	}
	rootOwned := stat.Uid == 0 && stat.Gid == 0
	panelOwned := stat.Uid == s.owner.uid && stat.Gid == s.owner.gid
	recoverableMode := pinnedInfo.Mode().Perm() == 0o700 || pinnedInfo.Mode().Perm() == 0o750
	if (rootOwned || panelOwned) && recoverableMode {
		return nil
	}
	return fmt.Errorf("canonical snapshot source parent metadata is not a secure normal or recoverable quarantine state")
}

func (s *quarantinedServiceOperationSnapshotSource) lockParent() error {
	if err := unix.Fchown(int(s.parent.Fd()), 0, 0); err != nil {
		return fmt.Errorf("quarantine canonical snapshot source ownership: %w", err)
	}
	if err := unix.Fchmod(int(s.parent.Fd()), 0o700); err != nil {
		return fmt.Errorf("quarantine canonical snapshot source mode: %w", err)
	}
	if err := s.parent.Sync(); err != nil {
		return fmt.Errorf("sync quarantined canonical snapshot source parent: %w", err)
	}
	s.locked = true
	return nil
}

func (s *quarantinedServiceOperationSnapshotSource) pinEntry(
	suffix string,
) (*exactPinnedSQLiteFile, error) {
	baseName := s.baseName + suffix
	var pathStat unix.Stat_t
	err := unix.Fstatat(int(s.parent.Fd()), baseName, &pathStat, unix.AT_SYMLINK_NOFOLLOW)
	if errors.Is(err, unix.ENOENT) && suffix != "" {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("inspect canonical SQLite source %s: %w", baseName, err)
	}
	if err := validatePanelOwnedDatabaseFile(pathStat, s.owner, "canonical SQLite source "+baseName); err != nil {
		return nil, err
	}
	fd, err := unix.Openat(
		int(s.parent.Fd()),
		baseName,
		unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		0,
	)
	if err != nil {
		return nil, fmt.Errorf("pin canonical SQLite source %s: %w", baseName, err)
	}
	file := os.NewFile(uintptr(fd), filepath.Join(s.parentPath, baseName))
	if file == nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("open canonical SQLite source %s handle", baseName)
	}
	var descriptorStat unix.Stat_t
	if err := unix.Fstat(fd, &descriptorStat); err != nil {
		file.Close()
		return nil, fmt.Errorf("inspect pinned canonical SQLite source %s: %w", baseName, err)
	}
	if !sameExactUnixFileMetadata(pathStat, descriptorStat) {
		file.Close()
		return nil, fmt.Errorf("canonical SQLite source %s changed while pinning", baseName)
	}
	return &exactPinnedSQLiteFile{file: file, stat: descriptorStat}, nil
}

func (s *quarantinedServiceOperationSnapshotSource) pinDatabaseAndSidecars() error {
	database, err := s.pinEntry("")
	if err != nil {
		return err
	}
	s.database = *database
	if !sameExactUnixFileMetadata(s.initial, s.database.stat) {
		return fmt.Errorf("canonical SQLite source changed before it was pinned")
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		sidecar, err := s.pinEntry(suffix)
		if err != nil {
			return err
		}
		s.sidecars[suffix] = sidecar
		if sidecar != nil {
			return fmt.Errorf("canonical SQLite source %s%s must be absent before release snapshot", s.baseName, suffix)
		}
	}
	return nil
}

func (s *quarantinedServiceOperationSnapshotSource) verifyEntry(
	suffix string,
	pinned *exactPinnedSQLiteFile,
) error {
	baseName := s.baseName + suffix
	var pathStat unix.Stat_t
	err := unix.Fstatat(int(s.parent.Fd()), baseName, &pathStat, unix.AT_SYMLINK_NOFOLLOW)
	if pinned == nil {
		if errors.Is(err, unix.ENOENT) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("inspect canonical SQLite source %s: %w", baseName, err)
		}
		return fmt.Errorf("canonical SQLite source %s appeared after quarantine", baseName)
	}
	if err != nil {
		return fmt.Errorf("reinspect canonical SQLite source %s: %w", baseName, err)
	}
	var descriptorStat unix.Stat_t
	if err := unix.Fstat(int(pinned.file.Fd()), &descriptorStat); err != nil {
		return fmt.Errorf("reinspect pinned canonical SQLite source %s: %w", baseName, err)
	}
	if !sameExactUnixFileMetadata(pinned.stat, pathStat) ||
		!sameExactUnixFileMetadata(pinned.stat, descriptorStat) {
		return fmt.Errorf("canonical SQLite source %s changed while quarantined", baseName)
	}
	return nil
}

func (s *quarantinedServiceOperationSnapshotSource) verify() error {
	if err := s.verifyParent(true); err != nil {
		return err
	}
	if err := s.verifyEntry("", &s.database); err != nil {
		return err
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if err := s.verifyEntry(suffix, s.sidecars[suffix]); err != nil {
			return err
		}
	}
	return nil
}

func (s *quarantinedServiceOperationSnapshotSource) sqlitePath() string {
	return fmt.Sprintf("/proc/self/fd/%d", s.database.file.Fd())
}

func (s *quarantinedServiceOperationSnapshotSource) closePinnedEntries() error {
	var closeErr error
	for _, sidecar := range s.sidecars {
		if sidecar != nil && sidecar.file != nil {
			closeErr = errors.Join(closeErr, sidecar.file.Close())
			sidecar.file = nil
		}
	}
	if s.database.file != nil {
		closeErr = errors.Join(closeErr, s.database.file.Close())
		s.database.file = nil
	}
	return closeErr
}

func (s *quarantinedServiceOperationSnapshotSource) unlockParent() error {
	if s == nil || s.parent == nil || !s.locked {
		return nil
	}
	if err := rejectDatabaseQuarantineUsersAndHandles("/proc", s.owner.uid, s.database.stat); err != nil {
		return err
	}
	if err := s.verifyParent(true); err != nil {
		return err
	}
	var unlockErr error
	if err := unix.Fchown(int(s.parent.Fd()), int(s.owner.uid), int(s.owner.gid)); err != nil {
		unlockErr = errors.Join(unlockErr, fmt.Errorf("restore canonical snapshot source parent ownership: %w", err))
	}
	if err := unix.Fchmod(int(s.parent.Fd()), 0o750); err != nil {
		unlockErr = errors.Join(unlockErr, fmt.Errorf("restore canonical snapshot source parent mode: %w", err))
	}
	if err := s.parent.Sync(); err != nil {
		unlockErr = errors.Join(unlockErr, fmt.Errorf("sync restored canonical snapshot source parent: %w", err))
	}
	if unlockErr == nil {
		s.locked = false
	}
	return unlockErr
}

func (s *quarantinedServiceOperationSnapshotSource) close() error {
	if s == nil {
		return nil
	}
	closeErr := s.closePinnedEntries()
	if s.parent != nil {
		closeErr = errors.Join(closeErr, s.unlockParent())
		closeErr = errors.Join(closeErr, s.parent.Close())
		s.parent = nil
	}
	return closeErr
}
