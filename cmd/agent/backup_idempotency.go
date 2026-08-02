package main

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/alicelik/celikpanel/internal/backupspec"
)

const backupJobLockStripeCount = 64

var backupJobLockStripes [backupJobLockStripeCount]sync.Mutex

type backupCreateIdentity struct {
	scope     backupScope
	typeName  string
	origin    string
	jobKey    string
	databases []backupspec.DatabaseIdentity
}

func normalizeBackupCreateRequest(req *backupspec.CreateRequest) (backupCreateIdentity, error) {
	if req == nil {
		return backupCreateIdentity{}, errors.New("backup create request is required")
	}
	identity := backupCreateIdentity{
		scope:    createScope(req),
		typeName: req.Type,
		origin:   req.Origin,
		jobKey:   req.JobKey,
	}
	if err := validateV2Scope(identity.scope); err != nil {
		return backupCreateIdentity{}, err
	}
	if identity.origin == "" {
		identity.origin = backupspec.OriginManual
	}
	if !validBackupOrigin(identity.origin) {
		return backupCreateIdentity{}, errors.New("invalid backup origin")
	}
	if identity.jobKey != "" && !backupspec.ValidJobKey(identity.jobKey) {
		return backupCreateIdentity{}, errors.New("invalid backup job key")
	}
	if identity.origin == backupspec.OriginScheduled && identity.jobKey == "" {
		return backupCreateIdentity{}, errors.New("scheduled backup job key is required")
	}

	switch identity.typeName {
	case backupspec.TypeFiles:
	case backupspec.TypeDatabase:
		database, err := databaseFromCreateRequest(req)
		if err != nil {
			return backupCreateIdentity{}, err
		}
		identity.databases = []backupspec.DatabaseIdentity{database}
	case backupspec.TypeFull:
		databases, err := validateDatabaseSet(req.Databases)
		if err != nil {
			return backupCreateIdentity{}, err
		}
		identity.databases = databases
	default:
		return backupCreateIdentity{}, errors.New("invalid backup type")
	}
	return identity, nil
}

func backupJobMutex(identity backupCreateIdentity) *sync.Mutex {
	material := strconv.Itoa(identity.scope.SubscriptionID) + "\x00" +
		strconv.Itoa(identity.scope.DomainID) + "\x00" + identity.jobKey
	hash := sha256.Sum256([]byte(material))
	return &backupJobLockStripes[int(hash[0])%len(backupJobLockStripes)]
}

func findPublishedBackupForJob(identity backupCreateIdentity) (backupspec.Info, bool, error) {
	dir, err := scopeBackupDir(identity.scope)
	if err != nil {
		return backupspec.Info{}, false, err
	}
	if err := rejectSymlinkPath(dir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return backupspec.Info{}, false, nil
		}
		return backupspec.Info{}, false, err
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return backupspec.Info{}, false, nil
	}
	if err != nil {
		return backupspec.Info{}, false, err
	}

	var found backupspec.Info
	for _, entry := range entries {
		if entry.IsDir() || !generatedBackupName.MatchString(entry.Name()) {
			continue
		}
		filePath := filepath.Join(dir, entry.Name())
		manifest, readErr := readBackupManifest(filePath)
		if readErr != nil {
			return backupspec.Info{}, false, fmt.Errorf(
				"inspect published backup candidate %q: %w", entry.Name(), readErr,
			)
		}
		if manifest.JobKey != identity.jobKey {
			continue
		}
		info, checked, inspectErr := inspectV2Backup(identity.scope, filePath, entry.Name())
		if inspectErr != nil {
			return backupspec.Info{}, false, fmt.Errorf("published backup for job key is invalid: %w", inspectErr)
		}
		if !backupManifestMatchesIdentity(checked, identity) {
			return backupspec.Info{}, false, errors.New("backup job key conflicts with a published backup")
		}
		if found.Name != "" {
			return backupspec.Info{}, false, errors.New("backup job key has multiple published backups")
		}
		info.Path = filePath
		found = info
	}
	return found, found.Name != "", nil
}

func backupManifestMatchesIdentity(manifest backupManifest, identity backupCreateIdentity) bool {
	if manifest.JobKey != identity.jobKey || manifest.Type != identity.typeName ||
		manifest.Origin != identity.origin ||
		manifest.SubscriptionID != identity.scope.SubscriptionID ||
		manifest.DomainID != identity.scope.DomainID ||
		len(manifest.Databases) != len(identity.databases) {
		return false
	}
	for i, database := range identity.databases {
		if manifest.Databases[i].Identity != database {
			return false
		}
	}
	return true
}
