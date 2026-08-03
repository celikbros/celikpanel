package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/hostingpath"
)

const (
	maxBackupRequestBytes       = 64 << 10
	maxPanelBackupDownloadBytes = 512 << 20
)

var (
	panelFilesBackupNamePattern    = regexp.MustCompile(`^(scheduled_)?(files|full)_[0-9]{8}_[0-9]{6}\.tar\.gz$`)
	panelDatabaseBackupNamePattern = regexp.MustCompile(`^db_(mysql|postgresql)_([1-9][0-9]*)_[0-9]{8}_[0-9]{6}\.sql\.gz$`)
)

type panelBackupDomain struct {
	SubscriptionID int
	DomainID       int
	Name           string
}

type panelBackupDatabase struct {
	ID   int
	Name string
	Type string
}

type panelBackupInfo struct {
	Name       string    `json:"name"`
	Size       int64     `json:"size"`
	Type       string    `json:"type"`
	DatabaseID int       `json:"database_id,omitempty"`
	Scheduled  bool      `json:"scheduled,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

func parseBackupDomainID(urlPath string) (int, error) {
	parts := strings.Split(urlPath, "/")
	if len(parts) < 5 {
		return 0, errors.New("invalid path")
	}
	domainID, err := strconv.Atoi(parts[4])
	if err != nil || domainID <= 0 {
		return 0, errors.New("invalid domain ID")
	}
	return domainID, nil
}

func (p *Panel) lookupBackupDomain(ctx context.Context, domainID int) (panelBackupDomain, error) {
	var domain panelBackupDomain
	err := p.db.GetDB().QueryRowContext(ctx,
		`SELECT subscription_id, id, name FROM domains WHERE id = ?`,
		domainID,
	).Scan(&domain.SubscriptionID, &domain.DomainID, &domain.Name)
	if err != nil {
		return panelBackupDomain{}, err
	}
	if domain.SubscriptionID <= 0 || domain.DomainID <= 0 {
		return panelBackupDomain{}, errors.New("domain has no valid subscription identity")
	}
	return domain, nil
}

func (p *Panel) lookupBackupDatabase(
	ctx context.Context,
	domain panelBackupDomain,
	databaseID int,
) (panelBackupDatabase, error) {
	if databaseID <= 0 {
		return panelBackupDatabase{}, errors.New("database ID must be positive")
	}
	var database panelBackupDatabase
	var serverType string
	err := p.db.GetDB().QueryRowContext(ctx, `
		SELECT db.id, db.name, dst.name
		FROM databases_v2 db
		JOIN database_servers ds ON ds.id = db.server_id
		JOIN database_server_types dst ON dst.id = ds.type_id
		WHERE db.id = ?
		  AND db.domain_id = ?
		  AND db.subscription_id = ?
	`, databaseID, domain.DomainID, domain.SubscriptionID).Scan(
		&database.ID, &database.Name, &serverType,
	)
	if err != nil {
		return panelBackupDatabase{}, err
	}
	database.Type = apiTypeNameFor(serverType)
	if database.Type != "mysql" && database.Type != "postgresql" {
		return panelBackupDatabase{}, errors.New("database engine is not supported for backups")
	}
	return database, nil
}

func decodeBackupJSON(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBackupRequestBytes)
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			http.Error(w, "Request body too large", http.StatusRequestEntityTooLarge)
		} else {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
		}
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return errors.New("request body must contain one JSON object")
	}
	return nil
}

func parsePanelBackupName(name string) (backupType, databaseType string, databaseID int, err error) {
	if err := hostingpath.ValidateFileName(name); err != nil {
		return "", "", 0, err
	}
	if match := panelFilesBackupNamePattern.FindStringSubmatch(name); match != nil {
		return match[2], "", 0, nil
	}
	match := panelDatabaseBackupNamePattern.FindStringSubmatch(name)
	if match == nil {
		return "", "", 0, errors.New("unrecognized backup name")
	}
	databaseID64, parseErr := strconv.ParseInt(match[2], 10, 32)
	if parseErr != nil || databaseID64 <= 0 {
		return "", "", 0, errors.New("invalid database backup identity")
	}
	return "database", match[1], int(databaseID64), nil
}

func (p *Panel) handleDomainBackups(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}
	domainID, err := parseBackupDomainID(r.URL.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	domain, err := p.lookupBackupDomain(r.Context(), domainID)
	if err != nil {
		http.Error(w, "Domain not found", http.StatusNotFound)
		return
	}

	switch r.Method {
	case http.MethodGet:
		p.handleListBackups(w, domain)
	case http.MethodPost:
		p.handleCreateBackup(w, r, domain)
	case http.MethodDelete:
		p.handleDeleteBackup(w, r, domain)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (p *Panel) handleListBackups(w http.ResponseWriter, domain panelBackupDomain) {
	var resp struct {
		Backups []panelBackupInfo `json:"backups"`
	}
	if err := p.agentClient.Call("Agent.ListBackups", &struct {
		SubscriptionID int
		DomainID       int
	}{domain.SubscriptionID, domain.DomainID}, &resp); err != nil {
		writeServerError(w, err)
		return
	}
	for _, backup := range resp.Backups {
		backupType, _, databaseID, err := parsePanelBackupName(backup.Name)
		if err != nil || backup.Type != backupType || backup.DatabaseID != databaseID ||
			backup.Scheduled != strings.HasPrefix(backup.Name, "scheduled_") || backup.Size < 0 {
			writeServerError(w, errors.New("agent returned invalid backup metadata"))
			return
		}
	}
	_ = json.NewEncoder(w).Encode(resp)
}

func (p *Panel) handleCreateBackup(w http.ResponseWriter, r *http.Request, domain panelBackupDomain) {
	var req struct {
		Type       string `json:"type"`
		DatabaseID int    `json:"database_id,omitempty"`
	}
	if err := decodeBackupJSON(w, r, &req); err != nil {
		return
	}
	if req.Type != "files" && req.Type != "full" && req.Type != "database" {
		http.Error(w, "Invalid backup type", http.StatusBadRequest)
		return
	}

	var database panelBackupDatabase
	if req.Type == "database" {
		var err error
		database, err = p.lookupBackupDatabase(r.Context(), domain, req.DatabaseID)
		if err != nil {
			http.Error(w, "Database not found for this domain", http.StatusNotFound)
			return
		}
	} else if req.DatabaseID != 0 {
		http.Error(w, "database_id is only valid for database backups", http.StatusBadRequest)
		return
	}

	var resp struct {
		Success bool            `json:"success"`
		Backup  panelBackupInfo `json:"backup,omitempty"`
		Error   string          `json:"error,omitempty"`
	}
	if err := p.agentClient.Call("Agent.CreateBackup", &struct {
		SubscriptionID int
		DomainID       int
		Type           string
		DatabaseID     int
		DatabaseName   string
		DatabaseType   string
	}{
		SubscriptionID: domain.SubscriptionID,
		DomainID:       domain.DomainID,
		Type:           req.Type,
		DatabaseID:     database.ID,
		DatabaseName:   database.Name,
		DatabaseType:   database.Type,
	}, &resp); err != nil {
		writeServerError(w, err)
		return
	}
	_ = json.NewEncoder(w).Encode(resp)
}

func (p *Panel) handleDeleteBackup(w http.ResponseWriter, r *http.Request, domain panelBackupDomain) {
	backupName := r.URL.Query().Get("name")
	if _, _, _, err := parsePanelBackupName(backupName); err != nil {
		http.Error(w, "Invalid backup name", http.StatusBadRequest)
		return
	}
	var success bool
	if err := p.agentClient.Call("Agent.DeleteBackup", &struct {
		SubscriptionID int
		DomainID       int
		BackupName     string
	}{domain.SubscriptionID, domain.DomainID, backupName}, &success); err != nil {
		writeServerError(w, err)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]bool{"success": success})
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
	domainID, err := parseBackupDomainID(r.URL.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	domain, err := p.lookupBackupDomain(r.Context(), domainID)
	if err != nil {
		http.Error(w, "Domain not found", http.StatusNotFound)
		return
	}
	var req struct {
		BackupName string `json:"backup_name"`
	}
	if err := decodeBackupJSON(w, r, &req); err != nil {
		return
	}
	backupType, backupDatabaseType, backupDatabaseID, err := parsePanelBackupName(req.BackupName)
	if err != nil {
		http.Error(w, "Invalid backup name", http.StatusBadRequest)
		return
	}
	var database panelBackupDatabase
	if backupType == "database" {
		database, err = p.lookupBackupDatabase(r.Context(), domain, backupDatabaseID)
		if err != nil || database.Type != backupDatabaseType {
			http.Error(w, "Database backup does not belong to this domain", http.StatusForbidden)
			return
		}
	}

	var resp struct {
		Success bool   `json:"success"`
		Error   string `json:"error,omitempty"`
	}
	if err := p.agentClient.Call("Agent.RestoreBackup", &struct {
		SubscriptionID int
		DomainID       int
		BackupName     string
		DatabaseID     int
		DatabaseName   string
		DatabaseType   string
	}{
		SubscriptionID: domain.SubscriptionID,
		DomainID:       domain.DomainID,
		BackupName:     req.BackupName,
		DatabaseID:     database.ID,
		DatabaseName:   database.Name,
		DatabaseType:   database.Type,
	}, &resp); err != nil {
		writeServerError(w, err)
		return
	}
	_ = json.NewEncoder(w).Encode(resp)
}

func (p *Panel) handleDownloadBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	domainID, err := parseBackupDomainID(r.URL.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	domain, err := p.lookupBackupDomain(r.Context(), domainID)
	if err != nil {
		http.Error(w, "Domain not found", http.StatusNotFound)
		return
	}
	name := r.URL.Query().Get("name")
	if _, _, _, err := parsePanelBackupName(name); err != nil {
		http.Error(w, "Invalid backup name", http.StatusBadRequest)
		return
	}

	var list struct {
		Backups []panelBackupInfo `json:"backups"`
	}
	if err := p.agentClient.Call("Agent.ListBackups", &struct {
		SubscriptionID int
		DomainID       int
	}{domain.SubscriptionID, domain.DomainID}, &list); err != nil {
		writeServerError(w, err)
		return
	}
	found := false
	for _, backup := range list.Backups {
		if backup.Name == name {
			found = true
			break
		}
	}
	if !found {
		http.Error(w, "Backup not found", http.StatusNotFound)
		return
	}

	var resp struct {
		Path     string `json:"path"`
		Content  string `json:"content"`
		Size     int64  `json:"size"`
		IsBinary bool   `json:"is_binary"`
	}
	if err := p.agentClient.Call("Agent.ReadBackup", &struct {
		SubscriptionID int
		DomainID       int
		BackupName     string
	}{domain.SubscriptionID, domain.DomainID, name}, &resp); err != nil {
		writeServerError(w, err)
		return
	}
	if resp.Path != name || !resp.IsBinary || resp.Size < 0 ||
		resp.Size > maxPanelBackupDownloadBytes ||
		len(resp.Content) > base64.StdEncoding.EncodedLen(maxPanelBackupDownloadBytes) {
		writeServerError(w, errors.New("agent returned invalid backup content"))
		return
	}
	content, err := base64.StdEncoding.DecodeString(resp.Content)
	if err != nil || int64(len(content)) != resp.Size {
		writeServerError(w, errors.New("agent returned invalid encoded backup content"))
		return
	}
	w.Header().Set("Content-Disposition", safeAttachmentDisposition(name))
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.Itoa(len(content)))
	_, _ = w.Write(content)
}
