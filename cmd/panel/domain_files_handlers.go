package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/hostingpath"
	"github.com/alicelik/celikpanel/internal/transport"
)

// File Manager API Handlers

// siteDocroot resolves the browsing root for a domain from the site's real
// document_root. The stored path is accepted only when it matches the
// immutable subscription/domain identity. Missing or inconsistent site rows
// fail closed; the privileged file manager must never guess a filesystem root.
//
// siteDocroot, bir domain'in gezinme kökünü sitenin gerçek document_root'undan
// çözer. Orchestrator siteleri /var/www/celikpanel/subscriptions/... altına
// koyar; bu yüzden /var/www/<ad> tahmini (eski davranış) dosya yöneticisini
// hiç var olmamış bir dizine yöneltiyordu; eski yol yalnızca site kaydı
// olmayan domain'ler için yedek olarak kalır.
type domainFileScope struct {
	SubscriptionID int
	DomainID       int
	Root           string
}

func (p *Panel) siteFileScope(ctx context.Context, domainID int) (domainFileScope, error) {
	var subscriptionID int
	var docroot string
	if err := p.db.GetDB().QueryRowContext(ctx,
		`SELECT d.subscription_id, s.document_root
		 FROM sites AS s
		 JOIN domains AS d ON d.id = s.domain_id
		 WHERE s.domain_id = ?
		 LIMIT 1`,
		domainID,
	).Scan(&subscriptionID, &docroot); err != nil {
		return domainFileScope{}, err
	}
	docroot = filepath.Clean(docroot)
	if err := hostingpath.ValidateDocumentRoot(docroot, subscriptionID, domainID); err != nil {
		return domainFileScope{}, err
	}
	return domainFileScope{
		SubscriptionID: subscriptionID,
		DomainID:       domainID,
		Root:           docroot,
	}, nil
}

func (p *Panel) siteDocroot(ctx context.Context, domainID int) (string, error) {
	scope, err := p.siteFileScope(ctx, domainID)
	return scope.Root, err
}

// withinRoot guards against path traversal: the resolved path must be the
// root itself or live under it (plain prefix matching would let
// /var/www/foo leak into /var/www/foobar).
// withinRoot, yol kaçışına karşı korur: çözülen yol kökün kendisi olmalı ya
// da altında yaşamalıdır (düz önek eşleşmesi /var/www/foo'nun
// /var/www/foobar'a sızmasına izin verirdi).
func withinRoot(fullPath, root string) bool {
	return fullPath == root || strings.HasPrefix(fullPath, root+string(filepath.Separator))
}

func cleanDomainFilePath(raw string) (string, error) {
	if strings.ContainsRune(raw, '\x00') {
		return "", fmt.Errorf("path contains NUL")
	}
	normalized := strings.ReplaceAll(raw, `\`, "/")
	for _, component := range strings.Split(normalized, "/") {
		if component == ".." {
			return "", fmt.Errorf("path traversal is not allowed")
		}
	}
	normalized = strings.TrimLeft(normalized, "/")
	cleaned := path.Clean(normalized)
	if cleaned == "." {
		return "", nil
	}
	if path.IsAbs(cleaned) || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("path leaves site root")
	}
	return cleaned, nil
}

func displayDomainFilePath(relative string) string {
	if relative == "" {
		return "/"
	}
	return "/" + relative
}

func (p *Panel) handleDomainFiles(w http.ResponseWriter, r *http.Request) {
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

	scope, err := p.siteFileScope(r.Context(), domainID)
	if err != nil {
		http.Error(w, "Domain not found", http.StatusNotFound)
		return
	}

	// Get requested path from query
	reqPath := r.URL.Query().Get("path")
	relativePath, err := cleanDomainFilePath(reqPath)
	if err != nil {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	switch r.Method {
	case "GET":
		p.handleListFiles(w, r, relativePath, scope)
	case "POST":
		p.handleFileAction(w, r, relativePath, scope)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (p *Panel) handleListFiles(w http.ResponseWriter, r *http.Request, relativePath string, scope domainFileScope) {
	// ModTime must be time.Time to match the agent's FileInfo over gob; a
	// string field poisons the RPC stream (same class as BackupInfo.CreatedAt).
	// ModTime, gob üzerinden agent'ın FileInfo'suyla eşleşmek için time.Time
	// olmalıdır; string alan RPC akışını zehirler (BackupInfo.CreatedAt ile
	// aynı sınıf).
	type FileInfo struct {
		Name        string    `json:"name"`
		Path        string    `json:"path"`
		IsDir       bool      `json:"is_dir"`
		Size        int64     `json:"size"`
		Permissions string    `json:"permissions"`
		ModTime     time.Time `json:"mod_time"`
	}

	type ListResponse struct {
		CurrentPath string     `json:"current_path"`
		ParentPath  string     `json:"parent_path"`
		Files       []FileInfo `json:"files"`
	}

	// Call agent to list files
	var resp transport.ListFilesResponse

	err := p.callAgentContext(r.Context(), "Agent.ListFiles", &transport.ListFilesRequest{
		SubscriptionID: scope.SubscriptionID,
		DomainID:       scope.DomainID,
		Path:           relativePath,
	}, &resp)
	if err != nil {
		writeServerError(w, err)
		return
	}

	// Convert to relative paths
	result := ListResponse{
		CurrentPath: displayDomainFilePath(relativePath),
		ParentPath:  "",
		Files:       make([]FileInfo, 0, len(resp.Files)),
	}

	// Calculate parent path
	if result.CurrentPath != "/" {
		result.ParentPath = path.Dir(result.CurrentPath)
	}

	for _, f := range resp.Files {
		relPath, cleanErr := cleanDomainFilePath(f.Path)
		if cleanErr != nil {
			continue
		}
		result.Files = append(result.Files, FileInfo{
			Name:        f.Name,
			Path:        displayDomainFilePath(relPath),
			IsDir:       f.IsDir,
			Size:        f.Size,
			Permissions: f.Permissions,
			ModTime:     f.ModTime,
		})
	}

	json.NewEncoder(w).Encode(result)
}

func (p *Panel) handleFileAction(w http.ResponseWriter, r *http.Request, relativePath string, scope domainFileScope) {
	r.Body = http.MaxBytesReader(w, r.Body, 16<<20)
	var req struct {
		Action      string `json:"action"`
		Content     string `json:"content,omitempty"`
		NewPath     string `json:"new_path,omitempty"`
		Permissions string `json:"permissions,omitempty"`
		IsDir       bool   `json:"is_dir,omitempty"`
		FileName    string `json:"file_name,omitempty"`
	}

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	switch req.Action {
	case "read":
		var resp transport.ReadFileResponse
		err := p.callAgentContext(r.Context(), "Agent.ReadFile", &transport.ReadFileRequest{
			SubscriptionID: scope.SubscriptionID,
			DomainID:       scope.DomainID,
			Path:           relativePath,
		}, &resp)
		if err != nil {
			writeServerError(w, err)
			return
		}
		json.NewEncoder(w).Encode(resp)

	case "write":
		var success bool
		err := p.callAgentContext(r.Context(), "Agent.WriteFile", &transport.WriteFileRequest{
			SubscriptionID: scope.SubscriptionID,
			DomainID:       scope.DomainID,
			Path:           relativePath,
			Content:        req.Content,
		}, &success)
		if err != nil {
			writeServerError(w, err)
			return
		}
		if !success {
			writeAgentError(w, nil, "file write was not completed")
			return
		}
		json.NewEncoder(w).Encode(map[string]bool{"success": true})

	case "create":
		var success bool
		err := p.callAgentContext(r.Context(), "Agent.CreateFileOrDir", &transport.CreateFileRequest{
			SubscriptionID: scope.SubscriptionID,
			DomainID:       scope.DomainID,
			Path:           relativePath,
			IsDir:          req.IsDir,
		}, &success)
		if err != nil {
			writeServerError(w, err)
			return
		}
		if !success {
			writeAgentError(w, nil, "file creation was not completed")
			return
		}
		json.NewEncoder(w).Encode(map[string]bool{"success": true})

	case "delete":
		var success bool
		err := p.callAgentContext(r.Context(), "Agent.DeleteFileOrDir", &transport.DeleteFileRequest{
			SubscriptionID: scope.SubscriptionID,
			DomainID:       scope.DomainID,
			Path:           relativePath,
		}, &success)
		if err != nil {
			writeServerError(w, err)
			return
		}
		if !success {
			writeAgentError(w, nil, "file deletion was not completed")
			return
		}
		json.NewEncoder(w).Encode(map[string]bool{"success": true})

	case "rename":
		newRelativePath, cleanErr := cleanDomainFilePath(req.NewPath)
		if cleanErr != nil || newRelativePath == "" {
			http.Error(w, "Access denied", http.StatusForbidden)
			return
		}
		var success bool
		err := p.callAgentContext(r.Context(), "Agent.RenameFile", &transport.RenameFileRequest{
			SubscriptionID: scope.SubscriptionID,
			DomainID:       scope.DomainID,
			OldPath:        relativePath,
			NewPath:        newRelativePath,
		}, &success)
		if err != nil {
			writeServerError(w, err)
			return
		}
		if !success {
			writeAgentError(w, nil, "file rename was not completed")
			return
		}
		json.NewEncoder(w).Encode(map[string]bool{"success": true})

	case "chmod":
		var success bool
		err := p.callAgentContext(r.Context(), "Agent.ChmodFile", &transport.ChmodFileRequest{
			SubscriptionID: scope.SubscriptionID,
			DomainID:       scope.DomainID,
			Path:           relativePath,
			Permissions:    req.Permissions,
		}, &success)
		if err != nil {
			writeServerError(w, err)
			return
		}
		if !success {
			writeAgentError(w, nil, "file permission change was not completed")
			return
		}
		json.NewEncoder(w).Encode(map[string]bool{"success": true})

	case "upload":
		if req.FileName == "" || filepath.Base(req.FileName) != req.FileName || req.FileName == "." || req.FileName == ".." {
			http.Error(w, "Invalid file name", http.StatusBadRequest)
			return
		}
		var success bool
		err := p.callAgentContext(r.Context(), "Agent.UploadFile", &transport.UploadFileRequest{
			SubscriptionID: scope.SubscriptionID,
			DomainID:       scope.DomainID,
			Path:           relativePath,
			Name:           req.FileName,
			Content:        req.Content,
		}, &success)
		if err != nil {
			writeServerError(w, err)
			return
		}
		if !success {
			writeAgentError(w, nil, "file upload was not completed")
			return
		}
		json.NewEncoder(w).Encode(map[string]bool{"success": true})

	default:
		http.Error(w, "Unknown action", http.StatusBadRequest)
	}
}

func (p *Panel) handleDomainFileDownload(w http.ResponseWriter, r *http.Request) {
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

	scope, err := p.siteFileScope(r.Context(), domainID)
	if err != nil {
		http.Error(w, "Domain not found", http.StatusNotFound)
		return
	}

	reqPath := r.URL.Query().Get("path")
	if reqPath == "" {
		http.Error(w, "Path required", http.StatusBadRequest)
		return
	}

	relativePath, err := cleanDomainFilePath(reqPath)
	if err != nil || relativePath == "" {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	var resp transport.ReadFileResponse

	err = p.callAgentContext(r.Context(), "Agent.ReadFile", &transport.ReadFileRequest{
		SubscriptionID: scope.SubscriptionID,
		DomainID:       scope.DomainID,
		Path:           relativePath,
	}, &resp)
	if err != nil {
		writeServerError(w, err)
		return
	}

	filename := path.Base(relativePath)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
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
