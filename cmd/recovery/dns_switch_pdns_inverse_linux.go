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

// completeInstalledPDNSAdoptionInverse binds the already durable rollback
// decision to fixed installed paths and the native owner PowerDNS proof. It is
// intentionally not exposed as a CLI command until disposable native
// interruption/reboot and owner-edit acceptance has passed. It never installs
// or updates CelikPanel or changes the owner's native PowerDNS service.
func completeInstalledPDNSAdoptionInverse(ctx context.Context, requestID string) error {
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
	locks, err := acquireDNSObservationLocks(
		"/var/lib/celikpanel-release-transaction/transaction.lock",
		"/run/celikpanel/service-mutation.lock",
		hostmutationlock.Owner{UID: owner.UID, GID: owner.GID},
	)
	if err != nil {
		return fmt.Errorf("hold release and DNS host locks: %w", err)
	}
	defer locks.Close()
	root := hostingpath.ServiceMutationStateRoot()
	policy := installedDNSJournalPolicy(owner.GID)
	return dnsenginerecovery.CompletePDNSAdoptionInverse(ctx, dnsenginerecovery.PDNSAdoptionInverseOps{
		Read: func(ctx context.Context) (dnsenginerecovery.SwitchEvidence, bool, error) {
			if err := ctx.Err(); err != nil {
				return dnsenginerecovery.SwitchEvidence{}, false, err
			}
			evidence, present, err := dnsenginerecovery.ReadSwitchEvidence(root, owner, policy, time.Now().UTC())
			if err != nil || !present {
				return evidence, present, err
			}
			if evidence.Journal.MutationRequestID != requestID {
				return dnsenginerecovery.SwitchEvidence{}, true, errors.New("a different DNS operation owns the retained journal")
			}
			return evidence, true, nil
		},
		ExcludeWorker: excludeInstalledDNSInverseWorker,
		ProveNative: func(ctx context.Context, journal dnsengineartifact.SwitchJournalV1) error {
			_, err := proveInstalledPDNSAdoptionNative(ctx, policy, journal)
			return err
		},
		RemoveState: func(ctx context.Context, journal dnsengineartifact.SwitchJournalV1) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			return dnsenginerecovery.RemoveExactPDNSAdoptionTargetReceipt(policy, owner, journal)
		},
		WritePhase: func(ctx context.Context, before, after dnsengineartifact.SwitchJournalV1) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			return dnsenginerecovery.ReplaceRollbackJournalPhase(policy, owner, before, after)
		},
		PublishFailed: func(ctx context.Context, journal dnsengineartifact.SwitchJournalV1) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			return dnsenginerecovery.PublishExactDNSRollbackVerdict(policy, owner, journal, time.Now().UTC())
		},
		RemoveJournal: func(ctx context.Context, journal dnsengineartifact.SwitchJournalV1) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			return dnsenginerecovery.RemoveExactRollbackJournal(policy, owner, journal)
		},
	})
}
