package dnsenginerecovery

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
)

// EvidenceStatus is a read-only classification, never admission to mutate DNS.
// Host locks, worker liveness, native service state and owner edits remain
// unproved even when the journal and ledger agree.
type EvidenceStatus string

const (
	EvidenceActive              EvidenceStatus = "accepted-active"
	EvidenceLeaseExpired        EvidenceStatus = "active-lease-expired"
	EvidenceWorkerRecorded      EvidenceStatus = "worker-recorded"
	EvidenceExpiredCancellation EvidenceStatus = "expired-cancellation"
	EvidenceFinalized           EvidenceStatus = "finalized-with-journal"
)

// TargetReceiptStatus describes only the currently observed state document.
// Even an exact receipt is not a native DNS service proof or recovery admission.
type TargetReceiptStatus string

const (
	TargetReceiptAbsent    TargetReceiptStatus = "absent"
	TargetReceiptExact     TargetReceiptStatus = "exact-journal-target"
	TargetReceiptDifferent TargetReceiptStatus = "different-from-journal-target"
)

// SourceReceiptStatus compares the current state document with the journal's
// frozen source. It is only a receipt observation, not an inverse admission.
type SourceReceiptStatus string

const (
	SourceReceiptMutualAbsence SourceReceiptStatus = "source-and-current-absent"
	SourceReceiptExact         SourceReceiptStatus = "exact-journal-source"
	SourceReceiptDifferent     SourceReceiptStatus = "different-from-journal-source"
)

// SourceOwnershipStatus describes the frozen source's per-engine ownership
// receipt only. An exact match remains insufficient for a native inverse.
type SourceOwnershipStatus string

const (
	SourceOwnershipNotApplicable SourceOwnershipStatus = "no-source-engine"
	SourceOwnershipAbsent        SourceOwnershipStatus = "absent"
	SourceOwnershipExact         SourceOwnershipStatus = "exact-journal-source"
	SourceOwnershipDifferent     SourceOwnershipStatus = "different-from-journal-source"
)

type EvidenceObservation struct {
	// EvidenceSHA256 fingerprints the exact installed bytes read by InspectFiles.
	// Decoded-only observations leave it empty; it never admits a mutation.
	EvidenceSHA256  string
	Status          EvidenceStatus
	RequestID       string
	SourceEngine    string
	TargetEngine    string
	Phase           string
	WorkerPID       int
	WorkerStarted   string
	TargetReceipt   TargetReceiptStatus
	SourceReceipt   SourceReceiptStatus
	SourceOwnership SourceOwnershipStatus
	NativeUnits     []string
}

// InspectEvidence binds a previously decoded canonical journal to a canonical
// accepted ledger. An unexpected combination is unknown, not absence or proof
// that an inverse is safe. The caller must obtain bytes from trusted fixed paths.
func InspectEvidence(policy dnsengineartifact.JournalPolicy, journal dnsengineartifact.SwitchJournalV1, ledger servicemutationledger.Ledger, now time.Time) (EvidenceObservation, error) {
	if err := policy.ValidateSwitchJournal(journal); err != nil {
		return EvidenceObservation{}, fmt.Errorf("DNS switch journal is not accepted: %w", err)
	}
	if err := servicemutationledger.Validate(&ledger); err != nil {
		return EvidenceObservation{}, fmt.Errorf("DNS switch ledger is not accepted: %w", err)
	}
	id := dnsengineartifact.SwitchIdentity{RequestID: journal.MutationRequestID, OwnerID: journal.MutationOwnerID, Target: journal.TargetEngine, Qualifier: journal.ManifestQualifier}
	if err := id.Validate(); err != nil {
		return EvidenceObservation{}, err
	}
	job := ledger.Jobs[id.RequestID]
	if job == nil {
		return EvidenceObservation{}, errors.New("DNS switch journal has no matching accepted ledger job")
	}
	observation := EvidenceObservation{RequestID: id.RequestID, Phase: journal.Phase, SourceEngine: string(journal.SourceEngine), TargetEngine: string(journal.TargetEngine)}
	unitNames := make(map[string]bool, len(journal.SourceUnitsBefore)+len(journal.TargetUnitsBefore))
	for _, snapshots := range [][]dnsengineartifact.UnitSnapshot{journal.SourceUnitsBefore, journal.TargetUnitsBefore} {
		for _, snapshot := range snapshots {
			unitNames[snapshot.Name] = true
		}
	}
	for name := range unitNames {
		observation.NativeUnits = append(observation.NativeUnits, name)
	}
	sort.Strings(observation.NativeUnits)
	if ledger.ActiveRequestID == id.RequestID {
		switch {
		case id.ActiveJob(job):
			if now.Before(job.StartedAt) {
				return EvidenceObservation{}, errors.New("DNS switch observer clock precedes the accepted operation")
			}
			if !now.Before(job.LeaseExpiresAt) {
				observation.Status = EvidenceLeaseExpired
			} else {
				observation.Status = EvidenceActive
			}
		case id.ActiveJobWithRegisteredWorker(job):
			observation.Status = EvidenceWorkerRecorded
			observation.WorkerPID = job.WorkerPID
			observation.WorkerStarted = job.WorkerStarted
		case id.ExpiredCancellingJob(job, now):
			observation.Status = EvidenceExpiredCancellation
		default:
			return EvidenceObservation{}, errors.New("DNS switch journal and active ledger job disagree")
		}
		return observation, nil
	}
	if err := id.ValidateFinalizedLedger(ledger); err == nil {
		observation.Status = EvidenceFinalized
		return observation, nil
	}
	return EvidenceObservation{}, errors.New("DNS switch journal has no exact active or finalized ledger authority")
}
