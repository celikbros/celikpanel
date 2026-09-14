//go:build linux

package recoveryobs

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

func panelGID() (uint32, error) {
	g, err := user.LookupGroup("celikpanel")
	if err != nil {
		return 0, ErrUnavailable
	}
	v, err := strconv.ParseUint(g.Gid, 10, 32)
	if err != nil || v == 0 {
		return 0, ErrUnavailable
	}
	return uint32(v), nil
}

func Read(id string) Status {
	if !ValidRequestID(id) {
		return unavailable(id)
	}
	gid, err := panelGID()
	if err != nil {
		return unavailable(id)
	}
	return readAt(Root, id, 0, gid, "/")
}

func readAt(root, id string, uid, gid uint32, anchor string) Status {
	if !ValidRequestID(id) {
		return unavailable(id)
	}
	fd, err := openRoot(root, uid, gid, anchor, false)
	if err != nil {
		return unavailable(id)
	}
	defer unix.Close(fd)
	r, err := readRecord(fd, id, uid, gid)
	if err != nil {
		return unavailable(id)
	}
	return r.Status()
}

// Every directory component is opened relative to the verified preceding FD.
// Only tests inject an anchor; production always starts at the filesystem root.
func openRoot(root string, uid, gid uint32, anchor string, create bool) (int, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || filepath.Clean(anchor) != anchor {
		return -1, ErrUnavailable
	}
	rel, err := filepath.Rel(anchor, root)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, "../") {
		return -1, ErrUnavailable
	}
	fd, err := unix.Open(anchor, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, ErrUnavailable
	}
	parts := strings.Split(rel, string(filepath.Separator))
	for i, part := range parts {
		var st unix.Stat_t
		if unix.Fstat(fd, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFDIR || st.Uid != uid || st.Mode&0o022 != 0 {
			unix.Close(fd)
			return -1, ErrUnavailable
		}
		created := false
		if create && i == len(parts)-1 {
			err = unix.Mkdirat(fd, part, 0o750)
			created = err == nil
			if err != nil && !errors.Is(err, unix.EEXIST) {
				unix.Close(fd)
				return -1, ErrUnavailable
			}
		}
		next, err := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if err != nil {
			unix.Close(fd)
			return -1, ErrUnavailable
		}
		if created {
			if unix.Fchown(next, int(uid), int(gid)) != nil || unix.Fchmod(next, 0o750) != nil || unix.Fsync(next) != nil || unix.Fsync(fd) != nil {
				unix.Close(next)
				unix.Close(fd)
				return -1, ErrUnavailable
			}
		}
		unix.Close(fd)
		fd = next
	}
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || st.Uid != uid || st.Gid != gid || st.Mode&unix.S_IFMT != unix.S_IFDIR || st.Mode&0o7777 != 0o750 {
		unix.Close(fd)
		return -1, ErrUnavailable
	}
	return fd, nil
}

func readRecord(rootFD int, id string, uid, gid uint32) (Record, error) {
	fd, err := unix.Openat(rootFD, id+".status", unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return Record{}, err
	}
	f := os.NewFile(uintptr(fd), "recovery-observation")
	defer f.Close()
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Mode&0o7777 != 0o640 || st.Uid != uid || st.Gid != gid || st.Nlink != 1 || st.Size <= 0 || st.Size > MaxRecordSize {
		return Record{}, ErrUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(f, MaxRecordSize+1))
	if err != nil {
		return Record{}, ErrUnavailable
	}
	var after unix.Stat_t
	if unix.Fstat(fd, &after) != nil || st.Size != after.Size || st.Mtim != after.Mtim || st.Ctim != after.Ctim || st.Nlink != after.Nlink {
		return Record{}, ErrUnavailable
	}
	return Decode(raw, id)
}

// Publish is best-effort observation only. Callers retain their original
// mutation result if publication fails; they must never substitute this error.
func Publish(r Record) error {
	if !ValidRequestID(r.RequestID) {
		return ErrUnavailable
	}
	if os.Geteuid() != 0 {
		return ErrUnavailable
	}
	gid, err := panelGID()
	if err != nil {
		return err
	}
	return publishAt(Root, r, 0, gid, "/")
}

func publishAt(root string, r Record, uid, gid uint32, anchor string) error {
	if !ValidRequestID(r.RequestID) {
		return ErrUnavailable
	}
	fd, err := openRoot(root, uid, gid, anchor, true)
	if err != nil {
		return ErrUnavailable
	}
	defer unix.Close(fd)
	// The Agent runs with root UID and the panel's GID; the native recovery
	// process uses root:root. Both producers share a root:root0600 lock.
	// Nonzero UID is used only by the private unprivileged test fixture.
	lockGID := uint32(0)
	if uid != 0 {
		lockGID = gid
	}
	lockFD, err := unix.Openat(fd, ".publish.lock", unix.O_RDWR|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	createdLock := err == nil
	if errors.Is(err, unix.EEXIST) {
		lockFD, err = unix.Openat(fd, ".publish.lock", unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	}
	if err != nil {
		return ErrUnavailable
	}
	defer unix.Close(lockFD)
	if createdLock {
		if unix.Fchown(lockFD, int(uid), int(lockGID)) != nil || unix.Fchmod(lockFD, 0o600) != nil || unix.Fsync(lockFD) != nil || unix.Fsync(fd) != nil {
			return ErrUnavailable
		}
	}
	var lockStat unix.Stat_t
	if unix.Fstat(lockFD, &lockStat) != nil || lockStat.Mode&unix.S_IFMT != unix.S_IFREG || lockStat.Mode&0o7777 != 0o600 || lockStat.Uid != uid || lockStat.Gid != lockGID || lockStat.Nlink != 1 || lockStat.Size != 0 || unix.Flock(lockFD, unix.LOCK_EX|unix.LOCK_NB) != nil {
		return ErrUnavailable
	}
	var previous *Record
	old, readErr := readRecord(fd, r.RequestID, uid, gid)
	if readErr == nil {
		previous = &old
	} else if !errors.Is(readErr, unix.ENOENT) {
		return ErrUnavailable
	}
	r, err = merge(previous, r)
	if err != nil {
		return err
	}
	raw, err := r.Encode()
	if err != nil {
		return err
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return ErrUnavailable
	}
	stage := ".observation-" + hex.EncodeToString(nonce[:])
	stageFD, err := unix.Openat(fd, stage, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return ErrUnavailable
	}
	f := os.NewFile(uintptr(stageFD), "recovery-observation-stage")
	defer func() { f.Close(); unix.Unlinkat(fd, stage, 0) }()
	if f.Chown(int(uid), int(gid)) != nil || f.Chmod(0o640) != nil {
		return ErrUnavailable
	}
	if _, err := f.Write(raw); err != nil {
		return ErrUnavailable
	}
	if f.Sync() != nil || f.Close() != nil {
		return ErrUnavailable
	}
	if unix.Renameat(fd, stage, fd, r.RequestID+".status") != nil || unix.Fsync(fd) != nil {
		return ErrUnavailable
	}
	return nil
}
