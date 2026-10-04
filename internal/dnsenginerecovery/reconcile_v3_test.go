package dnsenginerecovery

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/binddns"
	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/pdnsnative"
	"github.com/alicelik/celikpanel/internal/transport"
)

func committedFreshV3ReconcileFixture(t *testing.T) (dnsengineartifact.JournalPolicy, dnsengineartifact.SwitchJournalV1, dnsengineartifact.SwitchIdentity) {
	t.Helper()
	policy, _, _ := switchFixture(t)
	raw, err := os.ReadFile(filepath.Join("..", "dnsengineartifact", "testdata", "switch-journal", "alpha81-pdns-switch.json"))
	if err != nil {
		t.Fatal(err)
	}
	base, err := policy.DecodeSwitchJournal(raw)
	if err != nil {
		t.Fatal(err)
	}
	commitment, err := mutationpayload.CanonicalDNSEngineSwitchManifestWithPairIdentity(
		transport.DNSEngineSwitchModeSwitch, "", transport.DNSEnginePowerDNS, 0, 1, 0,
		transport.DNSTopologyPaired, transport.DNSPairRolePrimary,
		"192.0.2.10", "ns1.example.test", "192.0.2.11", "ns2.example.test", nil)
	if err != nil {
		t.Fatal(err)
	}
	base.SourceEngine, base.SourceEpoch, base.TargetEpoch, base.SourceRevision = "", 0, 1, 0
	base.Topology, base.PairRole = commitment.Topology, commitment.PairRole
	base.LocalIP, base.LocalNS, base.PeerIP, base.PeerNS = commitment.LocalIP, commitment.LocalNS, commitment.PeerIP, commitment.PeerNS
	base.ManifestQualifier, base.SnapshotBytes, base.Zones = commitment.Qualifier, commitment.SnapshotBytes, commitment.Zones
	base.PrimaryCatalogSerial = 1
	base.StateBefore = dnsengineartifact.FileSnapshot{Path: policy.StatePath}
	base.SourceUnitsBefore = nil
	base.TargetUnitsBefore = []dnsengineartifact.UnitSnapshot{{Name: "pdns.service", LoadState: "loaded", ActiveState: "inactive", UnitFileState: "disabled"}}
	after := append([]dnsengineartifact.FileSnapshot(nil), base.ConfigBefore...)
	for _, i := range []int{1, 2} {
		after[i] = dnsengineartifact.FileSnapshot{Path: base.ConfigBefore[i].Path, Exists: true, Mode: 0o644, OwnerKnown: true, Data: []byte("managed=1\n")}
		after[i].SHA256 = dnsengineartifact.DigestBytes(after[i].Data)
	}
	intent, err := policy.BuildPDNSFreshPrimaryJournalV3(base, after)
	if err != nil {
		t.Fatal(err)
	}
	measuredRaw, err := os.ReadFile(filepath.Join("..", "..", "deploy", "e2e", "dns-kill-matrix", "evidence", "pdns-master-bind-20260928", "debian-primary.json"))
	if err != nil {
		t.Fatal(err)
	}
	var measured struct {
		Staged struct {
			SQLite pdnsnative.Snapshot `json:"sqlite"`
		} `json:"staged"`
		Running struct {
			SQLite pdnsnative.Snapshot `json:"sqlite"`
		} `json:"running"`
	}
	if err := json.Unmarshal(measuredRaw, &measured); err != nil {
		t.Fatal(err)
	}
	candidate := dnsengineartifact.PDNSTargetCandidateProofV4{Path: intent.PDNSCandidatePath, Device: 9, Inode: 11, Mode: 0o640, UID: 0, GID: 42, Size: 4096, SHA256: strings.Repeat("f", 64), NoSidecars: true}
	staged, err := policy.StagePDNSFreshPrimaryCandidateV3(intent, candidate, measured.Staged.SQLite)
	if err != nil {
		t.Fatal(err)
	}
	staged.Phase = dnsengineartifact.SwitchPhaseTargetStarted
	catalog, err := binddns.CatalogDomain(staged.LocalIP)
	if err != nil {
		t.Fatal(err)
	}
	native, err := policy.AttachPDNSFreshNativeObservationV3(staged, catalog, measured.Running.SQLite)
	if err != nil {
		t.Fatal(err)
	}
	native.Phase = dnsengineartifact.SwitchPhaseCommitted
	if err := policy.ValidateSwitchJournal(native); err != nil {
		t.Fatal(err)
	}
	id := dnsengineartifact.SwitchIdentity{RequestID: native.MutationRequestID, OwnerID: native.MutationOwnerID, Target: native.TargetEngine, Qualifier: native.ManifestQualifier}
	return policy, native, id
}

func TestReconcileCommittedFreshV3OnlyReprovesTarget(t *testing.T) {
	policy, journal, id := committedFreshV3ReconcileFixture(t)
	tr := &trace{journal: journal, exists: true}
	got, err := Reconcile(context.Background(), policy, id, tr.operations())
	if err != nil || got != OutcomeCommitted || !reflect.DeepEqual(tr.steps, []string{"read", "verify"}) {
		t.Fatalf("committed retry=%q steps=%v err=%v", got, tr.steps, err)
	}
	tr = &trace{journal: journal, exists: true, targetErr: errors.New("native changed")}
	if got, err := Reconcile(context.Background(), policy, id, tr.operations()); err == nil || got != OutcomeAbsent || !reflect.DeepEqual(tr.steps, []string{"read", "verify"}) {
		t.Fatalf("changed native accepted=%q steps=%v err=%v", got, tr.steps, err)
	}
	pending := journal
	pending.Phase = dnsengineartifact.SwitchPhaseTargetStarted
	tr = &trace{journal: pending, exists: true}
	if got, err := Reconcile(context.Background(), policy, id, tr.operations()); err == nil || got != OutcomeAbsent || !reflect.DeepEqual(tr.steps, []string{"read"}) {
		t.Fatalf("pending v3 admitted=%q steps=%v err=%v", got, tr.steps, err)
	}
	wrong := id
	wrong.OwnerID = strings.Repeat("f", 32)
	tr = &trace{journal: journal, exists: true}
	if got, err := Reconcile(context.Background(), policy, wrong, tr.operations()); err == nil || got != OutcomeAbsent || !reflect.DeepEqual(tr.steps, []string{"read"}) {
		t.Fatalf("wrong owner admitted=%q steps=%v err=%v", got, tr.steps, err)
	}
}
