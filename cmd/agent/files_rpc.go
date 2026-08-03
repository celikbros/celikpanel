package main

import (
	"encoding/base64"
	"fmt"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/hostingpath"
)

const (
	maxFileManagerFileBytes = 10 << 20
	maxFileManagerEntries   = 10_000
)

// FileInfo represents one entry below a tenant's derived document root.
// Path is always canonical and relative; the privileged agent never returns
// or accepts an absolute filesystem path through the file-manager contract.
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

type FileScopeRequest struct {
	SubscriptionID int    `json:"subscription_id"`
	DomainID       int    `json:"domain_id"`
	Path           string `json:"path"`
}

type ListFilesRequest = FileScopeRequest

type ListFilesResponse struct {
	Path  string     `json:"path"`
	Files []FileInfo `json:"files"`
}

type ReadFileRequest = FileScopeRequest

type ReadFileResponse struct {
	Path     string `json:"path"`
	Content  string `json:"content"`
	Size     int64  `json:"size"`
	IsBinary bool   `json:"is_binary"`
}

type WriteFileRequest struct {
	SubscriptionID int    `json:"subscription_id"`
	DomainID       int    `json:"domain_id"`
	Path           string `json:"path"`
	Content        string `json:"content"`
}

type CreateRequest struct {
	SubscriptionID int    `json:"subscription_id"`
	DomainID       int    `json:"domain_id"`
	Path           string `json:"path"`
	IsDir          bool   `json:"is_dir"`
}

type DeleteRequest = FileScopeRequest

type ChmodRequest struct {
	SubscriptionID int    `json:"subscription_id"`
	DomainID       int    `json:"domain_id"`
	Path           string `json:"path"`
	Permissions    string `json:"permissions"`
}

type RenameRequest struct {
	SubscriptionID int    `json:"subscription_id"`
	DomainID       int    `json:"domain_id"`
	OldPath        string `json:"old_path"`
	NewPath        string `json:"new_path"`
}

type UploadFileRequest struct {
	SubscriptionID int    `json:"subscription_id"`
	DomainID       int    `json:"domain_id"`
	Path           string `json:"path"`
	Name           string `json:"name"`
	Content        string `json:"content"`
}

// fileManagerScope is the trust boundary for every file-manager RPC. The
// root comes only from immutable numeric identities; the caller can supply
// only an already-canonical relative path.
func fileManagerScope(subscriptionID, domainID int, relativePath string) (string, string, error) {
	root, err := hostingpath.DocumentRoot(subscriptionID, domainID)
	if err != nil {
		return "", "", err
	}
	relativePath, err = hostingpath.ValidateRelativePath(relativePath)
	if err != nil {
		return "", "", fmt.Errorf("invalid relative path: %w", err)
	}
	return root, relativePath, nil
}

func (a *Agent) ListFiles(req *ListFilesRequest, resp *ListFilesResponse) error {
	root, relativePath, err := fileManagerScope(req.SubscriptionID, req.DomainID, req.Path)
	if err != nil {
		return err
	}

	files, err := secureListFiles(root, relativePath, maxFileManagerEntries)
	if err != nil {
		return err
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].IsDir != files[j].IsDir {
			return files[i].IsDir
		}
		return strings.ToLower(files[i].Name) < strings.ToLower(files[j].Name)
	})
	resp.Path = relativePath
	resp.Files = files
	return nil
}

func (a *Agent) ReadFile(req *ReadFileRequest, resp *ReadFileResponse) error {
	root, relativePath, err := fileManagerScope(req.SubscriptionID, req.DomainID, req.Path)
	if err != nil {
		return err
	}
	content, info, err := secureReadFile(root, relativePath, maxFileManagerFileBytes)
	if err != nil {
		return err
	}

	resp.Path = relativePath
	resp.Size = info.Size
	resp.IsBinary = false
	for _, b := range content[:min(len(content), 512)] {
		if b == 0 {
			resp.IsBinary = true
			break
		}
	}
	if resp.IsBinary {
		resp.Content = base64.StdEncoding.EncodeToString(content)
	} else {
		resp.Content = string(content)
	}
	return nil
}

func (a *Agent) WriteFile(req *WriteFileRequest, resp *bool) error {
	if len(req.Content) > maxFileManagerFileBytes {
		return fmt.Errorf("file content exceeds %d bytes", maxFileManagerFileBytes)
	}
	root, relativePath, err := fileManagerScope(req.SubscriptionID, req.DomainID, req.Path)
	if err != nil {
		return err
	}
	if err := secureWriteFile(root, relativePath, []byte(req.Content)); err != nil {
		return err
	}
	*resp = true
	return nil
}

func (a *Agent) CreateFileOrDir(req *CreateRequest, resp *bool) error {
	root, relativePath, err := fileManagerScope(req.SubscriptionID, req.DomainID, req.Path)
	if err != nil {
		return err
	}
	if err := secureCreateFileOrDir(root, relativePath, req.IsDir); err != nil {
		return err
	}
	*resp = true
	return nil
}

func (a *Agent) DeleteFileOrDir(req *DeleteRequest, resp *bool) error {
	root, relativePath, err := fileManagerScope(req.SubscriptionID, req.DomainID, req.Path)
	if err != nil {
		return err
	}
	if relativePath == "." {
		return fmt.Errorf("document root cannot be deleted")
	}
	if err := secureDeleteFileOrDir(root, relativePath); err != nil {
		return err
	}
	*resp = true
	return nil
}

func (a *Agent) ChmodFile(req *ChmodRequest, resp *bool) error {
	root, relativePath, err := fileManagerScope(req.SubscriptionID, req.DomainID, req.Path)
	if err != nil {
		return err
	}
	if relativePath == "." {
		return fmt.Errorf("document root permissions cannot be changed")
	}
	mode, err := parseOctalPermissions(req.Permissions)
	if err != nil {
		return err
	}
	if err := secureChmodFile(root, relativePath, mode); err != nil {
		return err
	}
	*resp = true
	return nil
}

func (a *Agent) RenameFile(req *RenameRequest, resp *bool) error {
	root, oldPath, err := fileManagerScope(req.SubscriptionID, req.DomainID, req.OldPath)
	if err != nil {
		return err
	}
	newRoot, newPath, err := fileManagerScope(req.SubscriptionID, req.DomainID, req.NewPath)
	if err != nil {
		return err
	}
	if root != newRoot {
		return os.ErrPermission
	}
	if oldPath == "." || newPath == "." {
		return fmt.Errorf("document root cannot be renamed")
	}
	if err := secureRenameFile(root, oldPath, newPath); err != nil {
		return err
	}
	*resp = true
	return nil
}

func (a *Agent) UploadFile(req *UploadFileRequest, resp *bool) error {
	if err := hostingpath.ValidateFileName(req.Name); err != nil {
		return fmt.Errorf("invalid upload file name: %w", err)
	}
	if len(req.Content) > base64.StdEncoding.EncodedLen(maxFileManagerFileBytes) {
		return fmt.Errorf("upload exceeds %d bytes", maxFileManagerFileBytes)
	}
	content, err := base64.StdEncoding.DecodeString(req.Content)
	if err != nil {
		return fmt.Errorf("invalid upload encoding: %w", err)
	}
	if len(content) > maxFileManagerFileBytes {
		return fmt.Errorf("upload exceeds %d bytes", maxFileManagerFileBytes)
	}

	root, relativeDir, err := fileManagerScope(req.SubscriptionID, req.DomainID, req.Path)
	if err != nil {
		return err
	}
	relativePath := path.Join(relativeDir, req.Name)
	if _, err := hostingpath.ValidateRelativePath(relativePath); err != nil {
		return err
	}
	if err := secureUploadFile(root, relativePath, content); err != nil {
		return err
	}
	*resp = true
	return nil
}

func parseOctalPermissions(perm string) (os.FileMode, error) {
	if len(perm) == 4 && perm[0] == '0' {
		perm = perm[1:]
	}
	if len(perm) != 3 {
		return 0, os.ErrInvalid
	}
	value, err := strconv.ParseUint(perm, 8, 16)
	if err != nil || value > 0o777 {
		return 0, os.ErrInvalid
	}
	return os.FileMode(value), nil
}
