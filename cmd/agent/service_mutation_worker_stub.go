//go:build !linux

package main

import (
	"fmt"
	"os"
)

func serviceMutationProcessStartIdentity(pid int) (string, error) {
	if pid <= 0 {
		return "", fmt.Errorf("invalid pid")
	}
	return fmt.Sprintf("%d", pid), nil
}

func serviceMutationWorkerMatches(pid int, started string) bool {
	if pid <= 0 || started == "" {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(os.Signal(nil)) == nil
}
