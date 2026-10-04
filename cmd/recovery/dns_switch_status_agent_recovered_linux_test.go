//go:build linux

package main

import (
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/dnsenginerecovery"
	"github.com/alicelik/celikpanel/internal/transport"
)

func v1BINDAgentRecoveredEvidence(t *testing.T, mode string, source transport.DNSEngine, epoch int64, phase string) dnsenginerecovery.SwitchEvidence {
	t.Helper()
	sourceEpoch := int64(0)
	if source != "" {
		sourceEpoch = epoch
	}
	j := dnsengineartifact.SwitchJournalV1{
		Schema: dnsengineartifact.SwitchJournalSchemaV1, Phase: phase, Mode: mode,
		MutationRequestID: ownerGuidanceRequest, SourceEngine: source, TargetEngine: transport.DNSEngineBIND,
		SourceEpoch: sourceEpoch, TargetEpoch: epoch, SourceRevision: 3,
		Topology: transport.DNSTopologyStandalone,
		// The owner-installed, disabled named.service of the takeover cells;
		// bind9.service is the APT alias, absent while disabled.
		TargetUnitsBefore: []dnsengineartifact.UnitSnapshot{
			{Name: "bind9.service", LoadState: "not-found", ActiveState: "inactive"},
			{Name: "named.service", LoadState: "loaded", ActiveState: "inactive", UnitFileState: "disabled"},
		},
	}
	e := dnsenginerecovery.SwitchEvidence{Journal: j, Observation: dnsenginerecovery.EvidenceObservation{
		EvidenceSHA256: "secured-evidence-fingerprint", Status: dnsenginerecovery.EvidenceActive,
		RequestID: j.MutationRequestID, Phase: j.Phase, SourceEngine: string(j.SourceEngine),
		TargetEngine: string(j.TargetEngine), TargetEpoch: j.TargetEpoch,
		InverseKind: dnsenginerecovery.NativeInverseBINDSwitch,
	}}
	return withReconstructedManifest(t, e)
}

// Batch 6a c10 / 6b c7: before a stopped-BIND takeover (and any V1 BIND
// install or reinstall) reached its target, status said "no owner recovery
// command applies ... contact support". The CelikPanel Agent resolves these
// journals itself; the text now says who acts and how the request resumes.
func TestV1BINDJournalBeforeTargetNamesTheAgentNotSupport(t *testing.T) {
	for _, tc := range []struct {
		name      string
		mode      string
		source    transport.DNSEngine
		epoch     int64
		operation string
	}{
		{"first-install", transport.DNSEngineSwitchModeSwitch, "", 1, "This BIND installation"},
		{"reinstall", transport.DNSEngineSwitchModeReinstall, transport.DNSEngineBIND, 1, "This BIND reinstall"},
	} {
		for _, phase := range []string{
			dnsengineartifact.SwitchPhaseIntent, dnsengineartifact.SwitchPhaseTargetStaged,
			dnsengineartifact.SwitchPhaseSourceStopped, dnsengineartifact.SwitchPhaseTargetStarted,
		} {
			t.Run(tc.name+"/"+phase, func(t *testing.T) {
				e := v1BINDAgentRecoveredEvidence(t, tc.mode, tc.source, tc.epoch, phase)
				if ownerDNSRecoveryCommand(e) != "" {
					t.Fatal("an owner command was admitted for an Agent-recovered V1 journal")
				}
				text := ownerDNSRecoveryGuidance(e, true)
				for _, want := range []string{
					tc.operation, "journal phase " + phase,
					"No owner recovery command applies, and none is needed: the CelikPanel Agent resolves this same request",
					"systemctl restart celikpanel-agent", "dns_engine_switch_rolled_back_after_restart",
					"--quiesced --request-id " + ownerGuidanceRequest, "does not start recovery",
				} {
					if !strings.Contains(text, want) {
						t.Fatalf("guidance lacks %q:\n%s", want, text)
					}
				}
				for _, unwanted := range []string{"contact support", "/usr/libexec/celikpanel/recovery recover-"} {
					if strings.Contains(text, unwanted) {
						t.Fatalf("guidance contains %q:\n%s", unwanted, text)
					}
				}
				before := phase != dnsengineartifact.SwitchPhaseTargetStarted
				if dnsenginerecovery.BINDV1BeforeActivationJournal(e.Journal) != before {
					t.Fatalf("typed never-started observation selected=%v at %s", !before, phase)
				}
			})
		}
	}
	// A decided or finished journal, a released lease and a V2 journal keep
	// their own texts.
	e := v1BINDAgentRecoveredEvidence(t, transport.DNSEngineSwitchModeSwitch, "", 1, dnsengineartifact.SwitchPhaseRollingBack)
	if dnsenginerecovery.BINDV1AgentRecoveredJournal(e.Journal) ||
		strings.Contains(ownerDNSRecoveryGuidance(e, true), "none is needed") {
		t.Fatal("a V1 journal with a rollback decision was described as undecided")
	}
	e = v1BINDAgentRecoveredEvidence(t, transport.DNSEngineSwitchModeSwitch, "", 1, dnsengineartifact.SwitchPhaseTargetStaged)
	e.Observation.Status = dnsenginerecovery.EvidenceReleasedUndecided
	if strings.Contains(ownerDNSRecoveryGuidance(e, true), "none is needed") {
		t.Fatal("a released V1 journal was described as live")
	}
	v2 := e.Journal
	v2.Schema = dnsengineartifact.SwitchJournalSchemaV2
	if dnsenginerecovery.BINDV1AgentRecoveredJournal(v2) {
		t.Fatal("a V2 journal was classified as Agent-recovered V1")
	}
}

// Batch 6a c08: the status for a V1 PowerDNS adoption named only the owner
// command, although the restarted Agent then finished the journal by itself.
func TestV1PDNSAdoptionStatusNamesAgentFirstAndCommandAsAlternative(t *testing.T) {
	for _, phase := range []string{dnsengineartifact.SwitchPhaseRollingBack, dnsengineartifact.SwitchPhaseRolledBack} {
		t.Run(phase, func(t *testing.T) {
			e := ownerGuidancePDNSAdoptionEvidence()
			e.Journal.Topology = transport.DNSTopologyStandalone
			e.Journal.Phase, e.Observation.Phase = phase, phase
			if phase == dnsengineartifact.SwitchPhaseRolledBack {
				e.Observation.SourceReceipt, e.Observation.TargetReceipt = dnsenginerecovery.SourceReceiptMutualAbsence, dnsenginerecovery.TargetReceiptAbsent
			}
			e = withReconstructedManifest(t, e)
			requireOwnerGuidance(t, e, ownerPDNSAdoptionInverseCommand)
			text := ownerDNSRecoveryGuidance(e, true)
			agent := strings.Index(text, "The CelikPanel Agent")
			command := strings.Index(text, "As an alternative the server owner can run /usr/libexec/celikpanel/recovery recover-dns-pdns-adoption")
			if agent < 0 || command < 0 || agent > command ||
				!strings.Contains(text, "systemctl restart celikpanel-agent") {
				t.Fatalf("V1 adoption guidance does not name the Agent first:\n%s", text)
			}
			if phase == dnsengineartifact.SwitchPhaseRolledBack &&
				!strings.Contains(text, "without changing anything") {
				t.Fatalf("rolled-back guidance does not say the Agent only re-proves:\n%s", text)
			}
		})
	}
	// V2 adoption journals are owner-only; their text is unchanged.
	e := ownerGuidancePDNSAdoptionEvidence()
	e.Journal.Schema = dnsengineartifact.SwitchJournalSchemaV2
	if agentFinishedPDNSAdoptionGuidance(e) != "" {
		t.Fatal("a V2 adoption promised an Agent finish")
	}
}
