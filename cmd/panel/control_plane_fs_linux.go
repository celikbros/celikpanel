//go:build linux

package main

import (
	"fmt"
	"os"
	"os/user"
	"strconv"
	"syscall"

	"golang.org/x/sys/unix"
)

// controlPlaneOpenNoFollow makes every staged write refuse a symlink that
// appeared under the destination between the check and the open.
const controlPlaneOpenNoFollow = unix.O_NOFOLLOW

// controlPlaneOwnership reads the live owner as NAMES. A numeric fallback is
// recorded only when the host itself cannot name the account, so the archive
// still carries something a restore can act on.
func controlPlaneOwnership(path string, info os.FileInfo) (string, string, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", "", fmt.Errorf("read the owner of %s", path)
	}
	userName := strconv.FormatUint(uint64(stat.Uid), 10)
	if resolved, err := user.LookupId(userName); err == nil {
		userName = resolved.Username
	}
	groupName := strconv.FormatUint(uint64(stat.Gid), 10)
	if resolved, err := user.LookupGroupId(groupName); err == nil {
		groupName = resolved.Name
	}
	return userName, groupName, nil
}

// controlPlaneResolveOwnership turns recorded account NAMES into the ids this
// host uses. It runs for every member before anything is placed, so a host
// missing an account is told which account it is missing while the target tree
// is still untouched.
func controlPlaneResolveOwnership(userName string, groupName string) (int, int, error) {
	uid, err := controlPlaneLookupUID(userName)
	if err != nil {
		return 0, 0, err
	}
	gid, err := controlPlaneLookupGID(groupName)
	if err != nil {
		return 0, 0, err
	}
	return uid, gid, nil
}

func controlPlaneLookupUID(name string) (int, error) {
	if resolved, err := user.Lookup(name); err == nil {
		id, err := strconv.Atoi(resolved.Uid)
		if err == nil {
			return id, nil
		}
	}
	if id, err := strconv.Atoi(name); err == nil && id >= 0 {
		return id, nil
	}
	return 0, fmt.Errorf(
		"the archive was taken with files owned by the account %q, which does not exist on this host",
		name,
	)
}

func controlPlaneLookupGID(name string) (int, error) {
	if resolved, err := user.LookupGroup(name); err == nil {
		id, err := strconv.Atoi(resolved.Gid)
		if err == nil {
			return id, nil
		}
	}
	if id, err := strconv.Atoi(name); err == nil && id >= 0 {
		return id, nil
	}
	return 0, fmt.Errorf(
		"the archive was taken with files in the group %q, which does not exist on this host",
		name,
	)
}

func controlPlaneApplyFileMetadata(
	file *os.File,
	mode os.FileMode,
	uid int,
	gid int,
) error {
	if err := unix.Fchown(int(file.Fd()), uid, gid); err != nil {
		return fmt.Errorf("set the owner of the staged file: %w", err)
	}
	if err := unix.Fchmod(int(file.Fd()), uint32(mode.Perm())); err != nil {
		return fmt.Errorf("set the mode of the staged file: %w", err)
	}
	return nil
}

func controlPlaneApplyDirectoryMetadata(
	path string,
	mode os.FileMode,
	uid int,
	gid int,
) error {
	if err := os.Chown(path, uid, gid); err != nil {
		return fmt.Errorf("set the owner of %s: %w", path, err)
	}
	if err := os.Chmod(path, mode.Perm()); err != nil {
		return fmt.Errorf("set the mode of %s: %w", path, err)
	}
	return nil
}

// controlPlaneFinalizeFileMode is a no-op on Linux: fchmod already ran on the
// staged descriptor before the content was written, exactly as the design says.
func controlPlaneFinalizeFileMode(_ string, _ os.FileMode) error {
	return nil
}

func controlPlaneSyncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s to flush it: %w", path, err)
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("flush %s: %w", path, err)
	}
	return nil
}
