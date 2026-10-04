//go:build linux

package main

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/certbotsource"
	"golang.org/x/sys/unix"
)

const (
	panelCertificateSourceMinLifetime = 24 * time.Hour
	panelCertificateSourceResolveRoot = certbotsource.ResolveRoot
)

var (
	panelCertificateSourceRoot        = "/etc/letsencrypt"
	panelCertificateSourceExpectedUID = uint32(0)
	panelCertificateSourceExpectedGID = uint32(0)
	panelCertificateSourceOpenat2     = unix.Openat2
	panelCertificateSourceNow         = time.Now
	panelCertificateSourceSystemRoots = x509.SystemCertPool
)

// readPanelCertificateSource supports Certbot's normal live symlinks while
// confining their resolution to an authenticated Let's Encrypt root.
func readPanelCertificateSource(domain string) (
	certificate, privateKey, leafDER []byte,
	notAfter time.Time,
	err error,
) {
	if domain != strings.ToLower(strings.TrimSpace(domain)) || !validPanelCertDomain.MatchString(domain) {
		return nil, nil, nil, time.Time{}, errors.New("invalid panel certificate source domain")
	}
	return readTrustedCertbotCertificateSource(domain, panelCertLineageName(domain))
}

func readTrustedCertbotCertificateSource(domain, lineage string) (certificate, privateKey, leafDER []byte, notAfter time.Time, err error) {
	if !validPanelCertDomain.MatchString(domain) || (lineage != panelCertLineageName(domain) && lineage != mailHostCertLineageName(domain)) {
		return nil, nil, nil, time.Time{}, errors.New("invalid managed certificate source identity")
	}

	certificateData, keyData, err := panelCertbotSourceReader().ReadPair(lineage)
	if err != nil {
		return nil, nil, nil, time.Time{}, err
	}
	pair, err := tls.X509KeyPair(certificateData, keyData)
	if err != nil {
		return nil, nil, nil, time.Time{}, fmt.Errorf("validate panel certificate source pair: %w", err)
	}
	if len(pair.Certificate) == 0 {
		return nil, nil, nil, time.Time{}, errors.New("validate panel certificate source identity: certificate chain is empty")
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return nil, nil, nil, time.Time{}, fmt.Errorf("parse panel certificate source leaf: %w", err)
	}
	now := panelCertificateSourceNow()
	if now.Before(leaf.NotBefore) || !now.Before(leaf.NotAfter) {
		return nil, nil, nil, time.Time{}, errors.New("panel certificate source leaf is not currently valid")
	}
	if leaf.NotAfter.Before(now.Add(panelCertificateSourceMinLifetime)) {
		return nil, nil, nil, time.Time{}, errors.New("panel certificate source leaf has insufficient remaining validity")
	}
	intermediates := x509.NewCertPool()
	for index, raw := range pair.Certificate[1:] {
		certificate, parseErr := x509.ParseCertificate(raw)
		if parseErr != nil {
			return nil, nil, nil, time.Time{}, fmt.Errorf(
				"parse panel certificate source intermediate %d: %w", index+1, parseErr,
			)
		}
		intermediates.AddCert(certificate)
	}
	roots, err := panelCertificateSourceSystemRoots()
	if err != nil {
		return nil, nil, nil, time.Time{}, fmt.Errorf("load panel certificate system trust roots: %w", err)
	}
	if roots == nil {
		return nil, nil, nil, time.Time{}, errors.New("load panel certificate system trust roots: no trust roots returned")
	}
	if _, err := leaf.Verify(x509.VerifyOptions{
		DNSName:       domain,
		Roots:         roots,
		Intermediates: intermediates,
		CurrentTime:   now,
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}); err != nil {
		return nil, nil, nil, time.Time{}, fmt.Errorf("verify panel certificate source trust chain: %w", err)
	}
	return bytes.Clone(certificateData), bytes.Clone(keyData), bytes.Clone(leaf.Raw), leaf.NotAfter, nil
}

func panelCertbotSourceReader() certbotsource.Reader {
	return certbotsource.Reader{Root: panelCertificateSourceRoot, UID: panelCertificateSourceExpectedUID, GID: panelCertificateSourceExpectedGID, Openat2: panelCertificateSourceOpenat2}
}
func openPanelCertificateSourceAt(fd int, relative string, flags int, resolve uint64) (int, error) {
	return panelCertbotSourceReader().OpenAt(fd, relative, flags, resolve)
}
func validatePanelCertificateSourceFileFD(fd int, privateKey bool) (unix.Stat_t, error) {
	return panelCertbotSourceReader().ValidateFileFD(fd, privateKey)
}
