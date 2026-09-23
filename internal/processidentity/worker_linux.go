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
