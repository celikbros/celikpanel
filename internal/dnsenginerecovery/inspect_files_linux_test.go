//go:build linux

package dnsenginerecovery

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alicelik/celikpanel/internal/servicemutationledger"
)

func TestInspectFilesUsesPrivateCanonicalEvidenceAndDistinguishesAbsence(t *testing.T) {
	policy, journal, ledger, now := inspectionFixture(t)
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	policy.StatePath = filepath.Join(root, "dns-engine-state.json")
	journal.StateBefore.Path = policy.StatePath
	journalRaw, err := policy.EncodeSwitchJournal(journal)
	if err != nil {
		t.Fatal(err)
	}
	ledgerRaw, err := servicemutationledger.Encode(&ledger)
	if err != nil {
		t.Fatal(err)
	}
	owner := servicemutationledger.FileOwner{UID: 0, GID: 0}
	journalPath := filepath.Join(root, "dns-engine-switch-journal.json")
	ledgerPath := filepath.Join(root, "service-mutations.json")
	if err := os.WriteFile(journalPath, journalRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ledgerPath, ledgerRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	got, present, err := InspectFiles(root, owner, policy, now)
	if err != nil || !present || got.Status != EvidenceActive {
		t.Fatalf("valid files: %+v, %v, %v", got, present, err)
	}
	if _, _, err := InspectFiles(root, servicemutationledger.FileOwner{UID: 0, GID: 1}, policy, now); err == nil {
		t.Fatal("different established owner accepted")
	}
	if err := os.Remove(ledgerPath); err != nil {
		t.Fatal(err)
	}
	if _, present, err := InspectFiles(root, owner, policy, now); err == nil || !present {
		t.Fatalf("missing ledger confused with missing journal: %v, %v", present, err)
	}
	if err := os.Remove(journalPath); err != nil {
		t.Fatal(err)
	}
	if _, present, err := InspectFiles(root, owner, policy, now); err != nil || present {
		t.Fatalf("missing journal not reported distinctly: %v, %v", present, err)
	}
	target := filepath.Join(root, "target")
	if err := os.WriteFile(target, journalRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, journalPath); err != nil {
		t.Fatal(err)
	}
	if _, _, err := InspectFiles(root, owner, policy, now); err == nil {
		t.Fatal("symlink evidence accepted")
	}
	if err := os.Remove(journalPath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journalPath, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, present, err := InspectFiles(root, owner, policy, now); err == nil || !present {
		t.Fatalf("malformed journal did not remain an unknown present artifact: %v, %v", present, err)
	}
	if err := os.WriteFile(journalPath, journalRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ledgerPath, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, present, err := InspectFiles(root, owner, policy, now); err == nil || !present {
		t.Fatalf("malformed ledger accepted: %v, %v", present, err)
	}
}
func TestInspectFilesRejectsUnboundHostPolicy(t *testing.T) {
	policy, _, _, now := inspectionFixture(t)
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, _, err := InspectFiles(root, servicemutationledger.FileOwner{UID: 0, GID: 0}, policy, now); err == nil {
		t.Fatal("host path mismatch accepted")
	}
}
