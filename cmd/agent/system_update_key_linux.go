//go:build linux

package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

func loadSystemUpdatePublicKey(path string) (ed25519.PublicKey, error) {
	if filepath.Clean(path) != systemUpdateKeyPath {
		return nil, errors.New("release public-key path is not the pinned canonical path")
	}
	raw, err := readRootOwnedSystemUpdateFile(path, 4096, true)
	if err != nil {
		return nil, err
	}
	return parseSystemUpdatePublicKey(raw)
}

func parseSystemUpdatePublicKey(raw []byte) (ed25519.PublicKey, error) {
	block, rest := pem.Decode(raw)
	if block == nil || block.Type != "PUBLIC KEY" || len(block.Headers) != 0 || len(rest) != 0 {
		return nil, errors.New("release public key is not one exact public PEM block")
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse release public key: %w", err)
	}
	publicKey, ok := parsed.(ed25519.PublicKey)
	if !ok || len(publicKey) != ed25519.PublicKeySize {
		return nil, errors.New("release public key is not Ed25519")
	}
	canonical := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: block.Bytes})
	if !bytes.Equal(raw, canonical) {
		return nil, errors.New("release public key PEM is not canonical")
	}
	return append(ed25519.PublicKey(nil), publicKey...), nil
}

// readRootOwnedSystemUpdateFile walks from / with O_NOFOLLOW and validates
// every directory before opening a single-link trusted regular leaf. The
// pinned public-key directory may be root:celikpanel 0750, matching the real
// ConfigurationDirectory contract; it is still root-owned and non-writable by
// group/other. Private release state remains root:root only.
func readRootOwnedSystemUpdateFile(path string, maximum int64, publicReadable bool) ([]byte, error) {
	clean := filepath.Clean(path)
	if clean != path || !filepath.IsAbs(clean) || maximum <= 0 {
		return nil, errors.New("trusted file path is invalid")
	}
	parts := strings.Split(strings.TrimPrefix(clean, "/"), "/")
	if len(parts) < 2 {
		return nil, errors.New("trusted file path is too shallow")
	}
	current, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer func() { _ = unix.Close(current) }()
	testPath := clean != systemUpdateKeyPath && systemUpdateIsTestPath(clean)
	temporaryRoot := "/tmp"
	walked := string(os.PathSeparator)
	for _, part := range parts[:len(parts)-1] {
		if part == "" || part == "." || part == ".." {
			return nil, errors.New("trusted file path component is invalid")
		}
		next, openErr := unix.Openat(current, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if openErr != nil {
			return nil, fmt.Errorf("open trusted directory %s: %w", part, openErr)
		}
		var stat unix.Stat_t
		walked = filepath.Join(walked, part)
		unsafeTestRoot := testPath && walked == temporaryRoot
		expectedUID, expectedGID := uint32(0), uint32(0)
		// Only /etc/celikpanel itself may use the service group. Every higher
		// ancestor remains root:root, matching the reviewed installer.
		requireGroup := systemUpdateDirectoryRequiresRootGroup(clean, walked)
		if testPath && !unsafeTestRoot {
			expectedUID, expectedGID = uint32(os.Geteuid()), uint32(os.Getegid())
			requireGroup = true
		}
		statErr := unix.Fstat(next, &stat)
		safeMetadata := trustedSystemUpdateDirectoryMetadata(&stat, expectedUID, expectedGID, requireGroup, unsafeTestRoot)
		if statErr != nil || !safeMetadata {
			unix.Close(next)
			if statErr != nil {
				return nil, statErr
			}
			return nil, fmt.Errorf("trusted directory %s has unsafe metadata", part)
		}
		unix.Close(current)
		current = next
	}
	fd, err := unix.Openat(current, parts[len(parts)-1], unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), clean)
	if file == nil {
		unix.Close(fd)
		return nil, errors.New("open trusted file handle")
	}
	defer file.Close()
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return nil, err
	}
	mode := stat.Mode & 0o777
	expectedUID, expectedGID := systemUpdateExpectedOwner(clean)
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Mode&0o7000 != 0 || stat.Nlink != 1 || stat.Uid != expectedUID || stat.Gid != expectedGID || stat.Size < 1 || stat.Size > maximum || mode&0o022 != 0 {
		return nil, errors.New("trusted file has unsafe metadata")
	}
	if publicReadable {
		if mode != 0o600 && mode != 0o644 {
			return nil, errors.New("trusted public file permissions are invalid")
		}
	} else if mode != 0o600 {
		return nil, errors.New("trusted private state file permissions are invalid")
	}
	raw := make([]byte, stat.Size)
	if _, err := io.ReadFull(file, raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func systemUpdateDirectoryRequiresRootGroup(filePath, directory string) bool {
	return filePath != systemUpdateKeyPath || directory != filepath.Dir(systemUpdateKeyPath)
}

func trustedSystemUpdateDirectoryMetadata(stat *unix.Stat_t, uid, gid uint32, requireGroup, allowWritable bool) bool {
	if stat == nil || stat.Mode&unix.S_IFMT != unix.S_IFDIR || stat.Uid != uid {
		return false
	}
	if requireGroup && stat.Gid != gid {
		return false
	}
	return allowWritable || stat.Mode&0o022 == 0
}
