package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/alicelik/celikpanel/internal/backupspec"
)

func (a *Agent) createBackup(req *backupspec.CreateRequest) (backupspec.Info, error) {
	scope := createScope(req)
	if err := validateV2Scope(scope); err != nil {
		return backupspec.Info{}, err
	}
	origin := req.Origin
	if origin == "" {
		origin = backupspec.OriginManual
	}
	if !validBackupOrigin(origin) {
		return backupspec.Info{}, errors.New("invalid backup origin")
	}
	backupDir, err := ensureBackupDir(scope)
	if err != nil {
		return backupspec.Info{}, err
	}
	workDir, err := os.MkdirTemp(backupDir, ".build-")
	if err != nil {
		return backupspec.Info{}, err
	}
	if err := os.Chmod(workDir, 0o700); err != nil {
		_ = os.RemoveAll(workDir)
		return backupspec.Info{}, err
	}
	defer os.RemoveAll(workDir)

	manifest := backupManifest{
		Version:        backupspec.ProtocolVersion,
		Type:           req.Type,
		Origin:         origin,
		SubscriptionID: scope.SubscriptionID,
		DomainID:       scope.DomainID,
		CreatedAt:      backupNow().UTC(),
	}
	switch req.Type {
	case backupspec.TypeFiles:
		if err := buildFilesPayload(scope, workDir, &manifest); err != nil {
			return backupspec.Info{}, err
		}
	case backupspec.TypeDatabase:
		database, err := databaseFromCreateRequest(req)
		if err != nil {
			return backupspec.Info{}, err
		}
		if err := buildDatabasePayload(workDir, database, &manifest); err != nil {
			return backupspec.Info{}, err
		}
	case backupspec.TypeFull:
		databases, err := validateDatabaseSet(req.Databases)
		if err != nil {
			return backupspec.Info{}, err
		}
		if err := buildFilesPayload(scope, workDir, &manifest); err != nil {
			return backupspec.Info{}, err
		}
		for _, database := range databases {
			if err := buildDatabasePayload(workDir, database, &manifest); err != nil {
				return backupspec.Info{}, err
			}
		}
	default:
		return backupspec.Info{}, errors.New("invalid backup type")
	}
	if err := validateManifest(manifest, scope); err != nil {
		return backupspec.Info{}, err
	}
	name, err := newBackupName(req.Type, manifestDatabaseID(manifest))
	if err != nil {
		return backupspec.Info{}, err
	}
	finalPath := filepath.Join(backupDir, name)
	if err := publishBackupPackage(finalPath, workDir, manifest); err != nil {
		return backupspec.Info{}, err
	}
	info, err := os.Stat(finalPath)
	if err != nil {
		return backupspec.Info{}, err
	}
	result := manifest.info(name, info.Size(), false)
	result.Path = finalPath
	return result, nil
}

func buildFilesPayload(scope backupScope, workDir string, manifest *backupManifest) error {
	docroot, err := backupDocumentRoot(scope.SubscriptionID, scope.DomainID)
	if err != nil {
		return err
	}
	payloadPath := filepath.Join(workDir, filepath.FromSlash(filesPayloadName))
	if err := mkdirPrivate(filepath.Dir(payloadPath)); err != nil {
		return err
	}
	if err := safeCreateFilesArchive(docroot, payloadPath); err != nil {
		return err
	}
	manifest.Files, err = describePayload(payloadPath, filesPayloadName)
	return err
}

func buildDatabasePayload(workDir string, database backupspec.DatabaseIdentity, manifest *backupManifest) error {
	database, err := validateDatabaseIdentity(database)
	if err != nil {
		return err
	}
	payloadName := databasePayloadName(database.ID)
	payloadPath := filepath.Join(workDir, filepath.FromSlash(payloadName))
	if err := mkdirPrivate(filepath.Dir(payloadPath)); err != nil {
		return err
	}
	if err := dumpDatabaseToFile(database, payloadPath); err != nil {
		return fmt.Errorf("database %d dump: %w", database.ID, err)
	}
	payload, err := describePayload(payloadPath, payloadName)
	if err != nil {
		return err
	}
	manifest.Databases = append(manifest.Databases, manifestDatabase{Identity: database, Payload: payload})
	return nil
}

func databaseFromCreateRequest(req *backupspec.CreateRequest) (backupspec.DatabaseIdentity, error) {
	database := req.Database
	if database.Name == "" && req.ProtocolVersion != backupspec.ProtocolVersion {
		database.Name = req.DatabaseName
		database.Type = req.DatabaseType
	}
	return validateDatabaseIdentity(database)
}
