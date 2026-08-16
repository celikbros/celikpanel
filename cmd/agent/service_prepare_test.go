package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestClamAVSignaturesReady(t *testing.T) {
	dir := t.TempDir()
	if clamAVSignaturesReady(dir) {
		t.Fatal("empty directory must not be ready")
	}
	if err := os.Mkdir(filepath.Join(dir, "daily.cvd"), 0o755); err != nil {
		t.Fatal(err)
	}
	if clamAVSignaturesReady(dir) {
		t.Fatal("a directory named daily.cvd is not a signature database")
	}
	if err := os.Remove(filepath.Join(dir, "daily.cvd")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "daily.cld"), []byte("signature"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !clamAVSignaturesReady(dir) {
		t.Fatal("daily.cld must make the database ready")
	}
}

func TestServiceStartsAfterPanelSetup(t *testing.T) {
	for _, id := range []string{"pdns", "bind", "postfix", "dovecot", "wireguard"} {
		if !serviceStartsAfterPanelSetup(id) {
			t.Errorf("%s must wait for panel configuration before start", id)
		}
	}
	for _, id := range []string{"nginx", "postgresql", "mariadb", "clamav"} {
		if serviceStartsAfterPanelSetup(id) {
			t.Errorf("%s must be started and verified by the install RPC", id)
		}
	}
}

func TestBINDPackageAutoStartGuardDoesNotAffectOtherServices(t *testing.T) {
	if !serviceUsesBINDPackageInstallGuard("bind") {
		t.Fatal("BIND must use the package-maintainer auto-start guard")
	}
	for _, id := range []string{"pdns", "nginx", "postfix", "dovecot", "wireguard"} {
		if serviceUsesBINDPackageInstallGuard(id) {
			t.Errorf("%s unexpectedly uses the BIND package install guard", id)
		}
	}
}
