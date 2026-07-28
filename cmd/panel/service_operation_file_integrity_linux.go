//go:build linux

package main

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"

	"golang.org/x/sys/unix"
)

func digestPinnedServiceOperationFile(
	file *os.File,
	size int64,
) ([sha256.Size]byte, error) {
	var digest [sha256.Size]byte
	if file == nil || size < 0 {
		return digest, fmt.Errorf("pinned service operation file is invalid")
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, io.NewSectionReader(file, 0, size)); err != nil {
		return digest, fmt.Errorf("hash pinned service operation file: %w", err)
	}
	copy(digest[:], hash.Sum(nil))
	return digest, nil
}

// samePublishedUnixFileMetadata permits only the ctime transition caused by
// rename(2). Exact descriptor/path metadata and a pinned SHA-256 digest are
// checked separately after publication.
// samePublishedUnixFileMetadata yalnız rename(2)'in neden olduğu ctime geçişine
// izin verir. Yayın sonrasında descriptor/yol metadata'sı ve sabitlenmiş SHA-256
// özeti ayrıca tam olarak doğrulanır.
func samePublishedUnixFileMetadata(validated unix.Stat_t, published unix.Stat_t) bool {
	return validated.Dev == published.Dev &&
		validated.Ino == published.Ino &&
		validated.Mode == published.Mode &&
		validated.Nlink == published.Nlink &&
		validated.Uid == published.Uid &&
		validated.Gid == published.Gid &&
		validated.Size == published.Size &&
		validated.Mtim == published.Mtim
}
