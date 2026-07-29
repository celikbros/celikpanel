//go:build !linux

package systemsqlite

import (
	"errors"
	"os"
)

func snapshotAvailableBytes(string) (int64, error) {
	// Capacity checks fail closed until a platform-specific implementation exists.
	// Platforma özgü bir uygulama bulunana kadar kapasite kontrolleri güvenli biçimde reddedilir.
	return 0, errors.New("snapshot storage capacity checks are not supported on this platform")
}

func snapshotFilesystemCapacityForFile(*os.File) (snapshotFilesystemCapacity, error) {
	return snapshotFilesystemCapacity{}, errors.New("snapshot filesystem capacity checks are not supported on this platform")
}
