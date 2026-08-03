package main

import (
	"os"

	"github.com/alicelik/celikpanel/internal/transport"
)

// FileInfo represents a file or directory
type FileInfo = transport.FileInfo

// ListFilesRequest for listing directory contents
type ListFilesRequest = transport.ListFilesRequest

// ListFilesResponse contains directory listing
type ListFilesResponse = transport.ListFilesResponse

// ReadFileRequest for reading file content
type ReadFileRequest = transport.ReadFileRequest

// ReadFileResponse contains file content
type ReadFileResponse = transport.ReadFileResponse

// WriteFileRequest for writing file content
type WriteFileRequest = transport.WriteFileRequest

// CreateRequest for creating file or directory
type CreateRequest = transport.CreateFileRequest

// DeleteRequest for deleting file or directory
type DeleteRequest = transport.DeleteFileRequest

// ChmodRequest for changing permissions
type ChmodRequest = transport.ChmodFileRequest

// RenameRequest for renaming/moving files
type RenameRequest = transport.RenameFileRequest

// UploadFileRequest for uploading files
type UploadFileRequest = transport.UploadFileRequest

// File Manager RPC Methods

// ListFiles lists directory contents
func (a *Agent) ListFiles(req *ListFilesRequest, resp *ListFilesResponse) error {
	return secureListSiteFiles(req, resp)
}

// ReadFile reads file content
func (a *Agent) ReadFile(req *ReadFileRequest, resp *ReadFileResponse) error {
	return secureReadSiteFile(req, resp)
}

// WriteFile writes content to file
func (a *Agent) WriteFile(req *WriteFileRequest, resp *bool) error {
	return secureWriteSiteFile(req, resp)
}

// CreateFileOrDir creates a file or directory
func (a *Agent) CreateFileOrDir(req *CreateRequest, resp *bool) error {
	return secureCreateSiteFile(req, resp)
}

// DeleteFileOrDir deletes a file or directory
func (a *Agent) DeleteFileOrDir(req *DeleteRequest, resp *bool) error {
	return secureDeleteSiteFile(req, resp)
}

// ChmodFile changes file permissions
func (a *Agent) ChmodFile(req *ChmodRequest, resp *bool) error {
	return secureChmodSiteFile(req, resp)
}

// RenameFile renames or moves a file
func (a *Agent) RenameFile(req *RenameRequest, resp *bool) error {
	return secureRenameSiteFile(req, resp)
}

// UploadFile handles file upload
func (a *Agent) UploadFile(req *UploadFileRequest, resp *bool) error {
	return secureUploadSiteFile(req, resp)
}

// Helper function to parse octal permissions
func parseOctalPermissions(perm string) (os.FileMode, error) {
	if len(perm) == 4 && perm[0] == '0' {
		perm = perm[1:]
	}
	if len(perm) != 3 {
		return 0, os.ErrInvalid
	}
	var mode os.FileMode
	for _, c := range perm {
		if c < '0' || c > '7' {
			return 0, os.ErrInvalid
		}
		mode = mode*8 + os.FileMode(c-'0')
	}
	return mode, nil
}
