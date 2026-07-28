//go:build linux

package main

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

const serviceOperationRestoreStageBasename = ".celikpanel.db.restore-stage"

type trustedServiceOperationRestoreSource struct {
	path       string
	parentPath string
	baseName   string
	parent     *os.File
	file       *os.File
	stat       unix.Stat_t
}

type serviceOperationRestoreTarget struct {
	path                 string
	parentPath           string
	finalBase            string
	stageBase            string
	parent               *os.File
	stage                *os.File
	owner                serviceOperationRestoreOwner
	existingDatabaseStat *unix.Stat_t
	validatedStageStat   *unix.Stat_t
	validatedStageHash   [sha256.Size]byte
	locked               bool
	published            bool
	hooks                serviceOperationRestoreHooks
}

func restoreServiceOperationSnapshotWithOwner(
	sourcePath string,
	schema serviceOperationSnapshotSchema,
	owner serviceOperationRestoreOwner,
	hooks serviceOperationRestoreHooks,
) (returnErr error) {
	if os.Geteuid() != 0 {
		return fmt.Errorf("service operation restore must run as root")
	}
	if owner.uid == 0 || owner.gid == 0 {
		return fmt.Errorf("service operation restore target owner must be non-root")
	}
	if schema != serviceOperationSnapshotSchemaNormal &&
		schema != serviceOperationSnapshotSchemaPreLedger {
		return fmt.Errorf("unsupported snapshot schema %q", schema)
	}
	targetPath := databaseFile()
	if targetPath == "" ||
		!filepath.IsAbs(targetPath) ||
		filepath.Clean(targetPath) != targetPath ||
		filepath.Base(targetPath) != serviceOperationSnapshotBasename {
		return fmt.Errorf("canonical panel database path must be a clean absolute %s path", serviceOperationSnapshotBasename)
	}
	if sourcePath == "" ||
		!filepath.IsAbs(sourcePath) ||
		filepath.Clean(sourcePath) != sourcePath ||
		filepath.Base(sourcePath) != serviceOperationSnapshotBasename {
		return fmt.Errorf("restore source must be a clean absolute %s path", serviceOperationSnapshotBasename)
	}
	if sourcePath == targetPath {
		return fmt.Errorf("restore source must not be the canonical panel database")
	}

	source, err := openTrustedServiceOperationRestoreSource(sourcePath)
	if err != nil {
		return err
	}
	defer func() {
		returnErr = errors.Join(returnErr, source.close())
	}()
	if err := source.verify(); err != nil {
		return err
	}
	if err := source.requireNoSidecars(); err != nil {
		return err
	}
	if err := source.validate(schema); err != nil {
		return err
	}
	if err := source.verify(); err != nil {
		return err
	}
	if err := source.requireNoSidecars(); err != nil {
		return err
	}

	target, err := prepareServiceOperationRestoreTarget(targetPath, owner, hooks)
	if err != nil {
		return err
	}
	defer func() {
		returnErr = errors.Join(returnErr, target.close())
	}()
	if err := target.copyAndPrepareStage(source); err != nil {
		return err
	}
	if err := source.verify(); err != nil {
		return err
	}
	if err := source.requireNoSidecars(); err != nil {
		return err
	}
	if err := target.validateStage(schema); err != nil {
		return err
	}
	if err := target.publish(); err != nil {
		return err
	}
	return nil
}

func openTrustedServiceOperationRestoreSource(
	sourcePath string,
) (*trustedServiceOperationRestoreSource, error) {
	parentPath := filepath.Dir(sourcePath)
	if err := validateRootOwnedSnapshotDirectoryChain(parentPath); err != nil {
		return nil, fmt.Errorf("validate restore source directory chain: %w", err)
	}
	parentFD, err := unix.Open(
		parentPath,
		unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		0,
	)
	if err != nil {
		return nil, fmt.Errorf("open restore source parent: %w", err)
	}
	parent := os.NewFile(uintptr(parentFD), parentPath)
	if parent == nil {
		_ = unix.Close(parentFD)
		return nil, fmt.Errorf("open restore source parent handle")
	}
	baseName := filepath.Base(sourcePath)
	sourceFD, err := unix.Openat(
		parentFD,
		baseName,
		unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		0,
	)
	if err != nil {
		parent.Close()
		return nil, fmt.Errorf("open trusted restore source: %w", err)
	}
	file := os.NewFile(uintptr(sourceFD), sourcePath)
	if file == nil {
		_ = unix.Close(sourceFD)
		parent.Close()
		return nil, fmt.Errorf("open trusted restore source handle")
	}
	source := &trustedServiceOperationRestoreSource{
		path:       sourcePath,
		parentPath: parentPath,
		baseName:   baseName,
		parent:     parent,
		file:       file,
	}
	if err := unix.Fstat(sourceFD, &source.stat); err != nil {
		source.close()
		return nil, fmt.Errorf("inspect trusted restore source: %w", err)
	}
	if err := validateTrustedServiceOperationRestoreSourceStat(source.stat); err != nil {
		source.close()
		return nil, err
	}
	if err := source.verify(); err != nil {
		source.close()
		return nil, err
	}
	return source, nil
}

func validateTrustedServiceOperationRestoreSourceStat(stat unix.Stat_t) error {
	if stat.Mode&unix.S_IFMT != unix.S_IFREG ||
		stat.Uid != 0 ||
		stat.Gid != 0 ||
		stat.Mode&0o777 != 0o600 ||
		stat.Nlink != 1 {
		return fmt.Errorf("restore source must be a root:root single-link 0600 regular file")
	}
	return nil
}

func (s *trustedServiceOperationRestoreSource) verify() error {
	if s == nil || s.parent == nil || s.file == nil {
		return fmt.Errorf("restore source is not open")
	}
	if err := validateRootOwnedSnapshotDirectoryChain(s.parentPath); err != nil {
		return fmt.Errorf("revalidate restore source directory chain: %w", err)
	}
	pathParentInfo, err := os.Lstat(s.parentPath)
	if err != nil {
		return fmt.Errorf("inspect restore source parent path: %w", err)
	}
	pinnedParentInfo, err := s.parent.Stat()
	if err != nil {
		return fmt.Errorf("inspect pinned restore source parent: %w", err)
	}
	if !os.SameFile(pathParentInfo, pinnedParentInfo) {
		return fmt.Errorf("restore source parent changed while pinned")
	}
	var pathStat unix.Stat_t
	if err := unix.Fstatat(
		int(s.parent.Fd()),
		s.baseName,
		&pathStat,
		unix.AT_SYMLINK_NOFOLLOW,
	); err != nil {
		return fmt.Errorf("reinspect restore source path: %w", err)
	}
	var descriptorStat unix.Stat_t
	if err := unix.Fstat(int(s.file.Fd()), &descriptorStat); err != nil {
		return fmt.Errorf("reinspect restore source descriptor: %w", err)
	}
	if err := validateTrustedServiceOperationRestoreSourceStat(pathStat); err != nil {
		return err
	}
	if err := validateTrustedServiceOperationRestoreSourceStat(descriptorStat); err != nil {
		return err
	}
	if !sameUnixFileIdentity(pathStat, descriptorStat) ||
		!sameUnixFileIdentity(s.stat, descriptorStat) ||
		s.stat.Size != descriptorStat.Size ||
		s.stat.Mtim != descriptorStat.Mtim ||
		s.stat.Ctim != descriptorStat.Ctim {
		return fmt.Errorf("restore source changed while pinned")
	}
	return nil
}

// validate opens SQLite through the pinned source descriptor. Replacing the
// source pathname cannot make validation inspect a different database than the
// descriptor later copied into the restore stage.
// validate, SQLite'ı sabitlenmiş kaynak descriptor'ı üzerinden açar. Kaynak
// yolunun değiştirilmesi, doğrulamanın restore stage'e kopyalanacak descriptor'dan
// farklı bir veritabanını incelemesine neden olamaz.
func (s *trustedServiceOperationRestoreSource) validate(
	schema serviceOperationSnapshotSchema,
) error {
	if s == nil || s.file == nil {
		return fmt.Errorf("restore source is not open")
	}
	var beforeValidation unix.Stat_t
	if err := unix.Fstat(int(s.file.Fd()), &beforeValidation); err != nil {
		return fmt.Errorf("capture trusted restore source before validation: %w", err)
	}
	descriptorPath := fmt.Sprintf("/proc/self/fd/%d", s.file.Fd())
	if err := validateServiceOperationSnapshot(descriptorPath, schema); err != nil {
		return fmt.Errorf("validate trusted %s restore source: %w", schema, err)
	}
	var afterValidation unix.Stat_t
	if err := unix.Fstat(int(s.file.Fd()), &afterValidation); err != nil {
		return fmt.Errorf("reinspect trusted restore source after validation: %w", err)
	}
	if !sameExactUnixFileMetadata(s.stat, beforeValidation) ||
		!sameExactUnixFileMetadata(beforeValidation, afterValidation) {
		return fmt.Errorf("trusted restore source changed while it was validated")
	}
	return nil
}

func (s *trustedServiceOperationRestoreSource) requireNoSidecars() error {
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if err := requireDirfdEntryAbsent(
			int(s.parent.Fd()),
			s.baseName+suffix,
			"restore source sidecar",
		); err != nil {
			return err
		}
	}
	return nil
}

func (s *trustedServiceOperationRestoreSource) close() error {
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

func prepareServiceOperationRestoreTarget(
	targetPath string,
	owner serviceOperationRestoreOwner,
	hooks serviceOperationRestoreHooks,
) (*serviceOperationRestoreTarget, error) {
	parentPath := filepath.Dir(targetPath)
	if err := validateRootOwnedSnapshotDirectoryChain(filepath.Dir(parentPath)); err != nil {
		return nil, fmt.Errorf("validate canonical database ancestor chain: %w", err)
	}
	parentFD, err := unix.Open(
		parentPath,
		unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		0,
	)
	if err != nil {
		return nil, fmt.Errorf("open canonical database parent: %w", err)
	}
	parent := os.NewFile(uintptr(parentFD), parentPath)
	if parent == nil {
		_ = unix.Close(parentFD)
		return nil, fmt.Errorf("open canonical database parent handle")
	}
	target := &serviceOperationRestoreTarget{
		path:       targetPath,
		parentPath: parentPath,
		finalBase:  serviceOperationSnapshotBasename,
		stageBase:  serviceOperationRestoreStageBasename,
		parent:     parent,
		owner:      owner,
		hooks:      hooks,
	}
	fail := func(cause error) (*serviceOperationRestoreTarget, error) {
		return nil, errors.Join(cause, target.close())
	}
	if err := target.verifyParent(false); err != nil {
		return fail(err)
	}
	if err := target.captureExistingFinal(); err != nil {
		return fail(err)
	}
	if err := target.rejectUsersAndHandles(); err != nil {
		return fail(err)
	}
	if err := target.lockParent(); err != nil {
		return fail(err)
	}
	if err := target.verifyParent(true); err != nil {
		return fail(err)
	}
	if err := target.rejectUsersAndHandles(); err != nil {
		return fail(err)
	}
	if err := target.removeStaleStageEntries(); err != nil {
		return fail(err)
	}
	if err := target.verifyExistingFinal(); err != nil {
		return fail(err)
	}
	if err := target.requireFinalSidecarsAbsent(); err != nil {
		return fail(err)
	}
	if err := target.parent.Sync(); err != nil {
		return fail(fmt.Errorf("sync canonical database parent after sidecar cleanup: %w", err))
	}
	return target, nil
}

func (t *serviceOperationRestoreTarget) verifyParent(locked bool) error {
	if t == nil || t.parent == nil {
		return fmt.Errorf("canonical database parent is not open")
	}
	if err := validateRootOwnedSnapshotDirectoryChain(filepath.Dir(t.parentPath)); err != nil {
		return fmt.Errorf("revalidate canonical database ancestor chain: %w", err)
	}
	pathInfo, err := os.Lstat(t.parentPath)
	if err != nil {
		return fmt.Errorf("inspect canonical database parent path: %w", err)
	}
	pinnedInfo, err := t.parent.Stat()
	if err != nil {
		return fmt.Errorf("inspect pinned canonical database parent: %w", err)
	}
	if !os.SameFile(pathInfo, pinnedInfo) {
		return fmt.Errorf("canonical database parent changed while pinned")
	}
	stat, ok := pinnedInfo.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("inspect canonical database parent ownership")
	}
	if locked {
		if !pinnedInfo.IsDir() ||
			pinnedInfo.Mode()&os.ModeSymlink != 0 ||
			pinnedInfo.Mode().Perm() != 0o700 ||
			stat.Uid != 0 ||
			stat.Gid != 0 {
			return fmt.Errorf("canonical database parent must be root:root mode 0700 during restore")
		}
		return nil
	}
	if !pinnedInfo.IsDir() || pinnedInfo.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("canonical database parent must be a real directory")
	}
	rootOwned := stat.Uid == 0 && stat.Gid == 0
	panelOwned := stat.Uid == t.owner.uid && stat.Gid == t.owner.gid
	recoverableMode := pinnedInfo.Mode().Perm() == 0o700 || pinnedInfo.Mode().Perm() == 0o750
	if (rootOwned || panelOwned) && recoverableMode {
		return nil
	}
	return fmt.Errorf("canonical database parent metadata is not a secure normal or recoverable quarantine state")
}

func (t *serviceOperationRestoreTarget) lockParent() error {
	if err := unix.Fchown(int(t.parent.Fd()), 0, 0); err != nil {
		return fmt.Errorf("lock canonical database parent ownership: %w", err)
	}
	if err := unix.Fchmod(int(t.parent.Fd()), 0o700); err != nil {
		return fmt.Errorf("lock canonical database parent mode: %w", err)
	}
	if err := t.parent.Sync(); err != nil {
		return fmt.Errorf("sync locked canonical database parent: %w", err)
	}
	t.locked = true
	return nil
}

func (t *serviceOperationRestoreTarget) captureExistingFinal() error {
	var stat unix.Stat_t
	err := unix.Fstatat(
		int(t.parent.Fd()),
		t.finalBase,
		&stat,
		unix.AT_SYMLINK_NOFOLLOW,
	)
	if errors.Is(err, unix.ENOENT) {
		t.existingDatabaseStat = nil
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect existing canonical panel database: %w", err)
	}
	if err := validatePanelOwnedDatabaseFile(stat, t.owner, "existing canonical panel database"); err != nil {
		return err
	}
	t.existingDatabaseStat = &stat
	return nil
}

func (t *serviceOperationRestoreTarget) rejectUsersAndHandles() error {
	databaseStat := unix.Stat_t{}
	if t.existingDatabaseStat != nil {
		databaseStat = *t.existingDatabaseStat
	}
	return rejectDatabaseQuarantineUsersAndHandles("/proc", t.owner.uid, databaseStat)
}

func (t *serviceOperationRestoreTarget) unlockParent() error {
	if t == nil || t.parent == nil || !t.locked {
		return nil
	}
	if err := t.verifyParent(true); err != nil {
		return err
	}
	if t.published {
		if err := t.verifyPublishedFinal(); err != nil {
			return err
		}
		if err := t.requireFinalSidecarsAbsent(); err != nil {
			return err
		}
	} else if err := t.verifyExistingFinal(); err != nil {
		return err
	}
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		if err := requireDirfdEntryAbsent(
			int(t.parent.Fd()),
			t.stageBase+suffix,
			"database restore stage before quarantine release",
		); err != nil {
			return err
		}
	}
	var currentDatabaseStat unix.Stat_t
	err := unix.Fstatat(
		int(t.parent.Fd()),
		t.finalBase,
		&currentDatabaseStat,
		unix.AT_SYMLINK_NOFOLLOW,
	)
	if err == nil {
		if err := validatePanelOwnedDatabaseFile(currentDatabaseStat, t.owner, "canonical panel database before quarantine release"); err != nil {
			return err
		}
	} else if !errors.Is(err, unix.ENOENT) {
		return fmt.Errorf("inspect canonical panel database before quarantine release: %w", err)
	}
	if err := rejectDatabaseQuarantineUsersAndHandles("/proc", t.owner.uid, currentDatabaseStat); err != nil {
		return err
	}
	var unlockErr error
	if err := unix.Fchown(
		int(t.parent.Fd()),
		int(t.owner.uid),
		int(t.owner.gid),
	); err != nil {
		unlockErr = errors.Join(unlockErr, fmt.Errorf("restore canonical database parent ownership: %w", err))
	}
	if err := unix.Fchmod(int(t.parent.Fd()), 0o750); err != nil {
		unlockErr = errors.Join(unlockErr, fmt.Errorf("restore canonical database parent mode: %w", err))
	}
	if err := t.parent.Sync(); err != nil {
		unlockErr = errors.Join(unlockErr, fmt.Errorf("sync restored canonical database parent ownership: %w", err))
	}
	if unlockErr == nil {
		t.locked = false
		if err := t.verifyParent(false); err != nil {
			return err
		}
	}
	return unlockErr
}

func (t *serviceOperationRestoreTarget) verifyExistingFinal() error {
	var stat unix.Stat_t
	err := unix.Fstatat(
		int(t.parent.Fd()),
		t.finalBase,
		&stat,
		unix.AT_SYMLINK_NOFOLLOW,
	)
	if errors.Is(err, unix.ENOENT) {
		if t.existingDatabaseStat != nil {
			return fmt.Errorf("existing canonical panel database disappeared during quarantine")
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect existing canonical panel database: %w", err)
	}
	if t.existingDatabaseStat == nil {
		return fmt.Errorf("canonical panel database appeared during quarantine")
	}
	if err := validatePanelOwnedDatabaseFile(stat, t.owner, "existing canonical panel database"); err != nil {
		return err
	}
	if !sameExactUnixFileMetadata(*t.existingDatabaseStat, stat) {
		return fmt.Errorf("existing canonical panel database changed during quarantine")
	}
	return nil
}

func (t *serviceOperationRestoreTarget) removeStaleStageEntries() error {
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		if err := unlinkDirfdEntryIfExpected(
			int(t.parent.Fd()),
			t.stageBase+suffix,
			"stale database restore stage",
			t.owner,
			true,
		); err != nil {
			return err
		}
	}
	if err := t.parent.Sync(); err != nil {
		return fmt.Errorf("sync canonical database parent after stale stage cleanup: %w", err)
	}
	return nil
}

func (t *serviceOperationRestoreTarget) requireFinalSidecarsAbsent() error {
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if err := requireDirfdEntryAbsent(
			int(t.parent.Fd()),
			t.finalBase+suffix,
			"canonical database sidecar",
		); err != nil {
			return err
		}
	}
	return nil
}

func (t *serviceOperationRestoreTarget) copyAndPrepareStage(
	source *trustedServiceOperationRestoreSource,
) error {
	if err := t.verifyParent(true); err != nil {
		return err
	}
	stageFD, anonymous, err := t.createStageFile()
	if err != nil {
		return err
	}
	t.stage = os.NewFile(uintptr(stageFD), filepath.Join(t.parentPath, t.stageBase))
	if t.stage == nil {
		_ = unix.Close(stageFD)
		return fmt.Errorf("open database restore stage handle")
	}
	if _, err := source.file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewind trusted restore source: %w", err)
	}
	if _, err := t.stage.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewind database restore stage: %w", err)
	}
	if _, err := io.Copy(t.stage, source.file); err != nil {
		return fmt.Errorf("copy trusted snapshot into database restore stage: %w", err)
	}
	if err := t.stage.Sync(); err != nil {
		return fmt.Errorf("sync copied database restore stage: %w", err)
	}
	if err := unix.Fchown(stageFD, int(t.owner.uid), int(t.owner.gid)); err != nil {
		return fmt.Errorf("set database restore stage ownership: %w", err)
	}
	if err := unix.Fchmod(stageFD, 0o600); err != nil {
		return fmt.Errorf("set database restore stage mode: %w", err)
	}
	if err := t.stage.Sync(); err != nil {
		return fmt.Errorf("sync prepared database restore stage: %w", err)
	}
	if anonymous {
		if err := unix.Linkat(
			stageFD,
			"",
			int(t.parent.Fd()),
			t.stageBase,
			unix.AT_EMPTY_PATH,
		); err != nil {
			return fmt.Errorf("link anonymous database restore stage: %w", err)
		}
		if err := t.repinLinkedStage(); err != nil {
			return err
		}
	}
	if err := t.verifyStage(); err != nil {
		return err
	}
	if err := t.parent.Sync(); err != nil {
		return fmt.Errorf("sync canonical database parent after stage creation: %w", err)
	}
	return nil
}

// repinLinkedStage replaces the O_TMPFILE descriptor with a descriptor opened
// from its newly linked stage name, but only after exact inode and metadata
// equality is proven. SQLite can then reopen /proc/self/fd/<n> reliably while
// the verified descriptor remains pinned through atomic publication.
// repinLinkedStage, O_TMPFILE descriptor'ını yeni bağlanan stage adından açılan
// descriptor ile ancak inode ve metadata tam eşitliği kanıtlandıktan sonra değiştirir.
// Böylece SQLite /proc/self/fd/<n> yolunu güvenilir biçimde yeniden açabilir ve
// doğrulanmış descriptor atomik yayına kadar sabit kalır.
func (t *serviceOperationRestoreTarget) repinLinkedStage() error {
	if t.stage == nil {
		return fmt.Errorf("database restore stage is not open")
	}
	fd, err := unix.Openat(
		int(t.parent.Fd()),
		t.stageBase,
		unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		0,
	)
	if err != nil {
		return fmt.Errorf("repin linked database restore stage: %w", err)
	}
	repinned := os.NewFile(uintptr(fd), filepath.Join(t.parentPath, t.stageBase))
	if repinned == nil {
		_ = unix.Close(fd)
		return fmt.Errorf("open repinned database restore stage handle")
	}

	var originalStat, repinnedStat, pathStat unix.Stat_t
	if err := unix.Fstat(int(t.stage.Fd()), &originalStat); err != nil {
		repinned.Close()
		return fmt.Errorf("inspect anonymous database restore stage: %w", err)
	}
	if err := unix.Fstat(fd, &repinnedStat); err != nil {
		repinned.Close()
		return fmt.Errorf("inspect repinned database restore stage: %w", err)
	}
	if err := unix.Fstatat(
		int(t.parent.Fd()),
		t.stageBase,
		&pathStat,
		unix.AT_SYMLINK_NOFOLLOW,
	); err != nil {
		repinned.Close()
		return fmt.Errorf("inspect linked database restore stage: %w", err)
	}
	if !sameExactUnixFileMetadata(originalStat, repinnedStat) ||
		!sameExactUnixFileMetadata(originalStat, pathStat) {
		repinned.Close()
		return fmt.Errorf("linked database restore stage does not match its anonymous descriptor")
	}

	original := t.stage
	t.stage = repinned
	if err := original.Close(); err != nil {
		return fmt.Errorf("close anonymous database restore stage descriptor: %w", err)
	}
	return nil
}

func (t *serviceOperationRestoreTarget) createStageFile() (int, bool, error) {
	fd, err := unix.Openat(
		int(t.parent.Fd()),
		".",
		unix.O_RDWR|unix.O_TMPFILE|unix.O_CLOEXEC,
		0o600,
	)
	if err == nil {
		return fd, true, nil
	}
	if !errors.Is(err, unix.EOPNOTSUPP) &&
		!errors.Is(err, unix.EINVAL) &&
		!errors.Is(err, unix.EISDIR) &&
		!errors.Is(err, unix.ENOSYS) {
		return -1, false, fmt.Errorf("create anonymous database restore stage: %w", err)
	}
	fd, err = unix.Openat(
		int(t.parent.Fd()),
		t.stageBase,
		unix.O_RDWR|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		0o600,
	)
	if err != nil {
		return -1, false, fmt.Errorf("create database restore stage: %w", err)
	}
	return fd, false, nil
}

func (t *serviceOperationRestoreTarget) verifyStage() error {
	if t.stage == nil {
		return fmt.Errorf("database restore stage is not open")
	}
	var descriptorStat unix.Stat_t
	if err := unix.Fstat(int(t.stage.Fd()), &descriptorStat); err != nil {
		return fmt.Errorf("inspect database restore stage descriptor: %w", err)
	}
	var pathStat unix.Stat_t
	if err := unix.Fstatat(
		int(t.parent.Fd()),
		t.stageBase,
		&pathStat,
		unix.AT_SYMLINK_NOFOLLOW,
	); err != nil {
		return fmt.Errorf("inspect database restore stage path: %w", err)
	}
	if !sameUnixFileIdentity(pathStat, descriptorStat) {
		return fmt.Errorf("database restore stage path does not match its pinned descriptor")
	}
	if pathStat.Mode&unix.S_IFMT != unix.S_IFREG ||
		pathStat.Uid != t.owner.uid ||
		pathStat.Gid != t.owner.gid ||
		pathStat.Mode&0o777 != 0o600 ||
		pathStat.Nlink != 1 {
		return fmt.Errorf("database restore stage must be a celikpanel-owned single-link 0600 regular file")
	}
	return nil
}

func (t *serviceOperationRestoreTarget) validateStage(
	schema serviceOperationSnapshotSchema,
) error {
	if err := t.verifyParent(true); err != nil {
		return err
	}
	if err := t.verifyStage(); err != nil {
		return err
	}
	var beforeValidation unix.Stat_t
	if err := unix.Fstat(int(t.stage.Fd()), &beforeValidation); err != nil {
		return fmt.Errorf("capture database restore stage before validation: %w", err)
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if err := requireDirfdEntryAbsent(
			int(t.parent.Fd()),
			t.stageBase+suffix,
			"database restore stage sidecar",
		); err != nil {
			return err
		}
	}
	stagePath := fmt.Sprintf("/proc/self/fd/%d", t.stage.Fd())
	if err := validateServiceOperationSnapshot(stagePath, schema); err != nil {
		return fmt.Errorf("validate copied %s database restore stage: %w", schema, err)
	}
	if err := t.verifyStage(); err != nil {
		return err
	}
	var afterValidation unix.Stat_t
	if err := unix.Fstat(int(t.stage.Fd()), &afterValidation); err != nil {
		return fmt.Errorf("reinspect database restore stage after validation: %w", err)
	}
	if !sameExactUnixFileMetadata(beforeValidation, afterValidation) {
		return fmt.Errorf("database restore stage changed while it was validated")
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if err := requireDirfdEntryAbsent(
			int(t.parent.Fd()),
			t.stageBase+suffix,
			"database restore stage sidecar",
		); err != nil {
			return err
		}
	}
	if err := t.stage.Sync(); err != nil {
		return fmt.Errorf("sync validated database restore stage: %w", err)
	}
	var validated unix.Stat_t
	if err := unix.Fstat(int(t.stage.Fd()), &validated); err != nil {
		return fmt.Errorf("capture validated database restore stage: %w", err)
	}
	digest, err := digestPinnedServiceOperationFile(t.stage, validated.Size)
	if err != nil {
		return fmt.Errorf("capture validated database restore content: %w", err)
	}
	var afterDigest unix.Stat_t
	if err := unix.Fstat(int(t.stage.Fd()), &afterDigest); err != nil {
		return fmt.Errorf("reinspect database restore stage after hashing: %w", err)
	}
	if !sameExactUnixFileMetadata(validated, afterDigest) {
		return fmt.Errorf("database restore stage changed while its content was hashed")
	}
	t.validatedStageStat = &validated
	t.validatedStageHash = digest
	return nil
}

func (t *serviceOperationRestoreTarget) verifyValidatedStage() error {
	if t.validatedStageStat == nil {
		return fmt.Errorf("database restore stage has not been validated")
	}
	if err := t.verifyStage(); err != nil {
		return err
	}
	var descriptorStat unix.Stat_t
	if err := unix.Fstat(int(t.stage.Fd()), &descriptorStat); err != nil {
		return fmt.Errorf("reinspect validated database restore stage descriptor: %w", err)
	}
	var pathStat unix.Stat_t
	if err := unix.Fstatat(
		int(t.parent.Fd()),
		t.stageBase,
		&pathStat,
		unix.AT_SYMLINK_NOFOLLOW,
	); err != nil {
		return fmt.Errorf("reinspect validated database restore stage path: %w", err)
	}
	if !sameExactUnixFileMetadata(*t.validatedStageStat, descriptorStat) ||
		!sameExactUnixFileMetadata(*t.validatedStageStat, pathStat) {
		return fmt.Errorf("database restore stage changed after validation")
	}
	digest, err := digestPinnedServiceOperationFile(t.stage, descriptorStat.Size)
	if err != nil {
		return fmt.Errorf("recheck validated database restore content: %w", err)
	}
	var afterDigest unix.Stat_t
	if err := unix.Fstat(int(t.stage.Fd()), &afterDigest); err != nil {
		return fmt.Errorf("reinspect validated database restore stage after hashing: %w", err)
	}
	if !sameExactUnixFileMetadata(descriptorStat, afterDigest) ||
		digest != t.validatedStageHash {
		return fmt.Errorf("database restore stage content changed after validation")
	}
	return nil
}

func (t *serviceOperationRestoreTarget) verifyPublishPreconditions() error {
	if err := t.verifyParent(true); err != nil {
		return err
	}
	if err := t.verifyValidatedStage(); err != nil {
		return err
	}
	if err := t.verifyExistingFinal(); err != nil {
		return err
	}
	if err := t.requireFinalSidecarsAbsent(); err != nil {
		return err
	}
	return nil
}

func (t *serviceOperationRestoreTarget) publish() error {
	if err := t.verifyPublishPreconditions(); err != nil {
		return err
	}
	if err := t.parent.Sync(); err != nil {
		return fmt.Errorf("sync canonical database parent before atomic restore: %w", err)
	}
	if t.hooks.beforeRename != nil {
		if err := t.hooks.beforeRename(); err != nil {
			return fmt.Errorf("prepare atomic database restore: %w", err)
		}
	}
	// Repeat every exact proof after the final injected fault boundary. The
	// validated descriptor, its pathname, the old final, and all sidecars must
	// still be the same objects immediately before rename.
	// Son enjekte hata sınırından sonra tüm tam kanıtları yinele. Doğrulanmış
	// descriptor, yolu, eski final ve tüm sidecar'lar rename'den hemen önce hâlâ
	// aynı nesneler olmalıdır.
	if err := t.verifyPublishPreconditions(); err != nil {
		return err
	}
	if err := t.parent.Sync(); err != nil {
		return fmt.Errorf("sync canonical database parent after final restore proof: %w", err)
	}
	if err := unix.Renameat2(
		int(t.parent.Fd()),
		t.stageBase,
		int(t.parent.Fd()),
		t.finalBase,
		0,
	); err != nil {
		return fmt.Errorf("atomically replace canonical panel database: %w", err)
	}
	t.published = true
	if t.hooks.afterRename != nil {
		if err := t.hooks.afterRename(); err != nil {
			return fmt.Errorf("database restore committed but durability is unconfirmed: %w", err)
		}
	}
	if err := t.verifyPublishedFinal(); err != nil {
		return fmt.Errorf("database restore committed but final verification failed: %w", err)
	}
	if err := t.requireFinalSidecarsAbsent(); err != nil {
		return fmt.Errorf("database restore committed but sidecar absence is unconfirmed: %w", err)
	}
	if err := t.parent.Sync(); err != nil {
		return fmt.Errorf("database restore committed but parent durability is unconfirmed: %w", err)
	}
	return nil
}

func (t *serviceOperationRestoreTarget) verifyPublishedFinal() error {
	finalFD, err := unix.Openat(
		int(t.parent.Fd()),
		t.finalBase,
		unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		0,
	)
	if err != nil {
		return fmt.Errorf("open restored canonical panel database: %w", err)
	}
	defer unix.Close(finalFD)
	var finalStat unix.Stat_t
	if err := unix.Fstat(finalFD, &finalStat); err != nil {
		return fmt.Errorf("inspect restored canonical panel database: %w", err)
	}
	var stageStat unix.Stat_t
	if err := unix.Fstat(int(t.stage.Fd()), &stageStat); err != nil {
		return fmt.Errorf("inspect published database restore descriptor: %w", err)
	}
	if t.validatedStageStat == nil ||
		!sameExactUnixFileMetadata(finalStat, stageStat) ||
		!samePublishedUnixFileMetadata(*t.validatedStageStat, finalStat) {
		return fmt.Errorf("restored canonical panel database does not match the validated stage")
	}
	if finalStat.Mode&unix.S_IFMT != unix.S_IFREG ||
		finalStat.Uid != t.owner.uid ||
		finalStat.Gid != t.owner.gid ||
		finalStat.Mode&0o777 != 0o600 ||
		finalStat.Nlink != 1 {
		return fmt.Errorf("restored canonical panel database must be a celikpanel-owned single-link 0600 regular file")
	}
	digest, err := digestPinnedServiceOperationFile(t.stage, stageStat.Size)
	if err != nil {
		return fmt.Errorf("verify restored canonical panel database content: %w", err)
	}
	var afterDigest unix.Stat_t
	if err := unix.Fstat(int(t.stage.Fd()), &afterDigest); err != nil {
		return fmt.Errorf("reinspect restored canonical panel database after hashing: %w", err)
	}
	if !sameExactUnixFileMetadata(stageStat, afterDigest) ||
		digest != t.validatedStageHash {
		return fmt.Errorf("restored canonical panel database content does not match the validated stage")
	}
	if err := unix.Fsync(finalFD); err != nil {
		return fmt.Errorf("sync restored canonical panel database: %w", err)
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if err := requireDirfdEntryAbsent(
			int(t.parent.Fd()),
			t.finalBase+suffix,
			"restored canonical database sidecar",
		); err != nil {
			return err
		}
	}
	return nil
}

func (t *serviceOperationRestoreTarget) verifyCleanupStageEntry() (bool, error) {
	var pathStat unix.Stat_t
	err := unix.Fstatat(int(t.parent.Fd()), t.stageBase, &pathStat, unix.AT_SYMLINK_NOFOLLOW)
	if errors.Is(err, unix.ENOENT) {
		return false, nil
	}
	if err != nil || t.stage == nil {
		return false, unix.ESTALE
	}
	var descriptorStat unix.Stat_t
	if err := unix.Fstat(int(t.stage.Fd()), &descriptorStat); err != nil {
		return false, err
	}
	if !sameExactUnixFileMetadata(pathStat, descriptorStat) {
		return false, unix.ESTALE
	}
	return true, nil
}

func (t *serviceOperationRestoreTarget) cleanup() error {
	if t == nil || t.parent == nil {
		return nil
	}
	var cleanupErr error
	stagePresent, err := t.verifyCleanupStageEntry()
	if err != nil {
		return err
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		cleanupErr = errors.Join(cleanupErr, requireDirfdEntryAbsent(
			int(t.parent.Fd()),
			t.stageBase+suffix,
			"incomplete database restore stage sidecar",
		))
	}
	if cleanupErr != nil {
		return cleanupErr
	}
	if stagePresent {
		if err := unix.Unlinkat(int(t.parent.Fd()), t.stageBase, 0); err != nil {
			return err
		}
	}
	if t.published {
		cleanupErr = errors.Join(cleanupErr, t.requireFinalSidecarsAbsent())
	}
	if err := t.parent.Sync(); err != nil {
		cleanupErr = errors.Join(cleanupErr, fmt.Errorf("sync canonical database parent after restore cleanup: %w", err))
	}
	return cleanupErr
}

func (t *serviceOperationRestoreTarget) close() error {
	if t == nil {
		return nil
	}
	var closeErr error
	var cleanupErr error
	if t.parent != nil {
		cleanupErr = t.cleanup()
		closeErr = errors.Join(closeErr, cleanupErr)
	}
	if t.parent != nil && cleanupErr == nil {
		closeErr = errors.Join(closeErr, t.unlockParent())
	}
	if t.stage != nil {
		closeErr = errors.Join(closeErr, t.stage.Close())
		t.stage = nil
	}
	if t.parent != nil {
		closeErr = errors.Join(closeErr, t.parent.Close())
		t.parent = nil
	}
	return closeErr
}

func unlinkDirfdEntryIfExpected(
	parentFD int,
	baseName string,
	purpose string,
	owner serviceOperationRestoreOwner,
	allowRootOwner bool,
) error {
	var stat unix.Stat_t
	err := unix.Fstatat(parentFD, baseName, &stat, unix.AT_SYMLINK_NOFOLLOW)
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect %s %s: %w", purpose, baseName, err)
	}
	panelOwned := stat.Uid == owner.uid && stat.Gid == owner.gid
	rootOwned := allowRootOwner && stat.Uid == 0 && stat.Gid == 0
	if stat.Mode&unix.S_IFMT != unix.S_IFREG ||
		stat.Mode&0o777 != 0o600 ||
		stat.Nlink != 1 ||
		(!panelOwned && !rootOwned) {
		return fmt.Errorf("%s %s has unexpected metadata and will not be removed", purpose, baseName)
	}
	if err := unix.Unlinkat(parentFD, baseName, 0); err != nil {
		return fmt.Errorf("remove %s %s: %w", purpose, baseName, err)
	}
	return nil
}

func requireDirfdEntryAbsent(parentFD int, baseName string, purpose string) error {
	var stat unix.Stat_t
	err := unix.Fstatat(parentFD, baseName, &stat, unix.AT_SYMLINK_NOFOLLOW)
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect %s %s: %w", purpose, baseName, err)
	}
	return fmt.Errorf("%s %s must be absent", purpose, baseName)
}

func sameUnixFileIdentity(left unix.Stat_t, right unix.Stat_t) bool {
	return left.Dev == right.Dev && left.Ino == right.Ino
}

func verifyInheritedReleaseTransactionFlock(inheritedFD int, probeFD int) error {
	if err := unix.Flock(probeFD, unix.LOCK_EX|unix.LOCK_NB); err == nil {
		_ = unix.Flock(probeFD, unix.LOCK_UN)
		return fmt.Errorf("inherited release transaction lock is not held")
	} else if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
		return fmt.Errorf("probe inherited release transaction lock: %w", err)
	}
	if err := unix.Flock(inheritedFD, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return fmt.Errorf("inherited descriptor does not own the release transaction flock")
		}
		return fmt.Errorf("verify inherited release transaction flock ownership: %w", err)
	}
	return nil
}

func verifyServiceOperationReleaseTransaction(
	transaction serviceOperationReleaseTransaction,
) error {
	if err := validateServiceOperationReleaseTransaction(transaction, transaction.operation); err != nil {
		return err
	}
	rootPath := serviceOperationReleaseTransactionRoot
	if err := validateRootOwnedSnapshotDirectoryChain(rootPath); err != nil {
		return fmt.Errorf("validate release transaction root chain: %w", err)
	}
	rootInfo, err := os.Lstat(rootPath)
	if err != nil {
		return fmt.Errorf("inspect release transaction root: %w", err)
	}
	rootStat, ok := rootInfo.Sys().(*syscall.Stat_t)
	if !ok ||
		!rootInfo.IsDir() ||
		rootInfo.Mode()&os.ModeSymlink != 0 ||
		rootInfo.Mode().Perm() != 0o700 ||
		rootStat.Uid != 0 ||
		rootStat.Gid != 0 {
		return fmt.Errorf("release transaction root must be root:root mode 0700")
	}
	rootFD, err := unix.Open(
		rootPath,
		unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		0,
	)
	if err != nil {
		return fmt.Errorf("open release transaction root: %w", err)
	}
	defer unix.Close(rootFD)

	lockBase := filepath.Base(serviceOperationReleaseTransactionLockPath)
	probeFD, err := unix.Openat(
		rootFD,
		lockBase,
		unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		0,
	)
	if err != nil {
		return fmt.Errorf("open release transaction lock: %w", err)
	}
	defer unix.Close(probeFD)
	var pathStat unix.Stat_t
	if err := unix.Fstatat(rootFD, lockBase, &pathStat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return fmt.Errorf("inspect release transaction lock path: %w", err)
	}
	var probeStat unix.Stat_t
	if err := unix.Fstat(probeFD, &probeStat); err != nil {
		return fmt.Errorf("inspect release transaction lock probe: %w", err)
	}
	var inheritedStat unix.Stat_t
	if err := unix.Fstat(transaction.fd, &inheritedStat); err != nil {
		return fmt.Errorf("inspect inherited release transaction descriptor: %w", err)
	}
	for _, stat := range []unix.Stat_t{pathStat, probeStat, inheritedStat} {
		if stat.Mode&unix.S_IFMT != unix.S_IFREG ||
			stat.Uid != 0 ||
			stat.Gid != 0 ||
			stat.Mode&0o777 != 0o600 ||
			stat.Nlink != 1 {
			return fmt.Errorf("release transaction lock must be a root:root single-link 0600 regular file")
		}
	}
	if !sameUnixFileIdentity(pathStat, probeStat) ||
		!sameUnixFileIdentity(pathStat, inheritedStat) {
		return fmt.Errorf("inherited descriptor does not name the canonical release transaction lock")
	}
	if err := verifyInheritedReleaseTransactionFlock(transaction.fd, probeFD); err != nil {
		return err
	}

	activeBase := filepath.Base(serviceOperationReleaseTransactionActivePath)
	activeFD, err := unix.Openat(
		rootFD,
		activeBase,
		unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		0,
	)
	if err != nil {
		return fmt.Errorf("open active release transaction marker: %w", err)
	}
	var activePathStat unix.Stat_t
	if err := unix.Fstatat(rootFD, activeBase, &activePathStat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		_ = unix.Close(activeFD)
		return fmt.Errorf("inspect active release transaction marker path: %w", err)
	}
	var activeFDStat unix.Stat_t
	if err := unix.Fstat(activeFD, &activeFDStat); err != nil {
		_ = unix.Close(activeFD)
		return fmt.Errorf("inspect active release transaction marker descriptor: %w", err)
	}
	expectedMarker := canonicalServiceOperationReleaseTransactionMarker(transaction)
	if !sameUnixFileIdentity(activePathStat, activeFDStat) ||
		activeFDStat.Mode&unix.S_IFMT != unix.S_IFREG ||
		activeFDStat.Uid != 0 ||
		activeFDStat.Gid != 0 ||
		activeFDStat.Mode&0o777 != 0o600 ||
		activeFDStat.Nlink != 1 ||
		activeFDStat.Size != int64(len(expectedMarker)) {
		_ = unix.Close(activeFD)
		return fmt.Errorf("active release transaction marker metadata is invalid")
	}
	markerFile := os.NewFile(uintptr(activeFD), serviceOperationReleaseTransactionActivePath)
	if markerFile == nil {
		_ = unix.Close(activeFD)
		return fmt.Errorf("open active release transaction marker handle")
	}
	markerBytes, err := io.ReadAll(io.LimitReader(markerFile, int64(len(expectedMarker)+1)))
	closeErr := markerFile.Close()
	if err != nil || closeErr != nil {
		if err == nil {
			err = closeErr
		}
		return fmt.Errorf("read active release transaction marker: %w", err)
	}
	if !bytes.Equal(markerBytes, expectedMarker) {
		return fmt.Errorf("active release transaction marker does not match token, operation, and snapshot")
	}
	var activeFinalPathStat unix.Stat_t
	if err := unix.Fstatat(rootFD, activeBase, &activeFinalPathStat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return fmt.Errorf("reinspect active release transaction marker path: %w", err)
	}
	if !sameExactUnixFileMetadata(activePathStat, activeFinalPathStat) {
		return fmt.Errorf("active release transaction marker changed while it was verified")
	}
	var finalLockPathStat unix.Stat_t
	if err := unix.Fstatat(rootFD, lockBase, &finalLockPathStat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return fmt.Errorf("reinspect release transaction lock path: %w", err)
	}
	var finalInheritedStat unix.Stat_t
	if err := unix.Fstat(transaction.fd, &finalInheritedStat); err != nil {
		return fmt.Errorf("reinspect inherited release transaction descriptor: %w", err)
	}
	if !sameExactUnixFileMetadata(pathStat, finalLockPathStat) ||
		!sameExactUnixFileMetadata(inheritedStat, finalInheritedStat) ||
		!sameUnixFileIdentity(finalLockPathStat, finalInheritedStat) {
		return fmt.Errorf("release transaction lock changed while it was verified")
	}
	finalRootInfo, err := os.Lstat(rootPath)
	if err != nil {
		return fmt.Errorf("reinspect release transaction root: %w", err)
	}
	if !os.SameFile(rootInfo, finalRootInfo) {
		return fmt.Errorf("release transaction root changed while it was pinned")
	}
	return nil
}
