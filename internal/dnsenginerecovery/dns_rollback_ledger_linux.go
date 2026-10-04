//go:build linux

package dnsenginerecovery

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
)

// PublishExactDNSRollbackVerdict durably closes only the active job of a
// rolled-back switch. The caller still has to hold the release and host locks,
// prove the native inverse, and keep the exact journal until this publication
// has completed. This helper rechecks the accepted worker immediately before
// the byte-exact ledger CAS; it cannot exclude future owner changes.
func PublishExactDNSRollbackVerdict(
	policy dnsengineartifact.JournalPolicy,
	owner servicemutationledger.FileOwner,
	expected dnsengineartifact.SwitchJournalV1,
	now time.Time,
) error {
	if !policy.RequireOwner || policy.StateUID != owner.UID || policy.StateGID != owner.GID ||
		filepath.Base(policy.StatePath) != "dns-engine-state.json" ||
		expected.Phase != dnsengineartifact.SwitchPhaseRolledBack {
		return errors.New("DNS rollback verdict requires the trusted owner and terminal journal")
	}
	if err := policy.ValidateSwitchJournal(expected); err != nil {
		return err
	}
	id := dnsengineartifact.SwitchIdentity{
		RequestID: expected.MutationRequestID, OwnerID: expected.MutationOwnerID,
		Target: expected.TargetEngine, Qualifier: expected.ManifestQualifier,
	}
	if err := id.Validate(); err != nil {
		return err
	}
	if now.IsZero() || now.Location() != time.UTC {
		return errors.New("DNS rollback verdict requires a UTC decision time")
	}
	journalPath := filepath.Join(filepath.Dir(policy.StatePath), "dns-engine-switch-journal.json")
	ledgerPath := filepath.Join(filepath.Dir(policy.StatePath), "service-mutations.json")
	expectedJournal, err := policy.EncodeSwitchJournal(expected)
	if err != nil {
		return err
	}
	checkJournal := func() error {
		raw, present, err := servicemutationledger.ReadFile(journalPath, dnsengineartifact.SwitchJournalLimit, owner)
		if err != nil || !present || !bytes.Equal(raw, expectedJournal) {
			return errors.Join(errors.New("exact rolled-back DNS journal changed or is absent"), err)
		}
		return nil
	}
	if err := checkJournal(); err != nil {
		return err
	}
	before, present, err := servicemutationledger.ReadFile(ledgerPath, servicemutationledger.MaxSize, owner)
	if err != nil || !present {
		return errors.Join(errors.New("accepted DNS mutation ledger is absent or unreadable"), err)
	}
	ledger, err := servicemutationledger.Decode(before)
	if err != nil {
		return err
	}
	job := ledger.Jobs[id.RequestID]
	if ledger.ActiveRequestID != id.RequestID || job == nil ||
		!(id.ActiveJob(job) || id.ActiveJobWithRegisteredWorker(job) ||
			id.ExpiredCancellingJob(job, now) || id.OrphanedWorkerJob(job)) {
		return errors.New("DNS rollback verdict lacks the exact active accepted job")
	}
	worker, err := InspectAcceptedWorker(id, job, now)
	if err != nil || worker == WorkerStillAlive {
		return errors.Join(errors.New("accepted DNS worker is still alive or unknown"), err)
	}
	if now.Before(job.UpdatedAt) {
		return errors.New("DNS rollback verdict clock precedes the accepted job")
	}
	job.Status = servicemutationledger.StatusFailed
	job.Phase = "interrupted"
	job.ErrorCode = "dns_engine_switch_rolled_back_by_owner_recovery"
	job.ErrorMessage = "The interrupted DNS engine switch was rolled back to the verified previous state."
	job.UpdatedAt = now
	job.FinishedAt = now
	job.LeaseExpiresAt = time.Time{}
	job.WorkerPID = 0
	job.WorkerStarted = ""
	job.WorkerCommand = ""
	ledger.ActiveRequestID = ""
	if !id.TerminalRolledBackJob(ledger) {
		return errors.New("DNS rollback verdict does not form an exact terminal job")
	}
	after, err := servicemutationledger.Encode(&ledger)
	if err != nil {
		return fmt.Errorf("encode DNS rollback terminal ledger: %w", err)
	}
	if err := checkJournal(); err != nil {
		return err
	}
	if err := servicemutationledger.ReplaceFileExact(ledgerPath, before, after, servicemutationledger.MaxSize, owner); err != nil {
		return fmt.Errorf("publish exact DNS rollback verdict: %w", err)
	}
	return checkJournal()
}
