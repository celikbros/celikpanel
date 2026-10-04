//go:build linux

package servicemutationledger

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// ArchiveFileExact publishes a durable, immutable-by-contract copy of an
// accepted evidence file before retiring its exact active preimage. The caller
// must hold the host/publication locks and prove the terminal operation. An
// existing archive is acceptable only if its secured read matches byte-for-byte.
// Neither a missing active file nor a matching archive proves terminal success.
func ArchiveFileExact(activePath, archivePath string, expected []byte, maxSize int64, owner FileOwner) error {
	return archiveFileExactWithHooks(activePath, archivePath, expected, maxSize, owner, archiveFaultHooks{})
}

type archiveFaultHooks struct {
	AfterPartialWrite func() error
	AfterLink         func() error
	AfterArchive      func() error
	AfterRetire       func() error
}

func archiveFileExactWithHooks(activePath, archivePath string, expected []byte, maxSize int64, owner FileOwner, hooks archiveFaultHooks) error {
	if !filepath.IsAbs(activePath) || filepath.Clean(activePath) != activePath ||
		!filepath.IsAbs(archivePath) || filepath.Clean(archivePath) != archivePath ||
		filepath.Dir(activePath) != filepath.Dir(archivePath) || activePath == archivePath ||
		len(expected) == 0 || int64(len(expected)) > maxSize {
		return errors.New("invalid exact evidence archive paths or size")
	}
	active, exists, err := ReadFile(activePath, maxSize, owner)
	if err != nil || !exists || !bytes.Equal(active, expected) {
		return errors.Join(errors.New("active evidence differs from archive preimage"), err)
	}
	if err := createArchiveFileExact(archivePath, expected, maxSize, owner, hooks); err != nil {
		return err
	}
	archived, archivedExists, err := ReadFile(archivePath, maxSize, owner)
	if err != nil || !archivedExists || !bytes.Equal(archived, expected) {
		return errors.Join(errors.New("durable archive readback differs"), err)
	}
	if hooks.AfterArchive != nil {
		if err := hooks.AfterArchive(); err != nil {
			return err
		}
	}
	if err := RemoveFileExact(activePath, expected, maxSize, owner); err != nil {
		return err
	}
	if hooks.AfterRetire != nil {
		if err := hooks.AfterRetire(); err != nil {
			return err
		}
	}
	archived, archivedExists, err = ReadFile(archivePath, maxSize, owner)
	if err != nil || !archivedExists || !bytes.Equal(archived, expected) {
		return errors.Join(errors.New("archive changed after active retirement"), err)
	}
	return nil
}

func createArchiveFileExact(path string, expected []byte, maxSize int64, owner FileOwner, hooks archiveFaultHooks) error {
	parent, name := filepath.Dir(path), filepath.Base(path)
	dirFD, err := openEvidenceDirectory(parent)
	if err != nil {
		return fmt.Errorf("open archive directory: %w", err)
	}
	defer unix.Close(dirFD)
	var directory unix.Stat_t
	if err = unix.Fstat(dirFD, &directory); err != nil || !evidenceDirectory(directory, owner) {
		return errors.New("archive requires established private evidence directory")
	}
	archived, exists, err := ReadFile(path, maxSize, owner)
	if err != nil {
		return err
	}
	if exists {
		if !bytes.Equal(archived, expected) {
			return errors.New("existing archive differs")
		}
		if err := verifyEvidenceDirectory(parent, dirFD, directory, owner); err != nil {
			return err
		}
		return unix.Fsync(dirFD)
	}
	// An unnamed inode cannot expose a partial archive after a write crash.
	// Filesystems without O_TMPFILE fail closed.
	fd, err := unix.Openat(dirFD, ".", unix.O_WRONLY|unix.O_TMPFILE|unix.O_CLOEXEC, 0600)
	if err != nil {
		return fmt.Errorf("create unnamed archive: %w", err)
	}
	file := os.NewFile(uintptr(fd), path+" (unpublished archive)")
	if file == nil {
		unix.Close(fd)
		return errors.New("archive descriptor unavailable")
	}
	defer file.Close()
	if err := unix.Fchown(fd, int(owner.UID), int(owner.GID)); err != nil {
		return fmt.Errorf("set archive owner: %w", err)
	}
	if err := unix.Fchmod(fd, 0600); err != nil {
		return fmt.Errorf("set archive mode: %w", err)
	}
	if hooks.AfterPartialWrite != nil {
		if _, err := file.Write(expected[:len(expected)/2]); err != nil {
			return fmt.Errorf("write partial archive: %w", err)
		}
		if err := hooks.AfterPartialWrite(); err != nil {
			return err
		}
		if _, err := file.Write(expected[len(expected)/2:]); err != nil {
			return fmt.Errorf("finish archive: %w", err)
		}
	} else if _, err := file.Write(expected); err != nil {
		return fmt.Errorf("write archive: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync archive: %w", err)
	}
	if err := verifyEvidenceDirectory(parent, dirFD, directory, owner); err != nil {
		return err
	}
	if err := unix.Linkat(fd, "", dirFD, name, unix.AT_EMPTY_PATH); errors.Is(err, unix.EEXIST) {
		archived, exists, readErr := ReadFile(path, maxSize, owner)
		if readErr != nil || !exists || !bytes.Equal(archived, expected) {
			return errors.Join(errors.New("existing archive differs or is unsafe"), readErr)
		}
		if err := verifyEvidenceDirectory(parent, dirFD, directory, owner); err != nil {
			return err
		}
		return unix.Fsync(dirFD)
	} else if err != nil {
		return fmt.Errorf("publish exclusive archive: %w", err)
	}
	if hooks.AfterLink != nil {
		if err := hooks.AfterLink(); err != nil {
			return err
		}
	}
	if err := unix.Fsync(dirFD); err != nil {
		return fmt.Errorf("sync archive directory: %w", err)
	}
	return nil
}
