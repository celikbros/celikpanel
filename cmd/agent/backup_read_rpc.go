package main

import (
	"encoding/base64"
	"fmt"
)

// ReadBackupRequest addresses a backup only by immutable tenant identities
// plus one validated leaf name. It cannot carry an absolute filesystem path.
type ReadBackupRequest struct {
	SubscriptionID int    `json:"subscription_id"`
	DomainID       int    `json:"domain_id"`
	BackupName     string `json:"backup_name"`
}

func (a *Agent) ReadBackup(req *ReadBackupRequest, resp *ReadFileResponse) error {
	scope, err := backupScope(req.SubscriptionID, req.DomainID)
	if err != nil {
		return err
	}
	if _, _, _, err := parseBackupName(req.BackupName); err != nil {
		return fmt.Errorf("invalid backup name: %w", err)
	}
	content, size, err := secureReadBackupFile(
		backupBaseDir, scope, req.BackupName, maxBackupDownloadBytes,
	)
	if err != nil {
		return err
	}
	// The path is a logical leaf, never the backup server's absolute path.
	resp.Path = req.BackupName
	resp.Size = size
	resp.IsBinary = true
	resp.Content = base64.StdEncoding.EncodeToString(content)
	return nil
}
