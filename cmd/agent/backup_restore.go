package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/alicelik/celikpanel/internal/backupspec"
)

func (a *Agent) restoreBackup(req *backupspec.RestoreRequest, resp *backupspec.RestoreResponse) error {
	scope := restoreScope(req)
	if err := validateV2Scope(scope); err != nil {
		return err
	}
	backupPath, legacy, err := resolveBackup(scope, req.BackupName)
	if err != nil {
		return err
	}
	docroot, err := backupDocumentRoot(scope.SubscriptionID, scope.DomainID)
	if err != nil {
		return err
	}
	if legacy {
		info, err := inspectLegacyBackup(backupPath, req.BackupName)
		if err != nil {
			return err
		}
		if !info.Restorable || (info.Type != backupspec.TypeFiles && info.Type != backupspec.TypeFull) {
			return errors.New("legacy database backups cannot be restored safely")
		}
		// Pre-v2 full archives did not contain a verifiable database manifest.
		// Restore them explicitly as file-only archives.
		safety, err := a.createSafetyBackup(scope, backupspec.TypeFiles, backupspec.DatabaseIdentity{}, nil)
		if err != nil {
			return fmt.Errorf("create pre-restore safety backup: %w", err)
		}
		resp.SafetyBackup = &safety
		if err := restoreFilesArchive(backupPath, docroot); err != nil {
			return err
		}
		resp.Success = true
		return nil
	}

	manifest, err := readBackupManifest(backupPath)
	if err != nil {
		return err
	}
	if err := validateManifest(manifest, scope); err != nil {
		return err
	}
	var requestedDatabases []backupspec.DatabaseIdentity
	var singleDatabase backupspec.DatabaseIdentity
	switch manifest.Type {
	case backupspec.TypeFiles:
		// No database identity participates in a files-only restore.
	case backupspec.TypeDatabase:
		if len(manifest.Databases) != 1 {
			return errors.New("database backup manifest is invalid")
		}
		singleDatabase, err = validateRestoreDatabase(manifest.Databases[0].Identity, req.Database)
		if err != nil {
			return err
		}
		requestedDatabases = []backupspec.DatabaseIdentity{singleDatabase}
	case backupspec.TypeFull:
		requestedDatabases, err = validateFullRestoreSet(manifest.databaseIdentities(), req.Databases)
		if err != nil {
			return err
		}
	default:
		return errors.New("unsupported backup type")
	}

	safety, err := a.createSafetyBackup(scope, manifest.Type, singleDatabase, requestedDatabases)
	if err != nil {
		return fmt.Errorf("create pre-restore safety backup: %w", err)
	}
	resp.SafetyBackup = &safety

	workDir, err := newRestoreWorkDir(scope)
	if err != nil {
		return err
	}
	defer os.RemoveAll(workDir)
	targetDir := filepath.Join(workDir, "target")
	unpacked, err := unpackBackupPackage(backupPath, targetDir, scope)
	if err != nil {
		return err
	}
	if unpacked.Type != manifest.Type {
		return errors.New("backup changed during restore preparation")
	}

	switch unpacked.Type {
	case backupspec.TypeFiles:
		if err := restoreFilesArchive(filepath.Join(targetDir, filepath.FromSlash(unpacked.Files.Name)), docroot); err != nil {
			return err
		}
	case backupspec.TypeDatabase:
		database, err := validateRestoreDatabase(unpacked.Databases[0].Identity, req.Database)
		if err != nil {
			return err
		}
		if err := a.restoreDatabasePackage(scope, targetDir, safety.Path, database); err != nil {
			return err
		}
	case backupspec.TypeFull:
		databases, err := validateFullRestoreSet(unpacked.databaseIdentities(), req.Databases)
		if err != nil {
			return err
		}
		if err := a.restoreFullPackage(scope, targetDir, safety.Path, docroot, unpacked, databases); err != nil {
			return err
		}
	}
	resp.Success = true
	return nil
}

func (a *Agent) createSafetyBackup(scope backupScope, backupType string, database backupspec.DatabaseIdentity, databases []backupspec.DatabaseIdentity) (backupspec.Info, error) {
	request := &backupspec.CreateRequest{
		ProtocolVersion: backupspec.ProtocolVersion,
		SubscriptionID:  scope.SubscriptionID,
		DomainID:        scope.DomainID,
		DomainName:      scope.DomainName,
		Type:            backupType,
		Origin:          backupspec.OriginPreRestore,
		Database:        database,
		Databases:       databases,
	}
	return a.createBackup(request)
}

func newRestoreWorkDir(scope backupScope) (string, error) {
	dir, err := ensureBackupDir(scope)
	if err != nil {
		return "", err
	}
	workDir, err := os.MkdirTemp(dir, ".restore-work-")
	if err != nil {
		return "", err
	}
	if err := os.Chmod(workDir, 0o700); err != nil {
		_ = os.RemoveAll(workDir)
		return "", err
	}
	return workDir, nil
}

func restoreFilesArchive(archivePath, docroot string) error {
	staged, err := prepareFilesRestore(archivePath, docroot)
	if err != nil {
		return err
	}
	defer staged.Cleanup()
	if err := staged.Publish(); err != nil {
		return err
	}
	// The restored directory is live and internally consistent at this point.
	// Failure to remove the old private tree must not turn a valid restore into
	// a mixed files/database state; cleanup can safely be retried later.
	_ = staged.Commit()
	return nil
}

func (a *Agent) restoreDatabasePackage(scope backupScope, targetDir, safetyPath string, database backupspec.DatabaseIdentity) error {
	safetyDir := filepath.Join(filepath.Dir(targetDir), "safety")
	safetyManifest, err := unpackBackupPackage(safetyPath, safetyDir, scope)
	if err != nil {
		return err
	}
	if safetyManifest.Type != backupspec.TypeDatabase || len(safetyManifest.Databases) != 1 {
		return errors.New("safety database backup is invalid")
	}
	if _, err := validateRestoreDatabase(safetyManifest.Databases[0].Identity, database); err != nil {
		return err
	}
	targetPayload := filepath.Join(targetDir, filepath.FromSlash(databasePayloadName(database.ID)))
	safetyPayload := filepath.Join(safetyDir, filepath.FromSlash(databasePayloadName(database.ID)))
	if err := restoreDatabaseFromFile(database, targetPayload); err != nil {
		rollbackErr := restoreDatabaseFromFile(database, safetyPayload)
		return restoreRollbackError(err, rollbackErr)
	}
	return nil
}

func (a *Agent) restoreFullPackage(scope backupScope, targetDir, safetyPath, docroot string, manifest backupManifest, databases []backupspec.DatabaseIdentity) error {
	safetyDir := filepath.Join(filepath.Dir(targetDir), "safety")
	safetyManifest, err := unpackBackupPackage(safetyPath, safetyDir, scope)
	if err != nil {
		return err
	}
	if safetyManifest.Type != backupspec.TypeFull {
		return errors.New("safety full backup is invalid")
	}
	if _, err := validateFullRestoreSet(safetyManifest.databaseIdentities(), databases); err != nil {
		return err
	}
	filesPayload := filepath.Join(targetDir, filepath.FromSlash(manifest.Files.Name))
	staged, err := prepareFilesRestore(filesPayload, docroot)
	if err != nil {
		return err
	}
	defer staged.Cleanup()

	attempted := make([]backupspec.DatabaseIdentity, 0, len(databases))
	for _, database := range databases {
		attempted = append(attempted, database)
		targetPayload := filepath.Join(targetDir, filepath.FromSlash(databasePayloadName(database.ID)))
		if err := restoreDatabaseFromFile(database, targetPayload); err != nil {
			rollbackErr := rollbackDatabases(attempted, safetyDir)
			return restoreRollbackError(err, rollbackErr)
		}
	}
	if err := staged.Publish(); err != nil {
		rollbackErr := rollbackDatabases(attempted, safetyDir)
		return restoreRollbackError(err, rollbackErr)
	}
	_ = staged.Commit()
	return nil
}

func rollbackDatabases(databases []backupspec.DatabaseIdentity, safetyDir string) error {
	errorsFound := make([]error, 0)
	for i := len(databases) - 1; i >= 0; i-- {
		database := databases[i]
		payload := filepath.Join(safetyDir, filepath.FromSlash(databasePayloadName(database.ID)))
		if err := restoreDatabaseFromFile(database, payload); err != nil {
			errorsFound = append(errorsFound, fmt.Errorf("database %d rollback: %w", database.ID, err))
		}
	}
	return errors.Join(errorsFound...)
}

func restoreRollbackError(restoreErr, rollbackErr error) error {
	if rollbackErr == nil {
		return fmt.Errorf("restore failed and safety backup was reapplied: %w", restoreErr)
	}
	return fmt.Errorf("restore failed: %v; safety rollback also failed: %w", restoreErr, rollbackErr)
}
