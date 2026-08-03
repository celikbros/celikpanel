//go:build !linux

package main

import (
	"errors"
	"os"
)

var errSecureFileManagerUnsupported = errors.New("secure file manager requires Linux openat2")

func secureListFiles(string, string, int) ([]FileInfo, error) {
	return nil, errSecureFileManagerUnsupported
}

func secureReadFile(string, string, int64) ([]byte, FileInfo, error) {
	return nil, FileInfo{}, errSecureFileManagerUnsupported
}

func secureWriteFile(string, string, []byte) error {
	return errSecureFileManagerUnsupported
}

func secureCreateFileOrDir(string, string, bool) error {
	return errSecureFileManagerUnsupported
}

func secureDeleteFileOrDir(string, string) error {
	return errSecureFileManagerUnsupported
}

func secureChmodFile(string, string, os.FileMode) error {
	return errSecureFileManagerUnsupported
}

func secureRenameFile(string, string, string) error {
	return errSecureFileManagerUnsupported
}

func secureUploadFile(string, string, []byte) error {
	return errSecureFileManagerUnsupported
}
