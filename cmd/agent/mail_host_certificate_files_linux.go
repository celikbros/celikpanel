//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/mailhoststore"
	"golang.org/x/sys/unix"
)

const (
	mailHostCertificateVersionMaxCount = 128
	mailHostCertificatePEMMaxSize      = mailhoststore.PEMMaxSize
)

func stageMailHostCertificateMaterial(domain, tlsDir string, certificate, privateKey []byte, receipt mailHostCertificateReceipt) (*mailHostCertificateStage, error) {
	if tlsDir != managedMailHostTLSDir || domain != receipt.Domain {
		return nil, errors.New("invalid mail host certificate issue stage target")
	}
	if _, err := mailhoststore.ValidateMaterial(certificate, privateKey, receipt); err != nil {
		return nil, err
	}
	// Directory acquisition and all operation/publication locks remain Agent-owned.
	dirFD, _, err := openManagedMailHostTLSDirectory(tlsDir)
	if err != nil {
		return nil, err
	}
	defer unix.Close(dirFD)
	prepared, err := mailhoststore.StageMaterialAt(dirFD, certificate, privateKey, receipt)
	if err != nil {
		return nil, err
	}
	return &mailHostCertificateStage{
		publishAction: prepared.Publish,
		cleanupAction: func(bool) error { return prepared.Close() },
	}, nil
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

func readCurrentMailHostCertificateVersionAt(dirFD int) (string, mailHostCertificateReceipt, []byte, time.Time, bool, error) {
	return mailhoststore.ReadCurrentAt(dirFD, validateMailHostCertificatePair)
}
func readMailHostCertificateReceiptAt(versionFD int) (mailHostCertificateReceipt, bool, error) {
	return mailhoststore.ReadReceiptAt(versionFD)
}
func verifyMailHostCertificateVersionAt(versionFD int, receipt mailHostCertificateReceipt) ([]byte, time.Time, error) {
	return mailhoststore.VerifyVersionAt(versionFD, receipt, validateMailHostCertificatePair)
}
func readMailHostCertificateRegularFileAt(dirFD int, name string, mode uint32, maxSize int64) ([]byte, error) {
	return mailhoststore.ReadRegularFileAt(dirFD, name, mode, maxSize)
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
