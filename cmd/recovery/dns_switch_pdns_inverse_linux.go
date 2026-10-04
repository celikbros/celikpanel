//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/dnsenginerecovery"
	"github.com/alicelik/celikpanel/internal/hostingpath"
	"github.com/alicelik/celikpanel/internal/hostmutationlock"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
	"github.com/alicelik/celikpanel/internal/transport"
)

// excludeInstalledDNSInverseWorker checks the exact accepted job after the
// caller's secured evidence read. It never treats an unknown process as gone.
func excludeInstalledDNSInverseWorker(ctx context.Context, evidence dnsenginerecovery.SwitchEvidence) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	journal, job := evidence.Journal, evidence.AcceptedJob
	id := dnsengineartifact.SwitchIdentity{
		RequestID: journal.MutationRequestID, OwnerID: journal.MutationOwnerID,
		Target: journal.TargetEngine, Qualifier: journal.ManifestQualifier,
	}
	if err := id.Validate(); err != nil {
		return err
	}
	if job.RequestID != id.RequestID || job.OwnerID != id.OwnerID ||
		job.Kind != "dns_engine_switch" || job.Target != string(id.Target) ||
		job.PackageName != id.Qualifier || evidence.Observation.RequestID != id.RequestID {
		return errors.New("accepted DNS worker job differs from the exact journal")
	}
	if evidence.Observation.Status == dnsenginerecovery.EvidenceTerminalRolledBack {
		if journal.Phase != dnsengineartifact.SwitchPhaseRolledBack ||
			job.WorkerPID != 0 || job.WorkerStarted != "" || job.WorkerCommand != "" {
			return errors.New("terminal DNS rollback still records a worker or wrong checkpoint")
		}
		return ctx.Err()
	}
	worker, err := dnsenginerecovery.InspectAcceptedWorker(id, &job, time.Now().UTC())
	if err != nil {
		return err
	}
	if worker == dnsenginerecovery.WorkerStillAlive {
		return errors.New("the exact accepted DNS worker is still alive")
	}
	return ctx.Err()
}

// excludeInstalledReleasedDNSInverseWorker is the worker exclusion of
// recover-dns-bind-switch, recover-dns-bind-adoption and
// recover-dns-pdns-adoption. Beyond the shared exclusion it accepts only the
// Agent's deliberate release of this exact request: the secured read bound
// that job to an idle ledger (no active request of any kind), the job records
// no worker or lease, and the Agent wrote it after proving the worker gone
// under the host lock this command now holds. Any worker must hold that lock
// to change DNS. Other commands keep excludeInstalledDNSInverseWorker, which
// refuses every released job.
func excludeInstalledReleasedDNSInverseWorker(ctx context.Context, evidence dnsenginerecovery.SwitchEvidence) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if evidence.Observation.Status != dnsenginerecovery.EvidenceReleasedUndecided {
		return excludeInstalledDNSInverseWorker(ctx, evidence)
	}
	if !dnsenginerecovery.AgentReleasedDNSInverseEvidence(evidence) {
		return errors.New("released DNS job is not the Agent's deliberate release of this exact request")
	}
	return ctx.Err()
}

// requirePDNSAdoptionInverseNativeProof admits only the part of the frozen
// source that is currently proved on the wire. A database row absence does not
// prove that the running daemon has stopped serving a deleted zone.
func requirePDNSAdoptionInverseNativeProof(proof pdnsAdoptionNativeProof) error {
	if proof.DeletedSOA < 0 || proof.DeletedSOAVerified < 0 || proof.DeletedSOA != proof.DeletedSOAVerified {
		return errors.New("PowerDNS adoption inverse lacks exact deleted-zone absence proof")
	}
	return nil
}

type pdnsInverseDurableEffect string

const (
	pdnsInverseReceiptRemoved      pdnsInverseDurableEffect = "receipt-removed"
	pdnsInverseCheckpointPublished pdnsInverseDurableEffect = "checkpoint-published"
	pdnsInverseVerdictPublished    pdnsInverseDurableEffect = "verdict-published"
	pdnsInverseJournalRetired      pdnsInverseDurableEffect = "journal-retired"
)

// The optional callback is used only by a build-tagged disposable native
// test. Installed recovery always passes nil; there is no runtime fault flag.
type pdnsInverseAfterEffect func(pdnsInverseDurableEffect)

// errPDNSInverseTerminalLedgerObserved means the same request has a secured,
// exact owner-recovery failed verdict after journal retirement. It is not proof
// of current native DNS health or of the retired frozen journal's preimage.
var errPDNSInverseTerminalLedgerObserved = errors.New("exact DNS rollback verdict is recorded but the retired journal prevents native preimage reproof")

// ownerRecoveryRollbackVerdictJob recognizes the exact terminal verdict that
// PublishExactDNSRollbackVerdict records for an owner recovery run.
func ownerRecoveryRollbackVerdictJob(job transport.ServiceMutationJob) bool {
	return job.Status == servicemutationledger.StatusFailed &&
		job.Phase == "interrupted" &&
		job.ErrorCode == "dns_engine_switch_rolled_back_by_owner_recovery" &&
		job.ErrorMessage == "The interrupted DNS engine switch was rolled back to the verified previous state."
}

func classifyJournalAbsentPDNSInverseLedger(ledger servicemutationledger.Ledger, requestID string) error {
	job := ledger.Jobs[requestID]
	if job == nil {
		return errors.New("exact DNS request is absent from the terminal ledger")
	}
	id := dnsengineartifact.SwitchIdentity{
		RequestID: job.RequestID,
		OwnerID:   job.OwnerID,
		Target:    transport.DNSEngine(job.Target),
		Qualifier: job.PackageName,
	}
	if id.RequestID != requestID || id.Target != transport.DNSEnginePowerDNS ||
		!id.TerminalRolledBackJob(ledger) ||
		job.Phase != "interrupted" ||
		job.ErrorCode != "dns_engine_switch_rolled_back_by_owner_recovery" ||
		job.ErrorMessage != "The interrupted DNS engine switch was rolled back to the verified previous state." {
		return errors.New("journal-absent DNS job does not prove this exact owner-recovery verdict")
	}
	return nil
}

func journalAbsentPDNSInverseOutcome(
	ctx context.Context, root string, owner servicemutationledger.FileOwner, requestID string,
) error {
	ledger, err := readJournalAbsentDNSLedger(ctx, root, owner, requestID)
	if err != nil {
		return fmt.Errorf("journal-absent DNS result is unknown: %w", err)
	}
	if err := classifyJournalAbsentPDNSInverseLedger(ledger, requestID); err != nil {
		if journalFreeAgentReleasedDNSJob(ledger, requestID, transport.DNSEnginePowerDNS) {
			return releasedDNSInverseReconciledOutcome(requestID)
		}
		return fmt.Errorf("journal-absent DNS result is unknown: %w", err)
	}
	return fmt.Errorf("%w: request %s; inspect native PowerDNS before treating current service as recovered",
		errPDNSInverseTerminalLedgerObserved, requestID)
}

// completeInstalledPDNSAdoptionInverse binds the already durable rollback
// decision to fixed installed paths and the native owner PowerDNS proof. It is
// exposed only through the exact owner recovery command for this selected
// adoption rollback. It never installs
// or updates CelikPanel or changes the owner's native PowerDNS service.
func completeInstalledPDNSAdoptionInverse(ctx context.Context, requestID string) error {
	return completeInstalledPDNSAdoptionInverseWithAfterEffect(ctx, requestID, nil)
}

func completeInstalledPDNSAdoptionInverseWithAfterEffect(
	ctx context.Context, requestID string, afterEffect pdnsInverseAfterEffect,
) error {
	if ctx == nil || !servicemutationledger.ValidIdentity(requestID) {
		return errors.New("PowerDNS adoption inverse requires an exact request ID and context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if os.Geteuid() != 0 {
		return errors.New("PowerDNS adoption inverse requires root or authorized sudo")
	}
	groupID, err := localCelikPanelGroupID("/etc/group")
	if err != nil {
		return fmt.Errorf("verify installed CelikPanel group: %w", err)
	}
	owner := servicemutationledger.FileOwner{UID: 0, GID: groupID}
	// Owner recovery can run after a reboot with both management services
	// disabled. The release lock is persistent; only this mutation command may
	// prepare the missing /run host-lock infrastructure under that release lock.
	releasePath := "/var/lib/celikpanel-release-transaction/transaction.lock"
	release, err := hostmutationlock.AcquireExisting(releasePath, hostmutationlock.Owner{})
	if err != nil {
		return fmt.Errorf("hold release lock: %w", err)
	}
	locks := &dnsObservationLocks{release: release}
	if err := hostmutationlock.VerifyInherited(releasePath, int(release.Fd()), hostmutationlock.Owner{}); err != nil {
		locks.Close()
		return fmt.Errorf("release lock changed before host lock preparation: %w", err)
	}
	hostOwner := hostmutationlock.Owner{UID: owner.UID, GID: owner.GID}
	locks.host, err = hostmutationlock.AcquireOrCreateOwnerRecovery(hostOwner)
	if err != nil {
		locks.Close()
		return fmt.Errorf("hold DNS host lock for owner recovery: %w", err)
	}
	defer locks.Close()
	verifyLocks := func() error {
		if err := hostmutationlock.VerifyInherited(releasePath, int(locks.release.Fd()), hostmutationlock.Owner{}); err != nil {
			return fmt.Errorf("release lock changed: %w", err)
		}
		if err := hostmutationlock.VerifyHeldOwnerRecovery(locks.host, hostOwner); err != nil {
			return fmt.Errorf("DNS host lock changed: %w", err)
		}
		return nil
	}
	root := hostingpath.ServiceMutationStateRoot()
	policy := installedDNSJournalPolicy(owner.GID)
	return dnsenginerecovery.CompletePDNSAdoptionInverse(ctx, pdnsAdoptionInverseOps(
		root, owner, policy, requestID, verifyLocks,
		func(ctx context.Context, journal dnsengineartifact.SwitchJournalV1) (pdnsAdoptionNativeProof, error) {
			return proveInstalledPDNSAdoptionNative(ctx, policy, journal)
		},
		afterEffect,
	))
}

// pdnsAdoptionInverseOps binds recover-dns-pdns-adoption's durable effects to
// one private state root. The installed command passes the fixed root, its held
// release/host locks and the native PowerDNS proof; nothing here acquires a
// lock or chooses a path from persisted evidence.
func pdnsAdoptionInverseOps(
	root string, owner servicemutationledger.FileOwner, policy dnsengineartifact.JournalPolicy,
	requestID string, verifyLocks func() error,
	proveNative func(context.Context, dnsengineartifact.SwitchJournalV1) (pdnsAdoptionNativeProof, error),
	afterEffect pdnsInverseAfterEffect,
) dnsenginerecovery.PDNSAdoptionInverseOps {
	return dnsenginerecovery.PDNSAdoptionInverseOps{
		Read: func(ctx context.Context) (dnsenginerecovery.SwitchEvidence, bool, error) {
			if err := ctx.Err(); err != nil {
				return dnsenginerecovery.SwitchEvidence{}, false, err
			}
			if err := verifyLocks(); err != nil {
				return dnsenginerecovery.SwitchEvidence{}, false, err
			}
			evidence, present, err := dnsenginerecovery.ReadSwitchEvidence(root, owner, policy, time.Now().UTC())
			if err != nil {
				return evidence, present, err
			}
			if !present {
				return evidence, false, journalAbsentPDNSInverseOutcome(ctx, root, owner, requestID)
			}
			if evidence.Journal.MutationRequestID != requestID {
				return dnsenginerecovery.SwitchEvidence{}, true, errors.New("a different DNS operation owns the retained journal")
			}
			return evidence, true, nil
		},
		ExcludeWorker: excludeInstalledReleasedDNSInverseWorker,
		ProveNative: func(ctx context.Context, journal dnsengineartifact.SwitchJournalV1) error {
			if err := verifyLocks(); err != nil {
				return err
			}
			proof, err := proveNative(ctx, journal)
			if err != nil {
				return err
			}
			return requirePDNSAdoptionInverseNativeProof(proof)
		},
		RemoveState: func(ctx context.Context, journal dnsengineartifact.SwitchJournalV1) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := verifyLocks(); err != nil {
				return err
			}
			if err := dnsenginerecovery.RemoveExactPDNSAdoptionTargetReceipt(policy, owner, journal); err != nil {
				return err
			}
			if afterEffect != nil {
				afterEffect(pdnsInverseReceiptRemoved)
			}
			return nil
		},
		WritePhase: func(ctx context.Context, before, after dnsengineartifact.SwitchJournalV1) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := verifyLocks(); err != nil {
				return err
			}
			if err := dnsenginerecovery.ReplaceRollbackJournalPhase(policy, owner, before, after); err != nil {
				return err
			}
			if afterEffect != nil {
				afterEffect(pdnsInverseCheckpointPublished)
			}
			return nil
		},
		PublishFailed: func(ctx context.Context, journal dnsengineartifact.SwitchJournalV1) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := verifyLocks(); err != nil {
				return err
			}
			if err := dnsenginerecovery.PublishExactDNSRollbackVerdict(policy, owner, journal, time.Now().UTC()); err != nil {
				return err
			}
			if afterEffect != nil {
				afterEffect(pdnsInverseVerdictPublished)
			}
			return nil
		},
		RemoveJournal: func(ctx context.Context, journal dnsengineartifact.SwitchJournalV1) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := verifyLocks(); err != nil {
				return err
			}
			if err := dnsenginerecovery.RemoveExactRollbackJournal(policy, owner, journal); err != nil {
				return err
			}
			if afterEffect != nil {
				afterEffect(pdnsInverseJournalRetired)
			}
			return nil
		},
	}
}
