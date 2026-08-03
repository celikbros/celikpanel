//go:build !linux

package main

import (
	"errors"
	"os"
)

var errSecureBackupUnsupported = errors.New("secure backup operations require Linux openat2")

func secureCreateBackupFile(string, string, string) (*os.File, func(), error) {
	return nil, nil, errSecureBackupUnsupported
}

func secureOpenBackupFile(string, string, string) (*os.File, int64, error) {
	return nil, 0, errSecureBackupUnsupported
}

func secureCreateFilesBackup(string, string, string, string) (int64, error) {
	return 0, errSecureBackupUnsupported
}

func secureListBackupFiles(string, string) ([]backupFileRecord, error) {
	return nil, errSecureBackupUnsupported
}

func secureRestoreFilesBackup(string, string, string, string) error {
	return errSecureBackupUnsupported
}

func secureDeleteBackupFile(string, string, string) error {
	return errSecureBackupUnsupported
}

func secureReadBackupFile(string, string, string, int64) ([]byte, int64, error) {
	return nil, 0, errSecureBackupUnsupported
}
