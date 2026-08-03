//go:build linux

package main

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

const secureMaildirResolve = unix.RESOLVE_BENEATH |
	unix.RESOLVE_NO_SYMLINKS |
	unix.RESOLVE_NO_MAGICLINKS

func ensureMailboxDirectory(domain, local string) (func() error, error) {
	if os.Getenv("CELIKPANEL_MAIL_DIR") != "" {
		return nil, nil
	}
	uid, gid := 5000, 5000
	rootFD, err := unix.Open(mailRootDir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fmt.Errorf("open mail root: %w", err)
	}
	defer unix.Close(rootFD)

	domainCreated, err := mkdirMaildirAt(rootFD, domain, uid, gid)
	if err != nil {
		return nil, err
	}
	domainFD, err := unix.Openat2(rootFD, domain, &unix.OpenHow{
		Flags:   uint64(unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW),
		Resolve: secureMaildirResolve,
	})
	if err != nil {
		if domainCreated {
			_ = unix.Unlinkat(rootFD, domain, unix.AT_REMOVEDIR)
		}
		return nil, fmt.Errorf("open mailbox domain directory: %w", err)
	}
	defer unix.Close(domainFD)

	mailboxCreated, err := mkdirMaildirAt(domainFD, local, uid, gid)
	if err != nil {
		if domainCreated {
			_ = unix.Unlinkat(rootFD, domain, unix.AT_REMOVEDIR)
		}
		return nil, err
	}
	rollback := func() error {
		return rollbackMailboxDirectory(domain, local, domainCreated, mailboxCreated)
	}
	return rollback, nil
}

func rollbackMailboxDirectory(domain, local string, domainCreated, mailboxCreated bool) error {
	rootFD, err := unix.Open(mailRootDir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("reopen mail root for rollback: %w", err)
	}
	defer unix.Close(rootFD)

	var rollbackErr error
	if mailboxCreated {
		domainFD, openErr := unix.Openat2(rootFD, domain, &unix.OpenHow{
			Flags:   uint64(unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW),
			Resolve: secureMaildirResolve,
		})
		if openErr != nil {
			if !errors.Is(openErr, unix.ENOENT) {
				rollbackErr = fmt.Errorf("reopen mailbox domain for rollback: %w", openErr)
			}
		} else {
			if removeErr := unix.Unlinkat(domainFD, local, unix.AT_REMOVEDIR); removeErr != nil && !errors.Is(removeErr, unix.ENOENT) {
				rollbackErr = fmt.Errorf("remove mailbox directory: %w", removeErr)
			}
			_ = unix.Close(domainFD)
		}
	}
	if domainCreated {
		if removeErr := unix.Unlinkat(rootFD, domain, unix.AT_REMOVEDIR); removeErr != nil && !errors.Is(removeErr, unix.ENOENT) && !errors.Is(removeErr, unix.ENOTEMPTY) {
			rollbackErr = errors.Join(rollbackErr, fmt.Errorf("remove mailbox domain directory: %w", removeErr))
		}
	}
	return rollbackErr
}

func mkdirMaildirAt(parentFD int, name string, uid, gid int) (bool, error) {
	created := false
	if err := unix.Mkdirat(parentFD, name, 0o700); err != nil {
		if !errors.Is(err, unix.EEXIST) {
			return false, fmt.Errorf("create mailbox directory %s: %w", name, err)
		}
	} else {
		created = true
	}
	fd, err := unix.Openat2(parentFD, name, &unix.OpenHow{
		Flags:   uint64(unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW),
		Resolve: secureMaildirResolve,
	})
	if err != nil {
		return false, fmt.Errorf("open mailbox directory %s: %w", name, err)
	}
	defer unix.Close(fd)
	if err := unix.Fchown(fd, uid, gid); err != nil {
		return false, fmt.Errorf("set mailbox ownership %s: %w", name, err)
	}
	if err := unix.Fchmod(fd, 0o700); err != nil {
		return false, fmt.Errorf("set mailbox permissions %s: %w", name, err)
	}
	return created, nil
}
