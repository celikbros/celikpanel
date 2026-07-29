//go:build linux

package systemsqlite

import (
	"errors"
	"math"
	"os"

	"golang.org/x/sys/unix"
)

func snapshotAvailableBytes(path string) (int64, error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return 0, err
	}
	return snapshotAvailableBytesFromStatfs(&stat)
}

func snapshotFilesystemCapacityForFile(file *os.File) (snapshotFilesystemCapacity, error) {
	if file == nil {
		return snapshotFilesystemCapacity{}, errors.New("snapshot filesystem descriptor is missing")
	}
	var identity unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &identity); err != nil {
		return snapshotFilesystemCapacity{}, err
	}
	if identity.Dev == 0 {
		return snapshotFilesystemCapacity{}, errors.New("snapshot filesystem identity is unavailable")
	}
	var stat unix.Statfs_t
	if err := unix.Fstatfs(int(file.Fd()), &stat); err != nil {
		return snapshotFilesystemCapacity{}, err
	}
	available, err := snapshotAvailableBytesFromStatfs(&stat)
	if err != nil {
		return snapshotFilesystemCapacity{}, err
	}
	return snapshotFilesystemCapacity{
		ID:             snapshotFilesystemID(identity.Dev),
		AvailableBytes: available,
	}, nil
}

func snapshotAvailableBytesFromStatfs(stat *unix.Statfs_t) (int64, error) {
	if stat == nil {
		return 0, errors.New("snapshot storage capacity is unavailable")
	}
	if stat.Bsize <= 0 {
		return 0, errors.New("snapshot storage reported an invalid block size")
	}
	blockSize := uint64(stat.Bsize)
	if stat.Bavail > uint64(math.MaxInt64)/blockSize {
		return math.MaxInt64, nil
	}
	return int64(stat.Bavail * blockSize), nil
}
