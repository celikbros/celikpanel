package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/repositories"
)

// Backup API Handlers

func (p *Panel) handleDomainBackups(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Extract domain ID from URL
	pathParts := strings.Split(r.URL.Path, "/")
	if len(pathParts) < 5 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	domainID, err := strconv.Atoi(pathParts[4])
	if err != nil {
		http.Error(w, "Invalid domain ID", http.StatusBadRequest)
		return
	}

	// Get domain name
	domainRepo := repositories.NewPostgresDomainRepository(p.db.GetDB())
	domains, err := domainRepo.List(context.Background())
	if err != nil {
		http.Error(w, "Failed to get domains", http.StatusInternalServerError)
		return
	}

	var domainName string
	for _, d := range domains {
		if d.ID == domainID {
			domainName = d.Name
			break
		}
	}

	if domainName == "" {
		http.Error(w, "Domain not found", http.StatusNotFound)
		return
	}

	switch r.Method {
	case "GET":
		p.handleListBackups(w, domainName)
	case "POST":
		p.handleCreateBackup(w, r, domainID, domainName)
	case "DELETE":
		p.handleDeleteBackup(w, r, domainName)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// The CreatedAt fields must be time.Time: the agent's BackupInfo sends
// time.Time over gob, and a string field on this side poisons the whole RPC
// stream with a decode error.
// CreatedAt alanları time.Time olmalıdır: agent'ın BackupInfo'su gob üzerinden
// time.Time gönderir; bu taraftaki bir string alan, tüm RPC akışını bir çözme
// hatasıyla zehirler.
func (p *Panel) handleListBackups(w http.ResponseWriter, domainName string) {
	var resp struct {
		Backups []struct {
			Name      string    `json:"name"`
			Path      string    `json:"path"`
			Size      int64     `json:"size"`
			Type      string    `json:"type"`
			CreatedAt time.Time `json:"created_at"`
		} `json:"backups"`
	}

	err := p.agentClient.Call("Agent.ListBackups", &struct{ DomainName string }{DomainName: domainName}, &resp)
	if err != nil {
		writeServerError(w, err)
		return
	}

	json.NewEncoder(w).Encode(resp)
}

func (p *Panel) handleCreateBackup(w http.ResponseWriter, r *http.Request, domainID int, domainName string) {
	var req struct {
		Type         string `json:"type"`
		DatabaseName string `json:"database_name,omitempty"`
		DatabaseType string `json:"database_type,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// The agent has no DB access; resolve the real document root here so file
	// backups archive the site's actual directory.
	// Agent'ın DB erişimi yoktur; dosya yedeklerinin sitenin gerçek dizinini
	// arşivlemesi için belge kökünü burada çöz.
	sourceDir, err := p.siteDocroot(r.Context(), domainID)
	if err != nil {
		http.Error(w, "Domain not found", http.StatusNotFound)
		return
	}

	var resp struct {
		Success bool `json:"success"`
		Backup  struct {
			Name      string    `json:"name"`
			Path      string    `json:"path"`
			Size      int64     `json:"size"`
			Type      string    `json:"type"`
			CreatedAt time.Time `json:"created_at"`
		} `json:"backup,omitempty"`
		Error string `json:"error,omitempty"`
	}

	err = p.agentClient.Call("Agent.CreateBackup", &struct {
		DomainName   string
		Type         string
		DatabaseName string
		DatabaseType string
		SourceDir    string
	}{
		DomainName:   domainName,
		Type:         req.Type,
		DatabaseName: req.DatabaseName,
		DatabaseType: req.DatabaseType,
		SourceDir:    sourceDir,
	}, &resp)

	if err != nil {
		writeServerError(w, err)
		return
	}

	json.NewEncoder(w).Encode(resp)
}

func (p *Panel) handleDeleteBackup(w http.ResponseWriter, r *http.Request, domainName string) {
	backupName := r.URL.Query().Get("name")
	if backupName == "" {
		http.Error(w, "Backup name required", http.StatusBadRequest)
		return
	}

	var success bool
	err := p.agentClient.Call("Agent.DeleteBackup", &struct {
		DomainName string
		BackupName string
	}{
		DomainName: domainName,
		BackupName: backupName,
	}, &success)

	if err != nil {
		writeServerError(w, err)
		return
	}

	json.NewEncoder(w).Encode(map[string]bool{"success": success})
}

func (p *Panel) handleRestoreBackup(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract domain ID from URL
	pathParts := strings.Split(r.URL.Path, "/")
	if len(pathParts) < 5 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	domainID, err := strconv.Atoi(pathParts[4])
	if err != nil {
		http.Error(w, "Invalid domain ID", http.StatusBadRequest)
		return
	}

	// Get domain name
	domainRepo := repositories.NewPostgresDomainRepository(p.db.GetDB())
	domains, err := domainRepo.List(context.Background())
	if err != nil {
		http.Error(w, "Failed to get domains", http.StatusInternalServerError)
		return
	}

	var domainName string
	for _, d := range domains {
		if d.ID == domainID {
			domainName = d.Name
			break
		}
	}

	if domainName == "" {
		http.Error(w, "Domain not found", http.StatusNotFound)
		return
	}

	var req struct {
		BackupName string `json:"backup_name"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	targetDir, err := p.siteDocroot(r.Context(), domainID)
	if err != nil {
		http.Error(w, "Domain not found", http.StatusNotFound)
		return
	}

	var resp struct {
		Success bool   `json:"success"`
		Error   string `json:"error,omitempty"`
	}

	err = p.agentClient.Call("Agent.RestoreBackup", &struct {
		DomainName string
		BackupName string
		TargetDir  string
	}{
		DomainName: domainName,
		BackupName: req.BackupName,
		TargetDir:  targetDir,
	}, &resp)

	if err != nil {
		writeServerError(w, err)
		return
	}

	json.NewEncoder(w).Encode(resp)
}

// handleDownloadBackup streams a backup archive to the browser. Backups live
// under /var/backups/celikpanel/<domain>/, outside the site's document root,
// so the file-manager download endpoint can never reach them (its traversal
// guard correctly refuses) — they need this dedicated route.
//
// handleDownloadBackup, bir yedek arşivini tarayıcıya akıtır. Yedekler,
// sitenin belge kökünün dışında, /var/backups/celikpanel/<domain>/ altında
// yaşar; bu yüzden dosya-yöneticisi indirme ucu onlara asla ulaşamaz (kaçış
// koruması haklı olarak reddeder) — bu özel rotaya ihtiyaç duyarlar.
func (p *Panel) handleDownloadBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	pathParts := strings.Split(r.URL.Path, "/")
	if len(pathParts) < 5 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	domainID, err := strconv.Atoi(pathParts[4])
	if err != nil {
		http.Error(w, "Invalid domain ID", http.StatusBadRequest)
		return
	}

	var domainName string
	if err := p.db.GetDB().QueryRowContext(r.Context(),
		`SELECT name FROM domains WHERE id = ?`, domainID).Scan(&domainName); err != nil {
		http.Error(w, "Domain not found", http.StatusNotFound)
		return
	}

	// The backup name is a single file name — never a path.
	// Yedek adı tek bir dosya adıdır — asla bir yol değildir.
	name := r.URL.Query().Get("name")
	if name == "" || name != filepath.Base(name) || strings.HasPrefix(name, ".") {
		http.Error(w, "Invalid backup name", http.StatusBadRequest)
		return
	}

	// Resolve the file through the agent's own listing instead of rebuilding
	// the path here — the agent owns the backup directory layout.
	// Yolu burada yeniden kurmak yerine dosyayı agent'ın kendi listesinden
	// çöz — yedek dizin düzeninin sahibi agent'tır.
	var list struct {
		Backups []struct {
			Name      string    `json:"name"`
			Path      string    `json:"path"`
			Size      int64     `json:"size"`
			Type      string    `json:"type"`
			CreatedAt time.Time `json:"created_at"`
		} `json:"backups"`
	}
	if err := p.agentClient.Call("Agent.ListBackups", &struct{ DomainName string }{DomainName: domainName}, &list); err != nil {
		writeServerError(w, err)
		return
	}
	backupPath := ""
	for _, b := range list.Backups {
		if b.Name == name {
			backupPath = b.Path
			break
		}
	}
	if backupPath == "" {
		http.Error(w, "Backup not found", http.StatusNotFound)
		return
	}

	var resp struct {
		Path     string `json:"path"`
		Content  string `json:"content"`
		Size     int64  `json:"size"`
		IsBinary bool   `json:"is_binary"`
	}
	if err := p.agentClient.Call("Agent.ReadFile", &struct{ Path string }{Path: backupPath}, &resp); err != nil {
		writeServerError(w, err)
		return
	}

	w.Header().Set("Content-Disposition", "attachment; filename="+name)
	w.Header().Set("Content-Type", "application/octet-stream")
	if resp.IsBinary {
		decoded, err := base64.StdEncoding.DecodeString(resp.Content)
		if err != nil {
			writeServerError(w, err)
			return
		}
		w.Write(decoded)
	} else {
		w.Write([]byte(resp.Content))
	}
}
