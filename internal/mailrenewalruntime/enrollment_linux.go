//go:build linux

package mailrenewalruntime

import (
	"errors"
	"golang.org/x/sys/unix"
)

var enrollmentLockNames = [...]string{"service-mutation.lock", "service-mutation.lock.ledger-publication"}

// RestoreEnrollmentLocks publishes the absent volatile directory and both lock
// identities atomically. The caller holds the release lock and proves the exact
// recorded enrollment before and after publication. This does not initialize
// durable state, repair an existing directory, or replace an existing lock.
func RestoreEnrollmentLocks(gid uint32, verify func() error) error {
	if verify == nil {
		return errors.New("recorded enrollment proof is required")
	}
	if err := verify(); err != nil {
		return err
	}
	parent, err := unix.Openat2(unix.AT_FDCWD, "/run", &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC, Resolve: unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS})
	if err != nil {
		return err
	}
	defer unix.Close(parent)
	if err = ensureRuntimeAt(parent, 0, gid, func(string) error { return verify() }, true); err != nil {
		return err
	}
	return verify()
}

func createRuntimeLocksAt(fd int, uid, gid uint32) error {
	for _, name := range enrollmentLockNames {
		child, err := unix.Openat(fd, name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
		if err != nil {
			return err
		}
		err = unix.Fchown(child, int(uid), int(gid))
		if err == nil {
			err = unix.Fchmod(child, 0600)
		}
		if err == nil {
			err = unix.Fsync(child)
		}
		closeErr := unix.Close(child)
		if err != nil || closeErr != nil {
			return errors.Join(err, closeErr)
		}
	}
	return unix.Fsync(fd)
}
func verifyLockFilesAt(fd int, uid, gid uint32) error {
	for _, name := range enrollmentLockNames {
		var s unix.Stat_t
		if unix.Fstatat(fd, name, &s, unix.AT_SYMLINK_NOFOLLOW) != nil || s.Mode != unix.S_IFREG|0600 || s.Uid != uid || s.Gid != gid || s.Nlink != 1 || s.Size != 0 {
			return errors.New("existing enrollment lock identity differs; preserve it for owner review")
		}
	}
	return nil
}
func verifyRuntimeLocksAt(parent int, uid, gid uint32) error {
	fd, err := unix.Openat(parent, directory, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	var before, named unix.Stat_t
	if unix.Fstat(fd, &before) != nil || before.Mode != unix.S_IFDIR|0750 || before.Uid != uid || before.Gid != gid {
		return errors.New("enrollment runtime identity differs")
	}
	if err = verifyLockFilesAt(fd, uid, gid); err != nil {
		return err
	}
	if unix.Fstatat(parent, directory, &named, unix.AT_SYMLINK_NOFOLLOW) != nil || !identity(before, named) {
		return errors.New("enrollment runtime changed")
	}
	return nil
}
