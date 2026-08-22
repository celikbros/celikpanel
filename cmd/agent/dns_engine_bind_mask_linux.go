//go:build linux

package main

import (
	"errors"
	"fmt"

	"golang.org/x/sys/unix"
)

type bindMaskIdentity struct {
	Device uint64
	Inode  uint64
	Mode   uint32
	UID    uint32
	GID    uint32
	Links  uint64
}

func verifyBINDPersistentMaskFiles() error {
	return verifyExactPersistentServiceMasks(
		[]string{"named.service", "bind9.service"},
	)
}

func verifyExactPersistentServiceMask(unit string) error {
	return verifyExactPersistentServiceMasks([]string{unit})
}

func verifyExactPersistentServiceMasks(units []string) error {
	rootFD, err := unix.Open(
		"/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0,
	)
	if err != nil {
		return fmt.Errorf("open filesystem root for BIND mask proof: %w", err)
	}
	defer unix.Close(rootFD)
	if _, err := validateExactBINDDirectoryFD(
		rootFD, 0, 0, bindManagedRootMode, "BIND mask filesystem root",
	); err != nil {
		return err
	}
	etcFD, _, err := openExactBINDDirectoryAt(
		rootFD, "etc", 0, 0, bindManagedRootMode, "/etc",
	)
	if err != nil {
		return err
	}
	defer unix.Close(etcFD)
	systemdFD, _, err := openExactBINDDirectoryAt(
		etcFD, "systemd", 0, 0, bindManagedRootMode, "/etc/systemd",
	)
	if err != nil {
		return err
	}
	defer unix.Close(systemdFD)
	systemFD, _, err := openExactBINDDirectoryAt(
		systemdFD, "system", 0, 0, bindManagedRootMode, "/etc/systemd/system",
	)
	if err != nil {
		return err
	}
	defer unix.Close(systemFD)
	for _, unit := range units {
		if err := verifyExactBINDPersistentMaskAt(systemFD, unit); err != nil {
			return err
		}
	}
	return nil
}

func verifyBINDPersistentMaskFilesAt(systemDirectoryFD int) error {
	for _, unit := range []string{"named.service", "bind9.service"} {
		if err := verifyExactBINDPersistentMaskAt(systemDirectoryFD, unit); err != nil {
			return err
		}
	}
	return nil
}

func verifyExactBINDPersistentMaskAt(directoryFD int, unit string) error {
	fd, err := unix.Openat2(directoryFD, unit, &unix.OpenHow{
		Flags: uint64(unix.O_PATH | unix.O_CLOEXEC | unix.O_NOFOLLOW),
		Resolve: unix.RESOLVE_BENEATH |
			unix.RESOLVE_NO_MAGICLINKS,
	})
	if errors.Is(err, unix.ENOSYS) {
		return fmt.Errorf("verify %s persistent mask requires Linux openat2: %w", unit, err)
	}
	if err != nil {
		return fmt.Errorf("open persistent BIND mask %s without following it: %w", unit, err)
	}
	defer unix.Close(fd)
	before, err := inspectExactBINDMaskFD(fd, unit)
	if err != nil {
		return err
	}
	buffer := make([]byte, len("/dev/null")+1)
	n, err := unix.Readlinkat(fd, "", buffer)
	if err != nil {
		return fmt.Errorf("read persistent BIND mask %s: %w", unit, err)
	}
	if n != len("/dev/null") || string(buffer[:n]) != "/dev/null" {
		return fmt.Errorf("persistent BIND mask %s does not point exactly to /dev/null", unit)
	}
	after, err := inspectExactBINDMaskFD(fd, unit)
	if err != nil {
		return err
	}
	if after != before {
		return fmt.Errorf("persistent BIND mask %s changed during verification", unit)
	}
	return nil
}

func inspectExactBINDMaskFD(fd int, unit string) (bindMaskIdentity, error) {
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return bindMaskIdentity{}, fmt.Errorf("stat persistent BIND mask %s: %w", unit, err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFLNK || stat.Uid != 0 || stat.Gid != 0 || stat.Nlink != 1 {
		return bindMaskIdentity{}, fmt.Errorf(
			"persistent BIND mask %s is not an exact root-owned single-link symlink",
			unit,
		)
	}
	return bindMaskIdentity{
		Device: uint64(stat.Dev),
		Inode:  stat.Ino,
		Mode:   stat.Mode,
		UID:    stat.Uid,
		GID:    stat.Gid,
		Links:  uint64(stat.Nlink),
	}, nil
}
