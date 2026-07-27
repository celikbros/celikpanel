package main

import (
	"errors"
	"os"

	"github.com/alicelik/celikpanel/internal/backupspec"
	"github.com/alicelik/celikpanel/internal/hostingpath"
)

var backupBaseDir = func() string {
	if dir := os.Getenv("CELIKPANEL_BACKUP_DIR"); dir != "" {
		return dir
	}
	return "/var/backups/celikpanel"
}()

var backupDocumentRoot = hostingpath.DocumentRoot

type backupScope struct {
	ProtocolVersion int
	SubscriptionID  int
	DomainID        int
	DomainName      string
}

func createScope(r *backupspec.CreateRequest) backupScope {
	return backupScope{r.ProtocolVersion, r.SubscriptionID, r.DomainID, r.DomainName}
}

func listScope(r *backupspec.ListRequest) backupScope {
	return backupScope{r.ProtocolVersion, r.SubscriptionID, r.DomainID, r.DomainName}
}

func inspectScope(r *backupspec.InspectRequest) backupScope {
	return backupScope{r.ProtocolVersion, r.SubscriptionID, r.DomainID, r.DomainName}
}

func restoreScope(r *backupspec.RestoreRequest) backupScope {
	return backupScope{r.ProtocolVersion, r.SubscriptionID, r.DomainID, r.DomainName}
}

func deleteScope(r *backupspec.DeleteRequest) backupScope {
	return backupScope{r.ProtocolVersion, r.SubscriptionID, r.DomainID, r.DomainName}
}

func readScope(r *backupspec.ReadChunkRequest) backupScope {
	return backupScope{r.ProtocolVersion, r.SubscriptionID, r.DomainID, r.DomainName}
}

func validateV2Scope(s backupScope) error {
	return validateScopeIDs(s.ProtocolVersion, s.SubscriptionID, s.DomainID)
}

func (a *Agent) CreateBackup(req *backupspec.CreateRequest, resp *backupspec.CreateResponse) error {
	info, err := a.createBackup(req)
	if err != nil {
		resp.Error = err.Error()
		return nil
	}
	resp.Success = true
	resp.Backup = info
	return nil
}

func (a *Agent) InspectBackup(req *backupspec.InspectRequest, resp *backupspec.InspectResponse) error {
	info, databases, err := inspectBackup(inspectScope(req), req.BackupName)
	if err != nil {
		resp.Error = err.Error()
		return nil
	}
	resp.Success = true
	resp.Backup = info
	resp.Databases = databases
	return nil
}

func (a *Agent) RestoreBackup(req *backupspec.RestoreRequest, resp *backupspec.RestoreResponse) error {
	if err := a.restoreBackup(req, resp); err != nil {
		resp.Error = err.Error()
		resp.Success = false
	}
	return nil
}

func (a *Agent) DeleteBackup(req *backupspec.DeleteRequest, resp *bool) error {
	filePath, _, err := resolveBackup(deleteScope(req), req.BackupName)
	if err != nil {
		return err
	}
	if err := secureRemoveRegular(filePath); err != nil {
		return err
	}
	*resp = true
	return nil
}

func invalidBackupName() error {
	return errors.New("invalid backup name")
}
