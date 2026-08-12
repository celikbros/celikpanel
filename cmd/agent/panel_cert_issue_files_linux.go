//go:build linux

package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

const (
	panelCertificateIssueVersionMaxCount = 128
	panelCertificateIssuePEMMaxSize      = 1 << 20
)

func stagePanelCertificateIssueMaterial(
	domain, tlsDir string,
	certificate, privateKey []byte,
	receipt panelCertificateIssueReceipt,
) (*panelCertificateIssueStage, error) {
	if tlsDir != managedPanelTLSDir || domain != receipt.Domain {
		return nil, errors.New("invalid panel certificate issue stage target")
	}
	if err := validatePanelCertificateIssueReceipt(receipt); err != nil {
		return nil, err
	}
	pair, err := tls.X509KeyPair(certificate, privateKey)
	if err != nil {
		return nil, fmt.Errorf("validate staged panel certificate pair: %w", err)
	}
	if len(pair.Certificate) == 0 {
		return nil, errors.New("validate staged panel certificate pair: chain is empty")
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return nil, fmt.Errorf("parse staged panel certificate leaf: %w", err)
	}
	if err := leaf.VerifyHostname(domain); err != nil {
		return nil, fmt.Errorf("validate staged panel certificate identity: %w", err)
	}
	if panelCertificateLeafSHA256(pair.Certificate[0]) != receipt.LeafSHA256 {
		return nil, errors.New("staged panel certificate leaf does not match receipt")
	}
	receiptRaw, err := canonicalPanelCertificateIssueReceipt(receipt)
	if err != nil {
		return nil, err
	}
	dirFD, panelGID, err := openManagedPanelTLSDirectory(tlsDir)
	if err != nil {
		return nil, err
	}
	versionName, err := randomPanelCertEntry(managedPanelCertVersionPrefix)
	if err != nil {
		unix.Close(dirFD)
		return nil, err
	}
	if err := unix.Mkdirat(dirFD, versionName, 0o750); err != nil {
		unix.Close(dirFD)
		return nil, fmt.Errorf("create certificate issue version directory: %w", err)
	}
	versionFD, err := openPanelCertDirectoryAt(dirFD, versionName)
	if err != nil {
		_ = unix.Unlinkat(dirFD, versionName, unix.AT_REMOVEDIR)
		unix.Close(dirFD)
		return nil, fmt.Errorf("open certificate issue version directory: %w", err)
	}
	staged := false
	defer func() {
		unix.Close(versionFD)
		if !staged {
			_ = removePanelCertificateIssueVersionFilesAt(dirFD, versionName)
			unix.Close(dirFD)
		}
	}()
	if err := unix.Fchown(versionFD, 0, panelGID); err != nil {
		return nil, fmt.Errorf("own certificate issue version directory: %w", err)
	}
	if err := unix.Fchmod(versionFD, 0o750); err != nil {
		return nil, fmt.Errorf("protect certificate issue version directory: %w", err)
	}
	for _, file := range []struct {
		name    string
		gid     int
		mode    uint32
		content []byte
	}{
		{name: "panel.crt", gid: panelGID, mode: 0o640, content: certificate},
		{name: "panel.key", gid: panelGID, mode: 0o640, content: privateKey},
		{name: "panel.domain", gid: panelGID, mode: 0o600, content: []byte(domain + "\n")},
		{name: panelCertificateIssueReceiptName, gid: 0, mode: 0o600, content: receiptRaw},
	} {
		if err := writePanelCertificateFile(
			versionFD, file.name, 0, file.gid, file.mode, file.content,
		); err != nil {
			return nil, err
		}
	}
	if err := unix.Fsync(versionFD); err != nil {
		return nil, fmt.Errorf("sync certificate issue version directory: %w", err)
	}
	if err := unix.Fsync(dirFD); err != nil {
		return nil, fmt.Errorf("sync staged panel certificate directory: %w", err)
	}
	staged = true
	stage := &panelCertificateIssueStage{}
	stage.publishAction = func() (bool, error) {
		return activatePanelCertificateIssueVersionAt(dirFD, versionName)
	}
	stage.cleanupAction = func(published bool) error {
		defer unix.Close(dirFD)
		if published {
			return nil
		}
		current, found, err := readCurrentPanelCertificateVersionAt(dirFD)
		if err != nil {
			return err
		}
		if found && current == versionName {
			return nil
		}
		if err := removeExactPanelCertificateIssueVersionAt(
			dirFD, versionName, receipt,
		); err != nil {
			return err
		}
		return unix.Fsync(dirFD)
	}
	return stage, nil
}

func activatePanelCertificateIssueVersionAt(
	dirFD int,
	versionName string,
) (published bool, err error) {
	if !validManagedPanelCertVersionName(versionName) {
		return false, errors.New("invalid staged panel certificate version")
	}
	linkName, err := randomPanelCertEntry(".current-")
	if err != nil {
		return false, err
	}
	if err := unix.Symlinkat(versionName, dirFD, linkName); err != nil {
		return false, fmt.Errorf("create staged panel certificate link: %w", err)
	}
	linkPublished := false
	defer func() {
		if !linkPublished {
			_ = unix.Unlinkat(dirFD, linkName, 0)
		}
	}()
	if err := unix.Renameat(dirFD, linkName, dirFD, "current"); err != nil {
		return false, fmt.Errorf("activate panel certificate atomically: %w", err)
	}
	linkPublished = true
	if err := unix.Fsync(dirFD); err != nil {
		return true, fmt.Errorf(
			"sync panel TLS directory after certificate activation: %w", err,
		)
	}
	return true, nil
}

func verifyPublishedPanelCertificateIssueReceipt(
	requestID, qualifier, domain string,
) (bool, error) {
	dirFD, err := openTrustedPanelTLSDirectoryOwned(managedPanelTLSDir, 0)
	if err != nil {
		return false, err
	}
	defer unix.Close(dirFD)
	_, receipt, leafDER, _, found, err :=
		readCurrentPanelCertificateIssueVersionAt(dirFD)
	if err != nil || !found {
		return false, err
	}
	return receipt.RequestID == requestID &&
		receipt.Qualifier == qualifier &&
		receipt.Domain == domain &&
		receipt.LeafSHA256 == panelCertificateLeafSHA256(leafDER), nil
}

func stabilizePublishedPanelCertificateIssue() error {
	dirFD, err := openTrustedPanelTLSDirectoryOwned(managedPanelTLSDir, 0)
	if err != nil {
		return err
	}
	defer unix.Close(dirFD)
	if err := unix.Fsync(dirFD); err != nil {
		return fmt.Errorf("sync trusted panel TLS directory: %w", err)
	}
	return nil
}

func reconcilePersistedPanelCertificateIssueHost(
	ctx context.Context,
	requestID, qualifier, domain string,
) (success bool, err error) {
	return reconcilePersistedPanelCertificateIssueHostAt(
		ctx,
		managedPanelTLSDir,
		requestID,
		qualifier,
		domain,
	)
}

func reconcilePersistedPanelCertificateIssueHostAt(
	ctx context.Context,
	tlsDir, requestID, qualifier, domain string,
) (success bool, err error) {
	if ctx == nil {
		return false, errors.New("panel certificate issue recovery context is required")
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	err = panelCertWithPublishLock(func() error {
		dirFD, openErr := openTrustedPanelTLSDirectoryOwned(tlsDir, 0)
		if openErr != nil {
			if errors.Is(openErr, os.ErrNotExist) {
				return clearInterruptedPanelCertificateActivation(
					requestID,
					qualifier,
					domain,
				)
			}
			return openErr
		}
		defer unix.Close(dirFD)
		currentVersion, receipt, leafDER, notAfter, found, readErr :=
			readCurrentPanelCertificateIssueVersionAt(dirFD)
		if readErr != nil {
			return readErr
		}
		if found && receipt.RequestID == requestID &&
			receipt.Qualifier == qualifier && receipt.Domain == domain {
			if err := unix.Fsync(dirFD); err != nil {
				return fmt.Errorf(
					"stabilize recovered panel certificate publication: %w", err,
				)
			}
			if err := ensurePublishedPanelCertificateActivation(
				receipt, leafDER, notAfter,
			); err != nil {
				return err
			}
			success = true
			return nil
		}
		matches, scanErr := findPanelCertificateIssueVersionsAt(
			dirFD, requestID, qualifier, domain,
		)
		if scanErr != nil {
			return scanErr
		}
		for _, match := range matches {
			if match == currentVersion {
				return errors.New(
					"current panel certificate receipt conflicts with recovery identity",
				)
			}
			versionFD, openErr := openPanelCertDirectoryAt(dirFD, match)
			if openErr != nil {
				return openErr
			}
			exactReceipt, exists, readErr :=
				readPanelCertificateIssueReceiptAt(versionFD)
			unix.Close(versionFD)
			if readErr != nil {
				return readErr
			}
			if !exists {
				return errors.New(
					"panel certificate issue stage receipt disappeared",
				)
			}
			if err := removeExactPanelCertificateIssueVersionAt(
				dirFD, match, exactReceipt,
			); err != nil {
				return err
			}
		}
		if len(matches) > 0 {
			if err := unix.Fsync(dirFD); err != nil {
				return fmt.Errorf(
					"sync panel TLS directory after stage cleanup: %w", err,
				)
			}
		}
		return clearInterruptedPanelCertificateActivation(
			requestID, qualifier, domain,
		)
	})
	return success, err
}

func readCurrentPanelCertificateIssueVersionAt(
	dirFD int,
) (
	version string,
	receipt panelCertificateIssueReceipt,
	leafDER []byte,
	notAfter time.Time,
	found bool,
	err error,
) {
	version, currentFound, err := readCurrentPanelCertificateVersionAt(dirFD)
	if err != nil || !currentFound {
		return "", panelCertificateIssueReceipt{}, nil, time.Time{}, false, err
	}
	versionFD, err := openPanelCertDirectoryAt(dirFD, version)
	if err != nil {
		return "", panelCertificateIssueReceipt{}, nil, time.Time{}, false,
			fmt.Errorf("open current panel certificate version: %w", err)
	}
	defer unix.Close(versionFD)
	receipt, receiptFound, err := readPanelCertificateIssueReceiptAt(versionFD)
	if err != nil || !receiptFound {
		return version, panelCertificateIssueReceipt{}, nil, time.Time{}, false, err
	}
	leafDER, notAfter, err = verifyPanelCertificateIssueVersionAt(
		versionFD, receipt,
	)
	if err != nil {
		return version, panelCertificateIssueReceipt{}, nil, time.Time{}, false, err
	}
	return version, receipt, leafDER, notAfter, true, nil
}

func readCurrentPanelCertificateVersionAt(dirFD int) (string, bool, error) {
	var stat unix.Stat_t
	if err := unix.Fstatat(
		dirFD, "current", &stat, unix.AT_SYMLINK_NOFOLLOW,
	); err != nil {
		if errors.Is(err, unix.ENOENT) {
			return "", false, nil
		}
		return "", false, fmt.Errorf(
			"inspect current panel certificate link: %w", err,
		)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFLNK || stat.Uid != 0 {
		return "", false, errors.New(
			"current panel certificate link is not trusted",
		)
	}
	target := make([]byte, 256)
	n, err := unix.Readlinkat(dirFD, "current", target)
	if err != nil {
		return "", false, fmt.Errorf("read current panel certificate link: %w", err)
	}
	if n == len(target) {
		return "", false, errors.New("current panel certificate link is too long")
	}
	version := string(target[:n])
	if !validManagedPanelCertVersionName(version) {
		return "", false, errors.New(
			"current panel certificate link target is invalid",
		)
	}
	return version, true, nil
}

func readPanelCertificateIssueReceiptAt(
	versionFD int,
) (panelCertificateIssueReceipt, bool, error) {
	fd, err := unix.Openat2(
		versionFD,
		panelCertificateIssueReceiptName,
		&unix.OpenHow{
			Flags: uint64(
				unix.O_RDONLY |
					unix.O_CLOEXEC |
					unix.O_NOFOLLOW |
					unix.O_NONBLOCK,
			),
			Resolve: panelCertSecureResolve,
		},
	)
	if errors.Is(err, unix.ENOENT) {
		return panelCertificateIssueReceipt{}, false, nil
	}
	if err != nil {
		return panelCertificateIssueReceipt{}, false, fmt.Errorf(
			"open panel certificate issue receipt: %w", err,
		)
	}
	file := os.NewFile(uintptr(fd), panelCertificateIssueReceiptName)
	if file == nil {
		unix.Close(fd)
		return panelCertificateIssueReceipt{}, false, errors.New(
			"open panel certificate issue receipt: invalid descriptor",
		)
	}
	defer file.Close()
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return panelCertificateIssueReceipt{}, false, err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG ||
		stat.Uid != 0 ||
		stat.Gid != 0 ||
		stat.Mode&0o7777 != 0o600 ||
		stat.Nlink != 1 ||
		stat.Size < 1 ||
		stat.Size > panelCertificateIssueReceiptMaxSize {
		return panelCertificateIssueReceipt{}, false, errors.New(
			"panel certificate issue receipt must be root-owned single-link 0600",
		)
	}
	raw, err := io.ReadAll(io.LimitReader(
		file, panelCertificateIssueReceiptMaxSize+1,
	))
	if err != nil {
		return panelCertificateIssueReceipt{}, false, err
	}
	if int64(len(raw)) != stat.Size {
		return panelCertificateIssueReceipt{}, false, errors.New(
			"panel certificate issue receipt changed while read",
		)
	}
	receipt, err := decodePanelCertificateIssueReceipt(raw)
	return receipt, err == nil, err
}

func verifyPanelCertificateIssueVersionAt(
	versionFD int,
	receipt panelCertificateIssueReceipt,
) ([]byte, time.Time, error) {
	domain, err := readPanelCertificateDomainAt(versionFD, 0)
	if err != nil {
		return nil, time.Time{}, err
	}
	if domain != receipt.Domain {
		return nil, time.Time{}, errors.New(
			"panel certificate issue receipt domain mismatch",
		)
	}
	certificate, err := readPanelCertificateIssueRegularFileAt(
		versionFD, "panel.crt", 0o640, panelCertificateIssuePEMMaxSize,
	)
	if err != nil {
		return nil, time.Time{}, err
	}
	block, _ := pem.Decode(certificate)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, time.Time{}, errors.New(
			"panel certificate issue version has invalid leaf PEM",
		)
	}
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, time.Time{}, err
	}
	if err := leaf.VerifyHostname(receipt.Domain); err != nil {
		return nil, time.Time{}, err
	}
	if panelCertificateLeafSHA256(leaf.Raw) != receipt.LeafSHA256 {
		return nil, time.Time{}, errors.New(
			"panel certificate issue receipt leaf mismatch",
		)
	}
	return leaf.Raw, leaf.NotAfter, nil
}

func readPanelCertificateIssueRegularFileAt(
	dirFD int,
	name string,
	mode uint32,
	maxSize int64,
) ([]byte, error) {
	fd, err := unix.Openat2(dirFD, name, &unix.OpenHow{
		Flags: uint64(
			unix.O_RDONLY |
				unix.O_CLOEXEC |
				unix.O_NOFOLLOW |
				unix.O_NONBLOCK,
		),
		Resolve: panelCertSecureResolve,
	})
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), name)
	if file == nil {
		unix.Close(fd)
		return nil, errors.New("invalid panel certificate file descriptor")
	}
	defer file.Close()
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return nil, err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG ||
		stat.Uid != 0 ||
		stat.Mode&0o7777 != mode ||
		stat.Nlink != 1 ||
		stat.Size < 1 ||
		stat.Size > maxSize {
		return nil, errors.New(
			"panel certificate issue version file is not trusted",
		)
	}
	data, err := io.ReadAll(io.LimitReader(file, maxSize+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) != stat.Size {
		return nil, errors.New(
			"panel certificate issue version changed while read",
		)
	}
	return data, nil
}

func findPanelCertificateIssueVersionsAt(
	dirFD int,
	requestID, qualifier, domain string,
) ([]string, error) {
	duplicateFD, err := unix.Dup(dirFD)
	if err != nil {
		return nil, err
	}
	directory := os.NewFile(uintptr(duplicateFD), "panel-tls")
	if directory == nil {
		unix.Close(duplicateFD)
		return nil, errors.New("invalid panel TLS directory descriptor")
	}
	defer directory.Close()
	entries, err := directory.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	count := 0
	var matches []string
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, managedPanelCertVersionPrefix) {
			continue
		}
		count++
		if count > panelCertificateIssueVersionMaxCount ||
			!validManagedPanelCertVersionName(name) {
			return nil, errors.New(
				"panel certificate issue recovery versions are ambiguous",
			)
		}
		versionFD, err := openPanelCertDirectoryAt(dirFD, name)
		if err != nil {
			return nil, err
		}
		receipt, found, readErr :=
			readPanelCertificateIssueReceiptAt(versionFD)
		if readErr == nil &&
			found &&
			receipt.RequestID == requestID &&
			receipt.Qualifier == qualifier &&
			receipt.Domain == domain {
			_, _, readErr = verifyPanelCertificateIssueVersionAt(
				versionFD, receipt,
			)
			matches = append(matches, name)
		}
		unix.Close(versionFD)
		if readErr != nil {
			return nil, readErr
		}
	}
	if len(matches) > 1 {
		return nil, errors.New(
			"multiple panel certificate issue versions match recovery identity",
		)
	}
	return matches, nil
}

func removeExactPanelCertificateIssueVersionAt(
	dirFD int,
	versionName string,
	expected panelCertificateIssueReceipt,
) error {
	if !validManagedPanelCertVersionName(versionName) {
		return errors.New("invalid panel certificate issue cleanup version")
	}
	versionFD, err := openPanelCertDirectoryAt(dirFD, versionName)
	if err != nil {
		return err
	}
	receipt, found, err :=
		readPanelCertificateIssueReceiptAt(versionFD)
	unix.Close(versionFD)
	if err != nil {
		return err
	}
	if !found || receipt != expected {
		return errors.New(
			"panel certificate issue cleanup receipt mismatch",
		)
	}
	return removePanelCertificateIssueVersionFilesAt(dirFD, versionName)
}

func removePanelCertificateIssueVersionFilesAt(
	dirFD int,
	versionName string,
) error {
	versionFD, err := openPanelCertDirectoryAt(dirFD, versionName)
	if err != nil {
		return err
	}
	for _, name := range []string{
		panelCertificateIssueReceiptName,
		"panel.domain",
		"panel.key",
		"panel.crt",
	} {
		if err := unix.Unlinkat(versionFD, name, 0); err != nil &&
			!errors.Is(err, unix.ENOENT) {
			unix.Close(versionFD)
			return err
		}
	}
	unix.Close(versionFD)
	if err := unix.Unlinkat(
		dirFD, versionName, unix.AT_REMOVEDIR,
	); err != nil {
		return err
	}
	return nil
}

func ensurePublishedPanelCertificateActivation(
	receipt panelCertificateIssueReceipt,
	leafDER []byte,
	notAfter time.Time,
) error {
	existing, found, err := panelCertificateActivationReadState()
	if err != nil {
		return err
	}
	if found {
		if existing.Origin != panelCertificateActivationOriginInteractive ||
			existing.RequestID != receipt.RequestID ||
			existing.Qualifier != receipt.Qualifier ||
			existing.Domain != receipt.Domain {
			return errors.New(
				"published panel certificate activation identity conflicts",
			)
		}
		if existing.Phase != panelCertificateActivationPendingSource {
			if existing.LeafSHA256 != receipt.LeafSHA256 {
				return errors.New(
					"published panel certificate activation leaf conflicts",
				)
			}
			return nil
		}
	}
	state, err := newInteractivePanelCertificateActivationState(
		receipt.Domain, receipt.RequestID, receipt.Qualifier,
	)
	if err != nil {
		return err
	}
	state, err = bindPanelCertificateActivationMaterial(
		state, leafDER, notAfter,
	)
	if err != nil {
		return err
	}
	return panelCertificateActivationWriteState(state)
}

func clearInterruptedPanelCertificateActivation(
	requestID, qualifier, domain string,
) error {
	state, found, err := panelCertificateActivationReadState()
	if err != nil || !found {
		return err
	}
	if state.Origin == panelCertificateActivationOriginInteractive &&
		state.RequestID == requestID &&
		state.Qualifier == qualifier &&
		state.Domain == domain {
		return panelCertificateActivationRemoveState()
	}
	return nil
}
