package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTLSSettingsPrefersAtomicallyPublishedManagedPair(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CELIKPANEL_TLS", "1")
	t.Setenv("CELIKPANEL_TLS_DIR", dir)
	t.Setenv("CELIKPANEL_TLS_CERT", "")
	t.Setenv("CELIKPANEL_TLS_KEY", "")
	versionName := managedPanelCertVersionPrefix + "00112233445566778899aabbccddeeff"
	versionDir := filepath.Join(dir, versionName)
	if err := os.Mkdir(versionDir, 0o750); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"panel.crt", "panel.key"} {
		if err := os.WriteFile(filepath.Join(versionDir, name), []byte(name), 0o640); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(versionName, filepath.Join(dir, "current")); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}

	enabled, certPath, keyPath, err := tlsSettings()
	if err != nil {
		t.Fatal(err)
	}
	if !enabled ||
		certPath != filepath.Join(versionDir, "panel.crt") ||
		keyPath != filepath.Join(versionDir, "panel.key") {
		t.Fatalf("TLS settings = %v, %q, %q", enabled, certPath, keyPath)
	}
}

func TestTLSSettingsRejectsIncompleteManagedPair(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CELIKPANEL_TLS", "1")
	t.Setenv("CELIKPANEL_TLS_DIR", dir)
	t.Setenv("CELIKPANEL_TLS_CERT", "")
	t.Setenv("CELIKPANEL_TLS_KEY", "")
	versionName := managedPanelCertVersionPrefix + "ffeeddccbbaa99887766554433221100"
	versionDir := filepath.Join(dir, versionName)
	if err := os.Mkdir(versionDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(versionDir, "panel.crt"), []byte("certificate"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(versionName, filepath.Join(dir, "current")); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}

	if _, _, _, err := tlsSettings(); err == nil {
		t.Fatal("incomplete managed TLS pair was accepted")
	}
}

func TestTLSSettingsRejectsIncompleteExplicitPair(t *testing.T) {
	t.Setenv("CELIKPANEL_TLS", "1")
	t.Setenv("CELIKPANEL_TLS_CERT", "/external/panel.crt")
	t.Setenv("CELIKPANEL_TLS_KEY", "")

	if _, _, _, err := tlsSettings(); err == nil {
		t.Fatal("incomplete explicit TLS pair was accepted")
	}
}

func TestTLSSettingsRejectsIncompleteLegacyPair(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CELIKPANEL_TLS", "1")
	t.Setenv("CELIKPANEL_TLS_DIR", dir)
	t.Setenv("CELIKPANEL_TLS_CERT", "")
	t.Setenv("CELIKPANEL_TLS_KEY", "")
	if err := os.WriteFile(filepath.Join(dir, "panel.crt"), []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, _, _, err := tlsSettings(); err == nil {
		t.Fatal("incomplete legacy TLS pair was accepted or overwritten")
	}
	if _, err := os.Stat(filepath.Join(dir, "panel.key")); !os.IsNotExist(err) {
		t.Fatalf("missing legacy key was unexpectedly created: %v", err)
	}
}
