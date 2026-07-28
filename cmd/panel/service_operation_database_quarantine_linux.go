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

func sameExactUnixFileMetadata(left unix.Stat_t, right unix.Stat_t) bool {
	return left.Dev == right.Dev &&
		left.Ino == right.Ino &&
		left.Mode == right.Mode &&
		left.Nlink == right.Nlink &&
		left.Uid == right.Uid &&
		left.Gid == right.Gid &&
		left.Size == right.Size &&
		left.Mtim == right.Mtim &&
		left.Ctim == right.Ctim
}

func rejectDatabaseQuarantineUsersAndHandles(
	procRoot string,
	ownerUID uint32,
	databaseStat unix.Stat_t,
) error {
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return fmt.Errorf("scan process table before database quarantine: %w", err)
	}
	currentPID := os.Getpid()
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 || pid == currentPID || !entry.IsDir() {
			continue
		}
		processPath := filepath.Join(procRoot, entry.Name())
		statusBytes, err := os.ReadFile(filepath.Join(processPath, "status"))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return fmt.Errorf("inspect process %d credentials: %w", pid, err)
		}
		for _, line := range strings.Split(string(statusBytes), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 2 || fields[0] != "Uid:" {
				continue
			}
			for _, value := range fields[1:] {
				uid, parseErr := strconv.ParseUint(value, 10, 32)
				if parseErr != nil {
					return fmt.Errorf("inspect process %d credentials: invalid UID", pid)
				}
				if uint32(uid) == ownerUID {
					return fmt.Errorf("process %d still uses the celikpanel UID", pid)
				}
			}
			break
		}

		fdEntries, err := os.ReadDir(filepath.Join(processPath, "fd"))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return fmt.Errorf("inspect process %d database handles: %w", pid, err)
		}
		for _, fdEntry := range fdEntries {
			var fdStat unix.Stat_t
			err := unix.Stat(filepath.Join(processPath, "fd", fdEntry.Name()), &fdStat)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					continue
				}
				return fmt.Errorf("inspect process %d descriptor %s: %w", pid, fdEntry.Name(), err)
			}
			if sameUnixFileIdentity(fdStat, databaseStat) {
				return fmt.Errorf("process %d still holds the canonical panel database open", pid)
			}
		}
	}
	return nil
}
