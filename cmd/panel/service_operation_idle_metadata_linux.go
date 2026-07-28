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
