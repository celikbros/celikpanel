//go:build linux || freebsd

package main

import "syscall"

func readDisk(path string) (used, total uint64) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0
	}

	// Explicit casts preserve the implementation across Linux and FreeBSD,
	// where Statfs_t uses different integer types for these fields.
	bs := uint64(st.Bsize)
	total = uint64(st.Blocks) * bs
	free := uint64(st.Bavail) * bs
	if free > total {
		free = total
	}
	return total - free, total
}
