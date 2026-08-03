package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/hostingpath"
)

const (
	maxFileManagerContentBytes = 10 << 20
	maxFileActionRequestBytes  = 16 << 20
)

// siteDocroot remains for backup and application workflows. The file manager
// intentionally does not use it: privileged file RPCs derive their root from
// subscription_id + domain_id and never trust a database path string.
func (p *Panel) siteDocroot(ctx context.Context, domainID int) (string, error) {
	var docroot string
	err := p.db.GetDB().QueryRowContext(ctx,
		`SELECT document_root FROM sites WHERE domain_id = ? LIMIT 1`, domainID).Scan(&docroot)
	if err == nil && docroot != "" {
		return filepath.Clean(docroot), nil
	}

	var name string
	if err := p.db.GetDB().QueryRowContext(ctx,
		`SELECT name FROM domains WHERE id = ?`, domainID).Scan(&name); err != nil {
		return "", err
	}
	return filepath.Join("/var/www", name), nil
}

type panelFileScope struct {
	SubscriptionID int    `json:"subscription_id"`
	DomainID       int    `json:"domain_id"`
	Path           string `json:"path"`
}

type panelFileInfo struct {
	Name        string    `json:"name"`
	Path        string    `json:"path"`
	IsDir       bool      `json:"is_dir"`
	Size        int64     `json:"size"`
	Permissions string    `json:"permissions"`
	ModTime     time.Time `json:"mod_time"`
}

type panelReadFileResponse struct {
	Path     string `json:"path"`
	Content  string `json:"content"`
	Size     int64  `json:"size"`
	IsBinary bool   `json:"is_binary"`
}

func (p *Panel) fileManagerSubscriptionID(ctx context.Context, domainID int) (int, error) {
	var subscriptionID int
	if err := p.db.GetDB().QueryRowContext(ctx,
		`SELECT subscription_id FROM domains WHERE id = ?`, domainID).Scan(&subscriptionID); err != nil {
		return 0, err
	}
	if subscriptionID <= 0 {
		return 0, errors.New("domain has no valid subscription")
	}
	return subscriptionID, nil
}

// normalizePanelFilePath accepts the UI's leading-slash notation but returns
// only the canonical relative value understood by the agent. A second leading
// slash stays absolute and is rejected rather than silently reinterpreted.
func normalizePanelFilePath(candidate string) (string, error) {
	if candidate == "" || candidate == "/" {
		return ".", nil
	}
	if strings.HasPrefix(candidate, "/") {
		candidate = strings.TrimPrefix(candidate, "/")
	}
	return hostingpath.NormalizeRelativePath(candidate)
}

func displayFilePath(relativePath string) string {
	if relativePath == "." {
		return "/"
	}
	return "/" + relativePath
}

func parseDomainIDFromFilesPath(urlPath string) (int, error) {
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

func (p *Panel) handleDomainFiles(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	domainID, err := parseDomainIDFromFilesPath(r.URL.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	subscriptionID, err := p.fileManagerSubscriptionID(r.Context(), domainID)
	if err != nil {
		http.Error(w, "Domain not found", http.StatusNotFound)
		return
	}
	relativePath, err := normalizePanelFilePath(r.URL.Query().Get("path"))
	if err != nil {
		http.Error(w, "Invalid file path", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		p.handleListFiles(w, subscriptionID, domainID, relativePath)
	case http.MethodPost:
		p.handleFileAction(w, r, subscriptionID, domainID, relativePath)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (p *Panel) handleListFiles(w http.ResponseWriter, subscriptionID, domainID int, relativePath string) {
	var rpcResponse struct {
		Path  string          `json:"path"`
		Files []panelFileInfo `json:"files"`
	}
	if err := p.agentClient.Call("Agent.ListFiles", &panelFileScope{
		SubscriptionID: subscriptionID,
		DomainID:       domainID,
		Path:           relativePath,
	}, &rpcResponse); err != nil {
		writeServerError(w, err)
		return
	}

	agentPath, err := hostingpath.ValidateRelativePath(rpcResponse.Path)
	if err != nil || agentPath != relativePath {
		writeServerError(w, errors.New("agent returned an invalid file path"))
		return
	}
	response := struct {
		CurrentPath string          `json:"current_path"`
		ParentPath  string          `json:"parent_path"`
		Files       []panelFileInfo `json:"files"`
	}{
		CurrentPath: displayFilePath(relativePath),
		Files:       make([]panelFileInfo, 0, len(rpcResponse.Files)),
	}
	if relativePath != "." {
		response.ParentPath = displayFilePath(path.Dir(relativePath))
	}

	for _, file := range rpcResponse.Files {
		agentFilePath, err := hostingpath.ValidateRelativePath(file.Path)
		if err != nil || path.Dir(agentFilePath) != relativePath || path.Base(agentFilePath) != file.Name {
			writeServerError(w, errors.New("agent returned an invalid directory entry"))
			return
		}
		file.Path = displayFilePath(agentFilePath)
		response.Files = append(response.Files, file)
	}
	_ = json.NewEncoder(w).Encode(response)
}

func decodeFileAction(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxFileActionRequestBytes)
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

func (p *Panel) handleFileAction(w http.ResponseWriter, r *http.Request, subscriptionID, domainID int, relativePath string) {
	var req struct {
		Action      string `json:"action"`
		Content     string `json:"content,omitempty"`
		NewPath     string `json:"new_path,omitempty"`
		Permissions string `json:"permissions,omitempty"`
		IsDir       bool   `json:"is_dir,omitempty"`
		FileName    string `json:"file_name,omitempty"`
	}
	if err := decodeFileAction(w, r, &req); err != nil {
		return
	}
	scope := panelFileScope{
		SubscriptionID: subscriptionID,
		DomainID:       domainID,
		Path:           relativePath,
	}

	switch req.Action {
	case "read":
		var resp panelReadFileResponse
		if err := p.agentClient.Call("Agent.ReadFile", &scope, &resp); err != nil {
			writeServerError(w, err)
			return
		}
		agentPath, err := hostingpath.ValidateRelativePath(resp.Path)
		if err != nil || agentPath != relativePath {
			writeServerError(w, errors.New("agent returned an invalid file path"))
			return
		}
		if resp.Size < 0 || resp.Size > maxFileManagerContentBytes {
			writeServerError(w, errors.New("agent returned an oversized file"))
			return
		}
		resp.Path = displayFilePath(relativePath)
		_ = json.NewEncoder(w).Encode(resp)

	case "write":
		if len(req.Content) > maxFileManagerContentBytes {
			http.Error(w, "File content too large", http.StatusRequestEntityTooLarge)
			return
		}
		var success bool
		if err := p.agentClient.Call("Agent.WriteFile", &struct {
			SubscriptionID int
			DomainID       int
			Path           string
			Content        string
		}{subscriptionID, domainID, relativePath, req.Content}, &success); err != nil {
			writeServerError(w, err)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]bool{"success": success})

	case "create":
		if relativePath == "." {
			http.Error(w, "Document root already exists", http.StatusBadRequest)
			return
		}
		var success bool
		if err := p.agentClient.Call("Agent.CreateFileOrDir", &struct {
			SubscriptionID int
			DomainID       int
			Path           string
			IsDir          bool
		}{subscriptionID, domainID, relativePath, req.IsDir}, &success); err != nil {
			writeServerError(w, err)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]bool{"success": success})

	case "delete":
		if relativePath == "." {
			http.Error(w, "Document root cannot be deleted", http.StatusBadRequest)
			return
		}
		var success bool
		if err := p.agentClient.Call("Agent.DeleteFileOrDir", &scope, &success); err != nil {
			writeServerError(w, err)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]bool{"success": success})

	case "rename":
		newPath, err := normalizePanelFilePath(req.NewPath)
		if err != nil || relativePath == "." || newPath == "." {
			http.Error(w, "Invalid rename path", http.StatusBadRequest)
			return
		}
		var success bool
		if err := p.agentClient.Call("Agent.RenameFile", &struct {
			SubscriptionID int
			DomainID       int
			OldPath        string
			NewPath        string
		}{subscriptionID, domainID, relativePath, newPath}, &success); err != nil {
			writeServerError(w, err)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]bool{"success": success})

	case "chmod":
		if relativePath == "." {
			http.Error(w, "Document root permissions cannot be changed", http.StatusBadRequest)
			return
		}
		var success bool
		if err := p.agentClient.Call("Agent.ChmodFile", &struct {
			SubscriptionID int
			DomainID       int
			Path           string
			Permissions    string
		}{subscriptionID, domainID, relativePath, req.Permissions}, &success); err != nil {
			writeServerError(w, err)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]bool{"success": success})

	case "upload":
		if err := hostingpath.ValidateFileName(req.FileName); err != nil {
			http.Error(w, "Invalid upload file name", http.StatusBadRequest)
			return
		}
		if len(req.Content) > base64.StdEncoding.EncodedLen(maxFileManagerContentBytes) {
			http.Error(w, "Upload too large", http.StatusRequestEntityTooLarge)
			return
		}
		var success bool
		if err := p.agentClient.Call("Agent.UploadFile", &struct {
			SubscriptionID int
			DomainID       int
			Path           string
			Name           string
			Content        string
		}{subscriptionID, domainID, relativePath, req.FileName, req.Content}, &success); err != nil {
			writeServerError(w, err)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]bool{"success": success})

	default:
		http.Error(w, "Unknown action", http.StatusBadRequest)
	}
}

func safeAttachmentDisposition(relativePath string) string {
	return mime.FormatMediaType("attachment", map[string]string{
		"filename": path.Base(relativePath),
	})
}

func (p *Panel) handleDomainFileDownload(w http.ResponseWriter, r *http.Request) {
	domainID, err := parseDomainIDFromFilesPath(r.URL.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	subscriptionID, err := p.fileManagerSubscriptionID(r.Context(), domainID)
	if err != nil {
		http.Error(w, "Domain not found", http.StatusNotFound)
		return
	}
	rawPath := r.URL.Query().Get("path")
	if rawPath == "" {
		http.Error(w, "Path required", http.StatusBadRequest)
		return
	}
	relativePath, err := normalizePanelFilePath(rawPath)
	if err != nil || relativePath == "." {
		http.Error(w, "Invalid file path", http.StatusBadRequest)
		return
	}

	var resp panelReadFileResponse
	if err := p.agentClient.Call("Agent.ReadFile", &panelFileScope{
		SubscriptionID: subscriptionID,
		DomainID:       domainID,
		Path:           relativePath,
	}, &resp); err != nil {
		writeServerError(w, err)
		return
	}
	agentPath, err := hostingpath.ValidateRelativePath(resp.Path)
	if err != nil || agentPath != relativePath {
		writeServerError(w, errors.New("agent returned an invalid file path"))
		return
	}
	if resp.Size < 0 || resp.Size > maxFileManagerContentBytes {
		writeServerError(w, errors.New("agent returned an oversized file"))
		return
	}

	var content []byte
	if resp.IsBinary {
		if len(resp.Content) > base64.StdEncoding.EncodedLen(maxFileManagerContentBytes) {
			writeServerError(w, errors.New("agent returned oversized encoded content"))
			return
		}
		content, err = base64.StdEncoding.DecodeString(resp.Content)
		if err != nil || len(content) > maxFileManagerContentBytes {
			writeServerError(w, errors.New("agent returned invalid encoded content"))
			return
		}
	} else {
		if len(resp.Content) > maxFileManagerContentBytes {
			writeServerError(w, errors.New("agent returned oversized content"))
			return
		}
		content = []byte(resp.Content)
	}

	w.Header().Set("Content-Disposition", safeAttachmentDisposition(relativePath))
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.Itoa(len(content)))
	_, _ = w.Write(content)
}
