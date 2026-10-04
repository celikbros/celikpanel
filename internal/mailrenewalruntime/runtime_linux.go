//go:build linux

// Package mailrenewalruntime publishes only the existing shared volatile runtime
// directory. It never repairs an existing directory or initializes durable state.
package mailrenewalruntime

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"golang.org/x/sys/unix"
	"os"
)

const directory = "celikpanel"

func Ensure(gid uint32) error {
	parent, err := unix.Openat2(unix.AT_FDCWD, "/run", &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC, Resolve: unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS})
	if err != nil {
		return err
	}
	defer unix.Close(parent)
	return ensureAt(parent, 0, gid, nil)
}

func identity(a, b unix.Stat_t) bool {
	return a.Dev == b.Dev && a.Ino == b.Ino && a.Uid == b.Uid && a.Gid == b.Gid && a.Mode == b.Mode
}
func existingAt(parent int, uid, gid uint32) (bool, error) {
	var named unix.Stat_t
	if err := unix.Fstatat(parent, directory, &named, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		if errors.Is(err, unix.ENOENT) {
			return false, nil
		}
		return false, err
	}
	if named.Mode&unix.S_IFMT != unix.S_IFDIR || named.Mode&07777 != 0750 || named.Uid != uid || named.Gid != gid {
		return true, errors.New("existing mail renewal runtime has different type, owner or permissions; preserve it for owner review")
	}
	return true, nil
}

func ensureAt(parent int, uid, gid uint32, beforePublish func(string) error) error {
	return ensureRuntimeAt(parent, uid, gid, beforePublish, false)
}

func ensureRuntimeAt(parent int, uid, gid uint32, beforePublish func(string) error, enrollmentLocks bool) (result error) {
	var root unix.Stat_t
	if err := unix.Fstat(parent, &root); err != nil || root.Mode&unix.S_IFMT != unix.S_IFDIR || root.Uid != uid || root.Mode&0022 != 0 {
		return errors.New("mail renewal runtime parent is not trusted")
	}
	if found, err := existingAt(parent, uid, gid); found || err != nil {
		if err == nil && enrollmentLocks {
			return verifyRuntimeLocksAt(parent, uid, gid)
		}
		return err
	}
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return err
	}
	stage := ".celikpanel-mail-runtime-" + hex.EncodeToString(random)
	if err := unix.Mkdirat(parent, stage, 0700); err != nil {
		return err
	}
	fd, err := unix.Openat(parent, stage, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(fd), stage)
	defer f.Close()
	published := false
	cleanupAllowed := false
	var cleanupIdentity unix.Stat_t
	defer func() {
		if published || !cleanupAllowed {
			return
		}
		var held, named unix.Stat_t
		// Remove only this process's still-identical empty stage. Owner additions,
		// replacements or uncertain metadata are retained, never recursively erased.
		if unix.Fstat(fd, &held) == nil && unix.Fstatat(parent, stage, &named, unix.AT_SYMLINK_NOFOLLOW) == nil && identity(held, named) && identity(held, cleanupIdentity) {
			_ = unix.Unlinkat(parent, stage, unix.AT_REMOVEDIR)
		}
	}()
	var initial unix.Stat_t
	if err = unix.Fstat(fd, &initial); err != nil || initial.Uid != uid || initial.Mode&unix.S_IFMT != unix.S_IFDIR {
		return errors.New("new runtime stage identity differs")
	}
	if err = unix.Fchown(fd, int(uid), int(gid)); err != nil {
		return err
	}
	if err = unix.Fchmod(fd, 0750); err != nil {
		return err
	}
	if err = unix.Fsync(fd); err != nil {
		return err
	}
	if err = unix.Fstat(fd, &cleanupIdentity); err != nil {
		return err
	}
	cleanupAllowed = true
	if enrollmentLocks {
		if err = createRuntimeLocksAt(fd, uid, gid); err != nil {
			return err
		}
	}
	if beforePublish != nil {
		if err = beforePublish(stage); err != nil {
			return err
		}
	}
	var held, named unix.Stat_t
	if err = unix.Fstat(fd, &held); err != nil {
		return err
	}
	if err = unix.Fstatat(parent, stage, &named, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return err
	}
	if !identity(held, named) || held.Uid != uid || held.Gid != gid || held.Mode&07777 != 0750 {
		return errors.New("runtime stage changed before publication")
	}
	entries, err := f.ReadDir(-1)
	if err != nil {
		return err
	}
	expectedEntries := 0
	if enrollmentLocks {
		expectedEntries = 2
		if err = verifyLockFilesAt(fd, uid, gid); err != nil {
			return err
		}
	}
	if len(entries) != expectedEntries {
		return errors.New("runtime stage contains owner files; preserve it for review")
	}
	if err = unix.Renameat2(parent, stage, parent, directory, unix.RENAME_NOREPLACE); err != nil {
		if errors.Is(err, unix.EEXIST) {
			_, err = existingAt(parent, uid, gid)
			if err == nil && enrollmentLocks {
				return verifyRuntimeLocksAt(parent, uid, gid)
			}
			return err
		}
		return fmt.Errorf("publish independent mail runtime: %w", err)
	}
	published = true
	if err = unix.Fsync(parent); err != nil {
		return err
	}
	var selected unix.Stat_t
	if err = unix.Fstatat(parent, directory, &selected, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return err
	}
	if !identity(held, selected) {
		return errors.New("runtime changed after publication")
	}
	return nil
}
