//go:build linux

package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

const (
	mailHostCertificateVersionMaxCount = 128
	mailHostCertificatePEMMaxSize      = 1 << 20
)

func stageMailHostCertificateMaterial(
	domain, tlsDir string,
	certificate, privateKey []byte,
	receipt mailHostCertificateReceipt,
) (*mailHostCertificateStage, error) {
	if tlsDir != managedMailHostTLSDir || domain != receipt.Domain {
		return nil, errors.New("invalid mail host certificate issue stage target")
	}
	if err := validateMailHostCertificateReceipt(receipt); err != nil {
		return nil, err
	}
	pair, err := tls.X509KeyPair(certificate, privateKey)
	if err != nil {
		return nil, fmt.Errorf("validate staged mail host certificate pair: %w", err)
	}
	if len(pair.Certificate) == 0 {
		return nil, errors.New("validate staged mail host certificate pair: chain is empty")
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return nil, fmt.Errorf("parse staged mail host certificate leaf: %w", err)
	}
	if err := leaf.VerifyHostname(domain); err != nil {
		return nil, fmt.Errorf("validate staged mail host certificate identity: %w", err)
	}
	if panelCertificateLeafSHA256(pair.Certificate[0]) != receipt.LeafSHA256 {
		return nil, errors.New("staged mail host certificate leaf does not match receipt")
	}
	receiptRaw, err := canonicalMailHostCertificateReceipt(receipt)
	if err != nil {
		return nil, err
	}
	dirFD, panelGID, err := openManagedMailHostTLSDirectory(tlsDir)
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
			_ = removeMailHostCertificateVersionFilesAt(dirFD, versionName)
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
		{name: "fullchain.pem", gid: panelGID, mode: 0o600, content: certificate},
		{name: "privkey.pem", gid: panelGID, mode: 0o600, content: privateKey},
		{name: "mail.domain", gid: panelGID, mode: 0o600, content: []byte(domain + "\n")},
		{name: mailHostCertificateReceiptName, gid: 0, mode: 0o600, content: receiptRaw},
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
		return nil, fmt.Errorf("sync staged mail host certificate directory: %w", err)
	}
	staged = true
	stage := &mailHostCertificateStage{}
	stage.publishAction = func() (bool, error) {
		return activateMailHostCertificateVersionAt(dirFD, versionName)
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
		if err := removeExactMailHostCertificateVersionAt(
			dirFD, versionName, receipt,
		); err != nil {
			return err
		}
		return unix.Fsync(dirFD)
	}
	return stage, nil
}

func activateMailHostCertificateVersionAt(
	dirFD int,
	versionName string,
) (published bool, err error) {
	if !validManagedPanelCertVersionName(versionName) {
		return false, errors.New("invalid staged mail host certificate version")
	}
	linkName, err := randomPanelCertEntry(".current-")
	if err != nil {
		return false, err
	}
	if err := unix.Symlinkat(versionName, dirFD, linkName); err != nil {
		return false, fmt.Errorf("create staged mail host certificate link: %w", err)
	}
	linkPublished := false
	defer func() {
		if !linkPublished {
			_ = unix.Unlinkat(dirFD, linkName, 0)
		}
	}()
	if err := unix.Renameat(dirFD, linkName, dirFD, "current"); err != nil {
		return false, fmt.Errorf("activate mail host certificate atomically: %w", err)
	}
	linkPublished = true
	if err := unix.Fsync(dirFD); err != nil {
		return true, fmt.Errorf(
			"sync mail host TLS directory after certificate activation: %w", err,
		)
	}
	return true, nil
}

func verifyPublishedMailHostCertificateReceipt(
	requestID, qualifier, domain string,
) (bool, error) {
	dirFD, err := openTrustedPanelTLSDirectoryOwned(managedMailHostTLSDir, 0)
	if err != nil {
		return false, err
	}
	defer unix.Close(dirFD)
	_, receipt, leafDER, _, found, err :=
		readCurrentMailHostCertificateVersionAt(dirFD)
	if err != nil || !found {
		return false, err
	}
	return receipt.RequestID == requestID &&
		receipt.Qualifier == qualifier &&
		receipt.Domain == domain &&
		receipt.LeafSHA256 == panelCertificateLeafSHA256(leafDER), nil
}

func stabilizePublishedMailHostCertificate() error {
	dirFD, err := openTrustedPanelTLSDirectoryOwned(managedMailHostTLSDir, 0)
	if err != nil {
		return err
	}
	defer unix.Close(dirFD)
	if err := unix.Fsync(dirFD); err != nil {
		return fmt.Errorf("sync trusted mail host TLS directory: %w", err)
	}
	return nil
}

func reconcilePersistedMailHostCertificateHost(
	ctx context.Context,
	requestID, qualifier, domain string,
) (success bool, err error) {
	return reconcilePersistedMailHostCertificateHostAt(
		ctx,
		managedMailHostTLSDir,
		requestID,
		qualifier,
		domain,
	)
}

func reconcilePersistedMailHostCertificateHostAt(
	ctx context.Context,
	tlsDir, requestID, qualifier, domain string,
) (success bool, err error) {
	if ctx == nil {
		return false, errors.New("mail host certificate issue recovery context is required")
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	err = panelCertWithPublishLock(func() error {
		dirFD, openErr := openTrustedPanelTLSDirectoryOwned(tlsDir, 0)
		if openErr != nil {
			if errors.Is(openErr, os.ErrNotExist) {
				return nil
			}
			return openErr
		}
		defer unix.Close(dirFD)
		currentVersion, receipt, leafDER, notAfter, found, readErr :=
			readCurrentMailHostCertificateVersionAt(dirFD)
		if readErr != nil {
			return readErr
		}
		if found && receipt.RequestID == requestID &&
			receipt.Qualifier == qualifier && receipt.Domain == domain {
			if err := unix.Fsync(dirFD); err != nil {
				return fmt.Errorf(
					"stabilize recovered mail host certificate publication: %w", err,
				)
			}
			_ = leafDER
			_ = notAfter
			if err := applyMailHostCertificateSelection(ctx, domain); err != nil {
				return err
			}
			success = true
			return nil
		}
		matches, scanErr := findMailHostCertificateVersionsAt(
			dirFD, requestID, qualifier, domain,
		)
		if scanErr != nil {
			return scanErr
		}
		for _, match := range matches {
			if match == currentVersion {
				return errors.New(
					"current mail host certificate receipt conflicts with recovery identity",
				)
			}
			versionFD, openErr := openPanelCertDirectoryAt(dirFD, match)
			if openErr != nil {
				return openErr
			}
			exactReceipt, exists, readErr :=
				readMailHostCertificateReceiptAt(versionFD)
			unix.Close(versionFD)
			if readErr != nil {
				return readErr
			}
			if !exists {
				return errors.New(
					"mail host certificate issue stage receipt disappeared",
				)
			}
			if err := removeExactMailHostCertificateVersionAt(
				dirFD, match, exactReceipt,
			); err != nil {
				return err
			}
		}
		if len(matches) > 0 {
			if err := unix.Fsync(dirFD); err != nil {
				return fmt.Errorf(
					"sync mail host TLS directory after stage cleanup: %w", err,
				)
			}
		}
		return nil
	})
	return success, err
}

func readCurrentMailHostCertificateVersionAt(
	dirFD int,
) (
	version string,
	receipt mailHostCertificateReceipt,
	leafDER []byte,
	notAfter time.Time,
	found bool,
	err error,
) {
	version, currentFound, err := readCurrentPanelCertificateVersionAt(dirFD)
	if err != nil || !currentFound {
		return "", mailHostCertificateReceipt{}, nil, time.Time{}, false, err
	}
	versionFD, err := openPanelCertDirectoryAt(dirFD, version)
	if err != nil {
		return "", mailHostCertificateReceipt{}, nil, time.Time{}, false,
			fmt.Errorf("open current mail host certificate version: %w", err)
	}
	defer unix.Close(versionFD)
	receipt, receiptFound, err := readMailHostCertificateReceiptAt(versionFD)
	if err != nil {
		return version, mailHostCertificateReceipt{}, nil, time.Time{}, false, err
	}
	if !receiptFound {
		return version, mailHostCertificateReceipt{}, nil, time.Time{}, false, errors.New("current host certificate lacks its exact receipt")
	}

	leafDER, notAfter, err = verifyMailHostCertificateVersionAt(
		versionFD, receipt,
	)
	if err != nil {
		return version, mailHostCertificateReceipt{}, nil, time.Time{}, false, err
	}
	return version, receipt, leafDER, notAfter, true, nil
}

func readMailHostCertificateReceiptAt(
	versionFD int,
) (mailHostCertificateReceipt, bool, error) {
	fd, err := unix.Openat2(
		versionFD,
		mailHostCertificateReceiptName,
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
		return mailHostCertificateReceipt{}, false, nil
	}
	if err != nil {
		return mailHostCertificateReceipt{}, false, fmt.Errorf(
			"open mail host certificate issue receipt: %w", err,
		)
	}
	file := os.NewFile(uintptr(fd), mailHostCertificateReceiptName)
	if file == nil {
		unix.Close(fd)
		return mailHostCertificateReceipt{}, false, errors.New(
			"open mail host certificate issue receipt: invalid descriptor",
		)
	}
	defer file.Close()
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return mailHostCertificateReceipt{}, false, err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG ||
		stat.Uid != 0 ||
		stat.Gid != 0 ||
		stat.Mode&0o7777 != 0o600 ||
		stat.Nlink != 1 ||
		stat.Size < 1 ||
		stat.Size > mailHostCertificateReceiptMaxSize {
		return mailHostCertificateReceipt{}, false, errors.New(
			"mail host certificate issue receipt must be root-owned single-link 0600",
		)
	}
	raw, err := io.ReadAll(io.LimitReader(
		file, mailHostCertificateReceiptMaxSize+1,
	))
	if err != nil {
		return mailHostCertificateReceipt{}, false, err
	}
	if int64(len(raw)) != stat.Size {
		return mailHostCertificateReceipt{}, false, errors.New(
			"mail host certificate issue receipt changed while read",
		)
	}
	receipt, err := decodeMailHostCertificateReceipt(raw)
	return receipt, err == nil, err
}

func verifyMailHostCertificateVersionAt(
	versionFD int,
	receipt mailHostCertificateReceipt,
) ([]byte, time.Time, error) {
	domain, err := readMailHostCertificateDomainAt(versionFD, 0)
	if err != nil {
		return nil, time.Time{}, err
	}
	if domain != receipt.Domain {
		return nil, time.Time{}, errors.New(
			"mail host certificate issue receipt domain mismatch",
		)
	}
	certificate, err := readMailHostCertificateRegularFileAt(
		versionFD, "fullchain.pem", 0o600, mailHostCertificatePEMMaxSize,
	)
	if err != nil {
		return nil, time.Time{}, err
	}
	key, err := readMailHostCertificateRegularFileAt(versionFD, "privkey.pem", 0600, mailHostCertificatePEMMaxSize)
	if err != nil {
		return nil, time.Time{}, err
	}
	leaf, expires, err := validateMailHostCertificatePair(certificate, key, receipt.Domain)
	if err != nil {
		return nil, time.Time{}, err
	}
	if panelCertificateLeafSHA256(leaf) != receipt.LeafSHA256 {
		return nil, time.Time{}, errors.New("host certificate receipt leaf mismatch")
	}
	return leaf, expires, nil
}

func readMailHostCertificateRegularFileAt(
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
		return nil, errors.New("invalid mail host certificate file descriptor")
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
			"mail host certificate issue version file is not trusted",
		)
	}
	data, err := io.ReadAll(io.LimitReader(file, maxSize+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) != stat.Size {
		return nil, errors.New(
			"mail host certificate issue version changed while read",
		)
	}
	return data, nil
}

func findMailHostCertificateVersionsAt(
	dirFD int,
	requestID, qualifier, domain string,
) ([]string, error) {
	duplicateFD, err := unix.Dup(dirFD)
	if err != nil {
		return nil, err
	}
	directory := os.NewFile(uintptr(duplicateFD), "mail-host-tls")
	if directory == nil {
		unix.Close(duplicateFD)
		return nil, errors.New("invalid mail host TLS directory descriptor")
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
		if count > mailHostCertificateVersionMaxCount ||
			!validManagedPanelCertVersionName(name) {
			return nil, errors.New(
				"mail host certificate issue recovery versions are ambiguous",
			)
		}
		versionFD, err := openPanelCertDirectoryAt(dirFD, name)
		if err != nil {
			return nil, err
		}
		receipt, found, readErr :=
			readMailHostCertificateReceiptAt(versionFD)
		if readErr == nil &&
			found &&
			receipt.RequestID == requestID &&
			receipt.Qualifier == qualifier &&
			receipt.Domain == domain {
			_, _, readErr = verifyMailHostCertificateVersionAt(
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
			"multiple mail host certificate issue versions match recovery identity",
		)
	}
	return matches, nil
}

func removeExactMailHostCertificateVersionAt(
	dirFD int,
	versionName string,
	expected mailHostCertificateReceipt,
) error {
	if !validManagedPanelCertVersionName(versionName) {
		return errors.New("invalid mail host certificate issue cleanup version")
	}
	versionFD, err := openPanelCertDirectoryAt(dirFD, versionName)
	if err != nil {
		return err
	}
	receipt, found, err :=
		readMailHostCertificateReceiptAt(versionFD)
	unix.Close(versionFD)
	if err != nil {
		return err
	}
	if !found || receipt != expected {
		return errors.New(
			"mail host certificate issue cleanup receipt mismatch",
		)
	}
	return removeMailHostCertificateVersionFilesAt(dirFD, versionName)
}

func removeMailHostCertificateVersionFilesAt(
	dirFD int,
	versionName string,
) error {
	versionFD, err := openPanelCertDirectoryAt(dirFD, versionName)
	if err != nil {
		return err
	}
	for _, name := range []string{
		mailHostCertificateReceiptName,
		"mail.domain",
		"privkey.pem",
		"fullchain.pem",
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
