package dnsengineartifact

import (
	"testing"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

func takeoverManifestFixture() mutationpayload.DNSEngineSwitchManifestCommitment {
	return mutationpayload.DNSEngineSwitchManifestCommitment{
		Mode:         transport.DNSEngineSwitchModeSwitch,
		TargetEngine: transport.DNSEngineBIND,
		SourceEpoch:  0,
		TargetEpoch:  1,
		Topology:     transport.DNSTopologyStandalone,
	}
}

func TestRunningBINDAdoptionJournalPreservesOwnerAuthority(t *testing.T) {
	manifest := takeoverManifestFixture()
	if !AdoptableRunningBINDManifest(manifest, false) || AdoptableRunningBINDManifest(manifest, true) {
		t.Fatal("historical takeover shape changed")
	}
	journal := SwitchJournalV1{
		TargetEngine: transport.DNSEngineBIND,
		TargetUnitsBefore: []UnitSnapshot{
			{Name: "named.service", LoadState: "loaded", ActiveState: "active"},
			{Name: "bind9.service", LoadState: "loaded", ActiveState: "active"},
		},
	}
	if selected, err := RunningBINDAdoptionJournal(manifest, journal); err != nil || !selected {
		t.Fatalf("running owner BIND was not selected: %v, %v", selected, err)
	}
	journal.TargetUnitsBefore[0].ActiveState = "inactive"
	if _, err := RunningBINDAdoptionJournal(manifest, journal); err == nil {
		t.Fatal("loaded alias disagreement accepted")
	}
	journal.TargetUnitsBefore[0].LoadState = "not-found"
	if selected, err := RunningBINDAdoptionJournal(manifest, journal); err != nil || !selected {
		t.Fatalf("unloaded alias was refused: %v, %v", selected, err)
	}
	journal.TargetUnitsBefore[1].ActiveState = "inactive"
	if selected, err := RunningBINDAdoptionJournal(manifest, journal); err != nil || selected {
		t.Fatalf("stopped target selected as running: %v, %v", selected, err)
	}
	journal.TargetUnitsBefore = journal.TargetUnitsBefore[:1]
	if _, err := RunningBINDAdoptionJournal(manifest, journal); err == nil {
		t.Fatal("missing unit preimage accepted")
	}
	journal.TargetUnitsBefore = append(journal.TargetUnitsBefore, journal.TargetUnitsBefore[0])
	if _, err := RunningBINDAdoptionJournal(manifest, journal); err == nil {
		t.Fatal("duplicate unit preimage accepted")
	}
	manifest.PeerIP = "192.0.2.1"
	if selected, err := RunningBINDAdoptionJournal(manifest, journal); err != nil || selected {
		t.Fatalf("foreign manifest entered takeover path: %v, %v", selected, err)
	}
}
