//go:build linux

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func ensureServiceOperationRescueSnapshotWithOwner(
	sourcePath string,
	destinationPath string,
	schema serviceOperationSnapshotSchema,
	owner serviceOperationRestoreOwner,
) (returnErr error) {
	if os.Geteuid() != 0 {
		return fmt.Errorf("service operation rescue snapshot must run as root")
	}
	if owner.uid == 0 || owner.gid == 0 {
		return fmt.Errorf("service operation rescue snapshot source owner must be non-root")
	}
	exists, err := validateExistingServiceOperationRescueSnapshot(destinationPath, schema)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}

	source, err := openQuarantinedServiceOperationSnapshotSource(sourcePath, owner)
	if err != nil {
		return err
	}
	defer func() {
		returnErr = errors.Join(returnErr, source.close())
	}()
	if err := source.createVerifiedSnapshotFromPinnedWAL(destinationPath, schema); err != nil {
		return err
	}
	exists, err = validateExistingServiceOperationRescueSnapshot(destinationPath, schema)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("service operation rescue snapshot disappeared after publication")
	}
	return nil
}

// validateExistingServiceOperationRescueSnapshot is deliberately descriptor-
// based. A pre-existing rescue artifact is idempotent only when its complete
// root-only path and exact inode remain unchanged while the schema and idle
// contracts are checked, and no SQLite sidecar exists before or after.
func validateExistingServiceOperationRescueSnapshot(
	destinationPath string,
	schema serviceOperationSnapshotSchema,
) (exists bool, returnErr error) {
	if destinationPath == "" ||
		!filepath.IsAbs(destinationPath) ||
		filepath.Clean(destinationPath) != destinationPath ||
		filepath.Base(destinationPath) != serviceOperationSnapshotBasename {
		return false, fmt.Errorf(
			"rescue snapshot destination must be a clean absolute %s path",
			serviceOperationSnapshotBasename,
		)
	}
	parentPath := filepath.Dir(destinationPath)
	if err := validateRootOwnedSnapshotDirectoryChain(parentPath); err != nil {
		return false, fmt.Errorf("validate rescue snapshot parent chain: %w", err)
	}
	parentFD, err := unix.Open(
		parentPath,
		unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		0,
	)
	if err != nil {
		return false, fmt.Errorf("open rescue snapshot parent: %w", err)
	}
	parent := os.NewFile(uintptr(parentFD), parentPath)
	if parent == nil {
		_ = unix.Close(parentFD)
		return false, fmt.Errorf("open rescue snapshot parent handle")
	}
	defer func() {
		returnErr = errors.Join(returnErr, parent.Close())
	}()
	if err := verifyPinnedRescueSnapshotParent(parentPath, parent); err != nil {
		return false, err
	}
	if err := requireRescueSnapshotSidecarsAbsent(parent, serviceOperationSnapshotBasename); err != nil {
		return false, err
	}

	var pathStat unix.Stat_t
	err = unix.Fstatat(
		int(parent.Fd()),
		serviceOperationSnapshotBasename,
		&pathStat,
		unix.AT_SYMLINK_NOFOLLOW,
	)
	if errors.Is(err, unix.ENOENT) {
		if err := verifyPinnedRescueSnapshotParent(parentPath, parent); err != nil {
			return false, err
		}
		if err := requireRescueSnapshotSidecarsAbsent(parent, serviceOperationSnapshotBasename); err != nil {
			return false, err
		}
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect rescue snapshot: %w", err)
	}
	if err := validateRootOwnedRescueSnapshotFile(pathStat); err != nil {
		return false, err
	}

	databaseFD, err := unix.Openat(
		int(parent.Fd()),
		serviceOperationSnapshotBasename,
		unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		0,
	)
	if err != nil {
		return false, fmt.Errorf("pin existing rescue snapshot: %w", err)
	}
	database := os.NewFile(uintptr(databaseFD), destinationPath)
	if database == nil {
		_ = unix.Close(databaseFD)
		return false, fmt.Errorf("open existing rescue snapshot handle")
	}
	defer func() {
		returnErr = errors.Join(returnErr, database.Close())
	}()
	var descriptorStat unix.Stat_t
	if err := unix.Fstat(databaseFD, &descriptorStat); err != nil {
		return false, fmt.Errorf("inspect pinned existing rescue snapshot: %w", err)
	}
	if !sameExactUnixFileMetadata(pathStat, descriptorStat) {
		return false, fmt.Errorf("existing rescue snapshot changed while it was pinned")
	}

	descriptorPath := fmt.Sprintf("/proc/self/fd/%d", database.Fd())
	if err := validateServiceOperationSnapshot(descriptorPath, schema); err != nil {
		return false, fmt.Errorf("validate existing rescue snapshot: %w", err)
	}
	var afterValidation unix.Stat_t
	if err := unix.Fstat(databaseFD, &afterValidation); err != nil {
		return false, fmt.Errorf("reinspect existing rescue snapshot after validation: %w", err)
	}
	var afterPathValidation unix.Stat_t
	if err := unix.Fstatat(
		int(parent.Fd()),
		serviceOperationSnapshotBasename,
		&afterPathValidation,
		unix.AT_SYMLINK_NOFOLLOW,
	); err != nil {
		return false, fmt.Errorf("reinspect rescue snapshot path after validation: %w", err)
	}
	if !sameExactUnixFileMetadata(descriptorStat, afterValidation) ||
		!sameExactUnixFileMetadata(descriptorStat, afterPathValidation) {
		return false, fmt.Errorf("existing rescue snapshot changed while it was validated")
	}
	if err := requireRescueSnapshotSidecarsAbsent(parent, serviceOperationSnapshotBasename); err != nil {
		return false, err
	}
	if err := verifyPinnedRescueSnapshotParent(parentPath, parent); err != nil {
		return false, err
	}
	return true, nil
}

func validateRootOwnedRescueSnapshotFile(stat unix.Stat_t) error {
	if stat.Mode&unix.S_IFMT != unix.S_IFREG ||
		stat.Uid != 0 ||
		stat.Gid != 0 ||
		stat.Mode&0o7777 != 0o600 ||
		stat.Nlink != 1 {
		return fmt.Errorf("existing rescue snapshot must be a root-owned single-link 0600 regular file")
	}
	return nil
}

func requireRescueSnapshotSidecarsAbsent(parent *os.File, baseName string) error {
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		var stat unix.Stat_t
		err := unix.Fstatat(
			int(parent.Fd()),
			baseName+suffix,
			&stat,
			unix.AT_SYMLINK_NOFOLLOW,
		)
		if errors.Is(err, unix.ENOENT) {
			continue
		}
		if err != nil {
			return fmt.Errorf("inspect rescue snapshot sidecar %s: %w", suffix, err)
		}
		return fmt.Errorf("rescue snapshot sidecar %s must be absent", suffix)
	}
	return nil
}

func verifyPinnedRescueSnapshotParent(parentPath string, parent *os.File) error {
	if err := validateRootOwnedSnapshotDirectoryChain(parentPath); err != nil {
		return fmt.Errorf("revalidate rescue snapshot parent chain: %w", err)
	}
	pathInfo, err := os.Lstat(parentPath)
	if err != nil {
		return fmt.Errorf("inspect rescue snapshot parent path: %w", err)
	}
	descriptorInfo, err := parent.Stat()
	if err != nil {
		return fmt.Errorf("inspect pinned rescue snapshot parent: %w", err)
	}
	if !descriptorInfo.IsDir() || !os.SameFile(pathInfo, descriptorInfo) {
		return fmt.Errorf("rescue snapshot parent changed while pinned")
	}
	return nil
}
