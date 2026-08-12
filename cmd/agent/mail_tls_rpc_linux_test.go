//go:build linux

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func requireMailTLSEntryRejected(t *testing.T, entry MailSNIEntry, wantDetail string) {
	t.Helper()
	_, err := validateMailSNIEntries([]MailSNIEntry{entry})
	if err == nil || !strings.Contains(err.Error(), wantDetail) {
		t.Fatalf("validateMailSNIEntries() error = %v, want detail %q", err, wantDetail)
	}
}

func TestValidateMailSNIEntriesRejectsSymlinkedSnapshotPaths(t *testing.T) {
	t.Run("certificate file", func(t *testing.T) {
		root := t.TempDir()
		t.Setenv("CELIKPANEL_CUSTOM_CERT_ROOT", root)
		snapshot := createManagedMailTLSSnapshot(t, root, "example.test", "")
		target := filepath.Join(t.TempDir(), "attacker.pem")
		if err := os.WriteFile(target, []byte("attacker"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(snapshot.certPath); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, snapshot.certPath); err != nil {
			t.Fatal(err)
		}
		requireMailTLSEntryRejected(t, validMailSNIEntry(snapshot), "not a regular file")
	})

	t.Run("version directory", func(t *testing.T) {
		root := t.TempDir()
		t.Setenv("CELIKPANEL_CUSTOM_CERT_ROOT", root)
		outside := createManagedMailTLSSnapshot(t, t.TempDir(), "example.test", "")
		domainDir := filepath.Join(root, "example.test")
		if err := os.MkdirAll(domainDir, 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(domainDir, 0o750); err != nil {
			t.Fatal(err)
		}
		versionDir := filepath.Join(domainDir, filepath.Base(outside.versionDir))
		if err := os.Symlink(outside.versionDir, versionDir); err != nil {
			t.Fatal(err)
		}
		entry := MailSNIEntry{
			Names:    []string{"mail.example.test"},
			CertPath: filepath.Join(versionDir, "fullchain.pem"),
			KeyPath:  filepath.Join(versionDir, "privkey.pem"),
		}
		requireMailTLSEntryRejected(t, entry, "not a safe directory")
	})

	t.Run("managed root", func(t *testing.T) {
		actualRoot := t.TempDir()
		snapshot := createManagedMailTLSSnapshot(t, actualRoot, "example.test", "")
		linkRoot := filepath.Join(t.TempDir(), "managed")
		if err := os.Symlink(actualRoot, linkRoot); err != nil {
			t.Fatal(err)
		}
		t.Setenv("CELIKPANEL_CUSTOM_CERT_ROOT", linkRoot)
		entry := validMailSNIEntry(snapshot)
		entry.CertPath = filepath.Join(linkRoot, "example.test", filepath.Base(snapshot.versionDir), "fullchain.pem")
		entry.KeyPath = filepath.Join(linkRoot, "example.test", filepath.Base(snapshot.versionDir), "privkey.pem")
		requireMailTLSEntryRejected(t, entry, "managed certificate root")
	})
}

func TestValidateMailSNIEntriesRejectsUnsafeSnapshotPermissionsAndHardlinks(t *testing.T) {
	t.Run("private key permissions", func(t *testing.T) {
		root := t.TempDir()
		t.Setenv("CELIKPANEL_CUSTOM_CERT_ROOT", root)
		snapshot := createManagedMailTLSSnapshot(t, root, "example.test", "")
		if err := os.Chmod(snapshot.keyPath, 0o644); err != nil {
			t.Fatal(err)
		}
		requireMailTLSEntryRejected(t, validMailSNIEntry(snapshot), "unsafe permissions")
	})

	t.Run("version directory permissions", func(t *testing.T) {
		root := t.TempDir()
		t.Setenv("CELIKPANEL_CUSTOM_CERT_ROOT", root)
		snapshot := createManagedMailTLSSnapshot(t, root, "example.test", "")
		if err := os.Chmod(snapshot.versionDir, 0o770); err != nil {
			t.Fatal(err)
		}
		requireMailTLSEntryRejected(t, validMailSNIEntry(snapshot), "unsafe permissions")
	})

	t.Run("hard linked certificate", func(t *testing.T) {
		root := t.TempDir()
		t.Setenv("CELIKPANEL_CUSTOM_CERT_ROOT", root)
		snapshot := createManagedMailTLSSnapshot(t, root, "example.test", "")
		target := filepath.Join(t.TempDir(), "target.pem")
		if err := os.WriteFile(target, []byte("certificate"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(target, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(snapshot.certPath); err != nil {
			t.Fatal(err)
		}
		if err := os.Link(target, snapshot.certPath); err != nil {
			t.Fatal(err)
		}
		requireMailTLSEntryRejected(t, validMailSNIEntry(snapshot), "hard links")
	})
}

func TestValidateMailSNIEntriesRejectsSnapshotOwnerMismatch(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("changing ownership requires root")
	}
	root := t.TempDir()
	t.Setenv("CELIKPANEL_CUSTOM_CERT_ROOT", root)
	snapshot := createManagedMailTLSSnapshot(t, root, "example.test", "")
	if err := os.Chown(snapshot.keyPath, 65534, 65534); err != nil {
		t.Fatal(err)
	}
	requireMailTLSEntryRejected(t, validMailSNIEntry(snapshot), "does not match managed root owner")
}
