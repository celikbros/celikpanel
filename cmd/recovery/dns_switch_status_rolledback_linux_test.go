//go:build linux

package main

import (
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/dnsenginerecovery"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/pdnsnative"
	"github.com/alicelik/celikpanel/internal/transport"
)

// rolledBackTerminal moves guidance evidence to a retained rolled-back
// journal beside its terminal ledger verdict.
func rolledBackTerminal(e dnsenginerecovery.SwitchEvidence) dnsenginerecovery.SwitchEvidence {
	e.Journal.Phase, e.Observation.Phase = dnsengineartifact.SwitchPhaseRolledBack, dnsengineartifact.SwitchPhaseRolledBack
	e.Observation.Status = dnsenginerecovery.EvidenceTerminalRolledBack
	return e
}

// withReconstructedManifest binds the journal to the manifest its fields
// reconstruct, as a real journal is.
func withReconstructedManifest(t *testing.T, e dnsenginerecovery.SwitchEvidence) dnsenginerecovery.SwitchEvidence {
	t.Helper()
	j := e.Journal
	manifest, err := mutationpayload.CanonicalDNSEngineSwitchManifestWithPairIdentity(
		j.Mode, j.SourceEngine, j.TargetEngine, j.SourceEpoch, j.TargetEpoch, j.SourceRevision,
		j.Topology, j.PairRole, j.LocalIP, j.LocalNS, j.PeerIP, j.PeerNS, j.Zones,
	)
	if err != nil {
		t.Fatalf("test journal does not form a manifest: %v", err)
	}
	e.Journal.ManifestQualifier, e.Journal.SnapshotBytes = manifest.Qualifier, manifest.SnapshotBytes
	if _, err := dnsengineartifact.SwitchJournalManifest(e.Journal); err != nil {
		t.Fatal(err)
	}
	return e
}

func requireRolledBackText(t *testing.T, text string, want, forbid []string) {
	t.Helper()
	if strings.Contains(text, "On a compatible Agent restart") {
		t.Fatalf("rolled-back text kept the unconditional Agent-restart promise:\n%s", text)
	}
	for _, fragment := range want {
		if !strings.Contains(text, fragment) {
			t.Fatalf("rolled-back text lacks %q:\n%s", fragment, text)
		}
	}
	for _, fragment := range forbid {
		if strings.Contains(text, fragment) {
			t.Fatalf("rolled-back text contains %q:\n%s", fragment, text)
		}
	}
}

// Item 2: the terminal rolled-back text is true per journal class. A V2
// journal is never finished by the Agent, so only the owner command is named.
func TestRolledBackStatusV2NamesOnlyTheOwnerCommand(t *testing.T) {
	e := ownerGuidanceBINDSwitchEvidence()
	e = rolledBackTerminal(e)
	e.Observation.SourceReceipt, e.Observation.TargetReceipt = dnsenginerecovery.SourceReceiptExact, dnsenginerecovery.TargetReceiptDifferent
	if ownerDNSRecoveryCommand(e) != ownerBINDSwitchInverseCommand {
		t.Fatal("fixture is not admitted by recover-dns-bind-switch")
	}
	text := terminalRolledBackDNSSwitchGuidance(e, false)
	requireRolledBackText(t, text,
		[]string{
			"does not run this journal's rollback itself, so restarting it will not retire the journal",
			"/usr/libexec/celikpanel/recovery recover-dns-bind-switch --request-id " + ownerGuidanceRequest + ";",
			"contact support with request id " + ownerGuidanceRequest,
		},
		[]string{"systemctl restart celikpanel-agent"},
	)
	// The same V2 journal without an admitting command names neither a
	// command nor an Agent restart.
	e.Journal.Topology = transport.DNSTopologyPaired
	if ownerDNSRecoveryCommand(e) != "" {
		t.Fatal("paired V2 fixture unexpectedly admitted")
	}
	text = terminalRolledBackDNSSwitchGuidance(e, true)
	requireRolledBackText(t, text,
		[]string{"no owner recovery command accepts this journal's recorded shape", "contact support with request id " + ownerGuidanceRequest},
		[]string{"systemctl restart celikpanel-agent", "/usr/libexec/celikpanel/recovery recover-"},
	)
}

// A V1 PowerDNS adoption at rolled-back is finished by a restarted Agent
// (re-proof only, no effect) and also by the admitted owner command.
func TestRolledBackStatusV1PDNSAdoptionNamesAgentRestartAndOwnerCommand(t *testing.T) {
	e := ownerGuidancePDNSAdoptionEvidence()
	e.Journal.Topology = transport.DNSTopologyStandalone
	e = withReconstructedManifest(t, rolledBackTerminal(e))
	e.Observation.SourceReceipt, e.Observation.TargetReceipt = dnsenginerecovery.SourceReceiptMutualAbsence, dnsenginerecovery.TargetReceiptAbsent
	if ownerDNSRecoveryCommand(e) != ownerPDNSAdoptionInverseCommand {
		t.Fatal("fixture is not admitted by recover-dns-pdns-adoption")
	}
	finishes, reproveOnly := agentFinishesRolledBackDNSJournal(e)
	if !finishes || !reproveOnly {
		t.Fatalf("V1 PowerDNS adoption classified finishes=%v reproveOnly=%v", finishes, reproveOnly)
	}
	requireRolledBackText(t, terminalRolledBackDNSSwitchGuidance(e, true),
		[]string{
			"systemctl restart celikpanel-agent",
			"re-proves the restored PowerDNS source under the host lock without changing it",
			"/usr/libexec/celikpanel/recovery recover-dns-pdns-adoption --request-id " + ownerGuidanceRequest + ";",
		},
		[]string{"does not run this journal's rollback itself"},
	)
}

// Other V1 inverses are re-run by the Agent; the text stays conditional. The
// paired-secondary PowerDNS reconfiguration is the one V1 class whose Agent
// proof accepts only rolling-back, so no restart is promised for it.
func TestRolledBackStatusV1SwitchIsConditionalAndReconfigureIsNot(t *testing.T) {
	j := dnsengineartifact.SwitchJournalV1{
		Schema: dnsengineartifact.SwitchJournalSchemaV1, Mode: transport.DNSEngineSwitchModeSwitch,
		MutationRequestID: ownerGuidanceRequest, SourceEngine: "", TargetEngine: transport.DNSEngineBIND,
		TargetEpoch: 1, Topology: transport.DNSTopologyStandalone,
	}
	e := dnsenginerecovery.SwitchEvidence{Journal: j, Observation: dnsenginerecovery.EvidenceObservation{
		RequestID: ownerGuidanceRequest, InverseKind: dnsenginerecovery.NativeInverseBINDSwitch,
	}}
	e = withReconstructedManifest(t, rolledBackTerminal(e))
	requireRolledBackText(t, terminalRolledBackDNSSwitchGuidance(e, true),
		[]string{"re-runs and re-proves the original rollback", "only if that proof passes", "contact support with request id " + ownerGuidanceRequest},
		[]string{"/usr/libexec/celikpanel/recovery recover-"},
	)

	reconfigure := dnsengineartifact.SwitchJournalV1{
		Schema: dnsengineartifact.SwitchJournalSchemaV1, Mode: transport.DNSEngineSwitchModeSwitch,
		MutationRequestID: ownerGuidanceRequest, TargetEngine: transport.DNSEnginePowerDNS,
		TargetEpoch: 1, Topology: transport.DNSTopologyPaired, PairRole: transport.DNSPairRoleSecondary,
		LocalIP: "192.0.2.20", LocalNS: "ns2.example.test", PeerIP: "192.0.2.10", PeerNS: "ns1.example.test",
		TargetUnitsBefore: []dnsengineartifact.UnitSnapshot{{Name: "pdns.service", ActiveState: "active"}},
	}
	e = dnsenginerecovery.SwitchEvidence{Journal: reconfigure, Observation: dnsenginerecovery.EvidenceObservation{
		RequestID: ownerGuidanceRequest, InverseKind: dnsenginerecovery.NativeInversePDNSSwitch,
	}}
	e = withReconstructedManifest(t, rolledBackTerminal(e))
	if finishes, _ := agentFinishesRolledBackDNSJournal(e); finishes {
		t.Fatal("paired-secondary reconfiguration was promised an Agent finish at rolled-back")
	}
	requireRolledBackText(t, terminalRolledBackDNSSwitchGuidance(e, true),
		[]string{"no owner recovery command accepts this journal's recorded shape"},
		[]string{"systemctl restart celikpanel-agent"},
	)
	// The fresh install sharing that manifest (inactive target preimage) is
	// re-run by the Agent.
	e.Journal.TargetUnitsBefore = []dnsengineartifact.UnitSnapshot{{Name: "pdns.service", ActiveState: "inactive"}}
	if finishes, _ := agentFinishesRolledBackDNSJournal(e); !finishes {
		t.Fatal("fresh paired-secondary install was refused an Agent finish")
	}
	// A journal whose manifest does not reconstruct is refused by the Agent too.
	e.Journal.ManifestQualifier = "dns-engine-switch/v1:sha256:" + strings.Repeat("0", 64)
	if finishes, _ := agentFinishesRolledBackDNSJournal(e); finishes {
		t.Fatal("unreconstructable journal was promised an Agent finish")
	}
}

// A V3 pre-start journal at rolled-back is re-proved by the Agent's own
// pre-start inverse without any effect, and the owner command is also named.
// V4 journals: the Agent never runs their inverse; a V4 command is named only
// under --quiesced, as the existing guidance does.
func TestRolledBackStatusV3AgentReprovesAndV4NeverPromisesAgentRestart(t *testing.T) {
	v3 := rolledBackTerminal(ownerGuidanceFreshPrestartEvidence())
	v3.AcceptedJob = ownerRecoveryVerdictTestJob()
	if finishes, reproveOnly := agentFinishesRolledBackDNSJournal(v3); !finishes || !reproveOnly {
		t.Fatalf("V3 pre-start rolled-back journal: finishes=%v reproveOnly=%v", finishes, reproveOnly)
	}
	requireRolledBackText(t, terminalRolledBackDNSSwitchGuidance(v3, false),
		[]string{"systemctl restart celikpanel-agent", "without changing anything, that PowerDNS is stopped",
			"recover-dns-pdns-fresh-prestart --request-id " + ownerGuidanceRequest + ";"},
		[]string{"does not run this journal's rollback itself", "restored PowerDNS source"},
	)
	started := v3
	started.Journal.PDNSFreshPlan = &dnsengineartifact.PDNSFreshPrimaryPlanV3{
		Candidate: &dnsengineartifact.PDNSTargetCandidateProofV4{}, Native: &pdnsnative.RecordedTransition{},
	}
	if finishes, _ := agentFinishesRolledBackDNSJournal(started); finishes {
		t.Fatal("a V3 journal with a native receipt was promised a pre-start re-proof")
	}
	v4 := dnsenginerecovery.SwitchEvidence{
		Journal: dnsengineartifact.SwitchJournalV1{
			Schema: dnsengineartifact.SwitchJournalSchemaV4, MutationRequestID: ownerGuidanceRequest,
			PDNSTargetPlan: &dnsengineartifact.PDNSTargetInversePlanV4{Candidate: &dnsengineartifact.PDNSTargetCandidateProofV4{}},
		},
		Observation: dnsenginerecovery.EvidenceObservation{RequestID: ownerGuidanceRequest, InverseKind: dnsenginerecovery.NativeInversePDNSSwitch},
	}
	v4 = rolledBackTerminal(v4)
	requireRolledBackText(t, terminalRolledBackDNSSwitchGuidance(v4, false),
		[]string{"rerun this check with --quiesced --request-id " + ownerGuidanceRequest},
		[]string{"systemctl restart celikpanel-agent", "/usr/libexec/celikpanel/recovery recover-"},
	)
	requireRolledBackText(t, terminalRolledBackDNSSwitchGuidance(v4, true),
		[]string{"/usr/libexec/celikpanel/recovery recover-dns-pdns-target-staged --request-id " + ownerGuidanceRequest + ";"},
		[]string{"systemctl restart celikpanel-agent"},
	)
}

func TestAgentRetriesReleasedDNSJournalByClass(t *testing.T) {
	for _, tc := range []struct {
		schema, phase string
		want          bool
	}{
		{dnsengineartifact.SwitchJournalSchemaV1, dnsengineartifact.SwitchPhaseRollingBack, true},
		{dnsengineartifact.SwitchJournalSchemaV1, dnsengineartifact.SwitchPhaseTargetStaged, true},
		{dnsengineartifact.SwitchJournalSchemaV2, dnsengineartifact.SwitchPhaseTargetStaged, true},
		{dnsengineartifact.SwitchJournalSchemaV2, dnsengineartifact.SwitchPhaseRollingBack, false},
		{dnsengineartifact.SwitchJournalSchemaV3, dnsengineartifact.SwitchPhaseTargetStaged, true},
		{dnsengineartifact.SwitchJournalSchemaV3, dnsengineartifact.SwitchPhaseTargetEnableIntent, true},
		{dnsengineartifact.SwitchJournalSchemaV3, dnsengineartifact.SwitchPhaseRollingBack, true},
		{dnsengineartifact.SwitchJournalSchemaV3, dnsengineartifact.SwitchPhaseCommitted, true},
		{dnsengineartifact.SwitchJournalSchemaV4, dnsengineartifact.SwitchPhaseTargetEnableIntent, false},
		{dnsengineartifact.SwitchJournalSchemaV4, dnsengineartifact.SwitchPhaseRollingBack, false},
		{dnsengineartifact.SwitchJournalSchemaV4, dnsengineartifact.SwitchPhaseIntent, true},
		{"unknown", dnsengineartifact.SwitchPhaseIntent, false},
	} {
		got := agentRetriesReleasedDNSJournal(dnsengineartifact.SwitchJournalV1{Schema: tc.schema, Phase: tc.phase})
		if got != tc.want {
			t.Fatalf("%s/%s: retries=%v, want %v", tc.schema, tc.phase, got, tc.want)
		}
	}
}
