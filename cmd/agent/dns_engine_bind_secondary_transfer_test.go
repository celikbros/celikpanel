package main

import (
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/binddns"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

// A switch journal, completed switch or state written by an earlier release
// names the peer-only secondary generation (version 1). The Agent recognises it
// as its own earlier output; new work renders the loopback catalog policy.
func TestBINDSecondaryEarlierRenderingIsRecognisedForJournalTargets(t *testing.T) {
	pairing := secondaryCatalogOptionsTestPair()
	layout := bindHostLayout{OptionsConfig: "/etc/bind/named.conf.options", AnchorConfig: "/etc/bind/named.conf.local", GenerationRoot: aptBINDGenerationRoot}
	manifest, err := mutationpayload.CanonicalDNSEngineSwitchManifestWithPairIdentity(transport.DNSEngineSwitchModeSwitch, "", transport.DNSEngineBIND, 0, 1, 0, transport.DNSTopologyPaired, pairing.Role, pairing.LocalIP, pairing.LocalNS, pairing.PeerIP, pairing.PeerNS, nil)
	if err != nil {
		t.Fatal(err)
	}
	journal := dnsEngineSwitchJournal{Mode: manifest.Mode, SourceEngine: manifest.SourceEngine, TargetEngine: manifest.TargetEngine, SourceEpoch: manifest.SourceEpoch, TargetEpoch: manifest.TargetEpoch, SourceRevision: manifest.SourceRevision, Topology: manifest.Topology, ManifestQualifier: manifest.Qualifier, SnapshotBytes: manifest.SnapshotBytes, Zones: manifest.Zones, PairRole: pairing.Role, LocalIP: pairing.LocalIP, LocalNS: pairing.LocalNS, PeerIP: pairing.PeerIP, PeerNS: pairing.PeerNS}
	plan, err := bindSwitchTreePlanWithPrimaryCatalogSerial(manifest, switchJournalBinding(journal), 0)
	if err != nil {
		t.Fatal(err)
	}
	current, err := binddns.RenderTree(layout.GenerationRoot, plan)
	if err != nil {
		t.Fatal(err)
	}
	previous, err := binddns.RenderPreviousSecondaryTree(layout.GenerationRoot, plan)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(current.Config), "allow-transfer { 192.0.2.10/32; 127.0.0.1; ::1; };") ||
		strings.Contains(string(previous.Config), "allow-transfer") {
		t.Fatalf("unexpected renderings:\n%s\n%s", current.Config, previous.Config)
	}
	for _, target := range []binddns.Generation{current, previous} {
		got, err := bindExpectedGenerationForTarget(layout.GenerationRoot, plan, target.ID)
		if err != nil || got.ID != target.ID || string(got.Config) != string(target.Config) {
			t.Fatalf("target %s not recognised: %v", target.ID, err)
		}
		journal.TargetGeneration = target.ID
		options, err := bindSecondaryOptionsFromJournal(layout, journal)
		if err != nil || options == nil || *options != *pairing {
			t.Fatalf("journal target %s lost its options identity: %+v %v", target.ID, options, err)
		}
	}
	// An unrelated target is never matched to either rendering.
	other, err := bindExpectedGenerationForTarget(layout.GenerationRoot, plan, strings.Repeat("a", 64))
	if err != nil || other.ID != current.ID {
		t.Fatalf("unrelated target changed the rendered expectation: %v", err)
	}
	journal.TargetGeneration = strings.Repeat("a", 64)
	if _, err := bindSecondaryOptionsFromJournal(layout, journal); err == nil {
		t.Fatal("unrelated journal target accepted")
	}
	for version, accepted := range map[int]bool{0: false, 1: true, 2: true, 3: false} {
		receipt := &binddns.PairingReceipt{Role: pairing.Role, LocalIP: pairing.LocalIP, LocalNS: pairing.LocalNS, PeerIP: pairing.PeerIP, PeerNS: pairing.PeerNS, SecondaryConfigVersion: version}
		options, err := bindSecondaryOptionsFromReceipt(receipt)
		if (err == nil && options != nil) != accepted {
			t.Fatalf("secondary receipt version %d accepted=%t: %v", version, err == nil, err)
		}
	}
}

func TestInspectionPendingCodeCarriesOnlyReviewedInspectorReasons(t *testing.T) {
	for reason, want := range map[string]string{
		"":                                 transport.DNSPeerPendingInspectionUnknown,
		"catalog_transfer_refused":         transport.DNSPeerPendingCatalogTransferRefused,
		"named_unavailable":                "dns_peer_inspection_unknown:named_unavailable",
		"catalog_transfer_failed":          "dns_peer_inspection_unknown:catalog_transfer_failed",
		"denied from 203.0.113.9 by peer":  transport.DNSPeerPendingInspectionUnknown,
		"dns_peer_inspection_unknown:evil": transport.DNSPeerPendingInspectionUnknown,
	} {
		got := inspectionPendingCode(reason)
		if got != want || !transport.ValidDNSPeerPendingCode(got) {
			t.Fatalf("reason %q -> %q, want %q", reason, got, want)
		}
		if pendingDNSPeerCode(pendingBINDPeer(got)) != want || dnsZoneV3PendingLedgerCode(got) != want {
			t.Fatalf("reason %q lost through the pending error or ledger", reason)
		}
	}
}
