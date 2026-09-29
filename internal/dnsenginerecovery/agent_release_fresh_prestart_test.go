package dnsenginerecovery

import (
	"strings"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/pdnsnative"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
	"github.com/alicelik/celikpanel/internal/transport"
)

// Component tests of the one released-undecided admission of
// recover-dns-pdns-fresh-prestart. They are not native DNS evidence.
func freshPrestartReleaseEvidence(phase string, code string) SwitchEvidence {
	j := dnsengineartifact.SwitchJournalV1{
		Schema: dnsengineartifact.SwitchJournalSchemaV3, Phase: phase,
		Mode:              transport.DNSEngineSwitchModeSwitch,
		MutationRequestID: strings.Repeat("a", 32), MutationOwnerID: strings.Repeat("b", 32),
		ManifestQualifier: "dns-engine-switch/v1:sha256:" + strings.Repeat("c", 64),
		TargetEngine:      transport.DNSEnginePowerDNS,
		PDNSFreshPlan:     &dnsengineartifact.PDNSFreshPrimaryPlanV3{Candidate: &dnsengineartifact.PDNSTargetCandidateProofV4{}},
	}
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	return SwitchEvidence{
		Journal: j,
		Observation: EvidenceObservation{
			Status: EvidenceReleasedUndecided, ReleaseReason: code,
			RequestID: j.MutationRequestID, Phase: j.Phase, TargetEngine: string(j.TargetEngine),
		},
		AcceptedJob: transport.ServiceMutationJob{
			RequestID: j.MutationRequestID, OwnerID: j.MutationOwnerID,
			Kind: "dns_engine_switch", Target: string(j.TargetEngine), PackageName: j.ManifestQualifier,
			Status: servicemutationledger.StatusFailed, Phase: "interrupted", Attempt: 1,
			StartedAt: now.Add(-time.Hour), UpdatedAt: now, FinishedAt: now, DeadlineAt: now.Add(time.Hour),
			ErrorCode:    code,
			ErrorMessage: "The interrupted DNS switch could not be verified after the Agent restarted.",
		},
	}
}

func TestAgentReleasedFreshPrimaryPrestartEvidenceV3(t *testing.T) {
	for _, phase := range []string{
		dnsengineartifact.SwitchPhaseTargetStaged, dnsengineartifact.SwitchPhaseTargetEnableIntent,
		dnsengineartifact.SwitchPhaseRollingBack, dnsengineartifact.SwitchPhaseRollingBackTargetEnable,
	} {
		if !AgentReleasedFreshPrimaryPrestartEvidenceV3(freshPrestartReleaseEvidence(phase, dnsengineartifact.ReleasedNativeUnknownCode)) {
			t.Fatalf("deliberate release at %s refused", phase)
		}
	}
	intent := freshPrestartReleaseEvidence(dnsengineartifact.SwitchPhaseIntent, dnsengineartifact.ReleasedNativeUnknownCode)
	intent.Journal.PDNSFreshPlan.Candidate = nil
	if !AgentReleasedFreshPrimaryPrestartEvidenceV3(intent) {
		t.Fatal("deliberate release of an unsealed intent refused")
	}
	for name, edit := range map[string]func(*SwitchEvidence){
		"other reason": func(e *SwitchEvidence) {
			e.Observation.ReleaseReason = dnsengineartifact.ReleasedHostWindowCode
			e.AcceptedJob.ErrorCode = dnsengineartifact.ReleasedHostWindowCode
		},
		"reason disagrees with job": func(e *SwitchEvidence) { e.AcceptedJob.ErrorCode = dnsengineartifact.ReleasedHostWindowCode },
		"active status":             func(e *SwitchEvidence) { e.Observation.Status = EvidenceActive },
		"foreign observation":       func(e *SwitchEvidence) { e.Observation.RequestID = strings.Repeat("d", 32) },
		"foreign owner":             func(e *SwitchEvidence) { e.AcceptedJob.OwnerID = strings.Repeat("e", 32) },
		"worker recorded": func(e *SwitchEvidence) {
			e.AcceptedJob.WorkerPID, e.AcceptedJob.WorkerStarted, e.AcceptedJob.WorkerCommand = 42, "7", "agent"
		},
		"lease kept": func(e *SwitchEvidence) {
			e.AcceptedJob.LeaseExpiresAt = e.AcceptedJob.FinishedAt.Add(time.Minute)
		},
		"started phase": func(e *SwitchEvidence) {
			e.Journal.Phase, e.Observation.Phase = dnsengineartifact.SwitchPhaseTargetStarted, dnsengineartifact.SwitchPhaseTargetStarted
		},
		"native receipt": func(e *SwitchEvidence) {
			e.Journal.PDNSFreshPlan.Native = &pdnsnative.RecordedTransition{}
		},
		"not V3": func(e *SwitchEvidence) { e.Journal.Schema = dnsengineartifact.SwitchJournalSchemaV1 },
	} {
		e := freshPrestartReleaseEvidence(dnsengineartifact.SwitchPhaseTargetStaged, dnsengineartifact.ReleasedNativeUnknownCode)
		plan := *e.Journal.PDNSFreshPlan
		e.Journal.PDNSFreshPlan = &plan
		edit(&e)
		if AgentReleasedFreshPrimaryPrestartEvidenceV3(e) {
			t.Fatalf("%s admitted", name)
		}
	}
	// The V2/V1 admission is unchanged: it still needs a rollback decision.
	if AgentReleasedDNSInverseEvidence(freshPrestartReleaseEvidence(dnsengineartifact.SwitchPhaseTargetStaged, dnsengineartifact.ReleasedNativeUnknownCode)) {
		t.Fatal("the V2/V1 release admission widened")
	}
}
