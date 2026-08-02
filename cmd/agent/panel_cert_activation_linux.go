//go:build linux

package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

const panelCertificateActivationSecureResolve = unix.RESOLVE_BENEATH |
	unix.RESOLVE_NO_SYMLINKS |
	unix.RESOLVE_NO_MAGICLINKS

var (
	panelCertificateActivationRenameat = unix.Renameat
	panelCertificateActivationFsync    = unix.Fsync
)

func readPanelCertificateActivationState() (
	panelCertificateActivationState,
	bool,
	error,
) {
	return readPanelCertificateActivationStateAt(
		filepath.Dir(panelCertificateActivationStatePath),
		0,
		0,
	)
}

func writePanelCertificateActivationState(
	state panelCertificateActivationState,
) error {
	return writePanelCertificateActivationStateAt(
		filepath.Dir(panelCertificateActivationStatePath),
		0,
		0,
		state,
	)
}

func removePanelCertificateActivationState() error {
	return removePanelCertificateActivationStateAt(
		filepath.Dir(panelCertificateActivationStatePath),
		0,
		0,
	)
}

func readPanelCertificateActivationStateAt(
	directory string,
	expectedUID, expectedGID int,
) (panelCertificateActivationState, bool, error) {
	directoryFD, err := openPanelCertificateActivationStateDirectory(
		directory,
		expectedUID,
	)
	if err != nil {
		return panelCertificateActivationState{}, false, err
	}
	defer unix.Close(directoryFD)

	return readPanelCertificateActivationStateFromDirectory(
		directoryFD,
		expectedUID,
		expectedGID,
	)
}

func writePanelCertificateActivationStateAt(
	directory string,
	expectedUID, expectedGID int,
	state panelCertificateActivationState,
) error {
	content, err := canonicalPanelCertificateActivationState(state)
	if err != nil {
		return err
	}
	directoryFD, err := openPanelCertificateActivationStateDirectory(
		directory,
		expectedUID,
	)
	if err != nil {
		return err
	}
	defer unix.Close(directoryFD)

	// Refuse to replace an unsafe or corrupt existing intent. This keeps a
	// damaged durable recovery record visible instead of silently erasing it.
	if _, _, err := readPanelCertificateActivationStateFromDirectory(
		directoryFD,
		expectedUID,
		expectedGID,
	); err != nil {
		return fmt.Errorf("inspect existing panel certificate activation state: %w", err)
	}

	tempName, tempFD, err := createPanelCertificateActivationTempFile(
		directoryFD,
	)
	if err != nil {
		return err
	}
	published := false
	defer func() {
		if !published {
			_ = unix.Unlinkat(directoryFD, tempName, 0)
		}
	}()

	tempFile := os.NewFile(
		uintptr(tempFD),
		filepath.Join(directory, tempName),
	)
	if tempFile == nil {
		unix.Close(tempFD)
		return errors.New("create panel certificate activation state: invalid file descriptor")
	}
	closed := false
	defer func() {
		if !closed {
			_ = tempFile.Close()
		}
	}()

	if err := unix.Fchown(tempFD, expectedUID, expectedGID); err != nil {
		return fmt.Errorf("set panel certificate activation state ownership: %w", err)
	}
	if err := unix.Fchmod(tempFD, 0o600); err != nil {
		return fmt.Errorf("set panel certificate activation state permissions: %w", err)
	}
	if _, err := io.Copy(tempFile, bytes.NewReader(content)); err != nil {
		return fmt.Errorf("write panel certificate activation state: %w", err)
	}
	if err := tempFile.Sync(); err != nil {
		return fmt.Errorf("sync panel certificate activation state: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("close panel certificate activation state: %w", err)
	}
	closed = true

	if err := panelCertificateActivationRenameat(
		directoryFD,
		tempName,
		directoryFD,
		panelCertificateActivationStateName,
	); err != nil {
		return fmt.Errorf("publish panel certificate activation state: %w", err)
	}
	published = true
	if err := panelCertificateActivationFsync(directoryFD); err != nil {
		return fmt.Errorf("sync panel certificate activation state directory: %w", err)
	}
	return nil
}

func removePanelCertificateActivationStateAt(
	directory string,
	expectedUID, expectedGID int,
) error {
	directoryFD, err := openPanelCertificateActivationStateDirectory(
		directory,
		expectedUID,
	)
	if err != nil {
		return err
	}
	defer unix.Close(directoryFD)

	_, exists, err := readPanelCertificateActivationStateFromDirectory(
		directoryFD,
		expectedUID,
		expectedGID,
	)
	if err != nil {
		return fmt.Errorf("inspect panel certificate activation state before removal: %w", err)
	}
	if !exists {
		return nil
	}
	if err := unix.Unlinkat(
		directoryFD,
		panelCertificateActivationStateName,
		0,
	); err != nil {
		return fmt.Errorf("remove panel certificate activation state: %w", err)
	}
	if err := panelCertificateActivationFsync(directoryFD); err != nil {
		return fmt.Errorf("sync panel certificate activation state directory: %w", err)
	}
	return nil
}

func openPanelCertificateActivationStateDirectory(
	directory string,
	expectedUID int,
) (int, error) {
	if expectedUID < 0 {
		return -1, errors.New("invalid panel certificate activation directory owner")
	}
	clean := filepath.Clean(directory)
	if directory != clean || !filepath.IsAbs(clean) || clean == string(os.PathSeparator) {
		return -1, errors.New(
			"panel certificate activation directory must be an absolute canonical path",
		)
	}
	relative := strings.TrimPrefix(clean, string(os.PathSeparator))
	rootFD, err := unix.Open(
		string(os.PathSeparator),
		unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		0,
	)
	if err != nil {
		return -1, fmt.Errorf("open panel certificate activation filesystem root: %w", err)
	}
	defer unix.Close(rootFD)

	directoryFD, err := unix.Openat2(rootFD, relative, &unix.OpenHow{
		Flags: uint64(
			unix.O_RDONLY |
				unix.O_DIRECTORY |
				unix.O_CLOEXEC |
				unix.O_NOFOLLOW,
		),
		Resolve: panelCertificateActivationSecureResolve,
	})
	if err != nil {
		if errors.Is(err, unix.ENOSYS) {
			return -1, fmt.Errorf(
				"open panel certificate activation directory refused because secure openat2 resolution is unavailable: %w",
				err,
			)
		}
		return -1, fmt.Errorf("open panel certificate activation directory: %w", err)
	}

	var stat unix.Stat_t
	if err := unix.Fstat(directoryFD, &stat); err != nil {
		unix.Close(directoryFD)
		return -1, fmt.Errorf("stat panel certificate activation directory: %w", err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR ||
		int(stat.Uid) != expectedUID ||
		stat.Mode&0o7777 != 0o700 {
		unix.Close(directoryFD)
		return -1, errors.New(
			"panel certificate activation directory must be owner-controlled mode 0700",
		)
	}
	return directoryFD, nil
}

func readPanelCertificateActivationStateFromDirectory(
	directoryFD, expectedUID, expectedGID int,
) (panelCertificateActivationState, bool, error) {
	if expectedUID < 0 || expectedGID < 0 {
		return panelCertificateActivationState{}, false, errors.New(
			"invalid panel certificate activation state owner",
		)
	}
	fd, err := unix.Openat2(
		directoryFD,
		panelCertificateActivationStateName,
		&unix.OpenHow{
			Flags: uint64(
				unix.O_RDONLY |
					unix.O_CLOEXEC |
					unix.O_NOFOLLOW |
					unix.O_NONBLOCK,
			),
			Resolve: panelCertificateActivationSecureResolve,
		},
	)
	if errors.Is(err, unix.ENOENT) {
		return panelCertificateActivationState{}, false, nil
	}
	if err != nil {
		return panelCertificateActivationState{}, false, fmt.Errorf(
			"open panel certificate activation state: %w",
			err,
		)
	}
	file := os.NewFile(uintptr(fd), panelCertificateActivationStateName)
	if file == nil {
		unix.Close(fd)
		return panelCertificateActivationState{}, false, errors.New(
			"open panel certificate activation state: invalid file descriptor",
		)
	}
	defer file.Close()

	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return panelCertificateActivationState{}, false, fmt.Errorf(
			"stat panel certificate activation state: %w",
			err,
		)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG ||
		int(stat.Uid) != expectedUID ||
		int(stat.Gid) != expectedGID ||
		stat.Mode&0o7777 != 0o600 ||
		stat.Nlink != 1 {
		return panelCertificateActivationState{}, false, errors.New(
			"panel certificate activation state must be an owned single-link 0600 regular file",
		)
	}
	if stat.Size < 1 || stat.Size > panelCertificateActivationStateMaxSize {
		return panelCertificateActivationState{}, false, errors.New(
			"panel certificate activation state has invalid size",
		)
	}
	raw, err := io.ReadAll(io.LimitReader(
		file,
		panelCertificateActivationStateMaxSize+1,
	))
	if err != nil {
		return panelCertificateActivationState{}, false, fmt.Errorf(
			"read panel certificate activation state: %w",
			err,
		)
	}
	if len(raw) > panelCertificateActivationStateMaxSize || int64(len(raw)) != stat.Size {
		return panelCertificateActivationState{}, false, errors.New(
			"panel certificate activation state changed while it was read",
		)
	}
	state, err := decodePanelCertificateActivationState(raw)
	if err != nil {
		return panelCertificateActivationState{}, false, err
	}
	return state, true, nil
}

func createPanelCertificateActivationTempFile(directoryFD int) (string, int, error) {
	var lastErr error
	for attempt := 0; attempt < 16; attempt++ {
		random := make([]byte, 12)
		if _, err := rand.Read(random); err != nil {
			return "", -1, fmt.Errorf(
				"prepare atomic panel certificate activation state write: %w",
				err,
			)
		}
		name := "." + panelCertificateActivationStateName + "." +
			hex.EncodeToString(random) + ".tmp"
		fd, err := unix.Openat(
			directoryFD,
			name,
			unix.O_WRONLY|
				unix.O_CREAT|
				unix.O_EXCL|
				unix.O_CLOEXEC|
				unix.O_NOFOLLOW,
			0o600,
		)
		if errors.Is(err, unix.EEXIST) {
			lastErr = err
			continue
		}
		if err != nil {
			return "", -1, fmt.Errorf(
				"create atomic panel certificate activation state: %w",
				err,
			)
		}
		return name, fd, nil
	}
	return "", -1, fmt.Errorf(
		"create atomic panel certificate activation state: no unique temporary name: %w",
		lastErr,
	)
}
