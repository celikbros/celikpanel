//go:build !linux && !freebsd && !windows

package main

import "testing"

func TestReadDiskUnsupportedPlatformFailsClosed(t *testing.T) {
	used, total := readDisk("ignored")
	if used != 0 || total != 0 {
		t.Fatalf("readDisk() = (%d, %d), want (0, 0)", used, total)
	}
}
