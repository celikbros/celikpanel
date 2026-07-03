package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/alicelik/celikpanel/internal/repositories"
)

// File Manager API Handlers

func (p *Panel) handleDomainFiles(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

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

	// Get domain using repository pattern
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

	// Domain root path (typically /var/www/domain.com)
	domainRoot := filepath.Join("/var/www", domainName)

	// Get requested path from query
	reqPath := r.URL.Query().Get("path")
	if reqPath == "" {
		reqPath = "/"
	}

	// Construct full path and validate it's within domain root
	fullPath := filepath.Clean(filepath.Join(domainRoot, reqPath))
	if !strings.HasPrefix(fullPath, domainRoot) {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	switch r.Method {
	case "GET":
		p.handleListFiles(w, r, fullPath, domainRoot)
	case "POST":
		p.handleFileAction(w, r, fullPath, domainRoot)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (p *Panel) handleListFiles(w http.ResponseWriter, r *http.Request, path, domainRoot string) {
	type FileInfo struct {
		Name        string `json:"name"`
		Path        string `json:"path"`
		IsDir       bool   `json:"is_dir"`
		Size        int64  `json:"size"`
		Permissions string `json:"permissions"`
		ModTime     string `json:"mod_time"`
	}

	type ListResponse struct {
		CurrentPath string     `json:"current_path"`
		ParentPath  string     `json:"parent_path"`
		Files       []FileInfo `json:"files"`
	}

	// Call agent to list files
	var resp struct {
		Path  string `json:"path"`
		Files []struct {
			Name        string `json:"name"`
			Path        string `json:"path"`
			IsDir       bool   `json:"is_dir"`
			Size        int64  `json:"size"`
			Permissions string `json:"permissions"`
			ModTime     string `json:"mod_time"`
		} `json:"files"`
	}

	err := p.agentClient.Call("Agent.ListFiles", &struct{ Path string }{Path: path}, &resp)
	if err != nil {
		writeServerError(w, err)
		return
	}

	// Convert to relative paths
	result := ListResponse{
		CurrentPath: strings.TrimPrefix(path, domainRoot),
		ParentPath:  "",
		Files:       make([]FileInfo, 0, len(resp.Files)),
	}

	if result.CurrentPath == "" {
		result.CurrentPath = "/"
	}

	// Calculate parent path
	if result.CurrentPath != "/" {
		result.ParentPath = filepath.Dir(result.CurrentPath)
	}

	for _, f := range resp.Files {
		relPath := strings.TrimPrefix(f.Path, domainRoot)
		if relPath == "" {
			relPath = "/"
		}
		result.Files = append(result.Files, FileInfo{
			Name:        f.Name,
			Path:        relPath,
			IsDir:       f.IsDir,
			Size:        f.Size,
			Permissions: f.Permissions,
			ModTime:     f.ModTime,
		})
	}

	json.NewEncoder(w).Encode(result)
}

func (p *Panel) handleFileAction(w http.ResponseWriter, r *http.Request, path, domainRoot string) {
	var req struct {
		Action      string `json:"action"`
		Content     string `json:"content,omitempty"`
		NewPath     string `json:"new_path,omitempty"`
		Permissions string `json:"permissions,omitempty"`
		IsDir       bool   `json:"is_dir,omitempty"`
		FileName    string `json:"file_name,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	switch req.Action {
	case "read":
		var resp struct {
			Path     string `json:"path"`
			Content  string `json:"content"`
			Size     int64  `json:"size"`
			IsBinary bool   `json:"is_binary"`
		}
		err := p.agentClient.Call("Agent.ReadFile", &struct{ Path string }{Path: path}, &resp)
		if err != nil {
			writeServerError(w, err)
			return
		}
		json.NewEncoder(w).Encode(resp)

	case "write":
		var success bool
		err := p.agentClient.Call("Agent.WriteFile", &struct {
			Path    string
			Content string
		}{Path: path, Content: req.Content}, &success)
		if err != nil {
			writeServerError(w, err)
			return
		}
		json.NewEncoder(w).Encode(map[string]bool{"success": success})

	case "create":
		var success bool
		err := p.agentClient.Call("Agent.CreateFileOrDir", &struct {
			Path  string
			IsDir bool
		}{Path: path, IsDir: req.IsDir}, &success)
		if err != nil {
			writeServerError(w, err)
			return
		}
		json.NewEncoder(w).Encode(map[string]bool{"success": success})

	case "delete":
		var success bool
		err := p.agentClient.Call("Agent.DeleteFileOrDir", &struct{ Path string }{Path: path}, &success)
		if err != nil {
			writeServerError(w, err)
			return
		}
		json.NewEncoder(w).Encode(map[string]bool{"success": success})

	case "rename":
		newFullPath := filepath.Clean(filepath.Join(domainRoot, req.NewPath))
		if !strings.HasPrefix(newFullPath, domainRoot) {
			http.Error(w, "Access denied", http.StatusForbidden)
			return
		}
		var success bool
		err := p.agentClient.Call("Agent.RenameFile", &struct {
			OldPath string
			NewPath string
		}{OldPath: path, NewPath: newFullPath}, &success)
		if err != nil {
			writeServerError(w, err)
			return
		}
		json.NewEncoder(w).Encode(map[string]bool{"success": success})

	case "chmod":
		var success bool
		err := p.agentClient.Call("Agent.ChmodFile", &struct {
			Path        string
			Permissions string
		}{Path: path, Permissions: req.Permissions}, &success)
		if err != nil {
			writeServerError(w, err)
			return
		}
		json.NewEncoder(w).Encode(map[string]bool{"success": success})

	case "upload":
		var success bool
		err := p.agentClient.Call("Agent.UploadFile", &struct {
			Path    string
			Name    string
			Content string
		}{Path: path, Name: req.FileName, Content: req.Content}, &success)
		if err != nil {
			writeServerError(w, err)
			return
		}
		json.NewEncoder(w).Encode(map[string]bool{"success": success})

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

	reqPath := r.URL.Query().Get("path")
	if reqPath == "" {
		http.Error(w, "Path required", http.StatusBadRequest)
		return
	}

	domainRoot := filepath.Join("/var/www", domainName)
	fullPath := filepath.Clean(filepath.Join(domainRoot, reqPath))
	if !strings.HasPrefix(fullPath, domainRoot) {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	var resp struct {
		Path     string `json:"path"`
		Content  string `json:"content"`
		Size     int64  `json:"size"`
		IsBinary bool   `json:"is_binary"`
	}

	err = p.agentClient.Call("Agent.ReadFile", &struct{ Path string }{Path: fullPath}, &resp)
	if err != nil {
		writeServerError(w, err)
		return
	}

	filename := filepath.Base(fullPath)
	w.Header().Set("Content-Disposition", "attachment; filename="+filename)
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
