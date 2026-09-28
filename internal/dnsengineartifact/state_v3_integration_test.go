package dnsengineartifact

import (
	"bytes"
	"testing"

	"github.com/alicelik/celikpanel/internal/pdnsnative"
	"github.com/alicelik/celikpanel/internal/transport"
)

func TestFreshPrimaryV3StateAndOwnershipRemainDistinct(t *testing.T) {
	_, journal := freshPrimaryV3Fixture(t)
	state := StateV1{
		Schema:               StateSchemaV1,
		Mode:                 transport.DNSEngineSwitchModeSwitch,
		Engine:               transport.DNSEnginePowerDNS,
		EngineEpoch:          journal.TargetEpoch,
		PairRole:             journal.PairRole,
		PairLocalIP:          journal.LocalIP,
		PairPeerIP:           journal.PeerIP,
		PrimaryCatalogSerial: 1790542951,
		SourceRevision:       journal.SourceRevision,
		ManifestQualifier:    journal.ManifestQualifier,
		MutationRequestID:    journal.MutationRequestID,
		MutationOwnerID:      journal.MutationOwnerID,
		NativeCatalogV3:      NativeCatalogDebian49V3,
	}
	current, err := CanonicalStateDocumentV2(state)
	if err != nil {
		t.Fatal(err)
	}
	ownership, err := CanonicalOwnershipDocumentV2(state)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(current, ownership) {
		t.Fatal("current state and ownership are the same document")
	}
	decodedCurrent, separated, err := DecodeStateDocument(current)
	if err != nil || !separated || decodedCurrent != state {
		t.Fatalf("state roundtrip: separated=%v err=%v", separated, err)
	}
	decodedOwner, separated, err := DecodeOwnershipDocument(ownership)
	if err != nil || !separated || decodedOwner != state {
		t.Fatalf("ownership roundtrip: separated=%v err=%v", separated, err)
	}
	if _, _, err := DecodeOwnershipDocument(current); err == nil {
		t.Fatal("current state accepted as ownership")
	}
	if _, _, err := DecodeStateDocument(ownership); err == nil {
		t.Fatal("ownership accepted as current state")
	}
	if _, err := CanonicalV1(state); err == nil {
		t.Fatal("native state silently downgraded to v1")
	}
	if relation, err := CompareV1(decodedOwner, decodedCurrent); err != nil || relation != SamePublication {
		t.Fatalf("same native tenure not comparable: relation=%v err=%v", relation, err)
	}
	legacy := state
	legacy.NativeCatalogV3 = ""
	if _, err := CompareV1(legacy, decodedCurrent); err == nil {
		t.Fatal("legacy owner accepted native state without marker")
	}
	if _, err := CompareV1(decodedOwner, legacy); err == nil {
		t.Fatal("native owner accepted legacy state without marker")
	}
	mutated := bytes.Replace(current, []byte(NativeCatalogDebian49V3), []byte("pdns-fresh-paired-primary/debian-4.9/v2"), 1)
	if bytes.Equal(mutated, current) {
		t.Fatal("fixture lacks native marker")
	}
	if _, _, err := DecodeStateDocument(mutated); err == nil {
		t.Fatal("changed native transform marker accepted")
	}
}
func TestFreshPrimaryV3TargetStateBindsObservedSerial(t *testing.T) {
	_, journal := freshPrimaryV3Fixture(t)
	journal.Schema = SwitchJournalSchemaV3
	journal.Phase = SwitchPhaseCommitted
	journal.PDNSFreshPlan = &PDNSFreshPrimaryPlanV3{Native: &pdnsnative.RecordedTransition{
		Observed: pdnsnative.CatalogTransition{SourceSerial: 1, NativeSerial: 1790542951},
	}}
	state := StateV1{
		Schema: StateSchemaV1, Mode: journal.Mode, Engine: journal.TargetEngine,
		EngineEpoch: journal.TargetEpoch, PairRole: journal.PairRole,
		PairLocalIP: journal.LocalIP, PairPeerIP: journal.PeerIP,
		PrimaryCatalogSerial: 1790542951, SourceRevision: journal.SourceRevision,
		ManifestQualifier: journal.ManifestQualifier,
		MutationRequestID: journal.MutationRequestID, MutationOwnerID: journal.MutationOwnerID,
		NativeCatalogV3: NativeCatalogDebian49V3,
	}
	if !ExactSwitchTargetStateV1(state, journal) {
		t.Fatal("recorded native serial target rejected")
	}
	for _, changed := range []StateV1{
		func() StateV1 { s := state; s.PrimaryCatalogSerial = journal.PrimaryCatalogSerial; return s }(),
		func() StateV1 { s := state; s.NativeCatalogV3 = ""; return s }(),
		func() StateV1 { s := state; s.PairPeerIP = "192.0.2.12"; return s }(),
	} {
		if ExactSwitchTargetStateV1(changed, journal) {
			t.Fatal("changed native target accepted")
		}
	}
}
