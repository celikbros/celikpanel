//go:build linux

package dnsenginerecovery

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
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
	if err != nil || !present || got.Status != EvidenceActive || got.TargetReceipt != TargetReceiptAbsent || got.SourceReceipt != SourceReceiptMutualAbsence || got.SourceOwnership != SourceOwnershipNotApplicable {
		t.Fatalf("valid files with absent state: %+v, %v, %v", got, present, err)
	}
	state := dnsengineartifact.StateV1{
		Schema: dnsengineartifact.StateSchemaV1, Mode: journal.Mode,
		Engine: journal.TargetEngine, EngineEpoch: journal.TargetEpoch,
		Generation: journal.TargetGeneration, SourceRevision: journal.SourceRevision,
		ManifestQualifier: journal.ManifestQualifier,
		MutationRequestID: journal.MutationRequestID, MutationOwnerID: journal.MutationOwnerID,
	}
	stateRaw, err := dnsengineartifact.CanonicalV1(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(policy.StatePath, stateRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	got, present, err = InspectFiles(root, owner, policy, now)
	if err != nil || !present || got.TargetReceipt != TargetReceiptExact || got.SourceReceipt != SourceReceiptDifferent {
		t.Fatalf("exact target receipt: %+v, %v, %v", got, present, err)
	}
	state.MutationOwnerID = "ffffffffffffffffffffffffffffffff"
	stateRaw, err = dnsengineartifact.CanonicalV1(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(policy.StatePath, stateRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	got, present, err = InspectFiles(root, owner, policy, now)
	if err != nil || !present || got.TargetReceipt != TargetReceiptDifferent || got.SourceReceipt != SourceReceiptDifferent {
		t.Fatalf("different target receipt: %+v, %v, %v", got, present, err)
	}
	if err := os.WriteFile(policy.StatePath, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, present, err := InspectFiles(root, owner, policy, now); err == nil || !present {
		t.Fatalf("malformed current state was accepted: %v, %v", present, err)
	}
	if err := os.Remove(policy.StatePath); err != nil {
		t.Fatal(err)
	}
	stateLinkTarget := filepath.Join(root, "state-link-target")
	if err := os.WriteFile(stateLinkTarget, stateRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(stateLinkTarget, policy.StatePath); err != nil {
		t.Fatal(err)
	}
	if _, present, err := InspectFiles(root, owner, policy, now); err == nil || !present {
		t.Fatalf("symlinked current state was accepted: %v, %v", present, err)
	}
	if err := os.Remove(policy.StatePath); err != nil {
		t.Fatal(err)
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

func TestInspectFilesDistinguishesFrozenSourceFromForeignReceipt(t *testing.T) {
	policy, _, ledger, now := inspectionFixture(t)
	fixture, err := os.ReadFile(filepath.Join("..", "dnsengineartifact", "testdata", "switch-journal", "alpha81-pdns-switch.json"))
	if err != nil {
		t.Fatal(err)
	}
	journal, err := policy.DecodeSwitchJournal(fixture)
	if err != nil {
		t.Fatal(err)
	}
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
	job := ledger.Jobs[journal.MutationRequestID]
	job.Target = string(journal.TargetEngine)
	job.PackageName = journal.ManifestQualifier
	ledgerRaw, err := servicemutationledger.Encode(&ledger)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{
		"dns-engine-switch-journal.json": journalRaw,
		"service-mutations.json":         ledgerRaw,
	} {
		if err := os.WriteFile(filepath.Join(root, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	owner := servicemutationledger.FileOwner{UID: 0, GID: 0}
	check := func(want SourceReceiptStatus, target TargetReceiptStatus, ownership SourceOwnershipStatus) {
		t.Helper()
		got, present, err := InspectFiles(root, owner, policy, now)
		if err != nil || !present || got.SourceReceipt != want || got.TargetReceipt != target || got.SourceOwnership != ownership || len(got.NativeUnits) != 3 || got.NativeUnits[0] != "bind9.service" || got.NativeUnits[1] != "named.service" || got.NativeUnits[2] != "pdns.service" {
			t.Fatalf("source/target/ownership classification: %+v, %v, %v; want %s/%s/%s", got, present, err, want, target, ownership)
		}
	}
	check(SourceReceiptDifferent, TargetReceiptAbsent, SourceOwnershipAbsent)
	if err := os.WriteFile(policy.StatePath, journal.StateBefore.Data, 0o600); err != nil {
		t.Fatal(err)
	}
	check(SourceReceiptExact, TargetReceiptDifferent, SourceOwnershipAbsent)
	source, _, err := dnsengineartifact.DecodeStateDocument(journal.StateBefore.Data)
	if err != nil {
		t.Fatal(err)
	}
	source.MutationOwnerID = "ffffffffffffffffffffffffffffffff"
	foreignRaw, err := dnsengineartifact.CanonicalV1(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(policy.StatePath, foreignRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	check(SourceReceiptDifferent, TargetReceiptDifferent, SourceOwnershipAbsent)
	targetState := dnsengineartifact.StateV1{
		Schema: dnsengineartifact.StateSchemaV1, Mode: journal.Mode,
		Engine: journal.TargetEngine, EngineEpoch: journal.TargetEpoch,
		Generation: journal.TargetGeneration, SourceRevision: journal.SourceRevision,
		ManifestQualifier: journal.ManifestQualifier,
		MutationRequestID: journal.MutationRequestID, MutationOwnerID: journal.MutationOwnerID,
	}
	targetRaw, err := dnsengineartifact.CanonicalV1(targetState)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(policy.StatePath, targetRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	check(SourceReceiptDifferent, TargetReceiptExact, SourceOwnershipAbsent)
	ownershipPath := filepath.Join(root, "dns-engine-ownership-bind.json")
	ownershipSource, _, err := dnsengineartifact.DecodeStateDocument(journal.StateBefore.Data)
	if err != nil {
		t.Fatal(err)
	}
	for _, encode := range []func(dnsengineartifact.StateV1) ([]byte, error){dnsengineartifact.CanonicalV1, dnsengineartifact.CanonicalOwnershipDocumentV2} {
		ownershipRaw, err := encode(ownershipSource)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(ownershipPath, ownershipRaw, 0o600); err != nil {
			t.Fatal(err)
		}
		check(SourceReceiptDifferent, TargetReceiptExact, SourceOwnershipExact)
	}
	if err := os.WriteFile(ownershipPath, foreignRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	check(SourceReceiptDifferent, TargetReceiptExact, SourceOwnershipDifferent)
	if err := os.WriteFile(ownershipPath, targetRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, present, err := InspectFiles(root, owner, policy, now); err == nil || !present {
		t.Fatalf("wrong-engine ownership became a different receipt: %v, %v", present, err)
	}
	if err := os.WriteFile(ownershipPath, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, present, err := InspectFiles(root, owner, policy, now); err == nil || !present {
		t.Fatalf("malformed ownership became an absent receipt: %v, %v", present, err)
	}
	if err := os.Remove(ownershipPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(policy.StatePath, ownershipPath); err != nil {
		t.Fatal(err)
	}
	if _, present, err := InspectFiles(root, owner, policy, now); err == nil || !present {
		t.Fatalf("symlinked ownership became an absent receipt: %v, %v", present, err)
	}
}
