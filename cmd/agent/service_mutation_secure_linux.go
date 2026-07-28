//go:build linux

package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

var serviceMutationRequiredOwnerUID uint32 = 0
var serviceMutationRequiredOwnerGID = func() uint32 {
	if os.Geteuid() == 0 {
		if gid, ok := lookupGroupID("celikpanel"); ok && gid >= 0 {
			return uint32(gid)
		}
	}
	return uint32(os.Getgid())
}()

func secureServiceMutationStat(path string, info os.FileInfo, wantDirectory bool) error {
	if info == nil {
		return errors.New("missing file information")
	}
	if wantDirectory {
		if !info.IsDir() {
			return fmt.Errorf("%s must be a real directory", path)
		}
	} else if !info.Mode().IsRegular() {
		return fmt.Errorf("%s must be a regular file", path)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("inspect owner of %s", path)
	}
	if stat.Uid != serviceMutationRequiredOwnerUID || stat.Gid != serviceMutationRequiredOwnerGID {
		return fmt.Errorf(
			"%s must be owned by uid %d gid %d",
			path,
			serviceMutationRequiredOwnerUID,
			serviceMutationRequiredOwnerGID,
		)
	}
	if wantDirectory {
		if info.Mode().Perm()&0o022 != 0 {
			return fmt.Errorf("%s must not be writable by group or others", path)
		}
		return nil
	}
	if info.Mode().Perm() != 0o600 || stat.Nlink != 1 {
		return fmt.Errorf("%s must be a single-link 0600 file", path)
	}
	return nil
}

// The state directory's parent (normally /var/lib) is part of the trusted path,
// but it belongs to the host and need not use the CelikPanel service group.
// Require the trusted UID and non-writable real-directory contract while
// deliberately leaving the parent GID unconstrained.
// Durum dizininin üst dizini (normalde /var/lib) güvenilir yolun parçasıdır;
// ancak ana makineye aittir ve CelikPanel servis grubunu kullanması gerekmez.
// Güvenilir UID ile yazılamaz gerçek-dizin sözleşmesini zorunlu tutarken üst
// dizinin GID değerini bilinçli olarak sınırlamayız.
func secureServiceMutationParentDirectoryStat(path string, info os.FileInfo) error {
	if info == nil || !info.IsDir() {
		return fmt.Errorf("%s must be a real directory", path)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("inspect owner of %s", path)
	}
	if stat.Uid != serviceMutationRequiredOwnerUID {
		return fmt.Errorf("%s must be owned by uid %d", path, serviceMutationRequiredOwnerUID)
	}
	if info.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("%s must not be writable by group or others", path)
	}
	return nil
}

func secureServiceMutationStateDirectoryStat(path string, info os.FileInfo) error {
	if err := secureServiceMutationStat(path, info, true); err != nil {
		return err
	}
	if info.Mode().Perm() != 0o700 {
		return fmt.Errorf("%s must be mode 0700", path)
	}
	return nil
}

// A read-only pre-ledger proof also accepts the exact empty root:root 0700
// directory that can remain if the first initializer crashes after mkdir and
// before chown. Only the initializer may repair it.
// Salt okunur ledger-öncesi kanıt, ilk initializer mkdir sonrasında ve chown
// öncesinde çökerse kalabilen tam boş root:root 0700 dizini de kabul eder.
// Bu kalıntıyı yalnız initializer onarabilir.
func securePreLedgerServiceMutationStateDirectoryStat(path string, info os.FileInfo) error {
	strictErr := secureServiceMutationStateDirectoryStat(path, info)
	if strictErr == nil {
		return nil
	}
	if info == nil || !info.IsDir() || info.Mode().Perm() != 0o700 || serviceMutationRequiredOwnerUID != 0 {
		return strictErr
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 || stat.Gid != 0 {
		return strictErr
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return fmt.Errorf("inspect root-owned pre-ledger state directory: %w", err)
	}
	if len(entries) != 0 {
		return errors.New("root:root pre-ledger state directory must be empty")
	}
	return nil
}

func syncServiceMutationStateDirectoryAndParent(path string) error {
	for _, directory := range []string{path, filepath.Dir(path)} {
		fd, err := unix.Open(directory, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_DIRECTORY, 0)
		if err != nil {
			return fmt.Errorf("open service mutation directory for sync %s: %w", directory, err)
		}
		syncErr := unix.Fsync(fd)
		closeErr := unix.Close(fd)
		if syncErr != nil {
			return fmt.Errorf("sync service mutation directory %s: %w", directory, syncErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close service mutation directory %s: %w", directory, closeErr)
		}
	}
	return nil
}

func recoverEmptyRootOwnedServiceMutationStateDirectory(path string, info os.FileInfo) error {
	if info == nil || !info.IsDir() || info.Mode().Perm() != 0o700 || serviceMutationRequiredOwnerUID != 0 {
		return fmt.Errorf("%s is not a recoverable root:root 0700 state directory", path)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 || stat.Gid != 0 {
		return fmt.Errorf("%s is not a recoverable root:root 0700 state directory", path)
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return fmt.Errorf("inspect recoverable state directory: %w", err)
	}
	if len(entries) != 0 {
		return fmt.Errorf("recoverable root:root state directory must be empty")
	}
	if err := os.Chown(path, int(serviceMutationRequiredOwnerUID), int(serviceMutationRequiredOwnerGID)); err != nil {
		return fmt.Errorf("complete state directory ownership: %w", err)
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return fmt.Errorf("complete state directory mode: %w", err)
	}
	if err := syncServiceMutationStateDirectoryAndParent(path); err != nil {
		return err
	}
	return nil
}

func ensureSecureServiceMutationStateDirectory(path string) error {
	path = filepath.Clean(path)
	if !filepath.IsAbs(path) {
		return errors.New("service mutation state directory must be absolute")
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		parent := filepath.Dir(path)
		parentInfo, parentErr := os.Lstat(parent)
		if parentErr != nil {
			return fmt.Errorf("inspect state parent: %w", parentErr)
		}
		if err := secureServiceMutationParentDirectoryStat(parent, parentInfo); err != nil {
			return err
		}
		if err := os.Mkdir(path, 0o700); err != nil {
			return fmt.Errorf("create state directory: %w", err)
		}
		if err := os.Chown(path, int(serviceMutationRequiredOwnerUID), int(serviceMutationRequiredOwnerGID)); err != nil {
			return fmt.Errorf("set state directory ownership: %w", err)
		}
		if err := os.Chmod(path, 0o700); err != nil {
			return fmt.Errorf("set state directory mode: %w", err)
		}
		if err := syncServiceMutationStateDirectoryAndParent(path); err != nil {
			return err
		}
		info, err = os.Lstat(path)
	}
	if err != nil {
		return fmt.Errorf("inspect state directory: %w", err)
	}
	if err := secureServiceMutationStateDirectoryStat(path, info); err == nil {
		return nil
	}
	if err := recoverEmptyRootOwnedServiceMutationStateDirectory(path, info); err != nil {
		return err
	}
	info, err = os.Lstat(path)
	if err != nil {
		return fmt.Errorf("reinspect recovered state directory: %w", err)
	}
	return secureServiceMutationStateDirectoryStat(path, info)
}

func readSecureServiceMutationLedger(path string, maxSize int64) ([]byte, bool, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if errors.Is(err, unix.ENOENT) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("open service mutation ledger: %w", err)
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return nil, false, errors.New("open service mutation ledger file handle")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, false, fmt.Errorf("inspect service mutation ledger: %w", err)
	}
	if err := secureServiceMutationStat(path, info, false); err != nil {
		return nil, false, err
	}
	if info.Size() > maxSize {
		return nil, false, fmt.Errorf("service mutation ledger exceeds %d bytes", maxSize)
	}
	raw, err := io.ReadAll(io.LimitReader(file, maxSize+1))
	if err != nil {
		return nil, false, fmt.Errorf("read service mutation ledger: %w", err)
	}
	if int64(len(raw)) > maxSize {
		return nil, false, fmt.Errorf("service mutation ledger exceeds %d bytes", maxSize)
	}
	return raw, true, nil
}
func readRecoverableInitialServiceMutationStage(path string, maxSize int64) ([]byte, bool, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if errors.Is(err, unix.ENOENT) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("open recoverable initial service mutation stage: %w", err)
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return nil, false, errors.New("open recoverable initial service mutation stage handle")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, false, fmt.Errorf("inspect recoverable initial service mutation stage: %w", err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	groupAllowed := ok && (stat.Gid == serviceMutationRequiredOwnerGID ||
		(serviceMutationRequiredOwnerUID == 0 && stat.Gid == 0))
	if !ok || !info.Mode().IsRegular() || stat.Uid != serviceMutationRequiredOwnerUID ||
		!groupAllowed || info.Mode().Perm() != 0o600 || stat.Nlink != 1 || info.Size() > maxSize {
		return nil, false, errors.New("recoverable initial service mutation stage metadata is unsafe")
	}
	raw, err := io.ReadAll(io.LimitReader(file, maxSize+1))
	if err != nil {
		return nil, false, fmt.Errorf("read recoverable initial service mutation stage: %w", err)
	}
	if int64(len(raw)) > maxSize {
		return nil, false, fmt.Errorf("recoverable initial service mutation stage exceeds %d bytes", maxSize)
	}
	return raw, true, nil
}
