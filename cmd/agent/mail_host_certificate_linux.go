//go:build linux

package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/transport"
	"golang.org/x/sys/unix"
)

func openManagedMailHostTLSDirectory(path string) (int, int, error) {
	if path != managedMailHostTLSDir {
		return -1, 0, errors.New("invalid host certificate directory")
	}
	if _, err := prepareProductionMailTLSDirectory(path); err != nil {
		return -1, 0, err
	}
	fd, err := openTrustedPanelTLSDirectoryOwned(path, 0)
	return fd, 0, err
}

func readMailHostCertificateDomainAt(fd, uid int) (string, error) {
	if uid != 0 {
		return "", errors.New("host certificate owner must be root")
	}
	raw, err := readMailHostCertificateRegularFileAt(fd, "mail.domain", 0600, 254)
	if err != nil {
		return "", err
	}
	domain := strings.TrimSuffix(string(raw), "\n")
	if string(raw) != domain+"\n" || !serviceMutationCanonicalFQDN(domain) {
		return "", errors.New("invalid host certificate identity")
	}
	return domain, nil
}

func readMailHostCertificateSource(domain string) ([]byte, []byte, []byte, time.Time, error) {
	return readTrustedCertbotCertificateSource(domain, mailHostCertLineageName(domain))
}

func validateMailHostCertificatePair(cert, key []byte, domain string) ([]byte, time.Time, error) {
	pair, err := tls.X509KeyPair(cert, key)
	if err != nil {
		return nil, time.Time{}, err
	}
	if len(pair.Certificate) == 0 {
		return nil, time.Time{}, errors.New("empty host certificate")
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return nil, time.Time{}, err
	}
	roots, err := panelCertificateSourceSystemRoots()
	if err != nil {
		return nil, time.Time{}, err
	}
	if roots == nil {
		return nil, time.Time{}, errors.New("system trust roots unavailable")
	}
	intermediates := x509.NewCertPool()
	for _, der := range pair.Certificate[1:] {
		c, err := x509.ParseCertificate(der)
		if err != nil {
			return nil, time.Time{}, err
		}
		intermediates.AddCert(c)
	}
	if _, err = leaf.Verify(x509.VerifyOptions{DNSName: domain, Roots: roots, Intermediates: intermediates, CurrentTime: mailHostCertificateReceiptTime(leaf), KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}); err != nil {
		return nil, time.Time{}, err
	}
	return leaf.Raw, leaf.NotAfter, nil
}

// Absence permits the existing self-signed bootstrap pair. A present but
// corrupt/untrusted generation is an error, never a silent downgrade.
func selectedMailHostCertificate(domain string) (cert, key string, err error) {
	fd, err := openTrustedPanelTLSDirectoryOwned(managedMailHostTLSDir, 0)
	if errors.Is(err, os.ErrNotExist) {
		return defaultMailCert, defaultMailKey, nil
	}
	if err != nil {
		return "", "", err
	}
	defer unix.Close(fd)
	_, receipt, leafDER, _, found, err := readCurrentMailHostCertificateVersionAt(fd)
	if err != nil {
		return "", "", err
	}
	if !found {
		return defaultMailCert, defaultMailKey, nil
	}
	leaf, parseErr := x509.ParseCertificate(leafDER)
	if parseErr != nil || time.Now().Before(leaf.NotBefore) || !time.Now().Before(leaf.NotAfter) {
		return "", "", errors.New("mail host certificate is not currently valid")
	}
	if receipt.Domain != domain {
		return "", "", errors.New("configured host certificate belongs to another mail identity")
	}
	return managedMailHostTLSDir + "/current/fullchain.pem", managedMailHostTLSDir + "/current/privkey.pem", nil
}

func runMailHostCertificateCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	// Both preflight and execution only use the existing fixed mail command
	// inventory. No input supplies a program, directory, unit or shell text.
	switch filepath.Base(name) {
	case "postconf", "postfix", "postmap", "dovecot", "doveconf", "systemctl", "service":
	default:
		return nil, errors.New("unsupported host mail certificate command")
	}
	return runMailTLSMutationCommand(ctx, name, args...)
}

func renderMailHostCertificateDeployHook() string {
	return `#!/bin/sh
set -eu
# Managed by CelikPanel. Only the currently approved host lineage is queued.
lineage=${RENEWED_LINEAGE:-}
case "$lineage" in
 /etc/letsencrypt/live/celikpanel-mail-*)
  lineage_name=${lineage#/etc/letsencrypt/live/}
  case "$lineage_name" in ""|*/*) exit 0 ;; esac
  exec /opt/celikpanel/bin/agent --deploy-mail-host-certificate "$lineage_name"
  ;;
esac
exit 0
`
}

func writeMailHostCertificateDeployHook() error {
	const dir = "/etc/letsencrypt/renewal-hooks/deploy"
	if err := ensureRootOwnedPanelCertHookDirectory(dir); err != nil {
		return err
	}
	if err := publishPanelCertDeployHook(dir, "celikpanel-mail-host-cert", []byte(renderMailHostCertificateDeployHook())); err != nil {
		return err
	}
	return protectPanelCertDeployHook(dir + "/celikpanel-mail-host-cert")
}

func (a *Agent) MailHostCertificateStatus(req *transport.MailHostCertificateStatusRequest, resp *transport.MailHostCertificateStatusResponse) error {
	*resp = transport.MailHostCertificateStatusResponse{}
	if req == nil || !serviceMutationCanonicalFQDN(req.Domain) {
		resp.Error = "invalid mail host identity"
		return nil
	}
	resp.Domain = req.Domain
	fd, err := openTrustedPanelTLSDirectoryOwned(managedMailHostTLSDir, 0)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		resp.Error = err.Error()
		return nil
	}
	defer unix.Close(fd)
	_, receipt, leafDER, expires, found, err := readCurrentMailHostCertificateVersionAt(fd)
	if err != nil {
		resp.Error = err.Error()
		return nil
	}
	if !found || receipt.Domain != req.Domain {
		return nil
	}
	leaf, parseErr := x509.ParseCertificate(leafDER)
	if parseErr != nil || time.Now().Before(leaf.NotBefore) || !time.Now().Before(leaf.NotAfter) {
		return nil
	}
	resp.Ready = true
	resp.ExpiresAt = expires
	hook, ok := setupProtectedFile("/etc/letsencrypt/renewal-hooks/deploy/celikpanel-mail-host-cert")
	if !ok || string(hook) != renderMailHostCertificateDeployHook() {
		return nil
	}
	info, err := os.Stat("/etc/letsencrypt/renewal-hooks/deploy/celikpanel-mail-host-cert")
	if err != nil || info.Mode().Perm()&0100 == 0 {
		return nil
	}
	config, ok := setupProtectedFile(filepath.Join("/etc/letsencrypt/renewal", mailHostCertLineageName(req.Domain)+".conf"))
	if !ok {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if !setupPanelRenewalRouteReady(ctx, req.Domain, setupPanelRenewalAuthenticator(config)) {
		return nil
	}
	for _, timer := range []string{"certbot.timer", "certbot-renew.timer"} {
		if serviceMutationCommand(ctx, "systemctl", "is-active", "--quiet", timer).Run() == nil && serviceMutationCommand(ctx, "systemctl", "is-enabled", "--quiet", timer).Run() == nil {
			resp.RenewalReady = true
			return nil
		}
	}
	return nil
}

func queueMailHostCertificateRenewal(lineage string) error {
	if len(lineage) != len("celikpanel-mail-")+24 || strings.ContainsAny(lineage, "/\\") {
		return errors.New("invalid mail host lineage")
	}
	return panelCertWithPublishLock(func() error {
		fd, err := openTrustedPanelTLSDirectoryOwned(managedMailHostTLSDir, 0)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		defer unix.Close(fd)
		_, receipt, _, _, found, err := readCurrentMailHostCertificateVersionAt(fd)
		if err != nil {
			return err
		}
		if !found || mailHostCertLineageName(receipt.Domain) != lineage {
			return nil
		}
		_, _, leaf, _, err := readMailHostCertificateSource(receipt.Domain)
		if err != nil {
			return err
		}
		pending := mailHostRenewal{Lineage: lineage, LeafSHA256: panelCertificateLeafSHA256(leaf)}
		raw, _ := json.Marshal(pending)
		if err := writeMailHostRenewalPending(mailHostRenewalPendingPath(), raw); err != nil {
			return fmt.Errorf("queue mail host renewal: %w", err)
		}
		return nil
	})
}

func clearMailHostCertificateRenewal(expected mailHostRenewal) error {
	return panelCertWithPublishLock(func() error {
		raw, found, err := readSecureServiceMutationLedger(mailHostRenewalPendingPath(), 512)
		if err != nil || !found {
			return err
		}
		actual, err := decodeMailHostRenewal(raw)
		if err != nil {
			return err
		}
		if actual != expected {
			return errors.New("renewal queue identity changed")
		}

		fd, err := openMailHostRenewalStateDirectory(filepath.Dir(mailHostRenewalPendingPath()))
		if err != nil {
			return err
		}
		defer unix.Close(fd)
		if err = unix.Unlinkat(fd, filepath.Base(mailHostRenewalPendingPath()), 0); err != nil {
			return err
		}
		return unix.Fsync(fd)
	})
}

func currentMailHostCertificateIdentity() (string, string, error) {
	fd, err := openTrustedPanelTLSDirectoryOwned(managedMailHostTLSDir, 0)
	if err != nil {
		return "", "", err
	}
	defer unix.Close(fd)
	_, receipt, _, _, found, err := readCurrentMailHostCertificateVersionAt(fd)
	if err != nil {
		return "", "", err
	}
	if !found {
		return "", "", errors.New("active mail host certificate not found")
	}
	return receipt.Domain, receipt.LeafSHA256, nil
}

func mailHostCertificateReceiptTime(leaf *x509.Certificate) time.Time {
	now := time.Now()
	if now.Before(leaf.NotBefore) {
		return leaf.NotBefore
	}
	if !now.Before(leaf.NotAfter) {
		return leaf.NotAfter.Add(-time.Second)
	}
	return now
}

// Queue metadata uses the same 0600 ownership contract as its ledger reader.
// The service's dedicated group can differ from the root deploy-hook process.
func writeMailHostRenewalPending(path string, raw []byte) error {
	if filepath.Base(path) != "mail-host-certificate-renewal.pending" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("invalid mail host renewal queue path")
	}
	if _, err := decodeMailHostRenewal(raw); err != nil {
		return err
	}
	if err := ensureSecureServiceMutationStateDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	return secureWriteConfigOwnedBy(path, raw, 0600, serviceMutationRequiredOwnerUID, serviceMutationRequiredOwnerGID)
}

func openMailHostRenewalStateDirectory(path string) (int, error) {
	if err := ensureSecureServiceMutationStateDirectory(path); err != nil {
		return -1, err
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, err
	}
	file := os.NewFile(uintptr(fd), path)
	info, err := file.Stat()
	if err == nil {
		err = secureServiceMutationStateDirectoryStat(path, info)
	}
	if err != nil {
		file.Close()
		return -1, err
	}
	copyFD, err := duplicateSSLFD(fd)
	file.Close()
	return copyFD, err
}
