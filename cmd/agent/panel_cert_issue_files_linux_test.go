//go:build linux

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestActivatePanelCertificateIssueVersionAtomicallyReplacesCurrent(
	t *testing.T,
) {
	root := t.TempDir()
	oldVersion := managedPanelCertVersionPrefix + strings.Repeat("1", 32)
	newVersion := managedPanelCertVersionPrefix + strings.Repeat("2", 32)
	if err := os.Mkdir(filepath.Join(root, oldVersion), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, newVersion), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(oldVersion, filepath.Join(root, "current")); err != nil {
		t.Fatal(err)
	}
	directory, err := os.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer directory.Close()

	published, err := activatePanelCertificateIssueVersionAt(
		int(directory.Fd()),
		newVersion,
	)
	if err != nil || !published {
		t.Fatalf("activate published=%v err=%v", published, err)
	}
	target, err := os.Readlink(filepath.Join(root, "current"))
	if err != nil {
		t.Fatal(err)
	}
	if target != newVersion {
		t.Fatalf("current=%q want %q", target, newVersion)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".current-") {
			t.Fatalf("temporary current link remained: %q", entry.Name())
		}
	}
}

func TestReadPanelCertificateIssueReceiptRequiresRootOnlyFile(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root ownership assertion requires root")
	}
	root := t.TempDir()
	directory, err := os.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer directory.Close()
	receipt, err := newPanelCertificateIssueReceipt(
		testMutationRequestID,
		testPanelCertificateIssueQualifier(t),
		"panel.example.test",
		[]byte("leaf DER"),
	)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := canonicalPanelCertificateIssueReceipt(receipt)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, panelCertificateIssueReceiptName)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	got, found, err := readPanelCertificateIssueReceiptAt(int(directory.Fd()))
	if err != nil || !found || got != receipt {
		t.Fatalf("read found=%v receipt=%#v err=%v", found, got, err)
	}
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readPanelCertificateIssueReceiptAt(
		int(directory.Fd()),
	); err == nil {
		t.Fatal("group-readable receipt was trusted")
	}
}

func TestActivatePanelCertificateIssueVersionRejectsMalformedTarget(
	t *testing.T,
) {
	root := t.TempDir()
	directory, err := os.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer directory.Close()
	published, err := activatePanelCertificateIssueVersionAt(
		int(directory.Fd()),
		"../escape",
	)
	if err == nil || published {
		t.Fatalf("malformed target published=%v err=%v", published, err)
	}
	var stat unix.Stat_t
	if err := unix.Fstatat(
		int(directory.Fd()),
		"current",
		&stat,
		unix.AT_SYMLINK_NOFOLLOW,
	); err == nil || !os.IsNotExist(err) {
		t.Fatalf("malformed target created current: %v", err)
	}
}

func TestPanelCertificateIssueRecoveryTreatsMissingTLSDirectoryAsPrecommit(
	t *testing.T,
) {
	store := installPanelCertificateActivationMemoryStore(t)
	qualifier := testPanelCertificateIssueQualifier(t)
	state, err := newInteractivePanelCertificateActivationState(
		"panel.example.test",
		testMutationRequestID,
		qualifier,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := panelCertificateActivationWriteState(state); err != nil {
		t.Fatal(err)
	}
	originalLock := panelCertWithPublishLock
	panelCertWithPublishLock = func(action func() error) error {
		return action()
	}
	t.Cleanup(func() { panelCertWithPublishLock = originalLock })

	success, err := reconcilePersistedPanelCertificateIssueHostAt(
		context.Background(),
		filepath.Join(t.TempDir(), "missing"),
		testMutationRequestID,
		qualifier,
		"panel.example.test",
	)
	if err != nil || success {
		t.Fatalf("missing TLS recovery success=%v err=%v", success, err)
	}
	if retained, found := store.snapshot(); found {
		t.Fatalf("precommit activation state was retained: %+v", retained)
	}
}
