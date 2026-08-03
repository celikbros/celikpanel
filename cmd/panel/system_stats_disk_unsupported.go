//go:build !linux && !freebsd && !windows

package main

// readDisk fails closed on platforms whose filesystem statistics API has not
// been implemented. Callers receive an explicit unavailable value rather than
// platform-specific or guessed disk usage.
func readDisk(_ string) (used, total uint64) {
	return 0, 0
}
