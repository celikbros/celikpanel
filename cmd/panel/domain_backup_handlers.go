package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"

	"github.com/alicelik/celikpanel/internal/backupspec"
)

const maxBackupRequestBody = 64 << 10

type backupDomain struct {
	ID, SubscriptionID int
	Name, DocumentRoot string
}

type createBackupHTTPBody struct {
	Type       string `json:"type"`
	DatabaseID int    `json:"database_id,omitempty"`
}

func backupDomainIDFromPath(requestPath string) (int, error) {
	parts := strings.Split(requestPath, "/")
	if len(parts) < 5 {
		return 0, errors.New("invalid path")
	}
	id, err := strconv.Atoi(parts[4])
	if err != nil || id < 1 {
		return 0, errors.New("invalid domain ID")
	}
	return id, nil
}

// loadBackupDomain resolves immutable tenant identity and the rollback-only
// document root in one targeted query. A v2 agent derives paths from IDs.
func (p *Panel) loadBackupDomain(ctx context.Context, domainID int) (backupDomain, error) {
	var d backupDomain
	err := p.db.GetDB().QueryRowContext(ctx, `
		SELECT d.id, d.subscription_id, d.name,
		       COALESCE((SELECT s.document_root FROM sites s
		                 WHERE s.domain_id=d.id ORDER BY s.id LIMIT 1), '')
		FROM domains d WHERE d.id=?`, domainID,
	).Scan(&d.ID, &d.SubscriptionID, &d.Name, &d.DocumentRoot)
	if err != nil {
		return backupDomain{}, err
	}
	if strings.TrimSpace(d.DocumentRoot) == "" {
		d.DocumentRoot = path.Join("/var/www", d.Name)
	}
	return d, nil
}

// databaseIdentity authorizes only through databases_v2 -> database server ->
// server type, with domain and subscription ownership checked together.
func (p *Panel) databaseIdentity(ctx context.Context, d backupDomain, databaseID int) (backupspec.DatabaseIdentity, error) {
	var out backupspec.DatabaseIdentity
	err := p.db.GetDB().QueryRowContext(ctx, `
		SELECT db.id, db.name, dst.name
		FROM databases_v2 db
		JOIN database_servers ds ON ds.id=db.server_id
		JOIN database_server_types dst ON dst.id=ds.type_id
		WHERE db.id=? AND db.domain_id=? AND db.subscription_id=?
		  AND ds.subscription_id=?`, databaseID, d.ID, d.SubscriptionID, d.SubscriptionID,
	).Scan(&out.ID, &out.Name, &out.Type)
	return out, err
}

func (p *Panel) domainDatabaseIdentities(ctx context.Context, d backupDomain) ([]backupspec.DatabaseIdentity, error) {
	rows, err := p.db.GetDB().QueryContext(ctx, `
		SELECT db.id, db.name, dst.name
		FROM databases_v2 db
		JOIN database_servers ds ON ds.id=db.server_id
		JOIN database_server_types dst ON dst.id=ds.type_id
		WHERE db.domain_id=? AND db.subscription_id=? AND ds.subscription_id=?
		ORDER BY db.id`, d.ID, d.SubscriptionID, d.SubscriptionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	identities := make([]backupspec.DatabaseIdentity, 0)
	for rows.Next() {
		var identity backupspec.DatabaseIdentity
		if err := rows.Scan(&identity.ID, &identity.Name, &identity.Type); err != nil {
			return nil, err
		}
		identities = append(identities, identity)
	}
	return identities, rows.Err()
}

func decodeBackupJSON(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBackupRequestBody)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func (p *Panel) handleDomainBackups(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}
	domainID, err := backupDomainIDFromPath(r.URL.Path)
	if err != nil {
		writeClientError(w, http.StatusBadRequest, "invalid domain ID")
		return
	}
	d, err := p.loadBackupDomain(r.Context(), domainID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeClientError(w, http.StatusNotFound, "domain not found")
		} else {
			writeServerError(w, err)
		}
		return
	}
	switch r.Method {
	case http.MethodGet:
		p.handleListBackups(w, r, d)
	case http.MethodPost:
		p.handleCreateBackup(w, r, d)
	case http.MethodDelete:
		p.handleDeleteBackup(w, r, d)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (p *Panel) handleListBackups(w http.ResponseWriter, r *http.Request, d backupDomain) {
	req := backupspec.ListRequest{
		ProtocolVersion: backupspec.ProtocolVersion, SubscriptionID: d.SubscriptionID,
		DomainID: d.ID, DomainName: d.Name,
	}
	var resp backupspec.ListResponse
	if err := p.agentClient.CallContext(r.Context(), "Agent.ListBackups", &req, &resp); err != nil {
		writeServerError(w, err)
		return
	}
	_ = json.NewEncoder(w).Encode(resp)
}

func (p *Panel) handleCreateBackup(w http.ResponseWriter, r *http.Request, d backupDomain) {
	var body createBackupHTTPBody
	if err := decodeBackupJSON(w, r, &body); err != nil {
		p.auditBackupFailure(r, "create", "", d.ID, "invalid request body")
		writeClientError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req := backupspec.CreateRequest{
		ProtocolVersion: backupspec.ProtocolVersion, SubscriptionID: d.SubscriptionID,
		DomainID: d.ID, DomainName: d.Name, Type: body.Type,
		Origin: backupspec.OriginManual, SourceDir: d.DocumentRoot,
	}
	switch body.Type {
	case backupspec.TypeFiles:
	case backupspec.TypeDatabase:
		if body.DatabaseID < 1 {
			p.auditBackupFailure(r, "create", body.Type, d.ID, "database_id is required")
			writeClientError(w, http.StatusBadRequest, "database_id is required")
			return
		}
		identity, err := p.databaseIdentity(r.Context(), d, body.DatabaseID)
		if err != nil {
			p.auditBackupFailure(r, "create", body.Type, d.ID, "database lookup failed")
			if errors.Is(err, sql.ErrNoRows) {
				writeClientError(w, http.StatusNotFound, "database not found")
			} else {
				writeServerError(w, err)
			}
			return
		}
		req.Database = identity
		req.DatabaseName, req.DatabaseType = identity.Name, identity.Type
	case backupspec.TypeFull:
		identities, err := p.domainDatabaseIdentities(r.Context(), d)
		if err != nil {
			p.auditBackupFailure(r, "create", body.Type, d.ID, "database lookup failed")
			writeServerError(w, err)
			return
		}
		req.Databases = identities
	default:
		p.auditBackupFailure(r, "create", body.Type, d.ID, "unsupported backup type")
		writeClientError(w, http.StatusBadRequest, "type must be files, database or full")
		return
	}
	var resp backupspec.CreateResponse
	if err := p.agentClient.CallContext(r.Context(), "Agent.CreateBackup", &req, &resp); err != nil {
		p.auditBackupFailure(r, "create", body.Type, d.ID, err.Error())
		writeServerError(w, err)
		return
	}
	if !resp.Success || resp.Error != "" {
		p.auditBackupFailure(r, "create", body.Type, d.ID, resp.Error)
		writeAgentError(w, nil, resp.Error)
		return
	}
	p.audit(r, "backup.create:"+body.Type, "domain", d.ID)
	_ = json.NewEncoder(w).Encode(resp)
}

func (p *Panel) handleDeleteBackup(w http.ResponseWriter, r *http.Request, d backupDomain) {
	name := r.URL.Query().Get("name")
	if !validBackupName(name) {
		p.auditBackupFailure(r, "delete", "", d.ID, "invalid backup name")
		writeClientError(w, http.StatusBadRequest, "invalid backup name")
		return
	}
	req := backupspec.DeleteRequest{
		ProtocolVersion: backupspec.ProtocolVersion, SubscriptionID: d.SubscriptionID,
		DomainID: d.ID, DomainName: d.Name, BackupName: name,
	}
	var success bool
	if err := p.agentClient.CallContext(r.Context(), "Agent.DeleteBackup", &req, &success); err != nil {
		p.auditBackupFailure(r, "delete", "", d.ID, err.Error())
		writeServerError(w, err)
		return
	}
	if !success {
		p.auditBackupFailure(r, "delete", "", d.ID, "agent refused deletion")
		writeAgentError(w, nil, "backup deletion was not completed")
		return
	}
	p.audit(r, "backup.delete", "domain", d.ID)
	_ = json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

func (p *Panel) handleRestoreBackup(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	domainID, err := backupDomainIDFromPath(r.URL.Path)
	if err != nil {
		writeClientError(w, http.StatusBadRequest, "invalid domain ID")
		return
	}
	d, err := p.loadBackupDomain(r.Context(), domainID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeClientError(w, http.StatusNotFound, "domain not found")
		} else {
			writeServerError(w, err)
		}
		return
	}
	var body struct {
		BackupName string `json:"backup_name"`
	}
	if err := decodeBackupJSON(w, r, &body); err != nil || !validBackupName(body.BackupName) {
		p.auditBackupFailure(r, "restore", "", d.ID, "invalid request body or backup name")
		writeClientError(w, http.StatusBadRequest, "invalid request body or backup name")
		return
	}
	inspectReq := backupspec.InspectRequest{
		ProtocolVersion: backupspec.ProtocolVersion, SubscriptionID: d.SubscriptionID,
		DomainID: d.ID, DomainName: d.Name, BackupName: body.BackupName,
	}
	var inspection backupspec.InspectResponse
	if err := p.agentClient.CallContext(r.Context(), "Agent.InspectBackup", &inspectReq, &inspection); err != nil {
		p.auditBackupFailure(r, "restore", "", d.ID, err.Error())
		writeServerError(w, err)
		return
	}
	if !inspection.Success || inspection.Error != "" {
		p.auditBackupFailure(r, "restore", inspection.Backup.Type, d.ID, inspection.Error)
		writeAgentError(w, nil, inspection.Error)
		return
	}
	if !inspection.Backup.Restorable {
		p.auditBackupFailure(r, "restore", inspection.Backup.Type, d.ID, "unrestorable backup")
		writeClientError(w, http.StatusConflict, "this backup cannot be restored safely")
		return
	}
	restoreReq := backupspec.RestoreRequest{
		ProtocolVersion: backupspec.ProtocolVersion, SubscriptionID: d.SubscriptionID,
		DomainID: d.ID, DomainName: d.Name, BackupName: body.BackupName, TargetDir: d.DocumentRoot,
	}
	if err := p.authorizeRestoreDatabases(r.Context(), d, inspection, &restoreReq); err != nil {
		p.auditBackupFailure(r, "restore", inspection.Backup.Type, d.ID, err.Error())
		switch {
		case errors.Is(err, sql.ErrNoRows):
			writeClientError(w, http.StatusConflict, "backup database is no longer authorized for this domain")
		case errors.Is(err, errUnsafeBackupMetadata):
			writeClientError(w, http.StatusConflict, "backup metadata is incomplete or unsafe")
		default:
			writeServerError(w, err)
		}
		return
	}
	var resp backupspec.RestoreResponse
	if err := p.agentClient.CallContext(r.Context(), "Agent.RestoreBackup", &restoreReq, &resp); err != nil {
		p.auditBackupFailure(r, "restore", inspection.Backup.Type, d.ID, err.Error())
		writeServerError(w, err)
		return
	}
	if !resp.Success || resp.Error != "" {
		p.auditBackupFailure(r, "restore", inspection.Backup.Type, d.ID, resp.Error)
		writeAgentError(w, nil, resp.Error)
		return
	}
	p.audit(r, "backup.restore:"+inspection.Backup.Type, "domain", d.ID)
	_ = json.NewEncoder(w).Encode(resp)
}

var errUnsafeBackupMetadata = errors.New("unsafe backup metadata")

// authorizeRestoreDatabases discards agent-provided names/types and resolves
// every immutable database ID against the current tenant metadata again.
func (p *Panel) authorizeRestoreDatabases(ctx context.Context, d backupDomain, inspection backupspec.InspectResponse, req *backupspec.RestoreRequest) error {
	switch inspection.Backup.Type {
	case backupspec.TypeFiles:
		if len(inspection.Databases) != 0 || inspection.Backup.DatabaseID != 0 {
			return errUnsafeBackupMetadata
		}
		return nil
	case backupspec.TypeDatabase:
		ids, err := inspectedDatabaseIDs(inspection)
		if err != nil || len(ids) != 1 {
			return errUnsafeBackupMetadata
		}
		identity, err := p.databaseIdentity(ctx, d, ids[0])
		if err == nil {
			req.Database = identity
		}
		return err
	case backupspec.TypeFull:
		ids, err := inspectedDatabaseIDs(inspection)
		if err != nil {
			return errUnsafeBackupMetadata
		}
		req.Databases = make([]backupspec.DatabaseIdentity, 0, len(ids))
		for _, id := range ids {
			identity, err := p.databaseIdentity(ctx, d, id)
			if err != nil {
				return err
			}
			req.Databases = append(req.Databases, identity)
		}
		return nil
	default:
		return errUnsafeBackupMetadata
	}
}

func inspectedDatabaseIDs(inspection backupspec.InspectResponse) ([]int, error) {
	ids := make([]int, 0, len(inspection.Databases)+1)
	seen := make(map[int]struct{})
	for _, database := range inspection.Databases {
		if database.ID < 1 {
			return nil, errUnsafeBackupMetadata
		}
		if _, duplicate := seen[database.ID]; duplicate {
			return nil, errUnsafeBackupMetadata
		}
		seen[database.ID] = struct{}{}
		ids = append(ids, database.ID)
	}
	if inspection.Backup.DatabaseID > 0 {
		if _, duplicate := seen[inspection.Backup.DatabaseID]; !duplicate {
			ids = append(ids, inspection.Backup.DatabaseID)
		}
	}
	return ids, nil
}

func validBackupName(name string) bool {
	if name == "" || name == "." || name == ".." || strings.HasPrefix(name, ".") {
		return false
	}
	return !strings.Contains(name, "/") && !strings.Contains(name, `\`) && !strings.ContainsRune(name, '\x00')
}

func (p *Panel) auditBackupFailure(r *http.Request, operation, backupType string, domainID int, reason string) {
	suffix := ""
	if backupType != "" {
		suffix = ":" + backupType
	}
	if reason == "" {
		reason = "operation failed"
	}
	p.audit(r, "backup."+operation+".failed"+suffix+" — "+auditReason(reason), "domain", domainID)
}

// handleDownloadBackup streams bounded chunks from the agent. It never asks
// for a privileged path and never materializes the whole archive/base64 copy.
func (p *Panel) handleDownloadBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	domainID, err := backupDomainIDFromPath(r.URL.Path)
	if err != nil {
		writeClientError(w, http.StatusBadRequest, "invalid domain ID")
		return
	}
	d, err := p.loadBackupDomain(r.Context(), domainID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeClientError(w, http.StatusNotFound, "domain not found")
		} else {
			writeServerError(w, err)
		}
		return
	}
	name := r.URL.Query().Get("name")
	if !validBackupName(name) {
		writeClientError(w, http.StatusBadRequest, "invalid backup name")
		return
	}
	inspectReq := backupspec.InspectRequest{
		ProtocolVersion: backupspec.ProtocolVersion, SubscriptionID: d.SubscriptionID,
		DomainID: d.ID, DomainName: d.Name, BackupName: name,
	}
	var inspection backupspec.InspectResponse
	if err := p.agentClient.CallContext(r.Context(), "Agent.InspectBackup", &inspectReq, &inspection); err != nil {
		writeServerError(w, err)
		return
	}
	if !inspection.Success || inspection.Error != "" {
		writeAgentError(w, nil, inspection.Error)
		return
	}
	first, err := p.readBackupChunk(r.Context(), d, name, 0)
	if err != nil {
		writeServerError(w, err)
		return
	}
	if err := validateBackupChunk(first, 0, inspection.Backup.Size); err != nil {
		writeServerError(w, err)
		return
	}
	disposition := mime.FormatMediaType("attachment", map[string]string{"filename": name})
	w.Header().Set("Content-Disposition", disposition)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(inspection.Backup.Size, 10))

	chunk, offset := first, int64(0)
	for {
		if len(chunk.Data) > 0 {
			if _, err := w.Write(chunk.Data); err != nil {
				return
			}
		}
		offset = chunk.Offset
		if chunk.EOF {
			return
		}
		if len(chunk.Data) == 0 {
			log.Printf("backup download stopped: empty non-EOF chunk at %d", offset)
			return
		}
		chunk, err = p.readBackupChunk(r.Context(), d, name, offset)
		if err != nil {
			log.Printf("backup download stopped at %d: %v", offset, err)
			return
		}
		if err := validateBackupChunk(chunk, offset, inspection.Backup.Size); err != nil {
			log.Printf("backup download stopped at %d: %v", offset, err)
			return
		}
	}
}

func (p *Panel) readBackupChunk(ctx context.Context, d backupDomain, name string, offset int64) (backupspec.ReadChunkResponse, error) {
	req := backupspec.ReadChunkRequest{
		ProtocolVersion: backupspec.ProtocolVersion, SubscriptionID: d.SubscriptionID,
		DomainID: d.ID, DomainName: d.Name, BackupName: name,
		Offset: offset, MaxBytes: backupspec.MaxChunkBytes,
	}
	var resp backupspec.ReadChunkResponse
	err := p.agentClient.CallContext(ctx, "Agent.ReadBackupChunk", &req, &resp)
	return resp, err
}

func validateBackupChunk(chunk backupspec.ReadChunkResponse, requestOffset, expectedSize int64) error {
	expectedNext := requestOffset + int64(len(chunk.Data))
	if chunk.Offset != expectedNext {
		return fmt.Errorf("backup chunk offset %d, want %d", chunk.Offset, expectedNext)
	}
	if len(chunk.Data) > backupspec.MaxChunkBytes {
		return fmt.Errorf("backup chunk exceeds %d bytes", backupspec.MaxChunkBytes)
	}
	if chunk.Size != expectedSize {
		return fmt.Errorf("backup size changed from %d to %d", expectedSize, chunk.Size)
	}
	if chunk.Offset > expectedSize {
		return errors.New("backup chunk exceeds declared size")
	}
	if chunk.EOF && chunk.Offset != expectedSize {
		return errors.New("backup ended before declared size")
	}
	return nil
}
