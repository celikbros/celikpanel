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
	return secureWriteConfigReplacingSnapshot(path, content, mode, nil)
}

type secureConfigOwner struct {
	uid uint32
	gid uint32
}

type secureConfigWriteOptions struct {
	parentPolicy           *bindConfigOwnerPolicy
	requiredOwner          *secureConfigOwner
	beforeFinalParentProof func()
}

func sameSecureConfigStat(left, right unix.Stat_t) bool {
	return left.Dev == right.Dev && left.Ino == right.Ino &&
		left.Mode == right.Mode && left.Uid == right.Uid && left.Gid == right.Gid &&
		left.Nlink == right.Nlink && left.Size == right.Size &&
		left.Mtim == right.Mtim && left.Ctim == right.Ctim
}

func inspectBINDConfigParentFD(
	parentFD int,
	policy bindConfigOwnerPolicy,
) (unix.Stat_t, error) {
	var stat unix.Stat_t
	if err := unix.Fstat(parentFD, &stat); err != nil {
		return unix.Stat_t{}, fmt.Errorf("stat BIND config parent: %w", err)
	}
	allowedGID := stat.Gid == 0 || (policy.apt && stat.Gid == policy.bindGID)
	permissions := stat.Mode & 0o777
	special := stat.Mode & (unix.S_ISUID | unix.S_ISGID | unix.S_ISVTX)
	accessPermissions := uint32(0o005)
	if policy.apt && stat.Gid == policy.bindGID {
		accessPermissions = 0o050
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR || stat.Uid != 0 || !allowedGID ||
		permissions&0o700 != 0o700 || permissions&0o022 != 0 ||
		permissions&accessPermissions != accessPermissions ||
		(special != 0 && (!policy.apt || special != unix.S_ISGID)) {
		return unix.Stat_t{}, errors.New("BIND config parent directory has unsafe ownership or mode")
	}
	if err := rejectBINDDirectoryACL(parentFD, "BIND config parent directory"); err != nil {
		return unix.Stat_t{}, err
	}
	return stat, nil
}

func verifyBINDConfigParentFD(
	parentFD int,
	policy bindConfigOwnerPolicy,
) error {
	_, err := inspectBINDConfigParentFD(parentFD, policy)
	return err
}

func verifyBINDConfigParentPath(
	path string,
	policy bindConfigOwnerPolicy,
) error {
	relative, err := secureConfigRelativePath(path)
	if err != nil {
		return err
	}
	rootFD, err := openSecureConfigRoot()
	if err != nil {
		return err
	}
	defer unix.Close(rootFD)
	parentFD, err := unix.Openat2(rootFD, filepath.Dir(relative), &unix.OpenHow{
		Flags:   uint64(unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW),
		Resolve: secureConfigResolve,
	})
	if err != nil {
		return secureConfigOpenError("verify BIND config parent", path, err)
	}
	defer unix.Close(parentFD)
	return verifyBINDConfigParentFD(parentFD, policy)
}

func secureWriteConfigReplacingSnapshot(
	path string,
	content []byte,
	mode os.FileMode,
	expected *dnsFileSnapshot,
) error {
	return secureWriteConfigReplacingSnapshotWithBINDParent(
		path, content, mode, expected, nil,
	)
}

func secureWriteConfigReplacingSnapshotWithOwner(
	path string,
	content []byte,
	mode os.FileMode,
	expected *dnsFileSnapshot,
	requiredUID, requiredGID uint32,
) error {
	return secureWriteConfigReplacingSnapshotWithOwnerAndHook(
		path, content, mode, expected, requiredUID, requiredGID, nil,
	)
}

func secureWriteConfigReplacingSnapshotWithOwnerAndHook(
	path string,
	content []byte,
	mode os.FileMode,
	expected *dnsFileSnapshot,
	requiredUID, requiredGID uint32,
	beforeFinalParentProof func(),
) error {
	return secureWriteConfigReplacingSnapshotWithOptions(
		path,
		content,
		mode,
		expected,
		secureConfigWriteOptions{
			requiredOwner:          &secureConfigOwner{uid: requiredUID, gid: requiredGID},
			beforeFinalParentProof: beforeFinalParentProof,
		},
	)
}

func secureWriteBINDConfigReplacingSnapshot(
	path string,
	content []byte,
	mode os.FileMode,
	expected *dnsFileSnapshot,
	policy bindConfigOwnerPolicy,
) error {
	return secureWriteConfigReplacingSnapshotWithBINDParent(
		path, content, mode, expected, &policy,
	)
}

func secureWriteConfigReplacingSnapshotWithBINDParent(
	path string,
	content []byte,
	mode os.FileMode,
	expected *dnsFileSnapshot,
	parentPolicy *bindConfigOwnerPolicy,
) error {
	return secureWriteConfigReplacingSnapshotWithBINDParentAndHook(
		path, content, mode, expected, parentPolicy, nil,
	)
}

func secureWriteConfigReplacingSnapshotWithBINDParentAndHook(
	path string,
	content []byte,
	mode os.FileMode,
	expected *dnsFileSnapshot,
	parentPolicy *bindConfigOwnerPolicy,
	beforeFinalParentProof func(),
) error {
	return secureWriteConfigReplacingSnapshotWithOptions(
		path,
		content,
		mode,
		expected,
		secureConfigWriteOptions{
			parentPolicy:           parentPolicy,
			beforeFinalParentProof: beforeFinalParentProof,
		},
	)
}

func secureWriteConfigReplacingSnapshotWithOptions(
	path string,
	content []byte,
	mode os.FileMode,
	expected *dnsFileSnapshot,
	options secureConfigWriteOptions,
) error {
	requiredOwner := options.requiredOwner
	if requiredOwner != nil && expected == nil {
		return errors.New("managed configuration owner-controlled replacement requires an exact preimage")
	}
	if expected != nil {
		if err := validateDNSFileSnapshotIntegrity(*expected); err != nil {
			return err
		}
		if expected.Path != filepath.Clean(path) ||
			(expected.Exists &&
				(expected.Mode != uint32(mode.Perm()) || !expected.OwnerKnown)) ||
			(!expected.Exists && requiredOwner == nil) {
			return errors.New("managed configuration replacement preimage is invalid")
		}
		if requiredOwner != nil && expected.Exists &&
			(expected.UID != requiredOwner.uid || expected.GID != requiredOwner.gid) {
			return errors.New("managed configuration replacement preimage owner differs from the required contract")
		}
	}
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
	parentPolicy := options.parentPolicy
	var parentBefore unix.Stat_t
	if parentPolicy != nil {
		parentBefore, err = inspectBINDConfigParentFD(parentFD, *parentPolicy)
		if err != nil {
			return err
		}
	}

	fileMode := uint32(mode.Perm())
	ownerUID, ownerGID := -1, -1
	var existingStat unix.Stat_t
	existingExists := false
	existingFD, err := unix.Openat2(parentFD, base, &unix.OpenHow{
		Flags:   uint64(unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK),
		Resolve: secureConfigResolve,
	})
	if err == nil {
		if err := unix.Fstat(existingFD, &existingStat); err != nil {
			unix.Close(existingFD)
			return fmt.Errorf("stat managed configuration %s: %w", path, err)
		}
		if existingStat.Mode&unix.S_IFMT != unix.S_IFREG ||
			(expected != nil && existingStat.Nlink != 1) {
			unix.Close(existingFD)
			return configPathRefusal("write managed configuration refused for non-regular file: %s", path)
		}
		if expected != nil && !expected.Exists {
			unix.Close(existingFD)
			return errors.New("managed configuration replacement preimage appeared")
		}
		existingExists = true
		fileMode = existingStat.Mode & 0o777
		ownerUID, ownerGID = int(existingStat.Uid), int(existingStat.Gid)
		if expected != nil {
			if fileMode != expected.Mode || existingStat.Uid != expected.UID ||
				existingStat.Gid != expected.GID {
				unix.Close(existingFD)
				return errors.New("managed configuration replacement ownership or mode changed")
			}
			existing := os.NewFile(uintptr(existingFD), path+" (replacement preimage)")
			if existing == nil {
				unix.Close(existingFD)
				return errors.New("managed configuration replacement preimage has an invalid descriptor")
			}
			existingData, readErr := io.ReadAll(existing)
			var afterRead unix.Stat_t
			statErr := unix.Fstat(existingFD, &afterRead)
			closeErr := existing.Close()
			if readErr != nil || statErr != nil || closeErr != nil {
				return errors.Join(readErr, statErr, closeErr)
			}
			if !sameSecureConfigStat(existingStat, afterRead) ||
				digestDNSBytes(existingData) != expected.SHA256 ||
				!bytes.Equal(existingData, expected.Data) {
				return errors.New("managed configuration replacement preimage changed")
			}
		} else {
			unix.Close(existingFD)
		}
	} else if !errors.Is(err, unix.ENOENT) {
		return secureConfigOpenError("inspect managed configuration", path, err)
	} else if expected != nil && expected.Exists {
		return errors.New("managed configuration replacement preimage disappeared")
	}
	if requiredOwner != nil {
		ownerUID = int(requiredOwner.uid)
		ownerGID = int(requiredOwner.gid)
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
	var tempStat unix.Stat_t
	if err := unix.Fstat(fd, &tempStat); err != nil {
		return fmt.Errorf("stat atomic managed configuration %s: %w", path, err)
	}
	ownerMismatch := ownerUID >= 0 &&
		(int(tempStat.Uid) != ownerUID || int(tempStat.Gid) != ownerGID)
	if tempStat.Mode&unix.S_IFMT != unix.S_IFREG || tempStat.Nlink != 1 ||
		tempStat.Mode&0o777 != fileMode || ownerMismatch {
		return errors.New("atomic managed configuration metadata differs from the exact contract")
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close managed configuration %s: %w", path, err)
	}
	closed = true
	if existingExists {
		currentFD, openErr := unix.Openat2(parentFD, base, &unix.OpenHow{
			Flags:   uint64(unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK),
			Resolve: secureConfigResolve,
		})
		if openErr != nil {
			return secureConfigOpenError("reprove managed configuration", path, openErr)
		}
		var currentStat unix.Stat_t
		statErr := unix.Fstat(currentFD, &currentStat)
		closeErr := unix.Close(currentFD)
		if statErr != nil || closeErr != nil {
			return errors.Join(statErr, closeErr)
		}
		if !sameSecureConfigStat(existingStat, currentStat) {
			return errors.New("managed configuration changed before atomic replacement")
		}
	}
	if options.beforeFinalParentProof != nil {
		options.beforeFinalParentProof()
	}
	if parentPolicy != nil {
		parentAfter, err := inspectBINDConfigParentFD(parentFD, *parentPolicy)
		if err != nil {
			return err
		}
		if parentBefore.Dev != parentAfter.Dev || parentBefore.Ino != parentAfter.Ino ||
			parentBefore.Mode != parentAfter.Mode || parentBefore.Uid != parentAfter.Uid ||
			parentBefore.Gid != parentAfter.Gid || parentBefore.Nlink != parentAfter.Nlink {
			return errors.New("BIND config parent directory changed before atomic replacement")
		}
		currentParentFD, openErr := unix.Openat2(rootFD, parent, &unix.OpenHow{
			Flags:   uint64(unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW),
			Resolve: secureConfigResolve,
		})
		if openErr != nil {
			return secureConfigOpenError("reprove BIND config parent", path, openErr)
		}
		currentParent, inspectErr := inspectBINDConfigParentFD(currentParentFD, *parentPolicy)
		closeErr := unix.Close(currentParentFD)
		if inspectErr != nil || closeErr != nil {
			return errors.Join(inspectErr, closeErr)
		}
		if currentParent.Dev != parentAfter.Dev || currentParent.Ino != parentAfter.Ino ||
			currentParent.Mode != parentAfter.Mode || currentParent.Uid != parentAfter.Uid ||
			currentParent.Gid != parentAfter.Gid || currentParent.Nlink != parentAfter.Nlink {
			return errors.New("BIND config parent path changed before atomic replacement")
		}
	}
	if expected != nil && !expected.Exists {
		err = unix.Renameat2(
			parentFD, tempName, parentFD, base, unix.RENAME_NOREPLACE,
		)
	} else {
		err = unix.Renameat(parentFD, tempName, parentFD, base)
	}
	if err != nil {
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
