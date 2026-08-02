//go:build windows

package main

import "testing"

func TestReadDiskWindowsReportsUsableValues(t *testing.T) {
	used, total := readDisk(t.TempDir())
	if total == 0 {
		t.Fatal("readDisk() total = 0 for a valid temporary directory")
	}
	if used > total {
		t.Fatalf("readDisk() used = %d, total = %d; used exceeds total", used, total)
	}
}
