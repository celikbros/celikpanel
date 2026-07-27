package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/alicelik/celikpanel/internal/backupspec"
)

func validateScopeIDs(version, subscriptionID, domainID int) error {
	if version != backupspec.ProtocolVersion {
		return errors.New("unsupported backup protocol")
	}
	if subscriptionID < 1 {
		return errors.New("invalid subscription ID")
	}
	if domainID < 1 {
		return errors.New("invalid domain ID")
	}
	return nil
}

func scopeBackupDir(s backupScope) (string, error) {
	if err := validateV2Scope(s); err != nil {
		return "", err
	}
	if !filepath.IsAbs(backupBaseDir) || filepath.Clean(backupBaseDir) != backupBaseDir {
		return "", errors.New("backup base directory must be absolute and canonical")
	}
	return filepath.Join(
		backupBaseDir,
		"subscriptions", fmt.Sprint(s.SubscriptionID),
		"domains", fmt.Sprint(s.DomainID),
	), nil
}

func ensureBackupDir(s backupScope) (string, error) {
	dir, err := scopeBackupDir(s)
	if err != nil {
		return "", err
	}
	if err := mkdirPrivate(dir); err != nil {
		return "", err
	}
	return dir, nil
}

func legacyBackupDir(s backupScope) (string, error) {
	if err := validateV2Scope(s); err != nil {
		return "", err
	}
	if !validLegacyDomainName(s.DomainName) {
		return "", errors.New("invalid legacy domain")
	}
	if !filepath.IsAbs(backupBaseDir) || filepath.Clean(backupBaseDir) != backupBaseDir {
		return "", errors.New("backup base directory must be absolute and canonical")
	}
	return filepath.Join(backupBaseDir, s.DomainName), nil
}

func mkdirPrivate(dir string) error {
	if err := secureMkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.Chmod(dir, 0o700)
}
