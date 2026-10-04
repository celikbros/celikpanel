//go:build linux

package main

import (
	"os"
	"syscall"
)

func samePinnedSQLiteFileMetadata(left os.FileInfo, right os.FileInfo) bool {
	leftStat, leftOK := left.Sys().(*syscall.Stat_t)
	rightStat, rightOK := right.Sys().(*syscall.Stat_t)
	return leftOK &&
		rightOK &&
		leftStat.Dev == rightStat.Dev &&
		leftStat.Ino == rightStat.Ino &&
		leftStat.Mode == rightStat.Mode &&
		leftStat.Nlink == rightStat.Nlink &&
		leftStat.Uid == rightStat.Uid &&
		leftStat.Gid == rightStat.Gid &&
		leftStat.Size == rightStat.Size &&
		leftStat.Mtim == rightStat.Mtim &&
		leftStat.Ctim == rightStat.Ctim
}

// samePinnedSQLiteFileIdentity is true when only the content timestamps or size
// of the same file differ: the file was written, not replaced, relinked,
// re-owned or re-moded. Only such a change counts as a concurrent write.
func samePinnedSQLiteFileIdentity(left os.FileInfo, right os.FileInfo) bool {
	leftStat, leftOK := left.Sys().(*syscall.Stat_t)
	rightStat, rightOK := right.Sys().(*syscall.Stat_t)
	return leftOK &&
		rightOK &&
		leftStat.Dev == rightStat.Dev &&
		leftStat.Ino == rightStat.Ino &&
		leftStat.Mode == rightStat.Mode &&
		leftStat.Nlink == rightStat.Nlink &&
		leftStat.Uid == rightStat.Uid &&
		leftStat.Gid == rightStat.Gid
}

// samePinnedSQLiteFileOwnerAndMode is the weaker test for -wal and -shm: SQLite
// may remove them on its last close and create them again (a new inode) on the
// next write. The owner and mode must still match.
func samePinnedSQLiteFileOwnerAndMode(left os.FileInfo, right os.FileInfo) bool {
	leftStat, leftOK := left.Sys().(*syscall.Stat_t)
	rightStat, rightOK := right.Sys().(*syscall.Stat_t)
	return leftOK &&
		rightOK &&
		leftStat.Mode == rightStat.Mode &&
		leftStat.Uid == rightStat.Uid &&
		leftStat.Gid == rightStat.Gid
}
