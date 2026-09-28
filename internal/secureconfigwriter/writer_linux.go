//go:build linux

// Package secureconfigwriter publishes managed configuration using an exact
// snapshot preimage and a durable, no-symlink atomic replacement.
package secureconfigwriter

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

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"golang.org/x/sys/unix"
)

const resolve = unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS

// Owner selects the exact identity of a managed configuration file.
type Owner struct{ UID, GID uint32 }

// Options supplies the host's parent policy and the writer's owner contract.
// ParentValidator must inspect the supplied directory descriptor, including
// any host-specific ACL policy. It is invoked before and after staging and on
// a freshly resolved parent path before publication.
type Options struct {
	RequiredOwner          *Owner
	PublishedOwner         *Owner
	ParentValidator        func(int) (unix.Stat_t, error)
	BeforeFinalParentProof func()
	PathRefused            error
}

func relativePath(path string, refused error) (string, error) {
	clean := filepath.Clean(path)
	if !filepath.IsAbs(clean) || clean == string(os.PathSeparator) {
		return "", pathRefusal(refused, "managed configuration path must be an absolute file path: %s", path)
	}
	relative := strings.TrimPrefix(clean, string(os.PathSeparator))
	if relative == "" || relative == "." {
		return "", pathRefusal(refused, "managed configuration path must name a file: %s", path)
	}
	return relative, nil
}

func openRoot() (int, error) {
	fd, err := unix.Open("/", unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, fmt.Errorf("open managed configuration root: %w", err)
	}
	return fd, nil
}

func pathRefusal(refused error, format string, args ...any) error {
	if refused == nil {
		refused = errors.New("configuration path refused")
	}
	return fmt.Errorf("%w: %s", refused, fmt.Sprintf(format, args...))
}

func openError(operation, path string, err, refused error) error {
	if errors.Is(err, unix.ELOOP) || errors.Is(err, unix.EXDEV) {
		return pathRefusal(refused, "%s refused because the path contains a symbolic link or escapes the managed root (%s): %v", operation, path, err)
	}
	if errors.Is(err, unix.ENOSYS) {
		return fmt.Errorf("%s refused because secure openat2 path resolution is unavailable: %w", operation, err)
	}
	return fmt.Errorf("%s %s: %w", operation, path, err)
}

func sameStat(left, right unix.Stat_t) bool {
	return left.Dev == right.Dev && left.Ino == right.Ino &&
		left.Mode == right.Mode && left.Uid == right.Uid && left.Gid == right.Gid &&
		left.Nlink == right.Nlink && left.Size == right.Size &&
		left.Mtim == right.Mtim && left.Ctim == right.Ctim
}

func sameParent(left, right unix.Stat_t) bool {
	return left.Dev == right.Dev && left.Ino == right.Ino &&
		left.Mode == right.Mode && left.Uid == right.Uid &&
		left.Gid == right.Gid && left.Nlink == right.Nlink
}

// Write publishes content only if the typed snapshot still matches the live file.
func Write(
	path string,
	content []byte,
	mode os.FileMode,
	expected *dnsengineartifact.FileSnapshot,
	options Options,
) error {
	requiredOwner := options.RequiredOwner
	if requiredOwner != nil && expected == nil {
		return errors.New("managed configuration owner-controlled replacement requires an exact preimage")
	}
	if requiredOwner != nil && options.PublishedOwner != nil {
		return errors.New("managed configuration cannot take two conflicting owner contracts")
	}
	if expected != nil {
		if err := dnsengineartifact.ValidateFileSnapshotIntegrity(*expected); err != nil {
			return err
		}
		if expected.Path != filepath.Clean(path) ||
			(expected.Exists &&
				(expected.Mode != uint32(mode.Perm()) || !expected.OwnerKnown)) ||
			(!expected.Exists && requiredOwner == nil) {
			return errors.New("managed configuration replacement preimage is invalid")
		}
		if requiredOwner != nil && expected.Exists &&
			(expected.UID != requiredOwner.UID || expected.GID != requiredOwner.GID) {
			return errors.New("managed configuration replacement preimage owner differs from the required contract")
		}
	}
	relative, err := relativePath(path, options.PathRefused)
	if err != nil {
		return err
	}
	rootFD, err := openRoot()
	if err != nil {
		return err
	}
	defer unix.Close(rootFD)

	parent := filepath.Dir(relative)
	base := filepath.Base(relative)
	parentFD, err := unix.Openat2(rootFD, parent, &unix.OpenHow{
		Flags:   uint64(unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW),
		Resolve: resolve,
	})
	if err != nil {
		return openError("write managed configuration", path, err, options.PathRefused)
	}
	defer unix.Close(parentFD)
	parentPolicy := options.ParentValidator
	var parentBefore unix.Stat_t
	if parentPolicy != nil {
		parentBefore, err = parentPolicy(parentFD)
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
		Resolve: resolve,
	})
	if err == nil {
		if err := unix.Fstat(existingFD, &existingStat); err != nil {
			unix.Close(existingFD)
			return fmt.Errorf("stat managed configuration %s: %w", path, err)
		}
		if existingStat.Mode&unix.S_IFMT != unix.S_IFREG ||
			(expected != nil && existingStat.Nlink != 1) {
			unix.Close(existingFD)
			return pathRefusal(options.PathRefused, "write managed configuration refused for non-regular file: %s", path)
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
			if !sameStat(existingStat, afterRead) ||
				dnsengineartifact.DigestBytes(existingData) != expected.SHA256 ||
				!bytes.Equal(existingData, expected.Data) {
				return errors.New("managed configuration replacement preimage changed")
			}
		} else {
			unix.Close(existingFD)
		}
	} else if !errors.Is(err, unix.ENOENT) {
		return openError("inspect managed configuration", path, err, options.PathRefused)
	} else if expected != nil && expected.Exists {
		return errors.New("managed configuration replacement preimage disappeared")
	}
	if requiredOwner != nil {
		ownerUID = int(requiredOwner.UID)
		ownerGID = int(requiredOwner.GID)
	}
	if options.PublishedOwner != nil {
		ownerUID = int(options.PublishedOwner.UID)
		ownerGID = int(options.PublishedOwner.GID)
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
		return openError("create atomic managed configuration", path, err, options.PathRefused)
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
			Resolve: resolve,
		})
		if openErr != nil {
			return openError("reprove managed configuration", path, openErr, options.PathRefused)
		}
		var currentStat unix.Stat_t
		statErr := unix.Fstat(currentFD, &currentStat)
		closeErr := unix.Close(currentFD)
		if statErr != nil || closeErr != nil {
			return errors.Join(statErr, closeErr)
		}
		if !sameStat(existingStat, currentStat) {
			return errors.New("managed configuration changed before atomic replacement")
		}
	}
	if options.BeforeFinalParentProof != nil {
		options.BeforeFinalParentProof()
	}
	if parentPolicy != nil {
		parentAfter, err := parentPolicy(parentFD)
		if err != nil {
			return err
		}
		if !sameParent(parentBefore, parentAfter) {
			return errors.New("BIND config parent directory changed before atomic replacement")
		}
		currentParentFD, openErr := unix.Openat2(rootFD, parent, &unix.OpenHow{
			Flags:   uint64(unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW),
			Resolve: resolve,
		})
		if openErr != nil {
			return openError("reprove BIND config parent", path, openErr, options.PathRefused)
		}
		currentParent, inspectErr := parentPolicy(currentParentFD)
		closeErr := unix.Close(currentParentFD)
		if inspectErr != nil || closeErr != nil {
			return errors.Join(inspectErr, closeErr)
		}
		if !sameParent(currentParent, parentAfter) {
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
		return openError("publish atomic managed configuration", path, err, options.PathRefused)
	}
	published = true
	if err := unix.Fsync(parentFD); err != nil {
		return fmt.Errorf("sync managed configuration directory %s: %w", filepath.Dir(path), err)
	}
	return nil
}
