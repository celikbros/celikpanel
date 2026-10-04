//go:build linux

package servicemutationledger

import (
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// FileOwner is an established numeric identity, not inferred from the file being
// read. Reading never acquires authority or normalizes owner metadata.
type FileOwner struct{ UID, GID uint32 }

// ReadFile reads bounded, single-link 0600 evidence in a trusted private 0700
// directory. It refuses symlink components and observed path/metadata/content
// changes. Only a missing final name in a verified existing directory is absent;
// a missing parent is unknown. Callers still need the applicable publication and
// host leases: this cannot prevent an administrator changing state after return.
func ReadFile(path string, maxSize int64, owner FileOwner) ([]byte, bool, error) {
	return readFile(path, maxSize, owner, nil)
}

func evidenceDirectory(st unix.Stat_t, owner FileOwner) bool {
	return st.Mode&unix.S_IFMT == unix.S_IFDIR && st.Mode&07777 == 0700 && st.Uid == owner.UID && st.Gid == owner.GID
}
func evidenceFile(st unix.Stat_t, owner FileOwner, maxSize int64) bool {
	return st.Mode&unix.S_IFMT == unix.S_IFREG && st.Mode&07777 == 0600 && st.Uid == owner.UID && st.Gid == owner.GID && st.Nlink == 1 && st.Size >= 0 && st.Size <= maxSize
}
func sameFileEvidence(a, b unix.Stat_t) bool {
	return a.Dev == b.Dev && a.Ino == b.Ino && a.Mode == b.Mode && a.Uid == b.Uid && a.Gid == b.Gid && a.Nlink == b.Nlink && a.Size == b.Size && a.Mtim == b.Mtim && a.Ctim == b.Ctim
}
func openEvidenceDirectory(path string) (int, error) {
	return unix.Openat2(unix.AT_FDCWD, path, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_NOFOLLOW | unix.O_CLOEXEC, Resolve: unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS})
}
func verifyEvidenceDirectory(path string, fd int, before unix.Stat_t, owner FileOwner) error {
	currentFD, err := openEvidenceDirectory(path)
	if err != nil {
		return fmt.Errorf("reopen mutation evidence directory: %w", err)
	}
	defer unix.Close(currentFD)
	var opened, named unix.Stat_t
	if unix.Fstat(fd, &opened) != nil || unix.Fstat(currentFD, &named) != nil || !evidenceDirectory(opened, owner) || !evidenceDirectory(named, owner) || before.Dev != opened.Dev || before.Ino != opened.Ino || opened.Dev != named.Dev || opened.Ino != named.Ino {
		return errors.New("mutation evidence directory changed; preserve evidence and retry after the owner finishes the change")
	}
	return nil
}

func readFile(path string, maxSize int64, owner FileOwner, afterRead func()) ([]byte, bool, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == "/" || maxSize <= 0 || maxSize == math.MaxInt64 {
		return nil, false, errors.New("invalid mutation evidence path or size limit")
	}
	parent := filepath.Dir(path)
	name := filepath.Base(path)
	dirFD, err := openEvidenceDirectory(parent)
	if err != nil {
		return nil, false, fmt.Errorf("open mutation evidence directory: %w", err)
	}
	defer unix.Close(dirFD)
	var directory unix.Stat_t
	if err = unix.Fstat(dirFD, &directory); err != nil || !evidenceDirectory(directory, owner) {
		return nil, false, errors.New("mutation evidence requires an existing private 0700 directory with the established owner; inspect ownership before retrying")
	}
	fd, err := unix.Openat(dirFD, name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if errors.Is(err, unix.ENOENT) {
		if err = verifyEvidenceDirectory(parent, dirFD, directory, owner); err != nil {
			return nil, false, err
		}
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("open mutation evidence: %w", err)
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		unix.Close(fd)
		return nil, false, errors.New("open mutation evidence handle")
	}
	defer file.Close()
	var before unix.Stat_t
	if err = unix.Fstat(fd, &before); err != nil || !evidenceFile(before, owner, maxSize) {
		return nil, false, errors.New("mutation evidence must be a bounded single-link 0600 regular file with the established owner; preserve it for owner review")
	}
	raw, err := io.ReadAll(io.LimitReader(file, maxSize+1))
	if err != nil {
		return nil, false, fmt.Errorf("read mutation evidence: %w", err)
	}
	if int64(len(raw)) > maxSize {
		return nil, false, errors.New("mutation evidence exceeds the accepted size limit")
	}
	if afterRead != nil {
		afterRead()
	}
	var after, named unix.Stat_t
	if unix.Fstat(fd, &after) != nil || unix.Fstatat(dirFD, name, &named, unix.AT_SYMLINK_NOFOLLOW) != nil || !evidenceFile(after, owner, maxSize) || !evidenceFile(named, owner, maxSize) || !sameFileEvidence(before, after) || !sameFileEvidence(after, named) || int64(len(raw)) != after.Size {
		return nil, false, errors.New("mutation evidence changed while read; no state accepted; retry after the owner finishes the change")
	}
	if err = verifyEvidenceDirectory(parent, dirFD, directory, owner); err != nil {
		return nil, false, err
	}
	return raw, true, nil
}

// VerifyEmptyDirectory is a read-only proof for the existing first-initializer
// crash residue, not a way to accept any evidence with a different owner.
func VerifyEmptyDirectory(path string, owner FileOwner) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == "/" {
		return errors.New("invalid empty mutation directory path")
	}
	fd, err := openEvidenceDirectory(path)
	if err != nil {
		return fmt.Errorf("open empty mutation directory: %w", err)
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		unix.Close(fd)
		return errors.New("open empty mutation directory handle")
	}
	defer file.Close()
	var before, after unix.Stat_t
	if unix.Fstat(fd, &before) != nil || !evidenceDirectory(before, owner) {
		return errors.New("empty mutation directory identity is not trusted")
	}
	entries, err := file.ReadDir(1)
	if !errors.Is(err, io.EOF) || len(entries) != 0 {
		return errors.New("initial mutation directory must remain empty; preserve it for owner review")
	}
	if unix.Fstat(fd, &after) != nil || !sameFileEvidence(before, after) {
		return errors.New("empty mutation directory changed while inspected")
	}
	return verifyEvidenceDirectory(path, fd, before, owner)
}
