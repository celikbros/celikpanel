//go:build windows

package main

import (
	"path/filepath"

	"golang.org/x/sys/windows"
)

func readDisk(path string) (used, total uint64) {
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return 0, 0
	}

	pathPtr, err := windows.UTF16PtrFromString(absolutePath)
	if err != nil {
		return 0, 0
	}

	var freeAvailableToCaller uint64
	var totalBytes uint64
	var totalFreeBytes uint64
	if err := windows.GetDiskFreeSpaceEx(
		pathPtr,
		&freeAvailableToCaller,
		&totalBytes,
		&totalFreeBytes,
	); err != nil {
		return 0, 0
	}
	if freeAvailableToCaller > totalBytes {
		freeAvailableToCaller = totalBytes
	}
	return totalBytes - freeAvailableToCaller, totalBytes
}
