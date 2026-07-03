package main

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

// FileInfo represents a file or directory
type FileInfo struct {
	Name        string    `json:"name"`
	Path        string    `json:"path"`
	IsDir       bool      `json:"is_dir"`
	Size        int64     `json:"size"`
	Permissions string    `json:"permissions"`
	Owner       string    `json:"owner"`
	Group       string    `json:"group"`
	ModTime     time.Time `json:"mod_time"`
}

// ListFilesRequest for listing directory contents
type ListFilesRequest struct {
	Path string `json:"path"`
}

// ListFilesResponse contains directory listing
type ListFilesResponse struct {
	Path  string     `json:"path"`
	Files []FileInfo `json:"files"`
}

// ReadFileRequest for reading file content
type ReadFileRequest struct {
	Path string `json:"path"`
}

// ReadFileResponse contains file content
type ReadFileResponse struct {
	Path     string `json:"path"`
	Content  string `json:"content"`
	Size     int64  `json:"size"`
	IsBinary bool   `json:"is_binary"`
}

// WriteFileRequest for writing file content
type WriteFileRequest struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// CreateRequest for creating file or directory
type CreateRequest struct {
	Path  string `json:"path"`
	IsDir bool   `json:"is_dir"`
}

// DeleteRequest for deleting file or directory
type DeleteRequest struct {
	Path string `json:"path"`
}

// ChmodRequest for changing permissions
type ChmodRequest struct {
	Path        string `json:"path"`
	Permissions string `json:"permissions"` // e.g., "755"
}

// RenameRequest for renaming/moving files
type RenameRequest struct {
	OldPath string `json:"old_path"`
	NewPath string `json:"new_path"`
}

// UploadFileRequest for uploading files
type UploadFileRequest struct {
	Path    string `json:"path"`
	Name    string `json:"name"`
	Content string `json:"content"` // base64 encoded
}

// File Manager RPC Methods

// ListFiles lists directory contents
func (a *Agent) ListFiles(req *ListFilesRequest, resp *ListFilesResponse) error {
	path := req.Path
	if path == "" {
		path = "/"
	}

	// Security check - prevent directory traversal
	cleanPath := filepath.Clean(path)
	if strings.Contains(cleanPath, "..") {
		return os.ErrPermission
	}

	entries, err := os.ReadDir(cleanPath)
	if err != nil {
		return err
	}

	resp.Path = cleanPath
	resp.Files = make([]FileInfo, 0, len(entries))

	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}

		fileInfo := FileInfo{
			Name:        entry.Name(),
			Path:        filepath.Join(cleanPath, entry.Name()),
			IsDir:       entry.IsDir(),
			Size:        info.Size(),
			Permissions: info.Mode().String(),
			ModTime:     info.ModTime(),
		}

		// Get owner/group on Linux
		if stat, ok := info.Sys().(*syscall.Stat_t); ok {
			fileInfo.Owner = string(stat.Uid)
			fileInfo.Group = string(stat.Gid)
		}

		resp.Files = append(resp.Files, fileInfo)
	}

	// Sort: directories first, then by name
	sort.Slice(resp.Files, func(i, j int) bool {
		if resp.Files[i].IsDir != resp.Files[j].IsDir {
			return resp.Files[i].IsDir
		}
		return strings.ToLower(resp.Files[i].Name) < strings.ToLower(resp.Files[j].Name)
	})

	return nil
}

// ReadFile reads file content
func (a *Agent) ReadFile(req *ReadFileRequest, resp *ReadFileResponse) error {
	path := filepath.Clean(req.Path)
	if strings.Contains(path, "..") {
		return os.ErrPermission
	}

	info, err := os.Stat(path)
	if err != nil {
		return err
	}

	if info.IsDir() {
		return os.ErrInvalid
	}

	// Limit file size to 10MB for safety
	if info.Size() > 10*1024*1024 {
		return os.ErrInvalid
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	resp.Path = path
	resp.Size = info.Size()

	// Check if binary
	isBinary := false
	for _, b := range content[:min(len(content), 512)] {
		if b == 0 {
			isBinary = true
			break
		}
	}

	resp.IsBinary = isBinary
	if isBinary {
		resp.Content = base64.StdEncoding.EncodeToString(content)
	} else {
		resp.Content = string(content)
	}

	return nil
}

// WriteFile writes content to file
func (a *Agent) WriteFile(req *WriteFileRequest, resp *bool) error {
	path := filepath.Clean(req.Path)
	if strings.Contains(path, "..") {
		return os.ErrPermission
	}

	err := os.WriteFile(path, []byte(req.Content), 0644)
	if err != nil {
		return err
	}

	*resp = true
	return nil
}

// CreateFileOrDir creates a file or directory
func (a *Agent) CreateFileOrDir(req *CreateRequest, resp *bool) error {
	path := filepath.Clean(req.Path)
	if strings.Contains(path, "..") {
		return os.ErrPermission
	}

	var err error
	if req.IsDir {
		err = os.MkdirAll(path, 0755)
	} else {
		// Create parent directories if needed
		dir := filepath.Dir(path)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
		var f *os.File
		f, err = os.Create(path)
		if err == nil {
			f.Close()
		}
	}

	if err != nil {
		return err
	}

	*resp = true
	return nil
}

// DeleteFileOrDir deletes a file or directory
func (a *Agent) DeleteFileOrDir(req *DeleteRequest, resp *bool) error {
	path := filepath.Clean(req.Path)
	if strings.Contains(path, "..") {
		return os.ErrPermission
	}

	// Safety check - don't delete critical paths
	criticalPaths := []string{"/", "/etc", "/var", "/usr", "/bin", "/sbin", "/lib", "/root"}
	for _, cp := range criticalPaths {
		if path == cp {
			return os.ErrPermission
		}
	}

	err := os.RemoveAll(path)
	if err != nil {
		return err
	}

	*resp = true
	return nil
}

// ChmodFile changes file permissions
func (a *Agent) ChmodFile(req *ChmodRequest, resp *bool) error {
	path := filepath.Clean(req.Path)
	if strings.Contains(path, "..") {
		return os.ErrPermission
	}

	// Parse permission string (e.g., "755")
	mode, err := parseOctalPermissions(req.Permissions)
	if err != nil {
		return err
	}

	err = os.Chmod(path, mode)
	if err != nil {
		return err
	}

	*resp = true
	return nil
}

// RenameFile renames or moves a file
func (a *Agent) RenameFile(req *RenameRequest, resp *bool) error {
	oldPath := filepath.Clean(req.OldPath)
	newPath := filepath.Clean(req.NewPath)

	if strings.Contains(oldPath, "..") || strings.Contains(newPath, "..") {
		return os.ErrPermission
	}

	err := os.Rename(oldPath, newPath)
	if err != nil {
		return err
	}

	*resp = true
	return nil
}

// UploadFile handles file upload
func (a *Agent) UploadFile(req *UploadFileRequest, resp *bool) error {
	path := filepath.Clean(filepath.Join(req.Path, req.Name))
	if strings.Contains(path, "..") {
		return os.ErrPermission
	}

	// Decode base64 content
	content, err := base64.StdEncoding.DecodeString(req.Content)
	if err != nil {
		return err
	}

	// Create parent directories
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	err = os.WriteFile(path, content, 0644)
	if err != nil {
		return err
	}

	*resp = true
	return nil
}

// Helper function to parse octal permissions
func parseOctalPermissions(perm string) (os.FileMode, error) {
	var mode os.FileMode
	for _, c := range perm {
		if c < '0' || c > '7' {
			return 0, os.ErrInvalid
		}
		mode = mode*8 + os.FileMode(c-'0')
	}
	return mode, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
