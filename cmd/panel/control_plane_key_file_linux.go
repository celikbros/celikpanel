//go:build linux

package main

import (
	"errors"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

// maxControlPlaneKeyFileBytes bounds the inherited key input. A printed key is
// about seventy characters; anything materially larger is not a key.
const maxControlPlaneKeyFileBytes int64 = 256

// readControlPlaneKeyFile reads the backup key from the inherited descriptor
// with exactly the discipline the first-administrator credentials already use:
// a regular file must be a root-owned, single-link, 0600, size-bounded file, and
// a pipe is read under the same bound. The key is never written anywhere and
// never appears in a log line.
//
// readControlPlaneKeyFile, yedek anahtarını devralınan descriptor'dan, ilk
// yönetici kimlik bilgilerinin kullandığı disiplinin aynısıyla okur.
func readControlPlaneKeyFile(file *os.File) (string, error) {
	if file == nil {
		return "", errors.New("control-plane key input is unsafe")
	}
	var stat unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &stat); err != nil {
		return "", errors.New("control-plane key input is unsafe")
	}
	var content []byte
	var err error
	switch stat.Mode & unix.S_IFMT {
	case unix.S_IFREG:
		content, err = readInheritedRootOnlyFileContent(file, 0, maxControlPlaneKeyFileBytes)
	case unix.S_IFIFO:
		content, err = readBoundedInheritedStream(file, maxControlPlaneKeyFileBytes)
	default:
		err = errors.New("control-plane key input is unsafe")
	}
	if err != nil {
		return "", errors.New("control-plane key input is unsafe")
	}
	key := strings.TrimSpace(string(content))
	for index := range content {
		content[index] = 0
	}
	if key == "" {
		return "", errors.New("control-plane key input is unsafe")
	}
	return key, nil
}

func controlPlaneValidateRootOwnedDirectoryChain(path string) error {
	return validateRootOwnedSnapshotDirectoryChain(path)
}
