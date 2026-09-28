//go:build linux

package dnsenginerecovery

import (
	"testing"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/transport"
)

func inactivePDNSSwitchEvidence() SwitchEvidence {
	j := dnsengineartifact.SwitchJournalV1{
		Phase:             dnsengineartifact.SwitchPhaseRollingBack,
		Mode:              transport.DNSEngineSwitchModeSwitch,
		MutationRequestID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		SourceEngine:      transport.DNSEngineBIND,
		TargetEngine:      transport.DNSEnginePowerDNS,
		Topology:          transport.DNSTopologyStandalone,
		TargetEpoch:       2,
		StateBefore:       dnsengineartifact.FileSnapshot{Exists: true},
		TargetUnitsBefore: []dnsengineartifact.UnitSnapshot{{Name: "pdns.service", ActiveState: "inactive"}},
		SourceUnitsBefore: []dnsengineartifact.UnitSnapshot{
			{Name: "bind9.service", ActiveState: "active"},
			{Name: "named.service", ActiveState: "active"},
		},
	}
	return SwitchEvidence{
		Journal: j,
		Observation: EvidenceObservation{
			EvidenceSHA256:  "secured-evidence-fingerprint",
			Status:          EvidenceActive,
			RequestID:       j.MutationRequestID,
			Phase:           j.Phase,
			SourceEngine:    string(j.SourceEngine),
			TargetEngine:    string(j.TargetEngine),
			TargetEpoch:     j.TargetEpoch,
			InverseKind:     NativeInversePDNSSwitch,
			SourceOwnership: SourceOwnershipExact,
			SourceReceipt:   SourceReceiptDifferent,
			TargetReceipt:   TargetReceiptExact,
		},
	}
}

func TestRecognizeInactivePDNSSwitchRollbackEvidenceExactCheckpoints(t *testing.T) {
	e := inactivePDNSSwitchEvidence()
	if err := RecognizeInactivePDNSSwitchRollbackEvidence(e); err != nil {
		t.Fatal(err)
	}
	e.Observation.TargetReceipt = TargetReceiptDifferent
	e.Observation.SourceReceipt = SourceReceiptExact
	if err := RecognizeInactivePDNSSwitchRollbackEvidence(e); err != nil {
		t.Fatal(err)
	}
	e.Journal.Phase = dnsengineartifact.SwitchPhaseRolledBack
	e.Observation.Phase = e.Journal.Phase
	e.Observation.Status = EvidenceTerminalRolledBack
	if err := RecognizeInactivePDNSSwitchRollbackEvidence(e); err != nil {
		t.Fatal(err)
	}
}

func TestRecognizeInactivePDNSSwitchRollbackEvidenceRejectsWrongRecord(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*SwitchEvidence)
	}{
		{"owner-edit", func(e *SwitchEvidence) { e.Observation.SourceOwnership = SourceOwnershipDifferent }},
		{"unsecured", func(e *SwitchEvidence) { e.Observation.EvidenceSHA256 = "" }},
		{"foreign-request", func(e *SwitchEvidence) { e.Observation.RequestID = "dddddddddddddddddddddddddddddddd" }},
		{"wrong-epoch", func(e *SwitchEvidence) { e.Observation.TargetEpoch++ }},
		{"already-running-target", func(e *SwitchEvidence) { e.Journal.TargetUnitsBefore[0].ActiveState = "active" }},
		{"missing-source-alias", func(e *SwitchEvidence) { e.Journal.SourceUnitsBefore[1].Name = "bind9.service" }},
		{"stopped-source", func(e *SwitchEvidence) {
			e.Journal.SourceUnitsBefore[0].ActiveState = "inactive"
			e.Journal.SourceUnitsBefore[1].ActiveState = "inactive"
		}},
		{"different-state", func(e *SwitchEvidence) { e.Observation.TargetReceipt = TargetReceiptDifferent }},
		{"wrong-phase", func(e *SwitchEvidence) { e.Journal.Phase = dnsengineartifact.SwitchPhaseTargetStarted }},
		{"finalized-job", func(e *SwitchEvidence) { e.Observation.Status = EvidenceFinalized }},
		{"paired", func(e *SwitchEvidence) { e.Journal.Topology = transport.DNSTopologyPaired }},
		{"adoption", func(e *SwitchEvidence) { e.Journal.Mode = transport.DNSEngineSwitchModeAdopt }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := inactivePDNSSwitchEvidence()
			tc.edit(&e)
			if err := RecognizeInactivePDNSSwitchRollbackEvidence(e); err == nil {
				t.Fatal("unsupported PowerDNS switch rollback record recognized")
			}
		})
	}
}
