//go:build linux

package servicemutationledger

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// ReplaceFileExact publishes one bounded evidence checkpoint only when the
// established 0600 preimage still occupies the trusted private directory.
// The caller must hold the host and publication locks; this function does not
// grant authority or serialize an administrator's uncooperative root edits.
// Missing, unsafe or changed evidence is never created or normalized.
func ReplaceFileExact(path string, before, after []byte, maxSize int64, owner FileOwner) error {
	return replaceFileExact(path, before, after, maxSize, owner, nil)
}

func replaceFileExact(path string, before, after []byte, maxSize int64, owner FileOwner, beforePublish func()) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == "/" ||
		maxSize <= 0 || int64(len(before)) > maxSize || int64(len(after)) > maxSize || len(after) == 0 {
		return errors.New("invalid exact evidence replacement path or size")
	}
	parent, name := filepath.Dir(path), filepath.Base(path)
	dirFD, err := openEvidenceDirectory(parent)
	if err != nil {
		return fmt.Errorf("open exact evidence directory: %w", err)
	}
	defer unix.Close(dirFD)
	var directory unix.Stat_t
	if err = unix.Fstat(dirFD, &directory); err != nil || !evidenceDirectory(directory, owner) {
		return errors.New("exact evidence replacement requires the established private directory")
	}
	oldFD, err := unix.Openat(dirFD, name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return fmt.Errorf("open exact evidence preimage: %w", err)
	}
	var oldStat unix.Stat_t
	oldFile := os.NewFile(uintptr(oldFD), path+" (preimage)")
	if oldFile == nil {
		unix.Close(oldFD)
		return errors.New("open exact evidence preimage handle")
	}
	defer oldFile.Close()
	if err = unix.Fstat(oldFD, &oldStat); err != nil || !evidenceFile(oldStat, owner, maxSize) {
		return errors.New("exact evidence preimage has unsafe ownership or metadata")
	}
	observed, err := io.ReadAll(io.LimitReader(oldFile, maxSize+1))
	if err != nil || !bytes.Equal(observed, before) {
		return errors.New("exact evidence preimage differs from the accepted checkpoint")
	}
	var readStat unix.Stat_t
	if err = unix.Fstat(oldFD, &readStat); err != nil || !sameFileEvidence(oldStat, readStat) || int64(len(observed)) != oldStat.Size {
		return errors.New("exact evidence preimage changed while read")
	}
	var tempName string
	var tempFD int
	for attempt := 0; attempt < 16; attempt++ {
		random := make([]byte, 12)
		if _, err = rand.Read(random); err != nil {
			return fmt.Errorf("create exact evidence checkpoint name: %w", err)
		}
		tempName = "." + name + ".celikpanel-" + hex.EncodeToString(random)
		tempFD, err = unix.Openat(dirFD, tempName, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
		if !errors.Is(err, unix.EEXIST) {
			break
		}
	}
	if err != nil {
		return fmt.Errorf("create exact evidence checkpoint: %w", err)
	}
	defer unix.Unlinkat(dirFD, tempName, 0)
	file := os.NewFile(uintptr(tempFD), path+" (checkpoint)")
	if file == nil {
		unix.Close(tempFD)
		return errors.New("open exact evidence checkpoint handle")
	}
	closed := false
	defer func() {
		if !closed {
			_ = file.Close()
		}
	}()
	if err = unix.Fchown(tempFD, int(owner.UID), int(owner.GID)); err != nil {
		return fmt.Errorf("set exact evidence checkpoint owner: %w", err)
	}
	if err = unix.Fchmod(tempFD, 0600); err != nil {
		return fmt.Errorf("set exact evidence checkpoint mode: %w", err)
	}
	if _, err = file.Write(after); err != nil {
		return fmt.Errorf("write exact evidence checkpoint: %w", err)
	}
	if err = file.Sync(); err != nil {
		return fmt.Errorf("sync exact evidence checkpoint: %w", err)
	}
	var tempStat unix.Stat_t
	if err = unix.Fstat(tempFD, &tempStat); err != nil || !evidenceFile(tempStat, owner, maxSize) || tempStat.Size != int64(len(after)) {
		return errors.New("exact evidence checkpoint metadata differs from contract")
	}
	if err = file.Close(); err != nil {
		return fmt.Errorf("close exact evidence checkpoint: %w", err)
	}
	closed = true
	if beforePublish != nil {
		beforePublish()
	}
	var current unix.Stat_t
	if err = unix.Fstatat(dirFD, name, &current, unix.AT_SYMLINK_NOFOLLOW); err != nil ||
		!sameFileEvidence(oldStat, current) || !evidenceFile(current, owner, maxSize) {
		return errors.New("exact evidence preimage changed before publication")
	}
	if err = verifyEvidenceDirectory(parent, dirFD, directory, owner); err != nil {
		return err
	}
	if err = unix.Renameat(dirFD, tempName, dirFD, name); err != nil {
		return fmt.Errorf("publish exact evidence checkpoint: %w", err)
	}
	if err = unix.Fsync(dirFD); err != nil {
		return fmt.Errorf("sync exact evidence directory: %w", err)
	}
	verified, exists, err := ReadFile(path, maxSize, owner)
	if err != nil || !exists || !bytes.Equal(verified, after) {
		return errors.Join(errors.New("exact evidence checkpoint readback is uncertain"), err)
	}
	return nil
}
