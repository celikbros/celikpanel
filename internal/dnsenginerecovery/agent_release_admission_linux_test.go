//go:build linux

package dnsenginerecovery

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
	"github.com/alicelik/celikpanel/internal/transport"
)

// These tests hold the one released-undecided admission of the three owner
// inverse commands: the Agent's own deliberate release after its restart could
// not complete the rollback, for the exact request, at a durable rollback
// decision. They are component tests, not native DNS evidence.

const releaseTestQualifier = "dns-engine-switch/v1:sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"

func agentReleasedTestJob(j dnsengineartifact.SwitchJournalV1, code string) transport.ServiceMutationJob {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	return transport.ServiceMutationJob{
		RequestID: j.MutationRequestID, OwnerID: j.MutationOwnerID,
		Kind: "dns_engine_switch", Target: string(j.TargetEngine), PackageName: j.ManifestQualifier,
		Status: servicemutationledger.StatusFailed, Phase: "interrupted", Attempt: 1,
		StartedAt: now.Add(-time.Hour), UpdatedAt: now, FinishedAt: now, DeadlineAt: now.Add(time.Hour),
		ErrorCode:    code,
		ErrorMessage: "The interrupted DNS switch could not be verified after the Agent restarted.",
	}
}

func releasedTestEvidence(e SwitchEvidence, code string) SwitchEvidence {
	e.Observation.Status = EvidenceReleasedUndecided
	e.Observation.ReleaseReason = code
	e.AcceptedJob = agentReleasedTestJob(e.Journal, code)
	return e
}

func releaseBINDSwitchEvidence() SwitchEvidence {
	e := inactiveBINDSwitchEvidence()
	e.Journal.MutationOwnerID = strings.Repeat("b", 32)
	e.Journal.ManifestQualifier = releaseTestQualifier
	return e
}

func releasePDNSAdoptionEvidence() SwitchEvidence {
	j := dnsengineartifact.SwitchJournalV1{
		Phase:             dnsengineartifact.SwitchPhaseRollingBack,
		Mode:              transport.DNSEngineSwitchModeAdopt,
		MutationRequestID: strings.Repeat("a", 32),
		MutationOwnerID:   strings.Repeat("b", 32),
		ManifestQualifier: releaseTestQualifier,
		TargetEngine:      transport.DNSEnginePowerDNS,
		TargetEpoch:       1,
	}
	return SwitchEvidence{Journal: j, Observation: EvidenceObservation{
		EvidenceSHA256: "exact-secured-bytes", Status: EvidenceActive,
		RequestID: j.MutationRequestID, Phase: j.Phase,
		TargetEngine: string(j.TargetEngine), TargetEpoch: j.TargetEpoch,
		InverseKind:     NativeInversePDNSAdoption,
		SourceOwnership: SourceOwnershipNotApplicable,
		TargetReceipt:   TargetReceiptExact, SourceReceipt: SourceReceiptDifferent,
	}}
}

type releaseAdmissionCommand struct {
	name     string
	evidence func(*testing.T) SwitchEvidence
	validate func(SwitchEvidence) error
	restored func(*SwitchEvidence)
}

func releaseAdmissionCommands() []releaseAdmissionCommand {
	return []releaseAdmissionCommand{
		{
			name:     "recover-dns-bind-switch",
			evidence: func(*testing.T) SwitchEvidence { return releaseBINDSwitchEvidence() },
			validate: ValidateInactiveBINDSwitchInverseEvidence,
			restored: func(e *SwitchEvidence) {
				e.Observation.SourceReceipt, e.Observation.TargetReceipt = SourceReceiptExact, TargetReceiptDifferent
			},
		},
		{
			name:     "recover-dns-bind-adoption",
			evidence: runningBINDInverseEvidence,
			validate: ValidateRunningBINDAdoptionInverseEvidence,
			restored: func(e *SwitchEvidence) {
				e.Observation.SourceReceipt, e.Observation.TargetReceipt = SourceReceiptMutualAbsence, TargetReceiptAbsent
			},
		},
		{
			name:     "recover-dns-pdns-adoption",
			evidence: func(*testing.T) SwitchEvidence { return releasePDNSAdoptionEvidence() },
			validate: ValidatePDNSAdoptionInverseEvidence,
			restored: func(e *SwitchEvidence) {
				e.Observation.SourceReceipt, e.Observation.TargetReceipt = SourceReceiptMutualAbsence, TargetReceiptAbsent
			},
		},
	}
}

func setReleaseTestPhase(e *SwitchEvidence, phase string) {
	e.Journal.Phase, e.Observation.Phase = phase, phase
	e.AcceptedJob = agentReleasedTestJob(e.Journal, e.AcceptedJob.ErrorCode)
}

func TestOwnerInverseAdmitsOnlyAgentDeliberateRelease(t *testing.T) {
	for _, command := range releaseAdmissionCommands() {
		t.Run(command.name, func(t *testing.T) {
			admitted := func(name string, e SwitchEvidence) {
				t.Helper()
				if err := command.validate(e); err != nil {
					t.Fatalf("%s refused: %v", name, err)
				}
			}
			refused := func(name string, e SwitchEvidence) {
				t.Helper()
				if err := command.validate(e); err == nil {
					t.Fatalf("%s admitted", name)
				}
			}
			// Active statuses keep their existing admission.
			for _, status := range []EvidenceStatus{EvidenceActive, EvidenceLeaseExpired, EvidenceWorkerRecorded, EvidenceOrphanedWorker, EvidenceExpiredCancellation} {
				e := command.evidence(t)
				e.Observation.Status = status
				admitted("active status "+string(status), e)
			}
			finalized := command.evidence(t)
			finalized.Observation.Status = EvidenceFinalized
			refused("finalized job", finalized)

			base := releasedTestEvidence(command.evidence(t), dnsengineartifact.ReleasedNativeUnknownCode)
			admitted("deliberate release at rolling-back", base)

			rolledBack := base
			setReleaseTestPhase(&rolledBack, dnsengineartifact.SwitchPhaseRolledBack)
			command.restored(&rolledBack)
			admitted("deliberate release at rolled-back", rolledBack)

			unrestored := base
			setReleaseTestPhase(&unrestored, dnsengineartifact.SwitchPhaseRolledBack)
			refused("deliberate release at rolled-back without restored receipts", unrestored)

			for _, code := range []string{dnsengineartifact.ReleasedUnsupportedHostCode, dnsengineartifact.ReleasedHostWindowCode} {
				refused("release reason "+code, releasedTestEvidence(command.evidence(t), code))
			}
			mixed := base
			mixed.AcceptedJob.ErrorCode = dnsengineartifact.ReleasedHostWindowCode
			refused("observation and ledger job disagree on the reason", mixed)

			foreign := base
			foreign.Observation.RequestID = strings.Repeat("d", 32)
			refused("foreign observation request", foreign)
			foreignJob := base
			foreignJob.AcceptedJob.RequestID = strings.Repeat("d", 32)
			refused("foreign ledger job", foreignJob)
			foreignOwner := base
			foreignOwner.AcceptedJob.OwnerID = strings.Repeat("e", 32)
			refused("ledger job with another owner", foreignOwner)

			worker := base
			worker.AcceptedJob.WorkerPID, worker.AcceptedJob.WorkerStarted, worker.AcceptedJob.WorkerCommand = 42, "7", "agent"
			refused("released job recording a worker", worker)
			leased := base
			leased.AcceptedJob.LeaseExpiresAt = leased.AcceptedJob.FinishedAt.Add(time.Minute)
			refused("released job keeping a lease", leased)

			for _, phase := range []string{
				dnsengineartifact.SwitchPhaseIntent, dnsengineartifact.SwitchPhaseTargetStaged,
				dnsengineartifact.SwitchPhaseTargetStarted, dnsengineartifact.SwitchPhaseCommitted,
			} {
				e := base
				setReleaseTestPhase(&e, phase)
				refused("deliberate release at phase "+phase, e)
			}
		})
	}
}

// The admission is bound to the evidence reader: the Agent's release beside a
// rolling-back journal is reported released-undecided with its reason, and the
// same ledger beside the rolled-back checkpoint is terminal-rolled-back, so the
// checkpoint needs no ledger write.
func TestInspectEvidenceClassifiesAgentReleaseAroundRollbackCheckpoint(t *testing.T) {
	policy, _, journal, _ := adoptionStateRemovalFixture(t)
	job := agentReleasedTestJob(journal, dnsengineartifact.ReleasedNativeUnknownCode)
	ledger := servicemutationledger.Ledger{Version: servicemutationledger.Version, Jobs: map[string]*transport.ServiceMutationJob{job.RequestID: &job}}
	now := job.FinishedAt.Add(time.Minute)
	observed, err := InspectEvidence(policy, journal, ledger, now)
	if err != nil || observed.Status != EvidenceReleasedUndecided || observed.ReleaseReason != dnsengineartifact.ReleasedNativeUnknownCode {
		t.Fatalf("rolling-back release observed as %+v: %v", observed, err)
	}
	journal.Phase = dnsengineartifact.SwitchPhaseRolledBack
	observed, err = InspectEvidence(policy, journal, ledger, now)
	if err != nil || observed.Status != EvidenceTerminalRolledBack || observed.ReleaseReason != dnsengineartifact.ReleasedNativeUnknownCode {
		t.Fatalf("rolled-back release observed as %+v: %v", observed, err)
	}
	other := ledger
	otherJob := job
	otherJob.RequestID = strings.Repeat("d", 32)
	other.ActiveRequestID = otherJob.RequestID
	otherJob.Status, otherJob.Phase = servicemutationledger.StatusRunning, "leased"
	otherJob.FinishedAt, otherJob.ErrorCode, otherJob.ErrorMessage = time.Time{}, "", ""
	otherJob.LeaseExpiresAt = now.Add(time.Minute)
	other.Jobs = map[string]*transport.ServiceMutationJob{job.RequestID: &job, otherJob.RequestID: &otherJob}
	journal.Phase = dnsengineartifact.SwitchPhaseRollingBack
	if _, err := InspectEvidence(policy, journal, other, now); err == nil {
		t.Fatal("released journal was classified while another mutation holds the ledger")
	}
}

// classifyLikeInspect mirrors InspectEvidence for fixtures: the release ledger
// beside a rolled-back journal is terminal-rolled-back.
func classifyLikeInspect(e SwitchEvidence) SwitchEvidence {
	if e.Observation.Status == EvidenceReleasedUndecided && e.Journal.Phase == dnsengineartifact.SwitchPhaseRolledBack {
		e.Observation.Status = EvidenceTerminalRolledBack
	}
	return e
}

func TestBINDSwitchInverseCompletesAgentReleaseWithoutLedgerPublication(t *testing.T) {
	f := newBINDSwitchInverseFixture()
	f.evidence = releasedTestEvidence(releaseBINDSwitchEvidence(), dnsengineartifact.ReleasedNativeUnknownCode)
	ops := f.ops()
	read := ops.Read
	ops.Read = func(ctx context.Context) (SwitchEvidence, bool, error) {
		e, present, err := read(ctx)
		f.evidence = classifyLikeInspect(f.evidence)
		return classifyLikeInspect(e), present, err
	}
	if err := CompleteInactiveBINDSwitchInverse(context.Background(), ops); err != nil {
		t.Fatal(err)
	}
	if !f.retired || f.terminal || f.native != BINDSwitchNativeRestored {
		t.Fatalf("released BIND switch did not retire without a verdict publication: %v", f.calls)
	}
	for _, call := range f.calls {
		if call == "verdict" {
			t.Fatalf("released job received a second ledger verdict: %v", f.calls)
		}
	}
}

func TestBINDSwitchInverseRefusesOtherReleaseBeforeEffects(t *testing.T) {
	f := newBINDSwitchInverseFixture()
	f.evidence = releasedTestEvidence(releaseBINDSwitchEvidence(), dnsengineartifact.ReleasedHostWindowCode)
	if err := CompleteInactiveBINDSwitchInverse(context.Background(), f.ops()); err == nil {
		t.Fatal("host-window release was admitted")
	}
	for _, call := range f.calls {
		if call != "read" {
			t.Fatalf("refused release reached %s: %v", call, f.calls)
		}
	}
}

func TestRunningBINDAdoptionInverseCompletesAgentRelease(t *testing.T) {
	evidence := releasedTestEvidence(runningBINDInverseEvidence(t), dnsengineartifact.ReleasedNativeUnknownCode)
	restored, retired, calls := false, false, []string{}
	ops := BINDSwitchInverseOps{
		Read: func(context.Context) (SwitchEvidence, bool, error) {
			evidence = classifyLikeInspect(evidence)
			return evidence, !retired, nil
		},
		ExcludeWorker: func(context.Context, SwitchEvidence) error { return nil },
		AssessNative: func(context.Context, dnsengineartifact.SwitchJournalV1) (BINDSwitchNativeState, error) {
			if restored {
				return BINDSwitchNativeRestored, nil
			}
			return BINDSwitchNativeNeedsRestore, nil
		},
		RestoreNative: func(context.Context, dnsengineartifact.SwitchJournalV1) error {
			calls = append(calls, "restore")
			restored = true
			evidence.Observation.SourceReceipt, evidence.Observation.TargetReceipt = SourceReceiptMutualAbsence, TargetReceiptAbsent
			return nil
		},
		WritePhase: func(_ context.Context, _, after dnsengineartifact.SwitchJournalV1) error {
			calls = append(calls, "phase")
			evidence.Journal, evidence.Observation.Phase = after, after.Phase
			return nil
		},
		PublishFailed: func(context.Context, dnsengineartifact.SwitchJournalV1) error {
			calls = append(calls, "verdict")
			return errors.New("released job must not be republished")
		},
		RemoveJournal: func(context.Context, dnsengineartifact.SwitchJournalV1) error {
			calls = append(calls, "retire")
			retired = true
			return nil
		},
	}
	if err := CompleteRunningBINDAdoptionInverse(context.Background(), ops); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(calls, []string{"restore", "phase", "retire"}) {
		t.Fatalf("released running BIND adoption effects: %v", calls)
	}
}

func TestPDNSAdoptionInverseCompletesAgentRelease(t *testing.T) {
	for _, code := range []string{dnsengineartifact.ReleasedNativeUnknownCode, dnsengineartifact.ReleasedUnsupportedHostCode} {
		t.Run(code, func(t *testing.T) {
			f := newAdoptionInverseFixture()
			f.journal.ManifestQualifier = releaseTestQualifier
			ops := f.ops()
			read := ops.Read
			ops.Read = func(ctx context.Context) (SwitchEvidence, bool, error) {
				e, present, err := read(ctx)
				if err != nil || !present {
					return e, present, err
				}
				e.Observation.Status = EvidenceReleasedUndecided
				e.Observation.ReleaseReason = code
				e.AcceptedJob = agentReleasedTestJob(f.journal, code)
				return classifyLikeInspect(e), true, nil
			}
			err := CompletePDNSAdoptionInverse(context.Background(), ops)
			if code != dnsengineartifact.ReleasedNativeUnknownCode {
				if err == nil || len(f.calls) != 1 || f.calls[0] != "read" {
					t.Fatalf("other release reason reached effects: err=%v calls=%v", err, f.calls)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !f.removed || f.terminal || f.statePresent {
				t.Fatalf("released adoption did not retire exactly: %v", f.calls)
			}
			for _, call := range f.calls {
				if call == "ledger" {
					t.Fatalf("released job received a second ledger verdict: %v", f.calls)
				}
			}
		})
	}
}
