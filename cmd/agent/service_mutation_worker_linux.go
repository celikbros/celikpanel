//go:build linux

package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

func serviceMutationProcessStartIdentity(pid int) (string, error) {
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

func serviceMutationWorkerMatches(pid int, started string) bool {
	if pid <= 0 || started == "" {
		return false
	}
	current, err := serviceMutationProcessStartIdentity(pid)
	return err == nil && current == started
}
