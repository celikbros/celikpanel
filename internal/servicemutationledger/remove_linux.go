//go:build linux

package servicemutationledger

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// RemoveFileExact retires an established evidence file only after proving its
// full 0600 preimage in the trusted 0700 directory. The caller must hold the
// publication and host locks and separately prove the terminal ledger verdict.
// Missing or changed evidence is never interpreted as completed cleanup.
func RemoveFileExact(path string, expected []byte, maxSize int64, owner FileOwner) error {
	return removeFileExact(path, expected, maxSize, owner, nil)
}

func removeFileExact(path string, expected []byte, maxSize int64, owner FileOwner, beforeUnlink func()) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == "/" ||
		maxSize <= 0 || len(expected) == 0 || int64(len(expected)) > maxSize {
		return errors.New("invalid exact evidence removal path or size")
	}
	parent, name := filepath.Dir(path), filepath.Base(path)
	dirFD, err := openEvidenceDirectory(parent)
	if err != nil {
		return fmt.Errorf("open exact evidence directory: %w", err)
	}
	defer unix.Close(dirFD)
	var directory unix.Stat_t
	if err = unix.Fstat(dirFD, &directory); err != nil || !evidenceDirectory(directory, owner) {
		return errors.New("exact evidence removal requires the established private directory")
	}
	fd, err := unix.Openat(dirFD, name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return fmt.Errorf("open exact evidence for removal: %w", err)
	}
	file := os.NewFile(uintptr(fd), path+" (removal preimage)")
	if file == nil {
		unix.Close(fd)
		return errors.New("open exact evidence removal handle")
	}
	defer file.Close()
	var before, after, named unix.Stat_t
	if err = unix.Fstat(fd, &before); err != nil || !evidenceFile(before, owner, maxSize) {
		return errors.New("exact evidence removal preimage has unsafe metadata")
	}
	raw, err := io.ReadAll(io.LimitReader(file, maxSize+1))
	if err != nil || !bytes.Equal(raw, expected) {
		return errors.New("exact evidence removal preimage differs from accepted checkpoint")
	}
	if beforeUnlink != nil {
		beforeUnlink()
	}
	if unix.Fstat(fd, &after) != nil || unix.Fstatat(dirFD, name, &named, unix.AT_SYMLINK_NOFOLLOW) != nil ||
		!sameFileEvidence(before, after) || !sameFileEvidence(after, named) ||
		!evidenceFile(named, owner, maxSize) || int64(len(raw)) != before.Size {
		return errors.New("exact evidence removal preimage changed before unlink")
	}
	if err = verifyEvidenceDirectory(parent, dirFD, directory, owner); err != nil {
		return err
	}
	if err = unix.Unlinkat(dirFD, name, 0); err != nil {
		return fmt.Errorf("unlink exact evidence: %w", err)
	}
	if err = unix.Fsync(dirFD); err != nil {
		return fmt.Errorf("sync exact evidence removal: %w", err)
	}
	_, exists, err := ReadFile(path, maxSize, owner)
	if err != nil || exists {
		return errors.Join(errors.New("exact evidence removal readback is uncertain"), err)
	}
	return nil
}
