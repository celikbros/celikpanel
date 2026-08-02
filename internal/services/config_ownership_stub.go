//go:build !linux

package services

import "os"

type managedFileOwnership struct{}

func managedOwnershipFromInfo(os.FileInfo) (managedFileOwnership, error) {
	return managedFileOwnership{}, nil
}

func applyManagedOwnership(*os.File, managedFileOwnership) error { return nil }

// Windows does not provide POSIX directory fsync semantics. File fsync and
// atomic rename are still performed; Linux production hosts additionally
// fsync the containing directory in config_ownership_linux.go.
func syncManagedConfigDirectory(string) error { return nil }
