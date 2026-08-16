//go:build linux

package binddns

import (
	"io/fs"
	"syscall"
)

func platformOwnership(info fs.FileInfo) (uid, gid int, known bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, false
	}
	return int(stat.Uid), int(stat.Gid), true
}
