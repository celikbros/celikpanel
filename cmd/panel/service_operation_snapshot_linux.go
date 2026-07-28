//go:build linux

package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

type serviceOperationSnapshotDestination struct {
	parentPath         string
	finalBase          string
	stagePath          string
	stageBase          string
	parent             *os.File
	stage              *os.File
	validatedStageStat *unix.Stat_t
	validatedStageHash [sha256.Size]byte
	published          bool
	keepFinal          bool
	beforeRename       func() error
	afterRename        func() error
}

func prepareServiceOperationSnapshotDestination(
	destinationPath string,
) (*serviceOperationSnapshotDestination, error) {
	if destinationPath == "" || !filepath.IsAbs(destinationPath) {
		return nil, fmt.Errorf("snapshot destination must be an explicit absolute path")
	}
	destinationPath = filepath.Clean(destinationPath)
	if filepath.Base(destinationPath) != serviceOperationSnapshotBasename {
		return nil, fmt.Errorf("snapshot destination basename must be exactly %s", serviceOperationSnapshotBasename)
	}
	parentPath := filepath.Dir(destinationPath)
	if err := validateRootOwnedSnapshotDirectoryChain(parentPath); err != nil {
		return nil, err
	}
	parentFD, err := unix.Open(
		parentPath,
		unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		0,
	)
	if err != nil {
		return nil, fmt.Errorf("open snapshot destination parent: %w", err)
	}
	parent := os.NewFile(uintptr(parentFD), parentPath)
	if parent == nil {
		_ = unix.Close(parentFD)
		return nil, fmt.Errorf("open snapshot destination parent handle")
	}
	destination := &serviceOperationSnapshotDestination{
		parentPath: parentPath,
		finalBase:  serviceOperationSnapshotBasename,
		parent:     parent,
	}
	if err := destination.verifyParent(); err != nil {
		parent.Close()
		return nil, err
	}
	if err := destination.requireAbsent(destination.finalBase); err != nil {
		parent.Close()
		return nil, err
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if err := destination.requireAbsent(destination.finalBase + suffix); err != nil {
			parent.Close()
			return nil, err
		}
	}
	return destination, nil
}

// validateRootOwnedSnapshotDirectoryChain rejects every symlink, non-directory,
// non-root-owned, or group/other-writable path component from / downward.
// validateRootOwnedSnapshotDirectoryChain, / kökünden aşağıdaki her sembolik
// bağı, dizin olmayanı, root sahipliğinde olmayanı veya grup/diğer yazılabilir
// yol bileşenini reddeder.
func validateRootOwnedSnapshotDirectoryChain(directoryPath string) error {
	if !filepath.IsAbs(directoryPath) {
		return fmt.Errorf("snapshot destination parent must be absolute")
	}
	cleanPath := filepath.Clean(directoryPath)
	currentPath := string(os.PathSeparator)
	relativePath := strings.TrimPrefix(cleanPath, currentPath)
	components := []string{}
	if relativePath != "" {
		components = strings.Split(relativePath, string(os.PathSeparator))
	}
	if err := validateRootOwnedSnapshotDirectory(currentPath); err != nil {
		return err
	}
	for _, component := range components {
		if component == "" || component == "." || component == ".." {
			return fmt.Errorf("snapshot destination parent has an invalid component")
		}
		currentPath = filepath.Join(currentPath, component)
		if err := validateRootOwnedSnapshotDirectory(currentPath); err != nil {
			return err
		}
	}
	return nil
}

func validateRootOwnedSnapshotDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect snapshot destination directory %s: %w", path, err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("snapshot destination path component %s must be a real directory", path)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("inspect snapshot destination owner for %s", path)
	}
	if stat.Uid != 0 || stat.Gid != 0 {
		return fmt.Errorf("snapshot destination path component %s must be root-owned", path)
	}
	if info.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("snapshot destination path component %s must not be writable by group or others", path)
	}
	return nil
}

func (d *serviceOperationSnapshotDestination) verifyParent() error {
	if err := validateRootOwnedSnapshotDirectoryChain(d.parentPath); err != nil {
		return err
	}
	pathInfo, err := os.Lstat(d.parentPath)
	if err != nil {
		return fmt.Errorf("inspect snapshot destination parent: %w", err)
	}
	pinnedInfo, err := d.parent.Stat()
	if err != nil {
		return fmt.Errorf("inspect pinned snapshot destination parent: %w", err)
	}
	if !os.SameFile(pathInfo, pinnedInfo) {
		return fmt.Errorf("snapshot destination parent changed while pinned")
	}
	return nil
}

func (d *serviceOperationSnapshotDestination) requireAbsent(baseName string) error {
	var stat unix.Stat_t
	err := unix.Fstatat(int(d.parent.Fd()), baseName, &stat, unix.AT_SYMLINK_NOFOLLOW)
	switch {
	case err == nil:
		return fmt.Errorf("snapshot destination entry %s already exists", baseName)
	case errors.Is(err, unix.ENOENT):
		return nil
	default:
		return fmt.Errorf("inspect snapshot destination entry %s: %w", baseName, err)
	}
}

func (d *serviceOperationSnapshotDestination) createStage() (string, error) {
	if err := d.verifyParent(); err != nil {
		return "", err
	}
	randomBytes := make([]byte, 16)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", fmt.Errorf("generate snapshot staging name: %w", err)
	}
	d.stageBase = "." + serviceOperationSnapshotBasename + ".snapshot-" + hex.EncodeToString(randomBytes)
	d.stagePath = filepath.Join(d.parentPath, d.stageBase)
	fd, err := unix.Openat(
		int(d.parent.Fd()),
		d.stageBase,
		unix.O_RDWR|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		0o600,
	)
	if err != nil {
		return "", fmt.Errorf("create snapshot staging file: %w", err)
	}
	if err := unix.Fchmod(fd, 0o600); err != nil {
		_ = unix.Close(fd)
		return "", fmt.Errorf("set snapshot staging mode: %w", err)
	}
	d.stage = os.NewFile(uintptr(fd), d.stagePath)
	if d.stage == nil {
		_ = unix.Close(fd)
		return "", fmt.Errorf("open snapshot staging file handle")
	}
	return d.stagePath, nil
}

func (d *serviceOperationSnapshotDestination) syncAndVerifyStage() error {
	if err := d.verifyParent(); err != nil {
		return err
	}
	if err := d.verifyStageFile(); err != nil {
		return err
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if err := d.requireAbsent(d.stageBase + suffix); err != nil {
			return err
		}
	}
	if err := d.stage.Sync(); err != nil {
		return fmt.Errorf("sync snapshot staging file: %w", err)
	}
	return nil
}

func (d *serviceOperationSnapshotDestination) verifyStageFile() error {
	if d.stage == nil {
		return fmt.Errorf("snapshot staging file is not open")
	}
	var pathStat unix.Stat_t
	if err := unix.Fstatat(
		int(d.parent.Fd()),
		d.stageBase,
		&pathStat,
		unix.AT_SYMLINK_NOFOLLOW,
	); err != nil {
		return fmt.Errorf("inspect snapshot staging file: %w", err)
	}
	var descriptorStat unix.Stat_t
	if err := unix.Fstat(int(d.stage.Fd()), &descriptorStat); err != nil {
		return fmt.Errorf("inspect pinned snapshot staging file: %w", err)
	}
	for _, stat := range []unix.Stat_t{pathStat, descriptorStat} {
		if stat.Mode&unix.S_IFMT != unix.S_IFREG ||
			stat.Uid != 0 ||
			stat.Gid != 0 ||
			stat.Mode&0o777 != 0o600 ||
			stat.Nlink != 1 {
			return fmt.Errorf("snapshot staging file must be a root-owned single-link 0600 regular file")
		}
	}
	if !sameExactUnixFileMetadata(pathStat, descriptorStat) {
		return fmt.Errorf("snapshot staging path does not match its pinned descriptor")
	}
	return nil
}

// validateStage validates SQLite through the pinned descriptor and captures
// exact immutable publish evidence. A path exchange cannot make validation
// inspect different bytes than the later rename candidate.
// validateStage, SQLite'ı sabitlenmiş descriptor üzerinden doğrular ve tam,
// değişmez yayın kanıtını yakalar. Yol değişimi doğrulamanın sonraki rename
// adayından farklı baytları incelemesine neden olamaz.
func (d *serviceOperationSnapshotDestination) validateStage(
	schema serviceOperationSnapshotSchema,
) error {
	if err := d.verifyParent(); err != nil {
		return err
	}
	if err := d.verifyStageFile(); err != nil {
		return err
	}
	var beforeValidation unix.Stat_t
	if err := unix.Fstat(int(d.stage.Fd()), &beforeValidation); err != nil {
		return fmt.Errorf("capture snapshot staging file before validation: %w", err)
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if err := d.requireAbsent(d.stageBase + suffix); err != nil {
			return err
		}
	}
	stageDescriptorPath := fmt.Sprintf("/proc/self/fd/%d", d.stage.Fd())
	if err := validateServiceOperationSnapshot(stageDescriptorPath, schema); err != nil {
		return fmt.Errorf("validate %s service operation snapshot: %w", schema, err)
	}
	if err := d.verifyStageFile(); err != nil {
		return err
	}
	var afterValidation unix.Stat_t
	if err := unix.Fstat(int(d.stage.Fd()), &afterValidation); err != nil {
		return fmt.Errorf("reinspect snapshot staging file after validation: %w", err)
	}
	if !sameExactUnixFileMetadata(beforeValidation, afterValidation) {
		return fmt.Errorf("snapshot staging file changed while it was validated")
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if err := d.requireAbsent(d.stageBase + suffix); err != nil {
			return err
		}
	}
	if err := d.stage.Sync(); err != nil {
		return fmt.Errorf("sync validated snapshot staging file: %w", err)
	}
	var validated unix.Stat_t
	if err := unix.Fstat(int(d.stage.Fd()), &validated); err != nil {
		return fmt.Errorf("capture validated snapshot staging file: %w", err)
	}
	digest, err := digestPinnedServiceOperationFile(d.stage, validated.Size)
	if err != nil {
		return fmt.Errorf("capture validated snapshot content: %w", err)
	}
	var afterDigest unix.Stat_t
	if err := unix.Fstat(int(d.stage.Fd()), &afterDigest); err != nil {
		return fmt.Errorf("reinspect snapshot staging file after hashing: %w", err)
	}
	if !sameExactUnixFileMetadata(validated, afterDigest) {
		return fmt.Errorf("snapshot staging file changed while its content was hashed")
	}
	d.validatedStageStat = &validated
	d.validatedStageHash = digest
	return nil
}

func (d *serviceOperationSnapshotDestination) verifyValidatedStage() error {
	if d.validatedStageStat == nil {
		return fmt.Errorf("snapshot staging file has not been validated")
	}
	if err := d.verifyStageFile(); err != nil {
		return err
	}
	var descriptorStat unix.Stat_t
	if err := unix.Fstat(int(d.stage.Fd()), &descriptorStat); err != nil {
		return fmt.Errorf("reinspect validated snapshot staging descriptor: %w", err)
	}
	if !sameExactUnixFileMetadata(*d.validatedStageStat, descriptorStat) {
		return fmt.Errorf("snapshot staging file changed after validation")
	}
	digest, err := digestPinnedServiceOperationFile(d.stage, descriptorStat.Size)
	if err != nil {
		return fmt.Errorf("recheck validated snapshot content: %w", err)
	}
	var afterDigest unix.Stat_t
	if err := unix.Fstat(int(d.stage.Fd()), &afterDigest); err != nil {
		return fmt.Errorf("reinspect validated snapshot after hashing: %w", err)
	}
	if !sameExactUnixFileMetadata(descriptorStat, afterDigest) ||
		digest != d.validatedStageHash {
		return fmt.Errorf("snapshot staging content changed after validation")
	}
	return nil
}

func (d *serviceOperationSnapshotDestination) verifyPublishPreconditions() error {
	if err := d.verifyParent(); err != nil {
		return err
	}
	if err := d.verifyValidatedStage(); err != nil {
		return err
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if err := d.requireAbsent(d.stageBase + suffix); err != nil {
			return err
		}
	}
	if err := d.requireAbsent(d.finalBase); err != nil {
		return fmt.Errorf("publish snapshot without replacement: %w", err)
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if err := d.requireAbsent(d.finalBase + suffix); err != nil {
			return err
		}
	}
	return nil
}

func (d *serviceOperationSnapshotDestination) verifyPublishedFinal() error {
	if d.validatedStageStat == nil || d.stage == nil {
		return fmt.Errorf("published snapshot has no pinned validation evidence")
	}
	fd, err := unix.Openat(
		int(d.parent.Fd()),
		d.finalBase,
		unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		0,
	)
	if err != nil {
		return fmt.Errorf("open published snapshot for sync: %w", err)
	}
	defer unix.Close(fd)
	var finalStat unix.Stat_t
	if err := unix.Fstat(fd, &finalStat); err != nil {
		return fmt.Errorf("inspect published snapshot: %w", err)
	}
	var descriptorStat unix.Stat_t
	if err := unix.Fstat(int(d.stage.Fd()), &descriptorStat); err != nil {
		return fmt.Errorf("inspect published snapshot descriptor: %w", err)
	}
	if !sameExactUnixFileMetadata(finalStat, descriptorStat) ||
		!samePublishedUnixFileMetadata(*d.validatedStageStat, finalStat) {
		return fmt.Errorf("published snapshot does not match the validated staging file")
	}
	if finalStat.Mode&unix.S_IFMT != unix.S_IFREG ||
		finalStat.Uid != 0 ||
		finalStat.Gid != 0 ||
		finalStat.Mode&0o777 != 0o600 ||
		finalStat.Nlink != 1 {
		return fmt.Errorf("published snapshot must be a root-owned single-link 0600 regular file")
	}
	digest, err := digestPinnedServiceOperationFile(d.stage, descriptorStat.Size)
	if err != nil {
		return fmt.Errorf("verify published snapshot content: %w", err)
	}
	var afterDigest unix.Stat_t
	if err := unix.Fstat(int(d.stage.Fd()), &afterDigest); err != nil {
		return fmt.Errorf("reinspect published snapshot after hashing: %w", err)
	}
	if !sameExactUnixFileMetadata(descriptorStat, afterDigest) ||
		digest != d.validatedStageHash {
		return fmt.Errorf("published snapshot content does not match the validated staging file")
	}
	if err := unix.Fsync(fd); err != nil {
		return fmt.Errorf("sync published snapshot: %w", err)
	}
	return nil
}

func (d *serviceOperationSnapshotDestination) publish() error {
	if err := d.verifyPublishPreconditions(); err != nil {
		return err
	}
	if d.beforeRename != nil {
		if err := d.beforeRename(); err != nil {
			return fmt.Errorf("prepare snapshot atomic publish: %w", err)
		}
	}
	// The hook models the last adversarial/fault boundary before rename. Repeat
	// every exact proof after it so validated bytes cannot be replaced or edited.
	// Hook, rename öncesindeki son saldırgan/hata sınırını modeller. Doğrulanmış
	// baytların değiştirilmemesi için tüm tam kanıtları hook sonrasında yinele.
	if err := d.verifyPublishPreconditions(); err != nil {
		return err
	}
	if err := unix.Renameat2(
		int(d.parent.Fd()),
		d.stageBase,
		int(d.parent.Fd()),
		d.finalBase,
		unix.RENAME_NOREPLACE,
	); err != nil {
		return fmt.Errorf("publish snapshot without replacement: %w", err)
	}
	d.published = true
	if d.afterRename != nil {
		if err := d.afterRename(); err != nil {
			return fmt.Errorf("finish snapshot after atomic publish: %w", err)
		}
	}

	if err := d.verifyPublishedFinal(); err != nil {
		return err
	}
	if err := d.parent.Sync(); err != nil {
		return fmt.Errorf("sync snapshot destination parent: %w", err)
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if err := d.requireAbsent(d.finalBase + suffix); err != nil {
			return err
		}
	}
	d.stageBase = ""
	d.stagePath = ""
	d.keepFinal = true
	return nil
}

func (d *serviceOperationSnapshotDestination) verifyCleanupEntry(baseName string) error {
	var pathStat unix.Stat_t
	if err := unix.Fstatat(int(d.parent.Fd()), baseName, &pathStat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return err
	}
	var descriptorStat unix.Stat_t
	if err := unix.Fstat(int(d.stage.Fd()), &descriptorStat); err != nil {
		return err
	}
	if !sameExactUnixFileMetadata(pathStat, descriptorStat) {
		return unix.ESTALE
	}
	return nil
}

func (d *serviceOperationSnapshotDestination) cleanup() error {
	if d == nil || d.parent == nil {
		return nil
	}
	if d.published && !d.keepFinal {
		if err := d.verifyCleanupEntry(d.finalBase); err != nil {
			return err
		}
	} else if !d.published && len(d.stageBase) > 0 {
		if err := d.verifyCleanupEntry(d.stageBase); err != nil {
			return err
		}
	}
	var cleanupErr error
	remove := func(baseName string) {
		if baseName == "" {
			return
		}
		if err := unix.Unlinkat(int(d.parent.Fd()), baseName, 0); err != nil &&
			!errors.Is(err, unix.ENOENT) {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("remove incomplete snapshot entry %s: %w", baseName, err))
		}
	}
	if d.stageBase != "" {
		for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
			remove(d.stageBase + suffix)
		}
	}
	if d.published && !d.keepFinal {
		for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
			remove(d.finalBase + suffix)
		}
	}
	if err := d.parent.Sync(); err != nil {
		cleanupErr = errors.Join(cleanupErr, fmt.Errorf("sync snapshot destination parent after cleanup: %w", err))
	}
	return cleanupErr
}

func (d *serviceOperationSnapshotDestination) close() error {
	if d == nil {
		return nil
	}
	var closeErr error
	if d.stage != nil {
		closeErr = errors.Join(closeErr, d.stage.Close())
		d.stage = nil
	}
	if d.parent != nil {
		closeErr = errors.Join(closeErr, d.parent.Close())
		d.parent = nil
	}
	return closeErr
}
