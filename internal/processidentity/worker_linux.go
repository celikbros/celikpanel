//go:build linux

// Package processidentity checks the kernel start token of a recorded process.
// A matching instant is an observation, not a lease or permission to mutate.
package processidentity

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

func StartToken(pid int) (string, error) {
	if pid <= 0 {
		return "", errors.New("process ID must be positive")
	}
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return "", err
	}
	text := string(raw)
	end := strings.LastIndex(text, ")")
	if end < 0 || end+2 >= len(text) {
		return "", fmt.Errorf("invalid /proc stat for pid %d", pid)
	}
	fields := strings.Fields(text[end+2:])
	if len(fields) <= 19 {
		return "", fmt.Errorf("short /proc stat for pid %d", pid)
	}
	if _, err := strconv.ParseUint(fields[19], 10, 64); err != nil {
		return "", fmt.Errorf("invalid process start time for pid %d: %w", pid, err)
	}
	return fields[19], nil
}

func Matches(pid int, started string) (bool, error) {
	if pid <= 0 || started == "" {
		return false, errors.New("recorded worker identity is incomplete")
	}
	current, err := StartToken(pid)
	if err != nil {
		return false, err
	}
	return current == started, nil
}

// RecordedWorkerGone distinguishes an exited/replaced worker from an unreadable
// one. A missing /proc/PID is proof of exit only while /proc is the kernel procfs;
// an unmounted or replaced procfs must never authorize recovery.
func RecordedWorkerGone(pid int, started string) (bool, error) {
	return recordedWorkerGone(pid, started, verifyKernelProcFS, StartToken)
}

func verifyKernelProcFS() error {
	var fs unix.Statfs_t
	if err := unix.Statfs("/proc", &fs); err != nil {
		return fmt.Errorf("verify procfs before worker exclusion: %w", err)
	}
	if fs.Type != unix.PROC_SUPER_MAGIC {
		return errors.New("/proc is not the kernel procfs; worker exclusion is unknown")
	}
	return nil
}

func recordedWorkerGone(
	pid int, started string,
	verifyProcFS func() error,
	readStart func(int) (string, error),
) (bool, error) {
	if pid <= 0 || started == "" {
		return false, errors.New("recorded worker identity is incomplete")
	}
	parsed, err := strconv.ParseUint(started, 10, 64)
	if err != nil || parsed == 0 || strconv.FormatUint(parsed, 10) != started {
		return false, errors.New("recorded worker start identity is not canonical")
	}
	if err := verifyProcFS(); err != nil {
		return false, err
	}
	current, err := readStart(pid)
	if errors.Is(err, os.ErrNotExist) {
		if verifyErr := verifyProcFS(); verifyErr != nil {
			return false, fmt.Errorf("procfs changed during worker exclusion: %w", verifyErr)
		}
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect recorded worker identity: %w", err)
	}
	return current != started, nil
}
