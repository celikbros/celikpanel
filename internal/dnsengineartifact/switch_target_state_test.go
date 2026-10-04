package dnsengineartifact

import (
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

func TestExactSwitchTargetStatePreservesHistoricalBoundaries(t *testing.T) {
	journal := SwitchJournalV1{
		Phase: SwitchPhaseTargetVerified, Mode: transport.DNSEngineSwitchModeSwitch,
		TargetEngine: transport.DNSEngineBIND, TargetEpoch: 1,
		SourceRevision: 4, TargetGeneration: "generation-a",
		ManifestQualifier: "manifest-a", MutationRequestID: "request-a",
		MutationOwnerID: "owner-a",
	}
	state := StateV1{
		Schema: StateSchemaV1, Mode: transport.DNSEngineSwitchModeSwitch,
		Engine: transport.DNSEngineBIND, EngineEpoch: 1,
		SourceRevision: 4, Generation: "generation-a",
		ManifestQualifier: "manifest-a", MutationRequestID: "request-a",
		MutationOwnerID: "owner-a",
	}
	if !ExactSwitchTargetStateV1(state, journal) {
		t.Fatal("exact standalone BIND target refused")
	}
	changed := state
	changed.MutationOwnerID = "owner-b"
	if ExactSwitchTargetStateV1(changed, journal) {
		t.Fatal("another owner accepted")
	}
	changed = state
	changed.Generation = "generation-b"
	if ExactSwitchTargetStateV1(changed, journal) {
		t.Fatal("another BIND generation accepted")
	}

	// Released paired targets could have the all-empty legacy tuple, but only
	// after target verification. Modern paired receipts must match their tuple.
	paired := journal
	paired.PairRole = transport.DNSPairRolePrimary
	paired.LocalIP, paired.PeerIP = "192.0.2.1", "192.0.2.2"
	paired.PrimaryCatalogSerial = 2
	if !ExactSwitchTargetStateV1(state, paired) {
		t.Fatal("verified historical paired target refused")
	}
	paired.Phase = SwitchPhaseTargetStarted
	if ExactSwitchTargetStateV1(state, paired) {
		t.Fatal("unverified historical paired target accepted")
	}
	paired.Phase = SwitchPhaseCommitted
	modern := state
	modern.PairRole = paired.PairRole
	modern.PairLocalIP, modern.PairPeerIP = paired.LocalIP, paired.PeerIP
	modern.PrimaryCatalogSerial = paired.PrimaryCatalogSerial
	if !ExactSwitchTargetStateV1(modern, paired) {
		t.Fatal("exact directional paired target refused")
	}
	modern.PairPeerIP = "192.0.2.3"
	if ExactSwitchTargetStateV1(modern, paired) {
		t.Fatal("changed directional peer accepted")
	}

	reinstall := journal
	reinstall.Mode = transport.DNSEngineSwitchModeReinstall
	if !ExactSwitchTargetStateV1(state, reinstall) {
		t.Fatal("reinstall of original tenure refused")
	}
	stamped := state
	stamped.Mode = transport.DNSEngineSwitchModeReinstall
	if ExactSwitchTargetStateV1(stamped, reinstall) {
		t.Fatal("reinstall-stamped tenure accepted")
	}
	pdns := journal
	pdns.TargetEngine = transport.DNSEnginePowerDNS
	pdns.TargetGeneration = ""
	pdnsState := state
	pdnsState.Engine = transport.DNSEnginePowerDNS
	pdnsState.Generation = ""
	if !ExactSwitchTargetStateV1(pdnsState, pdns) {
		t.Fatal("exact PowerDNS target refused")
	}
	pdnsState.Generation = "unexpected"
	if ExactSwitchTargetStateV1(pdnsState, pdns) {
		t.Fatal("PowerDNS generation accepted")
	}
}
