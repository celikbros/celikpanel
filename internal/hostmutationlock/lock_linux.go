//go:build linux

// Package hostmutationlock observes and holds the historical host mutation flock.
// Exclusion is not operation authority, ledger readiness or service health.
package hostmutationlock

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

var ErrBusy = errors.New("the host mutation lock is busy")

// Owner is the already established numeric lock identity. A native consumer must
// retain this identity through an explicit enrollment, not infer permission from
// a missing management account or an arbitrary existing file. These APIs never
// create or normalize directories, files, ownership or permissions.
type Owner struct{ UID, GID uint32 }

// AcquireExisting returns the same nonblocking exclusive flock used by Agent and
// update/recovery executors. The caller retains it until publication/recovery is
// safe. Closing the returned file releases it. Missing evidence is an error.
func AcquireExisting(path string, owner Owner) (*os.File, error) {
	return acquire(path, owner, false, nil)
}

// ProbeIdle only observes exclusion. A missing file in a trusted existing parent
// is idle, preserving the historical pre-initialization probe. This result never
// grants permission to mutate; a writer must acquire its own lease afterwards.
func ProbeIdle(path string, owner Owner) error {
	file, err := acquire(path, owner, true, nil)
	if err != nil || file == nil {
		return err
	}
	return file.Close()
}

func trustedDirectory(st unix.Stat_t, owner Owner) bool {
	return st.Mode&unix.S_IFMT == unix.S_IFDIR && st.Uid == owner.UID && st.Gid == owner.GID && st.Mode&0022 == 0
}
func trustedFile(st unix.Stat_t, owner Owner) bool {
	return st.Mode&unix.S_IFMT == unix.S_IFREG && st.Uid == owner.UID && st.Gid == owner.GID && st.Mode&07777 == 0600 && st.Nlink == 1 && st.Size == 0
}
func sameIdentity(a, b unix.Stat_t) bool {
	return a.Dev == b.Dev && a.Ino == b.Ino && a.Mode == b.Mode && a.Uid == b.Uid && a.Gid == b.Gid && a.Nlink == b.Nlink
}

type location struct {
	path      string
	parent    int
	directory unix.Stat_t
	owner     Owner
}

func openLocation(path string, owner Owner) (*location, error) {
	path = filepath.Clean(path)
	if !filepath.IsAbs(path) || path == "/" {
		return nil, errors.New("host mutation lock path must name an absolute file")
	}
	// Refuse symlink components as well as the final component. No syscall fallback
	// silently weakens this contract on an unsupported kernel.
	fd, err := unix.Openat2(unix.AT_FDCWD, filepath.Dir(path), &unix.OpenHow{
		Flags:   unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW,
		Resolve: unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS,
	})
	if err != nil {
		return nil, fmt.Errorf("open host mutation lock directory: %w", err)
	}
	loc := &location{path: path, parent: fd, owner: owner}
	if err = unix.Fstat(fd, &loc.directory); err != nil || !trustedDirectory(loc.directory, owner) {
		unix.Close(fd)
		return nil, errors.New("host mutation lock directory has unsafe ownership or permissions; the server owner must inspect it before retrying")
	}
	return loc, nil
}
func (loc *location) verifyDirectory() error {
	var current, named unix.Stat_t
	if unix.Fstat(loc.parent, &current) != nil || unix.Lstat(filepath.Dir(loc.path), &named) != nil ||
		!trustedDirectory(current, loc.owner) || !trustedDirectory(named, loc.owner) || !sameIdentity(loc.directory, current) || !sameIdentity(current, named) {
		return errors.New("host mutation lock directory changed; no operation admitted; retry after the owner finishes the change")
	}
	return nil
}
func (loc *location) verifyFile(fd int, before *unix.Stat_t) (unix.Stat_t, error) {
	var current, named unix.Stat_t
	if err := loc.verifyDirectory(); err != nil {
		return current, err
	}
	if unix.Fstat(fd, &current) != nil || unix.Fstatat(loc.parent, filepath.Base(loc.path), &named, unix.AT_SYMLINK_NOFOLLOW) != nil ||
		!trustedFile(current, loc.owner) || !trustedFile(named, loc.owner) || !sameIdentity(current, named) ||
		(before != nil && !sameIdentity(*before, current)) {
		return current, errors.New("host mutation lock changed or is not an empty single-link 0600 file with the established owner; inspect it before retrying")
	}
	return current, nil
}
func (loc *location) openFile() (int, error) {
	// O_NONBLOCK is essential even on an observation: a replaced FIFO must never
	// wait for a writer before the regular-file check can reject it.
	return unix.Openat(loc.parent, filepath.Base(loc.path), unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
}
func acquire(path string, owner Owner, missingIdle bool, afterOpen func()) (*os.File, error) {
	loc, err := openLocation(path, owner)
	if err != nil {
		return nil, err
	}
	defer unix.Close(loc.parent)
	fd, err := loc.openFile()
	if err != nil {
		if errors.Is(err, unix.ENOENT) && missingIdle {
			return nil, loc.verifyDirectory()
		}
		return nil, fmt.Errorf("open existing host mutation lock: %w", err)
	}
	keep := false
	defer func() {
		if !keep {
			unix.Close(fd)
		}
	}()
	before, err := loc.verifyFile(fd, nil)
	if err != nil {
		return nil, err
	}
	if afterOpen != nil {
		afterOpen()
	}
	if err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return nil, ErrBusy
		}
		return nil, fmt.Errorf("acquire host mutation flock: %w", err)
	}
	if _, err = loc.verifyFile(fd, &before); err != nil {
		return nil, err
	}
	keep = true
	return os.NewFile(uintptr(fd), loc.path), nil
}

// VerifyInherited requires an already-held exclusive flock on this exact open
// file description, not merely a descriptor for a file another process holds.
// It neither acquires an unlocked descriptor nor releases the caller's lease.
func VerifyInherited(path string, fd int, owner Owner) error {
	if fd < 3 {
		return errors.New("host mutation lock must be an inherited descriptor")
	}
	loc, err := openLocation(path, owner)
	if err != nil {
		return err
	}
	defer unix.Close(loc.parent)
	duplicate, err := unix.FcntlInt(uintptr(fd), unix.F_DUPFD_CLOEXEC, 3)
	if err != nil {
		return fmt.Errorf("duplicate inherited host mutation lock: %w", err)
	}
	defer unix.Close(duplicate)
	before, err := loc.verifyFile(duplicate, nil)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile("/proc/self/fdinfo/" + strconv.Itoa(duplicate))
	if err != nil {
		return fmt.Errorf("inspect inherited flock ownership: %w", err)
	}
	if !hasExclusiveFlock(raw) {
		return errors.New("inherited host mutation descriptor does not already own the exclusive flock")
	}
	probe, err := loc.openFile()
	if err != nil {
		return fmt.Errorf("open independent host mutation lock proof: %w", err)
	}
	defer unix.Close(probe)
	if _, err = loc.verifyFile(probe, &before); err != nil {
		return err
	}
	if err = unix.Flock(probe, unix.LOCK_EX|unix.LOCK_NB); err == nil {
		unix.Flock(probe, unix.LOCK_UN)
		return errors.New("inherited host mutation descriptor does not exclude an independent opener")
	} else if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
		return fmt.Errorf("prove inherited flock contention: %w", err)
	}
	_, err = loc.verifyFile(duplicate, &before)
	return err
}
func hasExclusiveFlock(raw []byte) bool {
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 9 && fields[0] == "lock:" && fields[2] == "FLOCK" && fields[3] == "ADVISORY" && fields[4] == "WRITE" && fields[len(fields)-2] == "0" && fields[len(fields)-1] == "EOF" {
			return true
		}
	}
	return false
}
