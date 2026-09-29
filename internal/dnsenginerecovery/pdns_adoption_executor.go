package dnsenginerecovery

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
	"github.com/alicelik/celikpanel/internal/transport"
)

// PDNSAdoptionInverseOps are effects supplied by a privileged owner recovery
// executor while it holds the release and DNS host locks. Read must return the
// exact secured journal/ledger/state observation; ExcludeWorker must inspect
// the accepted process. ProveNative checks the unchanged owner PowerDNS source.
// A caller must bind these callbacks to fixed installed paths and native proof.
type PDNSAdoptionInverseOps struct {
	Read          func(context.Context) (SwitchEvidence, bool, error)
	ExcludeWorker func(context.Context, SwitchEvidence) error
	ProveNative   func(context.Context, dnsengineartifact.SwitchJournalV1) error
	RemoveState   func(context.Context, dnsengineartifact.SwitchJournalV1) error
	WritePhase    func(context.Context, dnsengineartifact.SwitchJournalV1, dnsengineartifact.SwitchJournalV1) error
	PublishFailed func(context.Context, dnsengineartifact.SwitchJournalV1) error
	RemoveJournal func(context.Context, dnsengineartifact.SwitchJournalV1) error
}

// CompletePDNSAdoptionInverse executes only an already durable rollback
// decision for an exact PowerDNS adoption. It does not decide to roll back a
// target, acquire locks, or give a caller mutation authority. Unknown results
// stop at their last durable checkpoint so the same request can be retried.
func CompletePDNSAdoptionInverse(ctx context.Context, ops PDNSAdoptionInverseOps) error {
	if ctx == nil || ops.Read == nil || ops.ExcludeWorker == nil ||
		ops.ProveNative == nil || ops.RemoveState == nil || ops.WritePhase == nil ||
		ops.PublishFailed == nil || ops.RemoveJournal == nil {
		return errors.New("PowerDNS adoption inverse requires complete owner recovery operations")
	}
	read := func() (SwitchEvidence, error) {
		if err := ctx.Err(); err != nil {
			return SwitchEvidence{}, err
		}
		evidence, present, err := ops.Read(ctx)
		if err != nil || !present {
			return SwitchEvidence{}, errors.Join(errors.New("exact PowerDNS adoption evidence is unavailable"), err)
		}
		return evidence, nil
	}
	first, err := read()
	if err != nil {
		return err
	}
	if err := validatePDNSAdoptionInverseEvidence(first); err != nil {
		return err
	}
	if err := ops.ExcludeWorker(ctx, first); err != nil {
		return fmt.Errorf("exclude accepted DNS worker: %w", err)
	}
	if err := ops.ProveNative(ctx, first.Journal); err != nil {
		return fmt.Errorf("prove owner PowerDNS before inverse: %w", err)
	}
	before, err := read()
	if err != nil || !samePDNSAdoptionInverseEvidence(first, before) {
		return errors.Join(errors.New("PowerDNS adoption evidence changed before inverse"), err)
	}
	if err := ops.ExcludeWorker(ctx, before); err != nil {
		return fmt.Errorf("recheck accepted DNS worker before inverse: %w", err)
	}
	if before.Observation.TargetReceipt == TargetReceiptExact {
		if err := ops.RemoveState(ctx, before.Journal); err != nil {
			return fmt.Errorf("remove exact adoption target receipt: %w", err)
		}
	}
	restored, err := read()
	if err != nil || !samePDNSAdoptionJournal(before.Journal, restored.Journal) ||
		restored.Observation.SourceReceipt != SourceReceiptMutualAbsence ||
		restored.Observation.TargetReceipt != TargetReceiptAbsent ||
		restored.Observation.Status != before.Observation.Status ||
		!reflect.DeepEqual(restored.AcceptedJob, before.AcceptedJob) {
		return errors.Join(errors.New("PowerDNS adoption source receipt was not restored"), err)
	}
	if err := ops.ProveNative(ctx, restored.Journal); err != nil {
		return fmt.Errorf("prove owner PowerDNS after inverse: %w", err)
	}
	if err := ops.ExcludeWorker(ctx, restored); err != nil {
		return fmt.Errorf("recheck accepted DNS worker after inverse: %w", err)
	}
	if restored.Journal.Phase == dnsengineartifact.SwitchPhaseRollingBack {
		if restored.Observation.Status == EvidenceTerminalRolledBack {
			return errors.New("terminal DNS rollback ledger precedes its rolled-back journal checkpoint")
		}
		next := restored.Journal
		next.Phase = dnsengineartifact.SwitchPhaseRolledBack
		if err := ops.WritePhase(ctx, restored.Journal, next); err != nil {
			return fmt.Errorf("publish DNS rolled-back checkpoint: %w", err)
		}
	}
	checkpoint, err := read()
	if err != nil || !samePDNSAdoptionJournalIgnoringPhase(first.Journal, checkpoint.Journal) ||
		checkpoint.Journal.Phase != dnsengineartifact.SwitchPhaseRolledBack ||
		checkpoint.Observation.SourceReceipt != SourceReceiptMutualAbsence ||
		!sameInverseCheckpointStatus(restored.Observation, checkpoint.Observation) ||
		!reflect.DeepEqual(checkpoint.AcceptedJob, restored.AcceptedJob) {
		return errors.Join(errors.New("PowerDNS adoption rollback checkpoint was not retained"), err)
	}
	// An Agent-released job is already terminal: no verdict is published
	// over it, and the re-read below must classify it terminal-rolled-back.
	if checkpoint.Observation.Status != EvidenceTerminalRolledBack {
		if !activeDNSInverseStatus(checkpoint.Observation.Status) {
			return errors.New("PowerDNS adoption rollback lost its exact active job before terminal verdict")
		}
		if err := ops.ExcludeWorker(ctx, checkpoint); err != nil {
			return fmt.Errorf("recheck accepted DNS worker before verdict: %w", err)
		}
		if err := ops.PublishFailed(ctx, checkpoint.Journal); err != nil {
			return fmt.Errorf("publish exact DNS rollback verdict: %w", err)
		}
	}
	terminal, err := read()
	if err != nil || !samePDNSAdoptionJournalIgnoringPhase(checkpoint.Journal, terminal.Journal) ||
		terminal.Journal.Phase != dnsengineartifact.SwitchPhaseRolledBack ||
		terminal.Observation.Status != EvidenceTerminalRolledBack ||
		terminal.Observation.SourceReceipt != SourceReceiptMutualAbsence {
		return errors.Join(errors.New("PowerDNS adoption terminal verdict could not be re-proved"), err)
	}
	if err := ops.ProveNative(ctx, terminal.Journal); err != nil {
		return fmt.Errorf("reprove owner PowerDNS before journal retirement: %w", err)
	}
	final, err := read()
	if err != nil || !samePDNSAdoptionInverseEvidence(terminal, final) {
		return errors.Join(errors.New("PowerDNS adoption evidence changed before journal retirement"), err)
	}
	if err := ops.ExcludeWorker(ctx, final); err != nil {
		return fmt.Errorf("recheck accepted DNS worker before journal retirement: %w", err)
	}
	if err := ops.RemoveJournal(ctx, final.Journal); err != nil {
		return fmt.Errorf("retire exact DNS rollback journal: %w", err)
	}
	// The terminal receipt is published and the exact journal retired. A
	// cancellation after that final durable effect cannot be retried.
	return nil
}

func activeDNSInverseStatus(status EvidenceStatus) bool {
	switch status {
	case EvidenceActive, EvidenceLeaseExpired, EvidenceWorkerRecorded,
		EvidenceOrphanedWorker, EvidenceExpiredCancellation:
		return true
	}
	return false
}

// AgentReleasedDNSInverseEvidence admits exactly one released-undecided case
// to recover-dns-bind-switch, recover-dns-bind-adoption and
// recover-dns-pdns-adoption: the restarted Agent's own deliberate release
// (ReleasedNativeUnknownCode) of this exact request, retained at a durable
// rollback decision. The Agent writes that release only after proving the
// recorded worker gone under the host mutation lock, and never executes a V2
// inverse itself, so without this admission such a journal has no owner path.
// Other release reasons (unreadable host, closed boot window), another
// request, or a pre-decision phase stay refused. The released job is already
// this operation's terminal ledger verdict; the command publishes no ledger
// change and InspectEvidence classifies the job beside a rolled-back journal
// as terminal-rolled-back. Success grants no lock, worker or native authority:
// the caller's journal-shape, evidence, owner-change and lock checks all
// still apply.
func AgentReleasedDNSInverseEvidence(e SwitchEvidence) bool {
	j, o := e.Journal, e.Observation
	if o.Status != EvidenceReleasedUndecided ||
		o.ReleaseReason != dnsengineartifact.ReleasedNativeUnknownCode ||
		o.RequestID != j.MutationRequestID || o.Phase != j.Phase ||
		(j.Phase != dnsengineartifact.SwitchPhaseRollingBack &&
			j.Phase != dnsengineartifact.SwitchPhaseRolledBack) {
		return false
	}
	// The secured read already bound this job to an idle ledger. Re-applying
	// the Agent's own release predicate to the copied job, bound to this
	// journal's identity, keeps the worker, lease and terminal fields exact
	// for callers that hold only the evidence.
	job := e.AcceptedJob
	return job.RequestID == j.MutationRequestID && job.OwnerID == j.MutationOwnerID &&
		job.Target == string(j.TargetEngine) && job.PackageName == j.ManifestQualifier &&
		AgentDeliberateReleaseJob(job)
}

// AgentDeliberateReleaseJob reports whether a ledger job, read on its own, is
// exactly the Agent's deliberate lease release (ReleasedNativeUnknownCode) for
// its own DNS switch identity. It reads nothing else: journal presence, native
// DNS state and recovery authority remain for the caller to observe.
func AgentDeliberateReleaseJob(job transport.ServiceMutationJob) bool {
	id := dnsengineartifact.SwitchIdentity{
		RequestID: job.RequestID, OwnerID: job.OwnerID,
		Target: transport.DNSEngine(job.Target), Qualifier: job.PackageName,
	}
	return job.ErrorCode == dnsengineartifact.ReleasedNativeUnknownCode &&
		id.ReleasedUndecidedJob(servicemutationledger.Ledger{
			Version: servicemutationledger.Version,
			Jobs:    map[string]*transport.ServiceMutationJob{id.RequestID: &job},
		})
}

// sameInverseCheckpointStatus allows the one status change a rollback
// checkpoint causes without any ledger write: the Agent's deliberate release
// beside a rolling-back journal becomes terminal-rolled-back once the journal
// reaches rolled-back. Every other status must remain unchanged.
func sameInverseCheckpointStatus(before, after EvidenceObservation) bool {
	if before.Status == after.Status {
		return true
	}
	return before.Status == EvidenceReleasedUndecided &&
		after.Status == EvidenceTerminalRolledBack &&
		before.ReleaseReason == dnsengineartifact.ReleasedNativeUnknownCode &&
		after.ReleaseReason == before.ReleaseReason
}

// ValidatePDNSAdoptionInverseEvidence is the evidence admission used by
// CompletePDNSAdoptionInverse. Read-only observers use it to name the owner
// command; its success grants no lock, worker or native mutation authority.
func ValidatePDNSAdoptionInverseEvidence(evidence SwitchEvidence) error {
	return validatePDNSAdoptionInverseEvidence(evidence)
}

func validatePDNSAdoptionInverseEvidence(evidence SwitchEvidence) error {
	journal, observed := evidence.Journal, evidence.Observation
	if err := PDNSAdoptionInverseJournal(journal); err != nil {
		return err
	}
	released := AgentReleasedDNSInverseEvidence(evidence)
	if observed.InverseKind != NativeInversePDNSAdoption ||
		observed.RequestID != journal.MutationRequestID ||
		observed.Phase != journal.Phase ||
		observed.SourceEngine != string(journal.SourceEngine) ||
		observed.TargetEngine != string(journal.TargetEngine) ||
		observed.TargetEpoch != journal.TargetEpoch ||
		observed.SourceOwnership != SourceOwnershipNotApplicable ||
		(observed.TargetReceipt != TargetReceiptExact && observed.TargetReceipt != TargetReceiptAbsent) ||
		(observed.TargetReceipt == TargetReceiptExact && observed.SourceReceipt != SourceReceiptDifferent) ||
		(observed.TargetReceipt == TargetReceiptAbsent && observed.SourceReceipt != SourceReceiptMutualAbsence) ||
		(!activeDNSInverseStatus(observed.Status) && observed.Status != EvidenceTerminalRolledBack && !released) ||
		(observed.Status == EvidenceTerminalRolledBack &&
			(journal.Phase != dnsengineartifact.SwitchPhaseRolledBack ||
				observed.TargetReceipt != TargetReceiptAbsent ||
				observed.SourceReceipt != SourceReceiptMutualAbsence)) ||
		(released && journal.Phase == dnsengineartifact.SwitchPhaseRolledBack &&
			(observed.TargetReceipt != TargetReceiptAbsent ||
				observed.SourceReceipt != SourceReceiptMutualAbsence)) ||
		observed.EvidenceSHA256 == "" {
		return errors.New("PowerDNS adoption inverse lacks its exact durable rollback evidence")
	}
	return nil
}

func samePDNSAdoptionJournal(before, after dnsengineartifact.SwitchJournalV1) bool {
	return reflect.DeepEqual(before, after)
}

func samePDNSAdoptionJournalIgnoringPhase(before, after dnsengineartifact.SwitchJournalV1) bool {
	after.Phase = before.Phase
	return reflect.DeepEqual(before, after)
}

func samePDNSAdoptionInverseEvidence(before, after SwitchEvidence) bool {
	return samePDNSAdoptionJournal(before.Journal, after.Journal) &&
		reflect.DeepEqual(before.Observation, after.Observation) &&
		reflect.DeepEqual(before.AcceptedJob, after.AcceptedJob)
}
