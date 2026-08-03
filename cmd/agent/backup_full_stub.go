//go:build !linux

package main

import "context"

func secureCreateFullBackup(
	context.Context,
	string,
	string,
	string,
	string,
	[]BackupDatabaseIdentity,
) (int64, error) {
	return 0, errSecureBackupUnsupported
}

func secureReadFullBackupDatabaseIDs(
	string, string, string,
) ([]int, error) {
	return nil, errSecureBackupUnsupported
}

func secureRestoreFullBackup(
	context.Context,
	string,
	string,
	string,
	string,
	[]BackupDatabaseIdentity,
) error {
	return errSecureBackupUnsupported
}
