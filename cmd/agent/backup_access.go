package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/alicelik/celikpanel/internal/backupspec"
)

func resolveBackup(scope backupScope, name string) (string, bool, error) {
	if err := validateV2Scope(scope); err != nil {
		return "", false, err
	}
	if !validBackupName(name) {
		return "", false, invalidBackupName()
	}
	if generatedBackupName.MatchString(name) {
		dir, err := scopeBackupDir(scope)
		if err != nil {
			return "", false, err
		}
		if err := rejectSymlinkPath(dir); err != nil {
			return "", false, err
		}
		filePath := filepath.Join(dir, name)
		file, _, err := secureOpenRegular(filePath)
		if err != nil {
			return "", false, err
		}
		if err := file.Close(); err != nil {
			return "", false, err
		}
		return filePath, false, nil
	}
	dir, err := legacyBackupDir(scope)
	if err != nil {
		return "", false, err
	}
	if err := rejectSymlinkPath(dir); err != nil {
		return "", false, err
	}
	filePath := filepath.Join(dir, name)
	file, _, err := secureOpenRegular(filePath)
	if err != nil {
		return "", false, err
	}
	if err := file.Close(); err != nil {
		return "", false, err
	}
	return filePath, true, nil
}

func (a *Agent) ListBackups(req *backupspec.ListRequest, resp *backupspec.ListResponse) error {
	scope := listScope(req)
	if err := validateV2Scope(scope); err != nil {
		return err
	}
	resp.Backups = make([]backupspec.Info, 0)
	if dir, err := scopeBackupDir(scope); err == nil {
		items, readErr := listBackupDir(scope, dir, false)
		if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
			return readErr
		}
		resp.Backups = append(resp.Backups, items...)
	}
	if dir, err := legacyBackupDir(scope); err == nil {
		items, readErr := listBackupDir(scope, dir, true)
		if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
			return readErr
		}
		resp.Backups = append(resp.Backups, items...)
	}
	sort.Slice(resp.Backups, func(i, j int) bool {
		return resp.Backups[i].CreatedAt.After(resp.Backups[j].CreatedAt)
	})
	return nil
}

func listBackupDir(scope backupScope, dir string, legacy bool) ([]backupspec.Info, error) {
	if err := rejectSymlinkPath(dir); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	result := make([]backupspec.Info, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		filePath := filepath.Join(dir, entry.Name())
		var info backupspec.Info
		if legacy {
			info, err = inspectLegacyBackup(filePath, entry.Name())
		} else {
			info, _, err = inspectV2Backup(scope, filePath, entry.Name())
		}
		if err == nil {
			info.Path = filePath
			result = append(result, info)
		}
	}
	return result, nil
}

func inspectBackup(scope backupScope, name string) (backupspec.Info, []backupspec.DatabaseIdentity, error) {
	filePath, legacy, err := resolveBackup(scope, name)
	if err != nil {
		return backupspec.Info{}, nil, err
	}
	if legacy {
		info, err := inspectLegacyBackup(filePath, name)
		info.Path = filePath
		return info, nil, err
	}
	info, manifest, err := inspectV2Backup(scope, filePath, name)
	info.Path = filePath
	return info, manifest.databaseIdentities(), err
}

func inspectV2Backup(scope backupScope, filePath, name string) (backupspec.Info, backupManifest, error) {
	if !generatedBackupName.MatchString(name) {
		return backupspec.Info{}, backupManifest{}, invalidBackupName()
	}
	file, stat, err := secureOpenRegular(filePath)
	if err != nil {
		return backupspec.Info{}, backupManifest{}, err
	}
	if err := file.Close(); err != nil {
		return backupspec.Info{}, backupManifest{}, err
	}
	manifest, err := readBackupManifest(filePath)
	if err != nil {
		return backupspec.Info{}, backupManifest{}, err
	}
	if err := validateManifest(manifest, scope); err != nil {
		return backupspec.Info{}, backupManifest{}, err
	}
	return manifest.info(name, stat.Size(), false), manifest, nil
}

func inspectLegacyBackup(filePath, name string) (backupspec.Info, error) {
	file, stat, err := secureOpenRegular(filePath)
	if err != nil {
		return backupspec.Info{}, err
	}
	if err := file.Close(); err != nil {
		return backupspec.Info{}, err
	}
	info := backupspec.Info{
		Name: name, Path: filePath, Size: stat.Size(), Origin: backupspec.OriginManual,
		Legacy: true, CreatedAt: stat.ModTime(),
	}
	if strings.HasPrefix(name, "files_") && legacyFilesName.MatchString(name) {
		info.Type, info.Restorable = backupspec.TypeFiles, true
		return info, nil
	}
	if strings.HasPrefix(name, "full_") && legacyFilesName.MatchString(name) {
		// Legacy full archives never carried a verifiable database manifest.
		// They remain restorable, explicitly as file-only archives.
		info.Type, info.Restorable = backupspec.TypeFull, true
		return info, nil
	}
	if legacyDatabaseName.MatchString(name) {
		info.Type = backupspec.TypeDatabase
		info.Restorable = false
		return info, nil
	}
	return backupspec.Info{}, invalidBackupName()
}

func (a *Agent) ReadBackupChunk(req *backupspec.ReadChunkRequest, resp *backupspec.ReadChunkResponse) error {
	if req.Offset < 0 {
		return errors.New("negative offset")
	}
	maxBytes := req.MaxBytes
	if maxBytes <= 0 || maxBytes > backupspec.MaxChunkBytes {
		maxBytes = backupspec.MaxChunkBytes
	}
	filePath, _, err := resolveBackup(readScope(req), req.BackupName)
	if err != nil {
		return err
	}
	file, info, err := secureOpenRegular(filePath)
	if err != nil {
		return err
	}
	defer file.Close()
	if req.Offset > info.Size() {
		return errors.New("offset past end")
	}
	data := make([]byte, maxBytes)
	n, readErr := file.ReadAt(data, req.Offset)
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return readErr
	}
	resp.Data = data[:n]
	resp.Offset = req.Offset + int64(n)
	resp.Size = info.Size()
	resp.EOF = resp.Offset >= info.Size()
	return nil
}
