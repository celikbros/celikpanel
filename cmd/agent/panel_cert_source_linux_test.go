//go:build linux

package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

type testPanelCertificateSource struct {
	root, domain, lineage, liveDirectory, archive string
	leafDER, rootDER, intermediateDER             []byte
	notAfter, now                                 time.Time
	leafKey                                       ed25519.PrivateKey
	intermediate                                  *x509.Certificate
	intermediateKey                               ed25519.PrivateKey
}

func createTestPanelCertificateSource(t *testing.T) testPanelCertificateSource {
	t.Helper()
	domain := "panel.example.test"
	lineage := panelCertLineageName(domain)
	root := t.TempDir()
	liveDirectory := filepath.Join(root, "live", lineage)
	archive := filepath.Join(root, "archive", lineage)
	for _, directory := range []string{liveDirectory, archive} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	_, rootKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2030, 5, 6, 7, 8, 9, 0, time.UTC)
	rootTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Test Root CA"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(72 * time.Hour),
		IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	rootDER, err := x509.CreateCertificate(
		rand.Reader, rootTemplate, rootTemplate, rootKey.Public(), rootKey,
	)
	if err != nil {
		t.Fatal(err)
	}
	rootCertificate, err := x509.ParseCertificate(rootDER)
	if err != nil {
		t.Fatal(err)
	}
	_, intermediateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	intermediateTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "Test Intermediate CA"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(48 * time.Hour),
		IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	intermediateDER, err := x509.CreateCertificate(
		rand.Reader,
		intermediateTemplate,
		rootCertificate,
		intermediateKey.Public(),
		rootKey,
	)
	if err != nil {
		t.Fatal(err)
	}
	intermediate, err := x509.ParseCertificate(intermediateDER)
	if err != nil {
		t.Fatal(err)
	}
	_, leafKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	notAfter := now.Add(24 * time.Hour)
	leafTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(3), Subject: pkix.Name{CommonName: domain}, DNSNames: []string{domain},
		NotBefore: now.Add(-time.Hour), NotAfter: notAfter,
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	leafDER, err := x509.CreateCertificate(
		rand.Reader, leafTemplate, intermediate, leafKey.Public(), intermediateKey,
	)
	if err != nil {
		t.Fatal(err)
	}
	certificate := append(
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER}),
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: intermediateDER})...,
	)
	keyDER, err := x509.MarshalPKCS8PrivateKey(leafKey)
	if err != nil {
		t.Fatal(err)
	}
	privateKey := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	for name, content := range map[string][]byte{"fullchain1.pem": certificate, "privkey1.pem": privateKey} {
		mode := os.FileMode(0o600)
		if strings.HasPrefix(name, "fullchain") {
			mode = 0o644
		}
		if err := os.WriteFile(filepath.Join(archive, name), content, mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(filepath.Join(archive, name), mode); err != nil {
			t.Fatal(err)
		}
	}
	for name, target := range map[string]string{
		"fullchain.pem": filepath.ToSlash(filepath.Join("..", "..", "archive", lineage, "fullchain1.pem")),
		"privkey.pem":   filepath.ToSlash(filepath.Join("..", "..", "archive", lineage, "privkey1.pem")),
	} {
		if err := os.Symlink(target, filepath.Join(liveDirectory, name)); err != nil {
			t.Fatal(err)
		}
	}

	previousRoot, previousUID, previousGID := panelCertificateSourceRoot, panelCertificateSourceExpectedUID, panelCertificateSourceExpectedGID
	previousNow, previousSystemRoots := panelCertificateSourceNow, panelCertificateSourceSystemRoots
	trustedRoots := x509.NewCertPool()
	trustedRoots.AddCert(rootCertificate)
	panelCertificateSourceRoot = root
	panelCertificateSourceExpectedUID, panelCertificateSourceExpectedGID = uint32(os.Geteuid()), uint32(os.Getegid())
	panelCertificateSourceNow = func() time.Time { return now }
	panelCertificateSourceSystemRoots = func() (*x509.CertPool, error) {
		return trustedRoots, nil
	}
	t.Cleanup(func() {
		panelCertificateSourceRoot, panelCertificateSourceExpectedUID, panelCertificateSourceExpectedGID = previousRoot, previousUID, previousGID
		panelCertificateSourceNow = previousNow
		panelCertificateSourceSystemRoots = previousSystemRoots
	})
	return testPanelCertificateSource{
		root:            root,
		domain:          domain,
		lineage:         lineage,
		liveDirectory:   liveDirectory,
		archive:         archive,
		leafDER:         leafDER,
		rootDER:         rootDER,
		intermediateDER: intermediateDER,
		notAfter:        notAfter,
		now:             now,
		leafKey:         leafKey,
		intermediate:    intermediate,
		intermediateKey: intermediateKey,
	}
}

func writeTestPanelCertificateChain(
	t *testing.T,
	fixture testPanelCertificateSource,
	certificates ...[]byte,
) {
	t.Helper()
	var fullchain []byte
	for _, certificate := range certificates {
		fullchain = append(
			fullchain,
			pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate})...,
		)
	}
	fullchainPath := filepath.Join(fixture.archive, "fullchain1.pem")
	if err := os.WriteFile(fullchainPath, fullchain, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(fullchainPath, 0o644); err != nil {
		t.Fatal(err)
	}
}

func rewriteTestPanelCertificateLeaf(
	t *testing.T,
	fixture testPanelCertificateSource,
	keyUsages []x509.ExtKeyUsage,
	notAfter time.Time,
) []byte {
	t.Helper()
	template := &x509.Certificate{
		SerialNumber: big.NewInt(4),
		Subject:      pkix.Name{CommonName: fixture.domain},
		DNSNames:     []string{fixture.domain},
		NotBefore:    fixture.now.Add(-time.Hour),
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  keyUsages,
	}
	leafDER, err := x509.CreateCertificate(
		rand.Reader,
		template,
		fixture.intermediate,
		fixture.leafKey.Public(),
		fixture.intermediateKey,
	)
	if err != nil {
		t.Fatal(err)
	}
	writeTestPanelCertificateChain(t, fixture, leafDER, fixture.intermediateDER)
	return leafDER
}

func TestReadPanelCertificateSourceAcceptsCertbotLiveArchiveLayout(t *testing.T) {
	fixture := createTestPanelCertificateSource(t)
	certificate, privateKey, leafDER, notAfter, err := readPanelCertificateSource(fixture.domain)
	if err != nil {
		t.Fatal(err)
	}
	if len(certificate) == 0 || len(privateKey) == 0 {
		t.Fatal("secure source reader returned empty TLS material")
	}
	if string(leafDER) != string(fixture.leafDER) {
		t.Fatal("secure source reader returned the wrong leaf fingerprint input")
	}
	if !notAfter.Equal(fixture.notAfter) {
		t.Fatalf("notAfter = %v, want %v", notAfter, fixture.notAfter)
	}
}

func TestReadPanelCertificateSourceRejectsMissingOrWrongIntermediate(t *testing.T) {
	for _, tc := range []struct {
		name  string
		chain func(testPanelCertificateSource) [][]byte
	}{
		{
			name: "missing",
			chain: func(fixture testPanelCertificateSource) [][]byte {
				return [][]byte{fixture.leafDER}
			},
		},
		{
			name: "wrong",
			chain: func(fixture testPanelCertificateSource) [][]byte {
				return [][]byte{fixture.leafDER, fixture.rootDER}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := createTestPanelCertificateSource(t)
			writeTestPanelCertificateChain(t, fixture, tc.chain(fixture)...)
			_, _, _, _, err := readPanelCertificateSource(fixture.domain)
			if err == nil || !strings.Contains(err.Error(), "verify panel certificate source trust chain") {
				t.Fatalf("error = %v, want trust-chain rejection", err)
			}
		})
	}
}

func TestReadPanelCertificateSourceRejectsUntrustedSuppliedIntermediate(t *testing.T) {
	fixture := createTestPanelCertificateSource(t)
	_, untrustedKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	untrustedTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(20),
		Subject:               pkix.Name{CommonName: "Untrusted Intermediate"},
		NotBefore:             fixture.now.Add(-time.Hour),
		NotAfter:              fixture.now.Add(48 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	untrustedDER, err := x509.CreateCertificate(
		rand.Reader,
		untrustedTemplate,
		untrustedTemplate,
		untrustedKey.Public(),
		untrustedKey,
	)
	if err != nil {
		t.Fatal(err)
	}
	untrusted, err := x509.ParseCertificate(untrustedDER)
	if err != nil {
		t.Fatal(err)
	}
	leafTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(21),
		Subject:      pkix.Name{CommonName: fixture.domain},
		DNSNames:     []string{fixture.domain},
		NotBefore:    fixture.now.Add(-time.Hour),
		NotAfter:     fixture.now.Add(panelCertificateSourceMinLifetime),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	leafDER, err := x509.CreateCertificate(
		rand.Reader,
		leafTemplate,
		untrusted,
		fixture.leafKey.Public(),
		untrustedKey,
	)
	if err != nil {
		t.Fatal(err)
	}
	writeTestPanelCertificateChain(t, fixture, leafDER, untrustedDER)
	_, _, _, _, err = readPanelCertificateSource(fixture.domain)
	if err == nil || !strings.Contains(err.Error(), "verify panel certificate source trust chain") {
		t.Fatalf("error = %v, want untrusted-chain rejection", err)
	}
}

func TestReadPanelCertificateSourceRejectsClientAuthOnlyLeaf(t *testing.T) {
	fixture := createTestPanelCertificateSource(t)
	rewriteTestPanelCertificateLeaf(
		t,
		fixture,
		[]x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		fixture.now.Add(panelCertificateSourceMinLifetime),
	)
	_, _, _, _, err := readPanelCertificateSource(fixture.domain)
	if err == nil || !strings.Contains(err.Error(), "verify panel certificate source trust chain") {
		t.Fatalf("error = %v, want ServerAuth rejection", err)
	}
}

func TestReadPanelCertificateSourceFailsClosedWhenSystemRootsFail(t *testing.T) {
	t.Run("error", func(t *testing.T) {
		fixture := createTestPanelCertificateSource(t)
		previous := panelCertificateSourceSystemRoots
		panelCertificateSourceSystemRoots = func() (*x509.CertPool, error) {
			return nil, errors.New("system root failure")
		}
		t.Cleanup(func() { panelCertificateSourceSystemRoots = previous })
		_, _, _, _, err := readPanelCertificateSource(fixture.domain)
		if err == nil || !strings.Contains(err.Error(), "load panel certificate system trust roots") {
			t.Fatalf("error = %v, want system-root failure", err)
		}
	})
	t.Run("nil-pool", func(t *testing.T) {
		fixture := createTestPanelCertificateSource(t)
		previous := panelCertificateSourceSystemRoots
		panelCertificateSourceSystemRoots = func() (*x509.CertPool, error) {
			return nil, nil
		}
		t.Cleanup(func() { panelCertificateSourceSystemRoots = previous })
		_, _, _, _, err := readPanelCertificateSource(fixture.domain)
		if err == nil || !strings.Contains(err.Error(), "no trust roots returned") {
			t.Fatalf("error = %v, want nil system-root rejection", err)
		}
	})
}

func TestReadPanelCertificateSourceMinimumRemainingLifetime(t *testing.T) {
	t.Run("exact-boundary", func(t *testing.T) {
		fixture := createTestPanelCertificateSource(t)
		_, _, _, _, err := readPanelCertificateSource(fixture.domain)
		if err != nil {
			t.Fatalf("exact minimum remaining lifetime was rejected: %v", err)
		}
	})
	t.Run("one-second-short", func(t *testing.T) {
		fixture := createTestPanelCertificateSource(t)
		rewriteTestPanelCertificateLeaf(
			t,
			fixture,
			[]x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
			fixture.now.Add(panelCertificateSourceMinLifetime-time.Second),
		)
		_, _, _, _, err := readPanelCertificateSource(fixture.domain)
		if err == nil || !strings.Contains(err.Error(), "insufficient remaining validity") {
			t.Fatalf("error = %v, want minimum-lifetime rejection", err)
		}
	})
}

func TestReadPanelCertificateSourceRejectsUnsafeLinks(t *testing.T) {
	for _, tc := range []struct{ name, target, want string }{
		{"escape", "../../../outside/fullchain.pem", "leaves its archive lineage"},
		{"magic-link", "/proc/self/fd/0", "not canonical and relative"},
		{"other-lineage", "../../archive/other-lineage/fullchain1.pem", "leaves its archive lineage"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := createTestPanelCertificateSource(t)
			link := filepath.Join(fixture.liveDirectory, "fullchain.pem")
			if err := os.Remove(link); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(tc.target, link); err != nil {
				t.Fatal(err)
			}
			_, _, _, _, err := readPanelCertificateSource(fixture.domain)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestReadPanelCertificateSourceRejectsUnsafeMetadata(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*testing.T, testPanelCertificateSource)
		want string
	}{
		{"group-writable-file", func(t *testing.T, f testPanelCertificateSource) {
			if err := os.Chmod(filepath.Join(f.archive, "fullchain1.pem"), 0o664); err != nil {
				t.Fatal(err)
			}
		}, "group/other writable"},
		{"hard-linked-file", func(t *testing.T, f testPanelCertificateSource) {
			if err := os.Link(filepath.Join(f.archive, "privkey1.pem"), filepath.Join(f.root, "extra-key")); err != nil {
				t.Fatal(err)
			}
		}, "single-link"},
		{"writable-lineage-directory", func(t *testing.T, f testPanelCertificateSource) {
			if err := os.Chmod(f.archive, 0o775); err != nil {
				t.Fatal(err)
			}
		}, "group/other writable"},
		{"world-readable-private-key-0644", func(t *testing.T, f testPanelCertificateSource) {
			if err := os.Chmod(filepath.Join(f.archive, "privkey1.pem"), 0o644); err != nil {
				t.Fatal(err)
			}
		}, "private key has unsafe permissions"},
		{"world-readable-private-key-0444", func(t *testing.T, f testPanelCertificateSource) {
			if err := os.Chmod(filepath.Join(f.archive, "privkey1.pem"), 0o444); err != nil {
				t.Fatal(err)
			}
		}, "private key has unsafe permissions"},
		{"executable-private-key-0755", func(t *testing.T, f testPanelCertificateSource) {
			if err := os.Chmod(filepath.Join(f.archive, "privkey1.pem"), 0o755); err != nil {
				t.Fatal(err)
			}
		}, "private key has unsafe permissions"},
		{"owner-executable-private-key-0700", func(t *testing.T, f testPanelCertificateSource) {
			if err := os.Chmod(filepath.Join(f.archive, "privkey1.pem"), 0o700); err != nil {
				t.Fatal(err)
			}
		}, "private key has unsafe permissions"},
		{"special-mode-private-key", func(t *testing.T, f testPanelCertificateSource) {
			if err := os.Chmod(filepath.Join(f.archive, "privkey1.pem"), 0o600|os.ModeSetuid); err != nil {
				t.Fatal(err)
			}
		}, "private key has unsafe permissions"},
		{"private-key-without-owner-read", func(t *testing.T, f testPanelCertificateSource) {
			if err := os.Chmod(filepath.Join(f.archive, "privkey1.pem"), 0o200); err != nil {
				t.Fatal(err)
			}
		}, "private key is not owner-readable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := createTestPanelCertificateSource(t)
			tc.edit(t, fixture)
			_, _, _, _, err := readPanelCertificateSource(fixture.domain)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestReadPanelCertificateSourceRejectsMismatchedGenerationAndKey(t *testing.T) {
	t.Run("generation", func(t *testing.T) {
		fixture := createTestPanelCertificateSource(t)
		if err := os.Rename(filepath.Join(fixture.archive, "privkey1.pem"), filepath.Join(fixture.archive, "privkey2.pem")); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(fixture.liveDirectory, "privkey.pem")
		if err := os.Remove(link); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.ToSlash(filepath.Join("..", "..", "archive", fixture.lineage, "privkey2.pem")), link); err != nil {
			t.Fatal(err)
		}
		_, _, _, _, err := readPanelCertificateSource(fixture.domain)
		if err == nil || !strings.Contains(err.Error(), "generation is inconsistent") {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("private-key", func(t *testing.T) {
		fixture := createTestPanelCertificateSource(t)
		_, key, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		keyDER, err := x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(fixture.archive, "privkey1.pem"), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
			t.Fatal(err)
		}
		_, _, _, _, err = readPanelCertificateSource(fixture.domain)
		if err == nil || !strings.Contains(err.Error(), "private key") {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestReadPanelCertificateSourceFailsClosedWithoutOpenat2(t *testing.T) {
	fixture := createTestPanelCertificateSource(t)
	previous := panelCertificateSourceOpenat2
	panelCertificateSourceOpenat2 = func(int, string, *unix.OpenHow) (int, error) { return -1, unix.ENOSYS }
	t.Cleanup(func() { panelCertificateSourceOpenat2 = previous })
	_, _, _, _, err := readPanelCertificateSource(fixture.domain)
	if err == nil || !strings.Contains(err.Error(), "requires Linux openat2") || !errors.Is(err, unix.ENOSYS) {
		t.Fatalf("error = %v", err)
	}
}

func TestReadPanelCertificateSourceOwnerSeamsFailClosed(t *testing.T) {
	fixture := createTestPanelCertificateSource(t)
	panelCertificateSourceExpectedUID++
	_, _, _, _, err := readPanelCertificateSource(fixture.domain)
	if err == nil || !strings.Contains(err.Error(), "not root-owned") {
		t.Fatalf("error = %v", err)
	}
}
