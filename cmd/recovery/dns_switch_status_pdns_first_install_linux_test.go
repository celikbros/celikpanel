//go:build linux

package main

import (
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/dnsenginerecovery"
	"github.com/alicelik/celikpanel/internal/transport"
)

func v1PDNSFirstInstallEvidence(t *testing.T, phase string, pairedSecondary bool) dnsenginerecovery.SwitchEvidence {
	t.Helper()
	j := dnsengineartifact.SwitchJournalV1{
		Schema: dnsengineartifact.SwitchJournalSchemaV1, Phase: phase,
		Mode:              transport.DNSEngineSwitchModeSwitch,
		MutationRequestID: ownerGuidanceRequest, TargetEngine: transport.DNSEnginePowerDNS,
		TargetEpoch: 1, SourceRevision: 3, Topology: transport.DNSTopologyStandalone,
		TargetUnitsBefore: []dnsengineartifact.UnitSnapshot{
			{Name: "pdns.service", LoadState: "not-found", ActiveState: "inactive"},
		},
	}
	if pairedSecondary {
		j.Topology, j.PairRole = transport.DNSTopologyPaired, transport.DNSPairRoleSecondary
		j.LocalIP, j.LocalNS = "192.0.2.20", "ns2.example.test"
		j.PeerIP, j.PeerNS = "192.0.2.10", "ns1.example.test"
	}
	e := dnsenginerecovery.SwitchEvidence{Journal: j, Observation: dnsenginerecovery.EvidenceObservation{
		EvidenceSHA256: "secured-evidence-fingerprint", Status: dnsenginerecovery.EvidenceActive,
		RequestID: j.MutationRequestID, Phase: j.Phase,
		TargetEngine: string(j.TargetEngine), TargetEpoch: j.TargetEpoch,
		InverseKind: dnsenginerecovery.NativeInversePDNSSwitch,
	}}
	return withReconstructedManifest(t, e)
}

func genericNoOwnerCommandGuidance() string {
	return "No owner recovery command applies to this journal's recorded shape and ledger status. Keep the journal and ledger. If this operation does not resume through CelikPanel or an Agent restart, contact support with request id " + ownerGuidanceRequest + ". This status check does not start recovery.\n"
}

// Batch 7 c08-c10 and batch 6a c03/c04: before recovery, status said "no
// owner recovery command applies ... contact support" for a V1 PowerDNS first
// install, although the restarted Agent rolled it back (or completed it) by
// itself and the same request then ran forward. The text now says who acts,
// what the Agent does, and how the request resumes.
func TestV1PDNSFirstInstallNamesTheAgentNotSupport(t *testing.T) {
	for _, paired := range []bool{false, true} {
		operation := "This PowerDNS installation"
		if paired {
			operation = "This PowerDNS secondary installation"
		}
		for _, tc := range []struct {
			phase               string
			rollsBack, succeeds bool
		}{
			{dnsengineartifact.SwitchPhaseIntent, true, false},
			{dnsengineartifact.SwitchPhaseTargetStaged, true, false},
			{dnsengineartifact.SwitchPhaseSourceStopped, true, false},
			{dnsengineartifact.SwitchPhaseTargetStarted, true, true},
			{dnsengineartifact.SwitchPhaseTargetVerified, false, true},
			{dnsengineartifact.SwitchPhaseCommitted, false, true},
		} {
			name := "standalone/" + tc.phase
			if paired {
				name = "paired-secondary/" + tc.phase
			}
			t.Run(name, func(t *testing.T) {
				e := v1PDNSFirstInstallEvidence(t, tc.phase, paired)
				if ownerDNSRecoveryCommand(e) != "" {
					t.Fatal("an owner command was admitted for an Agent-recovered V1 PowerDNS first install")
				}
				text := ownerDNSRecoveryGuidance(e, true)
				for _, want := range []string{
					operation + " ", "journal phase " + tc.phase,
					"No owner recovery command applies, and none is needed: the CelikPanel Agent",
					"systemctl restart celikpanel-agent",
					"--quiesced --request-id " + ownerGuidanceRequest,
					"This status check changed nothing and does not start recovery.",
				} {
					if !strings.Contains(text, want) {
						t.Fatalf("guidance lacks %q:\n%s", want, text)
					}
				}
				for _, unwanted := range []string{"contact support", "/usr/libexec/celikpanel/recovery recover-", "secondary installation"} {
					if unwanted == "secondary installation" && paired {
						continue
					}
					if strings.Contains(text, unwanted) {
						t.Fatalf("guidance contains %q:\n%s", unwanted, text)
					}
				}
				if got := strings.Contains(text, "dns_engine_switch_rolled_back_after_restart") &&
					strings.Contains(text, "the same change can be started again"); got != tc.rollsBack {
					t.Fatalf("rollback and retry named=%v, want %v:\n%s", got, tc.rollsBack, text)
				}
				if got := strings.Contains(text, "records the request as succeeded"); got != tc.succeeds {
					t.Fatalf("completion named=%v, want %v:\n%s", got, tc.succeeds, text)
				}
				if !tc.rollsBack && !strings.Contains(text, "never rolled back") {
					t.Fatalf("verified guidance does not say the target is not rolled back:\n%s", text)
				}
			})
		}
	}
}

// The exact wording of the most common class (batch 7 c08: standalone,
// intent), byte for byte.
func TestV1PDNSFirstInstallBeforeStartGuidanceText(t *testing.T) {
	e := v1PDNSFirstInstallEvidence(t, dnsengineartifact.SwitchPhaseIntent, false)
	want := "This PowerDNS installation has not recorded a PowerDNS start (journal phase intent), and no rollback decision is recorded. " +
		"No owner recovery command applies, and none is needed: the CelikPanel Agent resolves this same request. " +
		"While its worker is running it continues by itself. If CelikPanel shows no progress, the server owner restarts the Agent (systemctl restart celikpanel-agent); " +
		"at start it re-checks this request and rolls the install back: it stops PowerDNS, returns its configuration, database and unit state to what was recorded before the operation, proves that no managed DNS service answers on port 53, and records the request as failed (dns_engine_switch_rolled_back_after_restart). " +
		"CelikPanel then reports the change as not committed, and the same change can be started again. " +
		"If the rollback cannot be proved, the journal is kept, and this check, rerun with --quiesced --request-id " + ownerGuidanceRequest + ", names the next step. " +
		"This status check changed nothing and does not start recovery.\n"
	if got := ownerDNSRecoveryGuidance(e, true); got != want {
		t.Fatalf("guidance changed:\n got: %q\nwant: %q", got, want)
	}
}

// Journals the Agent does not resolve through this class keep their
// existing text: the paired-secondary reconfiguration of an active PowerDNS
// (its rollback proof has its own rules), a prior engine state receipt, a
// decided phase, a released lease and a terminal verdict.
func TestV1PDNSFirstInstallGuidanceLeavesOtherClassesUnchanged(t *testing.T) {
	reconfigure := v1PDNSFirstInstallEvidence(t, dnsengineartifact.SwitchPhaseIntent, true)
	reconfigure.Journal.TargetUnitsBefore = []dnsengineartifact.UnitSnapshot{
		{Name: "pdns.service", LoadState: "loaded", ActiveState: "active", UnitFileState: "enabled"},
	}
	priorState := v1PDNSFirstInstallEvidence(t, dnsengineartifact.SwitchPhaseTargetStaged, false)
	priorState.Journal.StateBefore = dnsengineartifact.FileSnapshot{Exists: true}
	cases := map[string]dnsenginerecovery.SwitchEvidence{
		"reconfiguration": reconfigure,
		"prior-state":     priorState,
	}
	for _, phase := range []string{dnsengineartifact.SwitchPhaseRollingBack, dnsengineartifact.SwitchPhaseRolledBack} {
		cases["decided/"+phase] = v1PDNSFirstInstallEvidence(t, phase, false)
	}
	for _, status := range []dnsenginerecovery.EvidenceStatus{
		dnsenginerecovery.EvidenceReleasedUndecided, dnsenginerecovery.EvidenceTerminalRolledBack,
		dnsenginerecovery.EvidenceFinalized,
	} {
		e := v1PDNSFirstInstallEvidence(t, dnsengineartifact.SwitchPhaseTargetStaged, false)
		e.Observation.Status = status
		cases["status/"+string(status)] = e
	}
	for name, e := range cases {
		t.Run(name, func(t *testing.T) {
			if ownerDNSRecoveryCommand(e) != "" {
				t.Fatal("fixture admits an owner command")
			}
			if got := ownerDNSRecoveryGuidance(e, true); got != genericNoOwnerCommandGuidance() {
				t.Fatalf("existing text changed:\n%s", got)
			}
		})
	}
	// Every active status of the accepted lease gets the Agent text.
	for _, status := range []dnsenginerecovery.EvidenceStatus{
		dnsenginerecovery.EvidenceLeaseExpired, dnsenginerecovery.EvidenceWorkerRecorded,
		dnsenginerecovery.EvidenceOrphanedWorker, dnsenginerecovery.EvidenceExpiredCancellation,
	} {
		e := v1PDNSFirstInstallEvidence(t, dnsengineartifact.SwitchPhaseTargetStaged, false)
		e.Observation.Status = status
		if !strings.Contains(ownerDNSRecoveryGuidance(e, true), "none is needed") {
			t.Fatalf("%s: active lease did not get the Agent-recovered text", status)
		}
	}
	// A request id that is not the journal's never gets it.
	e := v1PDNSFirstInstallEvidence(t, dnsengineartifact.SwitchPhaseIntent, false)
	e.Observation.RequestID = strings.Repeat("e", len(ownerGuidanceRequest))
	if strings.Contains(ownerDNSRecoveryGuidance(e, true), "none is needed") {
		t.Fatal("a foreign request id got the Agent-recovered text")
	}
}
