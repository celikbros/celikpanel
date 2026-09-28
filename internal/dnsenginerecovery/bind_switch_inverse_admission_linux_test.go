//go:build linux

package dnsenginerecovery

import (
	"testing"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/transport"
)

func inactiveBINDSwitchEvidence() SwitchEvidence {
	j := dnsengineartifact.SwitchJournalV1{
		Schema:            dnsengineartifact.SwitchJournalSchemaV2,
		InversePlan:       &dnsengineartifact.BINDSwitchInversePlanV2{Kind: dnsengineartifact.BINDSwitchInversePlanKindV2, SourcePDNS: &dnsengineartifact.PDNSSourceProofV2{Kind: dnsengineartifact.PDNSSourceProofKindV1}, BINDUnchangedConfig: []dnsengineartifact.FileSnapshot{{Path: "/etc/bind/named.conf", Exists: true}, {Path: "/etc/bind/named.conf.default-zones", Exists: true}}},
		Phase:             dnsengineartifact.SwitchPhaseRollingBack,
		Mode:              transport.DNSEngineSwitchModeSwitch,
		MutationRequestID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		SourceEngine:      transport.DNSEnginePowerDNS,
		TargetEngine:      transport.DNSEngineBIND,
		Topology:          transport.DNSTopologyStandalone,
		TargetGeneration:  "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		TargetEpoch:       2,
		StateBefore:       dnsengineartifact.FileSnapshot{Exists: true},
		SourceUnitsBefore: []dnsengineartifact.UnitSnapshot{{Name: "pdns.service", ActiveState: "active"}},
		TargetUnitsBefore: []dnsengineartifact.UnitSnapshot{
			{Name: "named.service", ActiveState: "inactive"},
			{Name: "bind9.service", ActiveState: "inactive"},
		},
	}
	return SwitchEvidence{
		Journal: j,
		Observation: EvidenceObservation{
			EvidenceSHA256:   "secured-evidence-fingerprint",
			Status:           EvidenceActive,
			RequestID:        j.MutationRequestID,
			Phase:            j.Phase,
			SourceEngine:     string(j.SourceEngine),
			TargetEngine:     string(j.TargetEngine),
			TargetGeneration: j.TargetGeneration,
			TargetEpoch:      j.TargetEpoch,
			InverseKind:      NativeInverseBINDSwitch,
			SourceOwnership:  SourceOwnershipExact,
			SourceReceipt:    SourceReceiptDifferent,
			TargetReceipt:    TargetReceiptExact,
		},
	}
}

func TestInactiveBINDSwitchInverseEvidenceAdmitsExactActiveAndTerminal(t *testing.T) {
	e := inactiveBINDSwitchEvidence()
	if err := ValidateInactiveBINDSwitchInverseEvidence(e); err != nil {
		t.Fatal(err)
	}
	e.Journal.Phase = dnsengineartifact.SwitchPhaseRolledBack
	e.Observation.Phase = e.Journal.Phase
	e.Observation.Status = EvidenceTerminalRolledBack
	e.Observation.SourceReceipt = SourceReceiptExact
	e.Observation.TargetReceipt = TargetReceiptDifferent
	if err := ValidateInactiveBINDSwitchInverseEvidence(e); err != nil {
		t.Fatal(err)
	}
}

func TestInactiveBINDSwitchInverseEvidenceRejectsWrongAuthorityAndPreimage(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*SwitchEvidence)
	}{
		{"legacy-v1-journal", func(e *SwitchEvidence) {
			e.Journal.Schema = dnsengineartifact.SwitchJournalSchemaV1
			e.Journal.InversePlan = nil
		}},
		{"missing-v2-plan", func(e *SwitchEvidence) { e.Journal.InversePlan = nil }},
		{"missing-unchanged-envelope", func(e *SwitchEvidence) { e.Journal.InversePlan.BINDUnchangedConfig = nil }},
		{"partial-unchanged-envelope", func(e *SwitchEvidence) {
			e.Journal.InversePlan.BINDUnchangedConfig = e.Journal.InversePlan.BINDUnchangedConfig[:1]
		}},
		{"owner-edit", func(e *SwitchEvidence) { e.Observation.SourceOwnership = SourceOwnershipDifferent }},
		{"unknown-evidence", func(e *SwitchEvidence) { e.Observation.EvidenceSHA256 = "" }},
		{"foreign-request", func(e *SwitchEvidence) { e.Observation.RequestID = "dddddddddddddddddddddddddddddddd" }},
		{"foreign-generation", func(e *SwitchEvidence) { e.Observation.TargetGeneration = "other" }},
		{"running-owner-bind", func(e *SwitchEvidence) { e.Journal.TargetUnitsBefore[0].ActiveState = "active" }},
		{"missing-alias", func(e *SwitchEvidence) { e.Journal.TargetUnitsBefore[1].Name = "named.service" }},
		{"wrong-source", func(e *SwitchEvidence) { e.Journal.SourceEngine = "" }},
		{"different-state", func(e *SwitchEvidence) { e.Observation.TargetReceipt = TargetReceiptDifferent }},
		{"wrong-phase", func(e *SwitchEvidence) { e.Journal.Phase = dnsengineartifact.SwitchPhaseTargetStarted }},
		{"finalized-job", func(e *SwitchEvidence) { e.Observation.Status = EvidenceFinalized }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := inactiveBINDSwitchEvidence()
			tc.edit(&e)
			if err := ValidateInactiveBINDSwitchInverseEvidence(e); err == nil {
				t.Fatal("unsupported BIND inverse evidence admitted")
			}
		})
	}
}
