package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testCertificateVersion = "sha256-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

type testManagedMailTLSSnapshot struct {
	root       string
	domain     string
	versionDir string
	certPath   string
	keyPath    string
}

func createManagedMailTLSSnapshot(t *testing.T, root, domain, version string) testManagedMailTLSSnapshot {
	t.Helper()
	if root == "" {
		root = t.TempDir()
	}
	if version == "" {
		version = testCertificateVersion
	}
	domainDir := filepath.Join(root, domain)
	versionDir := filepath.Join(domainDir, version)
	if err := os.MkdirAll(versionDir, 0o750); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{domainDir, versionDir} {
		if err := os.Chmod(path, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	certPath := filepath.Join(versionDir, "fullchain.pem")
	keyPath := filepath.Join(versionDir, "privkey.pem")
	if err := os.WriteFile(certPath, []byte("certificate"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(certPath, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, []byte("private key"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(keyPath, 0o600); err != nil {
		t.Fatal(err)
	}
	return testManagedMailTLSSnapshot{
		root:       root,
		domain:     domain,
		versionDir: versionDir,
		certPath:   certPath,
		keyPath:    keyPath,
	}
}

func validMailSNIEntry(snapshot testManagedMailTLSSnapshot) MailSNIEntry {
	return MailSNIEntry{
		Names:    []string{"mail." + snapshot.domain, snapshot.domain},
		CertPath: snapshot.certPath,
		KeyPath:  snapshot.keyPath,
	}
}

func TestValidateMailSNIEntriesRejectsInvalidInput(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CELIKPANEL_CUSTOM_CERT_ROOT", root)
	snapshot := createManagedMailTLSSnapshot(t, root, "example.test", "")
	otherVersion := createManagedMailTLSSnapshot(
		t,
		root,
		"example.test",
		"sha256-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	)
	otherDomain := createManagedMailTLSSnapshot(t, root, "other.test", "")
	outside := createManagedMailTLSSnapshot(t, t.TempDir(), "example.test", "")

	base := validMailSNIEntry(snapshot)
	tests := []struct {
		name       string
		mutate     func(*MailSNIEntry)
		wantDetail string
	}{
		{"no names", func(entry *MailSNIEntry) { entry.Names = nil }, "has no names"},
		{"blank name", func(entry *MailSNIEntry) { entry.Names = []string{"mail.example.test", "  "} }, "not a valid FQDN"},
		{"config injection name", func(entry *MailSNIEntry) { entry.Names = []string{"mail.example.test\nlocal_name attacker.test"} }, "not a valid FQDN"},
		{"unrelated name", func(entry *MailSNIEntry) { entry.Names = []string{"mail.attacker.test"} }, "does not belong"},
		{"mail name required", func(entry *MailSNIEntry) { entry.Names = []string{"example.test"} }, "does not include the managed mail hostname"},
		{"too many names", func(entry *MailSNIEntry) {
			entry.Names = []string{"mail.example.test", "example.test", "www.example.test"}
		}, "too many names"},
		{"empty certificate path", func(entry *MailSNIEntry) { entry.CertPath = "" }, "path is empty"},
		{"outside certificate path", func(entry *MailSNIEntry) { entry.CertPath = outside.certPath }, "outside the managed certificate root"},
		{"wrong certificate filename", func(entry *MailSNIEntry) { entry.CertPath = filepath.Join(snapshot.versionDir, "cert.pem") }, "must end in fullchain.pem"},
		{"non canonical certificate path", func(entry *MailSNIEntry) {
			entry.CertPath = snapshot.versionDir + string(filepath.Separator) + "nested" +
				string(filepath.Separator) + ".." + string(filepath.Separator) + "fullchain.pem"
		}, "canonical absolute path"},
		{"empty private key path", func(entry *MailSNIEntry) { entry.KeyPath = "" }, "path is empty"},
		{"different snapshot key", func(entry *MailSNIEntry) { entry.KeyPath = otherVersion.keyPath }, "same managed snapshot"},
		{"different domain key", func(entry *MailSNIEntry) { entry.KeyPath = otherDomain.keyPath }, "same managed snapshot"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			entry := base
			entry.Names = append([]string(nil), base.Names...)
			test.mutate(&entry)
			_, err := validateMailSNIEntries([]MailSNIEntry{entry})
			if err == nil || !strings.Contains(err.Error(), test.wantDetail) {
				t.Fatalf("validateMailSNIEntries() error = %v, want detail %q", err, test.wantDetail)
			}
		})
	}
}

func TestValidateMailSNIEntriesNormalisesNames(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CELIKPANEL_CUSTOM_CERT_ROOT", root)
	snapshot := createManagedMailTLSSnapshot(t, root, "example.test", "")

	got, err := validateMailSNIEntries([]MailSNIEntry{{
		Names:    []string{" Mail.Example.Test. ", " EXAMPLE.TEST. "},
		CertPath: snapshot.certPath,
		KeyPath:  snapshot.keyPath,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || len(got[0].Names) != 2 ||
		got[0].Names[0] != "mail.example.test" ||
		got[0].Names[1] != "example.test" {
		t.Fatalf("unexpected validated entries: %#v", got)
	}
}

func TestValidateSecureMailTLSRequestCanonicalisesAndRejectsHostnameInjection(t *testing.T) {
	got, entries, err := validateSecureMailTLSRequest(&SecureMailTLSRequest{
		Myhostname: " Boston.CelikHost.COM. ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "boston.celikhost.com" || len(entries) != 0 {
		t.Fatalf("validated request = hostname %q entries %#v", got, entries)
	}

	for _, candidate := range []string{
		"localhost",
		"127.0.0.1",
		"boston.celikhost.com\nsmtpd_tls_security_level=none",
		"bad_name.celikhost.com",
	} {
		if _, _, err := validateSecureMailTLSRequest(&SecureMailTLSRequest{
			Myhostname: candidate,
		}); err == nil {
			t.Fatalf("validateSecureMailTLSRequest(%q) accepted an unsafe hostname", candidate)
		}
	}
}

func TestValidateMailSNIEntriesCapsSnapshotSize(t *testing.T) {
	entries := make([]MailSNIEntry, maxMailSNIEntries+1)
	if _, err := validateMailSNIEntries(entries); err == nil || !strings.Contains(err.Error(), "too many entries") {
		t.Fatalf("oversized snapshot error = %v", err)
	}
}

func TestValidateMailSNIEntriesRejectsDuplicateNamesAcrossEntries(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CELIKPANEL_CUSTOM_CERT_ROOT", root)
	first := createManagedMailTLSSnapshot(t, root, "example.test", "")
	second := createManagedMailTLSSnapshot(
		t,
		root,
		"example.test",
		"sha256-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	)
	if _, err := validateMailSNIEntries([]MailSNIEntry{
		validMailSNIEntry(first),
		validMailSNIEntry(second),
	}); err == nil || !strings.Contains(err.Error(), "already claimed") {
		t.Fatalf("duplicate SNI name error = %v", err)
	}
}

func TestMailTLSFileSnapshotRestoresExistingAndAbsentFiles(t *testing.T) {
	dir := t.TempDir()
	existingPath := filepath.Join(dir, "existing.conf")
	if err := os.WriteFile(existingPath, []byte("old state"), 0o600); err != nil {
		t.Fatal(err)
	}
	existing, err := snapshotMailTLSFile(existingPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(existingPath, []byte("new state"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := existing.restore(); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(existingPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "old state" {
		t.Fatalf("restored content = %q, want old state", content)
	}

	absentPath := filepath.Join(dir, "new.conf")
	absent, err := snapshotMailTLSFile(absentPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absentPath, []byte("temporary state"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := absent.restore(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(absentPath); !os.IsNotExist(err) {
		t.Fatalf("restoring an absent snapshot left the file behind: %v", err)
	}
}

func TestSnapshotMailTLSFileRejectsNonRegularPath(t *testing.T) {
	if _, err := snapshotMailTLSFile(t.TempDir()); err == nil {
		t.Fatal("snapshotMailTLSFile() accepted a directory")
	}
}
