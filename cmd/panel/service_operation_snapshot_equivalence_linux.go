//go:build linux

package main

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

const maximumPreLedgerServiceOperationSnapshotBytes = int64(2 * 1024 * 1024 * 1024)

type pinnedPreLedgerServiceOperationSnapshot struct {
	parentPath string
	baseName   string
	parent     *os.File
	file       *os.File
	stat       unix.Stat_t
	digest     [sha256.Size]byte
}

// provePreLedgerSnapshotEquivalence is an offline, root-only, transaction-
// bound proof. It validates and pins the already published staging snapshot,
// creates only a private WAL-aware copy of the canonical database, and never
// opens or modifies the canonical SQLite source through SQLite.
func provePreLedgerSnapshotEquivalence(
	canonicalPath string,
	standaloneSnapshotPath string,
	transaction serviceOperationReleaseTransaction,
) (returnErr error) {
	if os.Geteuid() != 0 {
		return fmt.Errorf("pre-ledger snapshot equivalence proof must run as root")
	}
	if err := validateServiceOperationReleaseTransaction(transaction, "update"); err != nil {
		return err
	}
	if err := validateServiceOperationReleaseTransactionPath(
		standaloneSnapshotPath,
		transaction,
		"equivalence snapshot",
	); err != nil {
		return err
	}
	if err := verifyServiceOperationReleaseTransaction(transaction); err != nil {
		return err
	}
	if err := verifyCelikPanelServicesStopped(); err != nil {
		return err
	}

	snapshot, err := pinPreLedgerServiceOperationSnapshot(standaloneSnapshotPath)
	if err != nil {
		return err
	}
	defer func() {
		returnErr = errors.Join(returnErr, snapshot.close())
	}()
	if err := snapshot.verify(); err != nil {
		return err
	}
	if err := proveWALAwarePreLedgerSnapshotLogicalEquivalence(
		canonicalPath,
		snapshot.sqlitePath(),
	); err != nil {
		return err
	}
	if err := snapshot.verify(); err != nil {
		return err
	}
	if err := verifyCelikPanelServicesStopped(); err != nil {
		return err
	}
	return verifyServiceOperationReleaseTransaction(transaction)
}

func pinPreLedgerServiceOperationSnapshot(
	path string,
) (*pinnedPreLedgerServiceOperationSnapshot, error) {
	if path == "" ||
		!filepath.IsAbs(path) ||
		filepath.Clean(path) != path ||
		filepath.Base(path) != serviceOperationSnapshotBasename {
		return nil, fmt.Errorf(
			"pre-ledger equivalence snapshot must be a clean absolute %s path",
			serviceOperationSnapshotBasename,
		)
	}
	parentPath := filepath.Dir(path)
	if err := validateRootOwnedSnapshotDirectoryChain(parentPath); err != nil {
		return nil, fmt.Errorf("validate pre-ledger equivalence snapshot directory chain: %w", err)
	}
	parentFD, err := unix.Open(
		parentPath,
		unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		0,
	)
	if err != nil {
		return nil, fmt.Errorf("open pre-ledger equivalence snapshot parent: %w", err)
	}
	parent := os.NewFile(uintptr(parentFD), parentPath)
	if parent == nil {
		_ = unix.Close(parentFD)
		return nil, fmt.Errorf("open pre-ledger equivalence snapshot parent handle")
	}
	snapshot := &pinnedPreLedgerServiceOperationSnapshot{
		parentPath: parentPath,
		baseName:   filepath.Base(path),
		parent:     parent,
	}
	fail := func(cause error) (*pinnedPreLedgerServiceOperationSnapshot, error) {
		return nil, errors.Join(cause, snapshot.close())
	}
	if err := snapshot.verifyParent(); err != nil {
		return fail(err)
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if err := snapshot.requireAbsent(snapshot.baseName + suffix); err != nil {
			return fail(err)
		}
	}

	var pathStat unix.Stat_t
	if err := unix.Fstatat(
		int(parent.Fd()),
		snapshot.baseName,
		&pathStat,
		unix.AT_SYMLINK_NOFOLLOW,
	); err != nil {
		return fail(fmt.Errorf("inspect pre-ledger equivalence snapshot: %w", err))
	}
	if err := validatePinnedPreLedgerServiceOperationSnapshotStat(pathStat); err != nil {
		return fail(err)
	}
	fileFD, err := unix.Openat(
		int(parent.Fd()),
		snapshot.baseName,
		unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		0,
	)
	if err != nil {
		return fail(fmt.Errorf("pin pre-ledger equivalence snapshot: %w", err))
	}
	file := os.NewFile(uintptr(fileFD), path)
	if file == nil {
		_ = unix.Close(fileFD)
		return fail(fmt.Errorf("open pre-ledger equivalence snapshot handle"))
	}
	snapshot.file = file
	var descriptorStat unix.Stat_t
	if err := unix.Fstat(fileFD, &descriptorStat); err != nil {
		return fail(fmt.Errorf("inspect pinned pre-ledger equivalence snapshot: %w", err))
	}
	if !sameExactUnixFileMetadata(pathStat, descriptorStat) {
		return fail(fmt.Errorf("pre-ledger equivalence snapshot changed while it was pinned"))
	}
	digest, err := digestPinnedServiceOperationFile(file, descriptorStat.Size)
	if err != nil {
		return fail(fmt.Errorf("bind pinned pre-ledger equivalence snapshot content: %w", err))
	}
	var afterDigest unix.Stat_t
	if err := unix.Fstat(fileFD, &afterDigest); err != nil {
		return fail(fmt.Errorf("reinspect pinned pre-ledger equivalence snapshot: %w", err))
	}
	if !sameExactUnixFileMetadata(descriptorStat, afterDigest) {
		return fail(fmt.Errorf("pre-ledger equivalence snapshot changed while its content was bound"))
	}
	snapshot.stat = descriptorStat
	snapshot.digest = digest
	return snapshot, nil
}

func validatePinnedPreLedgerServiceOperationSnapshotStat(stat unix.Stat_t) error {
	if stat.Mode&unix.S_IFMT != unix.S_IFREG ||
		stat.Uid != 0 ||
		stat.Gid != 0 ||
		stat.Mode&0o777 != 0o600 ||
		stat.Nlink != 1 ||
		stat.Size <= 0 ||
		stat.Size > maximumPreLedgerServiceOperationSnapshotBytes {
		return fmt.Errorf(
			"pre-ledger equivalence snapshot must be a root-owned single-link 0600 regular file between 1 byte and %d bytes",
			maximumPreLedgerServiceOperationSnapshotBytes,
		)
	}
	return nil
}

func (s *pinnedPreLedgerServiceOperationSnapshot) sqlitePath() string {
	return fmt.Sprintf("/proc/self/fd/%d", s.file.Fd())
}

func (s *pinnedPreLedgerServiceOperationSnapshot) verifyParent() error {
	if s == nil || s.parent == nil {
		return fmt.Errorf("pre-ledger equivalence snapshot parent is not pinned")
	}
	if err := validateRootOwnedSnapshotDirectoryChain(s.parentPath); err != nil {
		return fmt.Errorf("revalidate pre-ledger equivalence snapshot directory chain: %w", err)
	}
	pathInfo, err := os.Lstat(s.parentPath)
	if err != nil {
		return fmt.Errorf("inspect pre-ledger equivalence snapshot parent: %w", err)
	}
	descriptorInfo, err := s.parent.Stat()
	if err != nil {
		return fmt.Errorf("inspect pinned pre-ledger equivalence snapshot parent: %w", err)
	}
	if !os.SameFile(pathInfo, descriptorInfo) {
		return fmt.Errorf("pre-ledger equivalence snapshot parent changed while pinned")
	}
	return nil
}

func (s *pinnedPreLedgerServiceOperationSnapshot) requireAbsent(baseName string) error {
	var stat unix.Stat_t
	err := unix.Fstatat(int(s.parent.Fd()), baseName, &stat, unix.AT_SYMLINK_NOFOLLOW)
	switch {
	case errors.Is(err, unix.ENOENT):
		return nil
	case err != nil:
		return fmt.Errorf("inspect pre-ledger equivalence snapshot entry %s: %w", baseName, err)
	default:
		return fmt.Errorf("pre-ledger equivalence snapshot sidecar %s must be absent", baseName)
	}
}

func (s *pinnedPreLedgerServiceOperationSnapshot) verify() error {
	if s == nil || s.parent == nil || s.file == nil {
		return fmt.Errorf("pre-ledger equivalence snapshot is not pinned")
	}
	if err := s.verifyParent(); err != nil {
		return err
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if err := s.requireAbsent(s.baseName + suffix); err != nil {
			return err
		}
	}
	var pathStat unix.Stat_t
	if err := unix.Fstatat(
		int(s.parent.Fd()),
		s.baseName,
		&pathStat,
		unix.AT_SYMLINK_NOFOLLOW,
	); err != nil {
		return fmt.Errorf("reinspect pre-ledger equivalence snapshot: %w", err)
	}
	var descriptorStat unix.Stat_t
	if err := unix.Fstat(int(s.file.Fd()), &descriptorStat); err != nil {
		return fmt.Errorf("reinspect pinned pre-ledger equivalence snapshot: %w", err)
	}
	if err := validatePinnedPreLedgerServiceOperationSnapshotStat(pathStat); err != nil {
		return err
	}
	if !sameExactUnixFileMetadata(s.stat, pathStat) ||
		!sameExactUnixFileMetadata(s.stat, descriptorStat) {
		return fmt.Errorf("pre-ledger equivalence snapshot changed while pinned")
	}
	digest, err := digestPinnedServiceOperationFile(s.file, descriptorStat.Size)
	if err != nil {
		return fmt.Errorf("rebind pinned pre-ledger equivalence snapshot content: %w", err)
	}
	var afterDigest unix.Stat_t
	if err := unix.Fstat(int(s.file.Fd()), &afterDigest); err != nil {
		return fmt.Errorf("reinspect pinned pre-ledger equivalence snapshot after hashing: %w", err)
	}
	if !sameExactUnixFileMetadata(descriptorStat, afterDigest) || digest != s.digest {
		return fmt.Errorf("pre-ledger equivalence snapshot content changed while pinned")
	}
	return nil
}

func (s *pinnedPreLedgerServiceOperationSnapshot) close() error {
	if s == nil {
		return nil
	}
	var closeErr error
	if s.file != nil {
		closeErr = errors.Join(closeErr, s.file.Close())
		s.file = nil
	}
	if s.parent != nil {
		closeErr = errors.Join(closeErr, s.parent.Close())
		s.parent = nil
	}
	return closeErr
}
