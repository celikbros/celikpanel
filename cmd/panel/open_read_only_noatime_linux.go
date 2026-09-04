//go:build linux

package main

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func openReadOnlyNoAtime(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOATIME|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fmt.Errorf("open read-only without updating access time: %w", err)
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("open read-only without updating access time: invalid descriptor")
	}
	return file, nil
}
