//go:build linux

// Package certpublishlock shares the historical panel/mail publication lock.
// This lock grants exclusion only; callers still need their operation authority.
package certpublishlock

import (
	"context"
	"errors"
	"fmt"
	"golang.org/x/sys/unix"
	"time"
)

const Name = "celikpanel-panel-cert.lock"

// With preserves the existing /run lock identity used by historical Agents.
// It never normalizes an owner-modified file. Native consumers may bound waiting
// with their own context without introducing a second publication lock.
func With(ctx context.Context, action func() error) error {
	if ctx == nil || action == nil {
		return errors.New("certificate publication context and action are required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	parent, err := unix.Open("/run", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("open certificate publication lock directory: %w", err)
	}
	defer unix.Close(parent)
	return withAt(ctx, parent, func() error {
		var pinned, named unix.Stat_t
		if unix.Fstat(parent, &pinned) != nil || pinned.Uid != 0 || pinned.Mode&0o022 != 0 || unix.Lstat("/run", &named) != nil || !sameObject(pinned, named) {
			return errors.New("certificate publication lock directory changed")
		}
		return action()
	})
}

func sameObject(a, b unix.Stat_t) bool {
	return a.Dev == b.Dev && a.Ino == b.Ino && a.Mode == b.Mode && a.Uid == b.Uid && a.Gid == b.Gid && a.Nlink == b.Nlink
}

func trusted(st unix.Stat_t) bool {
	// Historical Agent creation may use its primary celikpanel group; group gets
	// no access. Do not invent a root-group migration for the existing lock.
	return st.Mode&unix.S_IFMT == unix.S_IFREG && st.Uid == 0 && st.Nlink == 1 && st.Mode&0o7777 == 0o600
}

func withAt(ctx context.Context, parent int, action func() error) error {
	if ctx == nil || action == nil {
		return errors.New("certificate publication context and action are required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	var dir unix.Stat_t
	if err := unix.Fstat(parent, &dir); err != nil || dir.Mode&unix.S_IFMT != unix.S_IFDIR || dir.Uid != 0 || dir.Mode&0o022 != 0 {
		return errors.New("certificate publication lock directory is not trusted")
	}
	fd, err := unix.Openat(parent, Name, unix.O_RDWR|unix.O_CREAT|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0o600)
	if err != nil {
		return fmt.Errorf("open certificate publication lock: %w", err)
	}
	defer unix.Close(fd)
	return withDescriptor(ctx, parent, fd, action)
}

func withDescriptor(ctx context.Context, parent, fd int, action func() error) error {
	var before unix.Stat_t
	if err := unix.Fstat(fd, &before); err != nil || !trusted(before) {
		return errors.New("certificate publication lock is not trusted; the server owner must inspect /run/celikpanel-panel-cert.lock before retrying")
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EINTR) {
			return fmt.Errorf("lock certificate publication: %w", err)
		}
		timer := time.NewTimer(25 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	defer unix.Flock(fd, unix.LOCK_UN)
	if err := ctx.Err(); err != nil {
		return err
	}
	var current, named unix.Stat_t
	if unix.Fstat(fd, &current) != nil || unix.Fstatat(parent, Name, &named, unix.AT_SYMLINK_NOFOLLOW) != nil ||
		!trusted(current) || !trusted(named) || !sameObject(before, current) || !sameObject(current, named) {
		return errors.New("certificate publication lock changed while waiting; no publication started; retry after the owner finishes the change")
	}
	return action()
}
