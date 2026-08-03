package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPanelCertificateManagementBlockedForExplicitTLS(t *testing.T) {
	t.Setenv("CELIKPANEL_TLS_CERT", "/external/panel.crt")
	t.Setenv("CELIKPANEL_TLS_KEY", "/external/panel.key")
	t.Setenv("CELIKPANEL_TLS_DIR", panelManagedTLSDirectory)

	code, _, blocked := panelCertificateManagementBlocker()
	if !blocked || code != "panel_certificate_externally_managed" {
		t.Fatalf("blocker = %q, %v", code, blocked)
	}
}

func TestPanelCertificateManagementBlockedForCustomDirectory(t *testing.T) {
	t.Setenv("CELIKPANEL_TLS_CERT", "")
	t.Setenv("CELIKPANEL_TLS_KEY", "")
	t.Setenv("CELIKPANEL_TLS_DIR", t.TempDir())

	code, _, blocked := panelCertificateManagementBlocker()
	if !blocked || code != "panel_certificate_directory_unmanaged" {
		t.Fatalf("blocker = %q, %v", code, blocked)
	}
}

func TestCurrentPanelCertReportsExplicitPairBeforeManagedFiles(t *testing.T) {
	dir := t.TempDir()
	explicitCert := filepath.Join(dir, "external.crt")
	explicitKey := filepath.Join(dir, "external.key")
	if err := generateSelfSigned(explicitCert, explicitKey); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "panel.crt"), []byte("not a certificate"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CELIKPANEL_TLS_DIR", dir)
	t.Setenv("CELIKPANEL_TLS_CERT", explicitCert)
	t.Setenv("CELIKPANEL_TLS_KEY", explicitKey)

	info := currentPanelCert()
	if !info.HTTPSEnabled || !info.SelfSigned {
		t.Fatalf("explicit certificate was not reported: %+v", info)
	}
}
