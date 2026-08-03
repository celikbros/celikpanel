//go:build !linux

package main

import "errors"

var errSecureSiteFilesUnsupported = errors.New("secure site file management requires Linux openat2")

func secureListSiteFiles(*ListFilesRequest, *ListFilesResponse) error {
	return errSecureSiteFilesUnsupported
}

func secureReadSiteFile(*ReadFileRequest, *ReadFileResponse) error {
	return errSecureSiteFilesUnsupported
}

func secureWriteSiteFile(_ *WriteFileRequest, resp *bool) error {
	if resp != nil {
		*resp = false
	}
	return errSecureSiteFilesUnsupported
}

func secureCreateSiteFile(_ *CreateRequest, resp *bool) error {
	if resp != nil {
		*resp = false
	}
	return errSecureSiteFilesUnsupported
}

func secureDeleteSiteFile(_ *DeleteRequest, resp *bool) error {
	if resp != nil {
		*resp = false
	}
	return errSecureSiteFilesUnsupported
}

func secureChmodSiteFile(_ *ChmodRequest, resp *bool) error {
	if resp != nil {
		*resp = false
	}
	return errSecureSiteFilesUnsupported
}

func secureRenameSiteFile(_ *RenameRequest, resp *bool) error {
	if resp != nil {
		*resp = false
	}
	return errSecureSiteFilesUnsupported
}

func secureUploadSiteFile(_ *UploadFileRequest, resp *bool) error {
	if resp != nil {
		*resp = false
	}
	return errSecureSiteFilesUnsupported
}
