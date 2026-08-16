//go:build !linux

package binddns

import "io/fs"

func platformOwnership(fs.FileInfo) (uid, gid int, known bool) {
	return 0, 0, false
}
