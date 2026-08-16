package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

func testBINDSwitchJournal(t *testing.T) dnsEngineSwitchJournal {
	t.Helper()
	manifest, err := mutationpayload.CanonicalDNSEngineSwitchManifest(
		"", transport.DNSEngineBIND, 0, 1, 0,
		transport.DNSTopologyStandalone, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	return dnsEngineSwitchJournal{
		Schema: dnsEngineSwitchJournalSchema, Phase: dnsSwitchPhaseTargetStaged,
		MutationRequestID: strings.Repeat("a", 32), MutationOwnerID: strings.Repeat("b", 32),
		ManifestQualifier: manifest.Qualifier,
		SourceEngine:      manifest.SourceEngine, TargetEngine: manifest.TargetEngine,
		SourceEpoch: manifest.SourceEpoch, TargetEpoch: manifest.TargetEpoch,
		SourceRevision: manifest.SourceRevision, Topology: manifest.Topology,
		SnapshotBytes: manifest.SnapshotBytes, Zones: manifest.Zones,
		TargetGeneration: strings.Repeat("c", 64),
		StateBefore:      dnsFileSnapshot{Path: filepath.Join(root, "dns-engine-state.json")},
		ConfigBefore:     []dnsFileSnapshot{},
		TargetUnitsBefore: []dnsUnitSnapshot{{
			Name: "named.service", LoadState: "not-found", ActiveState: "inactive",
		}},
		SourceUnitsBefore: []dnsUnitSnapshot{},
	}
}

func TestDNSEngineSwitchJournalCanonicalRoundTrip(t *testing.T) {
	journal := testBINDSwitchJournal(t)
	encoded, err := encodeDNSEngineSwitchJournal(journal)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeDNSEngineSwitchJournal(encoded)
	if err != nil {
		t.Fatal(err)
	}
	reencoded, err := encodeDNSEngineSwitchJournal(decoded)
	if err != nil || !bytes.Equal(encoded, reencoded) {
		t.Fatalf("canonical round trip err=%v", err)
	}
	if _, err := decodeDNSEngineSwitchJournal(append([]byte(" "), encoded...)); err == nil {
		t.Fatal("noncanonical journal whitespace was accepted")
	}
	journal.TargetUnitsBefore[0].UnitFileState = "alias"
	if _, err := encodeDNSEngineSwitchJournal(journal); err == nil {
		t.Fatal("unrestorable alias unit state was accepted")
	}
}
