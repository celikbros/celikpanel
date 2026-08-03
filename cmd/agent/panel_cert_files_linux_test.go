//go:build linux

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestRenderPanelCertDeployHookIsGenericAndDoesNotRestartDirectly(t *testing.T) {
	script := renderPanelCertDeployHook()
	for _, want := range []string{
		"${RENEWED_LINEAGE:-}",
		"${lineage#/etc/letsencrypt/live/}",
		"/etc/letsencrypt/live/celikpanel-panel-*",
		"--deploy-panel-certificate",
		"$lineage_name",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("generic hook does not contain %q:\n%s", want, script)
		}
	}
	for _, forbidden := range []string{
		"old-panel.example.test",
		"new-panel.example.test",
		"systemctl restart",
	} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("generic hook unexpectedly contains %q:\n%s", forbidden, script)
		}
	}
}

func TestDeployRenewedPanelCertFilesSkipsUnrelatedLineage(t *testing.T) {
	store := installPanelCertificateActivationMemoryStore(t)
	originalActive := panelCertActiveIdentity
	originalLock := panelCertWithPublishLock
	t.Cleanup(func() {
		panelCertActiveIdentity = originalActive
		panelCertWithPublishLock = originalLock
	})
	panelCertWithPublishLock = func(action func() error) error { return action() }
	panelCertActiveIdentity = func(string) (string, bool, error) {
		return "active.example.test", true, nil
	}

	deployed, err := deployRenewedPanelCertFiles(
		panelCertLineageName("candidate.example.test"), managedPanelTLSDir,
	)
	if err != nil {
		t.Fatal(err)
	}
	if deployed {
		t.Fatal("unrelated lineage was accepted")
	}
	if _, present := store.snapshot(); present {
		t.Fatal("unrelated lineage created activation state")
	}
}

func TestDeployRenewedPanelCertFilesQueuesMatchingActiveLineage(t *testing.T) {
	store := installPanelCertificateActivationMemoryStore(t)
	originalActive := panelCertActiveIdentity
	originalLock := panelCertWithPublishLock
	t.Cleanup(func() {
		panelCertActiveIdentity = originalActive
		panelCertWithPublishLock = originalLock
	})
	panelCertWithPublishLock = func(action func() error) error { return action() }
	panelCertActiveIdentity = func(string) (string, bool, error) {
		return "active.example.test", true, nil
	}

	deployed, err := deployRenewedPanelCertFiles(
		panelCertLineageName("active.example.test"), managedPanelTLSDir,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !deployed {
		t.Fatal("matching active lineage was not queued")
	}
	state, present := store.snapshot()
	if !present {
		t.Fatal("matching active lineage did not create activation state")
	}
	if state.Domain != "active.example.test" ||
		state.LineageName != panelCertLineageName("active.example.test") ||
		state.Phase != panelCertificateActivationPendingSource {
		t.Fatalf("queued state = %+v", state)
	}
}

func TestDeployRenewedPanelCertFilesPreservesExistingActivation(t *testing.T) {
	store := installPanelCertificateActivationMemoryStore(t)
	existing, err := newPanelCertificateActivationState("other.example.test")
	if err != nil {
		t.Fatal(err)
	}
	store.seed(t, existing)
	originalActive := panelCertActiveIdentity
	originalLock := panelCertWithPublishLock
	t.Cleanup(func() {
		panelCertActiveIdentity = originalActive
		panelCertWithPublishLock = originalLock
	})
	panelCertWithPublishLock = func(action func() error) error { return action() }
	panelCertActiveIdentity = func(string) (string, bool, error) {
		return "active.example.test", true, nil
	}

	deployed, err := deployRenewedPanelCertFiles(
		panelCertLineageName("active.example.test"), managedPanelTLSDir,
	)
	if err != nil {
		t.Fatal(err)
	}
	if deployed {
		t.Fatal("hook replaced a different in-flight activation")
	}
	state, present := store.snapshot()
	if !present || state.Domain != existing.Domain || state.Phase != existing.Phase {
		t.Fatalf("existing state was not preserved: present=%v state=%+v", present, state)
	}
}

func TestPublishPanelCertDeployHookDoesNotPreserveUntrustedWritableInode(t *testing.T) {
	hookDir := t.TempDir()
	hookPath := filepath.Join(hookDir, "celikpanel-panel-cert")
	if err := os.WriteFile(hookPath, []byte("attacker predecessor"), 0o666); err != nil {
		t.Fatal(err)
	}
	held, err := os.OpenFile(hookPath, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()

	want := []byte("#!/bin/sh\nexit 0\n")
	if err := publishPanelCertDeployHookOwned(
		hookDir, filepath.Base(hookPath), want, os.Getuid(), os.Getgid(),
	); err != nil {
		t.Fatal(err)
	}
	if _, err := held.WriteAt([]byte("malicious"), 0); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("published hook = %q, want %q", got, want)
	}
	info, err := os.Stat(hookPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("published hook mode = %o, want 755", info.Mode().Perm())
	}
}

func TestPublishPanelCertificateVersionIsAtomicAndCarriesTrustedIdentity(t *testing.T) {
	tlsDir := t.TempDir()
	externalDir := t.TempDir()
	external := filepath.Join(externalDir, "must-not-change")
	if err := os.WriteFile(external, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(tlsDir, "current")); err != nil {
		t.Fatal(err)
	}
	dirFD, err := unix.Open(
		tlsDir,
		unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		0,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(dirFD)

	certificate := []byte("certificate bytes")
	privateKey := []byte("private key bytes")
	const domain = "panel.example.test"
	if err := publishPanelCertificateVersion(
		dirFD, os.Getuid(), os.Getgid(), domain, certificate, privateKey,
	); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(external); err != nil || string(got) != "outside" {
		t.Fatalf("external target changed: %q, %v", got, err)
	}
	currentInfo, err := os.Lstat(filepath.Join(tlsDir, "current"))
	if err != nil {
		t.Fatal(err)
	}
	if currentInfo.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("current mode = %v, want symlink", currentInfo.Mode())
	}
	for name, want := range map[string][]byte{
		"panel.crt":    certificate,
		"panel.key":    privateKey,
		"panel.domain": []byte(domain + "\n"),
	} {
		got, err := os.ReadFile(filepath.Join(tlsDir, "current", name))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(want) {
			t.Fatalf("%s = %q, want %q", name, got, want)
		}
		info, err := os.Stat(filepath.Join(tlsDir, "current", name))
		if err != nil {
			t.Fatal(err)
		}
		wantMode := os.FileMode(0o640)
		if name == "panel.domain" {
			wantMode = 0o600
		}
		if info.Mode().Perm() != wantMode {
			t.Fatalf("%s mode = %o, want %o", name, info.Mode().Perm(), wantMode)
		}
	}
	gotDomain, found, err := readActivePanelCertificateIdentityAt(dirFD, os.Getuid())
	if err != nil || !found || gotDomain != domain {
		t.Fatalf("active identity = %q, %v, %v", gotDomain, found, err)
	}
}

func TestTrustedPanelTLSDirectoryRejectsUnprivilegedOwner(t *testing.T) {
	tlsDir := t.TempDir()
	if os.Getuid() == 0 {
		if err := os.Chown(tlsDir, 1001, 1001); err != nil {
			t.Fatal(err)
		}
	}
	fd, err := openTrustedPanelTLSDirectoryOwned(tlsDir, 0)
	if fd >= 0 {
		unix.Close(fd)
	}
	if err == nil {
		t.Fatal("unprivileged TLS directory was accepted as root-authenticated")
	}
}

func TestActivePanelIdentityRejectsWritableMarker(t *testing.T) {
	tlsDir := t.TempDir()
	dirFD, err := unix.Open(
		tlsDir,
		unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		0,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(dirFD)
	if err := publishPanelCertificateVersion(
		dirFD, os.Getuid(), os.Getgid(), "panel.example.test",
		[]byte("certificate"), []byte("key"),
	); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(tlsDir, "current", "panel.domain")
	if err := os.Chmod(marker, 0o660); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readActivePanelCertificateIdentityAt(dirFD, os.Getuid()); err == nil {
		t.Fatal("group-writable identity marker was trusted")
	}
}
