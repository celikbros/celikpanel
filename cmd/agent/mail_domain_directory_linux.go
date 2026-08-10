//go:build linux

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

const (
	mailDomainQuarantineDirectory = ".celikpanel-quarantine"
	managedMailRootMode           = 0o710
)

func managedMailOwnerIDs() (int, int, error) {
	if os.Getenv("CELIKPANEL_MAIL_DIR") != "" {
		return os.Geteuid(), os.Getegid(), nil
	}
	gid, err := strconv.Atoi(vmailGID)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid vmail gid: %w", err)
	}
	return 0, gid, nil
}

func quarantineOwnerIDs() (int, int) {
	if os.Getenv("CELIKPANEL_MAIL_DIR") != "" {
		return os.Geteuid(), os.Getegid()
	}
	return 0, 0
}

func openMailAbsoluteDirectory(absolute string) (int, error) {
	clean := filepath.Clean(absolute)
	if !filepath.IsAbs(absolute) || clean != absolute {
		return -1, fmt.Errorf("mail directory must be an absolute canonical path")
	}
	if clean == "/" {
		return unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	}
	slashFD, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, err
	}
	defer unix.Close(slashFD)
	fd, err := unix.Openat2(slashFD, strings.TrimPrefix(clean, "/"), &unix.OpenHow{
		Flags:   uint64(unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW),
		Resolve: secureMaildirResolve,
	})
	if errors.Is(err, unix.ENOSYS) {
		return -1, fmt.Errorf("secure mail directory access requires openat2: %w", err)
	}
	return fd, err
}

func openMailDirectoryAt(parentFD int, name string) (int, error) {
	if name == "" || name == "." || name == ".." || filepath.Base(name) != name {
		return -1, fmt.Errorf("invalid mail directory component")
	}
	fd, err := unix.Openat2(parentFD, name, &unix.OpenHow{
		Flags:   uint64(unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW),
		Resolve: secureMaildirResolve,
	})
	if errors.Is(err, unix.ENOSYS) {
		return -1, fmt.Errorf("secure mail directory access requires openat2: %w", err)
	}
	return fd, err
}

func hardenManagedMailRoot(fd int) error {
	uid, gid, err := managedMailOwnerIDs()
	if err != nil {
		return err
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return fmt.Errorf("stat mail root: %w", err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR {
		return fmt.Errorf("mail root is not a directory")
	}
	if err := unix.Fchown(fd, uid, gid); err != nil {
		return fmt.Errorf("set mail root ownership: %w", err)
	}
	if err := unix.Fchmod(fd, managedMailRootMode); err != nil {
		return fmt.Errorf("set mail root mode: %w", err)
	}
	if err := unix.Fsync(fd); err != nil {
		return fmt.Errorf("sync mail root: %w", err)
	}
	return nil
}

func secureEnsureMailRoot(root string) error {
	clean := filepath.Clean(root)
	if !filepath.IsAbs(root) || clean != root || clean == "/" {
		return fmt.Errorf("invalid mail root")
	}
	parentFD, err := openMailAbsoluteDirectory(filepath.Dir(clean))
	if err != nil {
		return fmt.Errorf("open mail root parent: %w", err)
	}
	defer unix.Close(parentFD)
	base := filepath.Base(clean)
	if err := unix.Mkdirat(parentFD, base, managedMailRootMode); err != nil && !errors.Is(err, unix.EEXIST) {
		return fmt.Errorf("create mail root: %w", err)
	}
	fd, err := openMailDirectoryAt(parentFD, base)
	if err != nil {
		return fmt.Errorf("open mail root: %w", err)
	}
	defer unix.Close(fd)
	if err := hardenManagedMailRoot(fd); err != nil {
		return err
	}
	if err := unix.Fsync(parentFD); err != nil {
		return fmt.Errorf("sync mail root parent: %w", err)
	}
	return nil
}

func openManagedMailRoot() (int, error) {
	if filepath.Clean(mailRootDir) == "/" {
		return -1, fmt.Errorf("refusing root as mail root")
	}
	fd, err := openMailAbsoluteDirectory(mailRootDir)
	if err != nil {
		return -1, err
	}
	if err := hardenManagedMailRoot(fd); err != nil {
		unix.Close(fd)
		return -1, err
	}
	return fd, nil
}

func openMailQuarantineRoot(rootFD int, create bool) (int, bool, error) {
	fd, err := openMailDirectoryAt(rootFD, mailDomainQuarantineDirectory)
	if errors.Is(err, unix.ENOENT) {
		if !create {
			return -1, false, nil
		}
		if err := unix.Mkdirat(rootFD, mailDomainQuarantineDirectory, 0o700); err != nil && !errors.Is(err, unix.EEXIST) {
			return -1, false, fmt.Errorf("create mail quarantine: %w", err)
		}
		fd, err = openMailDirectoryAt(rootFD, mailDomainQuarantineDirectory)
	}
	if err != nil {
		return -1, false, fmt.Errorf("open mail quarantine: %w", err)
	}
	uid, gid := quarantineOwnerIDs()
	if err := unix.Fchown(fd, uid, gid); err != nil {
		unix.Close(fd)
		return -1, false, fmt.Errorf("set quarantine ownership: %w", err)
	}
	if err := unix.Fchmod(fd, 0o700); err != nil {
		unix.Close(fd)
		return -1, false, fmt.Errorf("set quarantine mode: %w", err)
	}
	if err := unix.Fsync(fd); err != nil {
		unix.Close(fd)
		return -1, false, fmt.Errorf("sync quarantine: %w", err)
	}
	if err := unix.Fsync(rootFD); err != nil {
		unix.Close(fd)
		return -1, false, fmt.Errorf("sync mail root: %w", err)
	}
	return fd, true, nil
}

func mailDirectoryExistsAt(parentFD int, name string) (bool, error) {
	fd, err := openMailDirectoryAt(parentFD, name)
	if errors.Is(err, unix.ENOENT) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, unix.Close(fd)
}

func quarantineMailDomainDirectory(domain string, domainID int) (func() error, bool, error) {
	rootFD, err := openManagedMailRoot()
	if errors.Is(err, unix.ENOENT) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("open mail root for domain quarantine: %w", err)
	}
	defer unix.Close(rootFD)

	sourceExists, err := mailDirectoryExistsAt(rootFD, domain)
	if err != nil {
		return nil, false, fmt.Errorf("inspect mail domain source: %w", err)
	}
	quarantineFD, quarantineExists, err := openMailQuarantineRoot(rootFD, false)
	if err != nil {
		return nil, false, err
	}
	if quarantineExists {
		defer unix.Close(quarantineFD)
	}
	target := mailDomainQuarantineName(domain, domainID)
	if !sourceExists {
		if !quarantineExists {
			return nil, false, nil
		}
		targetExists, inspectErr := mailDirectoryExistsAt(quarantineFD, target)
		if inspectErr != nil {
			return nil, false, fmt.Errorf("inspect quarantined mail domain: %w", inspectErr)
		}
		return nil, targetExists, nil
	}
	if !quarantineExists {
		quarantineFD, _, err = openMailQuarantineRoot(rootFD, true)
		if err != nil {
			return nil, false, err
		}
		defer unix.Close(quarantineFD)
	}
	targetExists, err := mailDirectoryExistsAt(quarantineFD, target)
	if err != nil {
		return nil, false, fmt.Errorf("inspect quarantine target: %w", err)
	}
	if targetExists {
		return nil, false, fmt.Errorf("mail domain source and quarantine target both exist")
	}
	if err := unix.Renameat2(rootFD, domain, quarantineFD, target, unix.RENAME_NOREPLACE); err != nil {
		return nil, false, fmt.Errorf("quarantine mail domain: %w", err)
	}
	rollback := func() error {
		return restoreQuarantinedMailDomain(domain, domainID)
	}
	if err := unix.Fsync(rootFD); err != nil {
		return rollback, true, fmt.Errorf("sync mail root after quarantine: %w", err)
	}
	if err := unix.Fsync(quarantineFD); err != nil {
		return rollback, true, fmt.Errorf("sync mail quarantine after rename: %w", err)
	}
	return nil, true, nil
}

func restoreQuarantinedMailDomain(domain string, domainID int) error {
	rootFD, err := openManagedMailRoot()
	if err != nil {
		return err
	}
	defer unix.Close(rootFD)
	quarantineFD, exists, err := openMailQuarantineRoot(rootFD, false)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("mail quarantine disappeared during rollback")
	}
	defer unix.Close(quarantineFD)
	sourceExists, err := mailDirectoryExistsAt(rootFD, domain)
	if err != nil {
		return err
	}
	if sourceExists {
		return fmt.Errorf("mail domain source reappeared during rollback")
	}
	target := mailDomainQuarantineName(domain, domainID)
	targetExists, err := mailDirectoryExistsAt(quarantineFD, target)
	if err != nil {
		return err
	}
	if !targetExists {
		return fmt.Errorf("quarantined mail domain disappeared during rollback")
	}
	if err := unix.Renameat2(quarantineFD, target, rootFD, domain, unix.RENAME_NOREPLACE); err != nil {
		return fmt.Errorf("restore quarantined mail domain: %w", err)
	}
	if err := unix.Fsync(quarantineFD); err != nil {
		return fmt.Errorf("sync mail quarantine after rollback: %w", err)
	}
	if err := unix.Fsync(rootFD); err != nil {
		return fmt.Errorf("sync mail root after rollback: %w", err)
	}
	return nil
}
