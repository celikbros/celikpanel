package main

import (
	"errors"
	"fmt"
	"sort"
)

const (
	fullBackupManifestVersion = 1
	fullBackupManifestEntry   = "celikpanel-manifest.json"
	fullBackupFilesPrefix     = "files"
)

// BackupDatabaseIdentity is resolved by the unprivileged panel from durable
// tenant metadata. The root agent validates it again before invoking a dump or
// restore command; no database name comes directly from the browser.
type BackupDatabaseIdentity struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

type fullBackupDatabase struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Type  string `json:"type"`
	Entry string `json:"entry"`
}

type fullBackupManifest struct {
	Version     int                  `json:"version"`
	Type        string               `json:"type"`
	FilesPrefix string               `json:"files_prefix"`
	Databases   []fullBackupDatabase `json:"databases"`
}

func fullBackupDatabaseEntry(databaseType string, databaseID int) string {
	return fmt.Sprintf("databases/%s_%d.sql", databaseType, databaseID)
}

func normalizeFullBackupDatabases(
	input []BackupDatabaseIdentity,
) ([]BackupDatabaseIdentity, error) {
	if len(input) == 0 {
		return nil, errors.New("full backup requires at least one selected database")
	}
	normalized := make([]BackupDatabaseIdentity, len(input))
	seen := make(map[int]struct{}, len(input))
	for i, database := range input {
		databaseType, err := validateDatabaseIdentity(
			database.ID, database.Name, database.Type,
		)
		if err != nil {
			return nil, fmt.Errorf("database %d: %w", database.ID, err)
		}
		if _, duplicate := seen[database.ID]; duplicate {
			return nil, fmt.Errorf("database %d is selected more than once", database.ID)
		}
		seen[database.ID] = struct{}{}
		database.Type = databaseType
		normalized[i] = database
	}
	sort.Slice(normalized, func(i, j int) bool {
		return normalized[i].ID < normalized[j].ID
	})
	return normalized, nil
}

func newFullBackupManifest(
	databases []BackupDatabaseIdentity,
) fullBackupManifest {
	manifest := fullBackupManifest{
		Version:     fullBackupManifestVersion,
		Type:        "full",
		FilesPrefix: fullBackupFilesPrefix,
		Databases:   make([]fullBackupDatabase, 0, len(databases)),
	}
	for _, database := range databases {
		manifest.Databases = append(manifest.Databases, fullBackupDatabase{
			ID:    database.ID,
			Name:  database.Name,
			Type:  database.Type,
			Entry: fullBackupDatabaseEntry(database.Type, database.ID),
		})
	}
	return manifest
}

func validateFullBackupManifest(
	manifest fullBackupManifest,
) ([]BackupDatabaseIdentity, error) {
	if manifest.Version != fullBackupManifestVersion ||
		manifest.Type != "full" ||
		manifest.FilesPrefix != fullBackupFilesPrefix {
		return nil, errors.New("unsupported full backup manifest")
	}
	databases := make([]BackupDatabaseIdentity, 0, len(manifest.Databases))
	for _, database := range manifest.Databases {
		databases = append(databases, BackupDatabaseIdentity{
			ID: database.ID, Name: database.Name, Type: database.Type,
		})
	}
	normalized, err := normalizeFullBackupDatabases(databases)
	if err != nil {
		return nil, err
	}
	if len(normalized) != len(manifest.Databases) {
		return nil, errors.New("full backup manifest database count is inconsistent")
	}
	for i, database := range normalized {
		entry := fullBackupDatabaseEntry(database.Type, database.ID)
		if manifest.Databases[i].ID != database.ID ||
			manifest.Databases[i].Name != database.Name ||
			manifest.Databases[i].Type != database.Type ||
			manifest.Databases[i].Entry != entry {
			return nil, errors.New("full backup manifest database identity is inconsistent")
		}
	}
	return normalized, nil
}

func fullBackupDatabaseIDs(databases []BackupDatabaseIdentity) []int {
	ids := make([]int, len(databases))
	for i, database := range databases {
		ids[i] = database.ID
	}
	return ids
}

func sameFullBackupDatabases(a, b []BackupDatabaseIdentity) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
