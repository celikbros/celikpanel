//go:build !linux

package main

import "os"

func samePinnedSQLiteFileMetadata(left os.FileInfo, right os.FileInfo) bool {
	return left.Mode() == right.Mode() &&
		left.Size() == right.Size() &&
		left.ModTime().Equal(right.ModTime()) &&
		os.SameFile(left, right)
}

func samePinnedSQLiteFileIdentity(left os.FileInfo, right os.FileInfo) bool {
	return left.Mode() == right.Mode() && os.SameFile(left, right)
}

func samePinnedSQLiteFileOwnerAndMode(left os.FileInfo, right os.FileInfo) bool {
	return left.Mode() == right.Mode()
}
