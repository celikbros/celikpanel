//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"time"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/dnsenginerecovery"
	"github.com/alicelik/celikpanel/internal/hostingpath"
	"github.com/alicelik/celikpanel/internal/hostmutationlock"
	"github.com/alicelik/celikpanel/internal/recoveryruntime"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
)

const ownerPDNSFreshPrestartV3Command = "recover-dns-pdns-fresh-prestart"

func runOwnerPDNSFreshPrestartV3(args []string, uid int, out, diagnostic io.Writer) int {
	return dispatchOwnerPDNSFreshPrestartV3(args, uid, func(ctx context.Context, request string) error {
		runtime, err := recoveryruntime.VerifiedLauncherRuntime()
		if err != nil {
			return err
		}
		defer runtime.Close()
		if err := runtime.VerifyExecutingBinary(); err != nil {
			return err
		}
		return completeInstalledFreshPDNSPrestartV3(ctx, request)
	}, out, diagnostic)
}

func dispatchOwnerPDNSFreshPrestartV3(
	args []string, uid int, inverse func(context.Context, string) error, out, diagnostic io.Writer,
) int {
	if len(args) != 3 || args[0] != ownerPDNSFreshPrestartV3Command || args[1] != "--request-id" ||
		!servicemutationledger.ValidIdentity(args[2]) {
		fmt.Fprintln(diagnostic, "Usage: recovery recover-dns-pdns-fresh-prestart --request-id <32 lowercase hex characters>")
		return exitUsage
	}
	if uid != 0 {
		fmt.Fprintln(diagnostic, "Run as root or authorized sudo; no DNS operation was started.")
		return exitNotOwner
	}
	request := args[2]
	if inverse == nil {
		fmt.Fprintln(diagnostic, "The independent DNS recovery executor is unavailable. Preserve the same request and its evidence; inspect the installed recovery runtime before retrying.")
		return exitUnavailable
	}
	err := inverse(context.Background(), request)
	if err == nil {
		if _, writeErr := fmt.Fprintln(out, "The exact fresh PowerDNS prestart operation was restored and its rollback verdict recorded."); writeErr != nil {
			return exitOutput
		}
		return exitOK
	}
	// This command admits the Agent's deliberate release, so its reconciled
	// re-run is complete too, exactly as for the three V2/V1 owner inverses.
	if code, complete := writeCompletedDNSInverse(err, "en", request, out); complete {
		return code
	}
	fmt.Fprintln(diagnostic, "Fresh PowerDNS prestart recovery did not reach a terminal verdict. Preserve the journal and ledger; inspect recovery dns-switch-status --quiesced --request-id "+request+". A running or changed target requires forward reconciliation, not deletion. Reason: "+err.Error())
	return exitUnavailable
}

func completeInstalledFreshPDNSPrestartV3(parent context.Context, request string) error {
	if parent == nil || os.Geteuid() != 0 || !servicemutationledger.ValidIdentity(request) {
		return errors.New("v3 prestart inverse requires root and an exact request")
	}
	ctx, cancel := context.WithTimeout(parent, 2*time.Minute)
	defer cancel()
	group, err := localCelikPanelGroupID("/etc/group")
	if err != nil {
		return err
	}
	owner := servicemutationledger.FileOwner{UID: 0, GID: group}
	releasePath := "/var/lib/celikpanel-release-transaction/transaction.lock"
	release, err := hostmutationlock.AcquireExisting(releasePath, hostmutationlock.Owner{})
	if err != nil {
		return err
	}
	locks := &dnsObservationLocks{release: release}
	if err := hostmutationlock.VerifyInherited(releasePath, int(release.Fd()), hostmutationlock.Owner{}); err != nil {
		locks.Close()
		return err
	}
	hostOwner := hostmutationlock.Owner{UID: 0, GID: group}
	locks.host, err = hostmutationlock.AcquireOrCreateOwnerRecovery(hostOwner)
	if err != nil {
		locks.Close()
		return err
	}
	defer locks.Close()
	verifyLocks := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := hostmutationlock.VerifyInherited(releasePath, int(locks.release.Fd()), hostmutationlock.Owner{}); err != nil {
			return err
		}
		return hostmutationlock.VerifyHeldOwnerRecovery(locks.host, hostOwner)
	}
	policy := installedDNSJournalPolicy(group)
	root := hostingpath.ServiceMutationStateRoot()
	read := func() (dnsenginerecovery.SwitchEvidence, error) {
		if err := verifyLocks(); err != nil {
			return dnsenginerecovery.SwitchEvidence{}, err
		}
		e, present, err := dnsenginerecovery.ReadSwitchEvidence(root, owner, policy, time.Now().UTC())
		if err != nil {
			return e, err
		}
		if !present {
			// A re-run after the journal was retired: only the exact
			// owner-recovery verdict of this request counts as complete.
			return dnsenginerecovery.SwitchEvidence{}, journalAbsentPDNSInverseOutcome(ctx, root, owner, request)
		}
		if e.Journal.MutationRequestID != request || e.Journal.Schema != dnsengineartifact.SwitchJournalSchemaV3 {
			return dnsenginerecovery.SwitchEvidence{}, errors.New("exact v3 request journal is absent or owned by another operation")
		}
		if err := excludeInstalledFreshPrestartV3Worker(ctx, e); err != nil {
			return e, err
		}
		return e, nil
	}
	assess := func(expected dnsengineartifact.SwitchJournalV1) (freshPDNSRecoveryShapeV3, error) {
		e, err := read()
		if err != nil {
			return freshPDNSRecoveryUnknownV3, err
		}
		if !reflect.DeepEqual(e.Journal, expected) {
			return freshPDNSRecoveryUnknownV3, errors.New("v3 journal changed before native proof")
		}
		shape, err := assessInstalledFreshPrimaryV3(ctx, policy, expected)
		if err != nil {
			return shape, err
		}
		switch shape {
		case freshPDNSRecoveryIntentCleanV3, freshPDNSRecoveryStagedV3, freshPDNSRecoveryRenamedV3, freshPDNSRecoveryRestoredV3:
			return shape, nil
		default:
			return freshPDNSRecoveryUnknownV3, errors.New("PowerDNS may have started; prestart inverse is forbidden")
		}
	}
	e, err := read()
	if err != nil {
		return err
	}
	j := e.Journal
	shape, err := assess(j)
	if err != nil {
		return err
	}
	if j.Phase != dnsengineartifact.SwitchPhaseRollingBack && j.Phase != dnsengineartifact.SwitchPhaseRollingBackTargetEnable && j.Phase != dnsengineartifact.SwitchPhaseRolledBack {
		if shape == freshPDNSRecoveryRestoredV3 {
			return errors.New("restored shape precedes rollback decision")
		}
		next := j
		if j.Phase == dnsengineartifact.SwitchPhaseTargetEnableIntent {
			next.Phase = dnsengineartifact.SwitchPhaseRollingBackTargetEnable
		} else {
			next.Phase = dnsengineartifact.SwitchPhaseRollingBack
		}
		if err := dnsenginerecovery.ReplaceFreshPrimaryJournalV3(policy, owner, j, next); err != nil {
			return err
		}
		j = next
	}
	guard := func(context.Context) error { _, err := assess(j); return err }
	if j.Phase != dnsengineartifact.SwitchPhaseRolledBack {
		if j.PDNSFreshPlan.Candidate != nil {
			if j.Phase == dnsengineartifact.SwitchPhaseRollingBackTargetEnable {
				if err := restorePDNSTargetUnitV3(ctx, j.TargetUnitsBefore[0], guard); err != nil {
					return err
				}
			}
			if current, err := assess(j); err != nil {
				return err
			} else if current == freshPDNSRecoveryRenamedV3 {
				if err := dnsenginerecovery.RestoreFreshPrimaryRenamedV3(policy, j, func() error { return guard(ctx) }); err != nil {
					return err
				}
			}
			pdnsGID, err := localServiceGroupID("/etc/group", "pdns")
			if err != nil {
				return err
			}
			if err := dnsenginerecovery.RestoreInstalledPDNSFreshConfigsV3(ctx, policy, j, pdnsGID, guard); err != nil {
				return err
			}
			if current, err := assess(j); err != nil {
				return err
			} else if current == freshPDNSRecoveryStagedV3 {
				if err := dnsenginerecovery.RemoveFreshPrimaryStagedV3(policy, j, func() error { return guard(ctx) }); err != nil {
					return err
				}
			}
		}
		if shape, err = assess(j); err != nil || (shape != freshPDNSRecoveryRestoredV3 && shape != freshPDNSRecoveryIntentCleanV3) {
			return errors.Join(errors.New("v3 prestart inverse is not exact"), err)
		}
		next := j
		next.Phase = dnsengineartifact.SwitchPhaseRolledBack
		if err := dnsenginerecovery.ReplaceFreshPrimaryJournalV3(policy, owner, j, next); err != nil {
			return err
		}
		j = next
	}
	if shape, err = assess(j); err != nil || (shape != freshPDNSRecoveryRestoredV3 && shape != freshPDNSRecoveryIntentCleanV3) {
		return errors.Join(errors.New("v3 restored native state changed before terminal verdict"), err)
	}
	current, err := read()
	if err != nil || !reflect.DeepEqual(current.Journal, j) {
		return errors.Join(errors.New("v3 journal changed before terminal verdict"), err)
	}
	if err := publishFreshPDNSPrestartVerdictV3(current, func() error {
		return dnsenginerecovery.PublishExactDNSRollbackVerdict(policy, owner, j, time.Now().UTC())
	}); err != nil {
		return err
	}
	if err := verifyLocks(); err != nil {
		return err
	}
	return dnsenginerecovery.RemoveExactRollbackJournal(policy, owner, j)
}

// publishFreshPDNSPrestartVerdictV3 publishes this command's rollback verdict
// for an active job. A terminal job beside the rolled-back journal is kept
// when it is exactly an owner recovery run's verdict (an earlier run of this
// command published it and was interrupted before retiring the journal) or
// the Agent's own deliberate release of this request, which already is the
// operation's terminal ledger verdict and is never rewritten. Any other
// terminal result is refused.
func publishFreshPDNSPrestartVerdictV3(current dnsenginerecovery.SwitchEvidence, publish func() error) error {
	if current.Observation.Status != dnsenginerecovery.EvidenceTerminalRolledBack {
		return publish()
	}
	if current.Journal.Phase != dnsengineartifact.SwitchPhaseRolledBack ||
		!(ownerRecoveryRollbackVerdictJob(current.AcceptedJob) ||
			freshPrestartV3ReleasedJob(current)) {
		return errors.New("the terminal ledger result of this v3 request was not recorded by an owner recovery run or the Agent's release; this command does not retire its journal")
	}
	return nil
}

// freshPrestartV3ReleasedJob reports whether the accepted job is the Agent's
// deliberate release of exactly this journal's request.
func freshPrestartV3ReleasedJob(e dnsenginerecovery.SwitchEvidence) bool {
	j, job := e.Journal, e.AcceptedJob
	return e.Observation.ReleaseReason == dnsengineartifact.ReleasedNativeUnknownCode &&
		job.RequestID == j.MutationRequestID && job.OwnerID == j.MutationOwnerID &&
		job.Target == string(j.TargetEngine) && job.PackageName == j.ManifestQualifier &&
		dnsenginerecovery.AgentDeliberateReleaseJob(job)
}

// excludeInstalledFreshPrestartV3Worker is this command's worker exclusion.
// Beyond the shared exclusion it admits only the Agent's deliberate release
// of this exact request with its journal still in the pre-start shape: the
// Agent wrote that release after proving the worker gone under the host lock
// this command now holds, and it records no worker or lease. Every native,
// lock and owner-change proof of the command still runs.
func excludeInstalledFreshPrestartV3Worker(ctx context.Context, e dnsenginerecovery.SwitchEvidence) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if e.Observation.Status != dnsenginerecovery.EvidenceReleasedUndecided {
		return excludeInstalledDNSInverseWorker(ctx, e)
	}
	if !dnsenginerecovery.AgentReleasedFreshPrimaryPrestartEvidenceV3(e) {
		return errors.New("released DNS job is not the Agent's deliberate release of this exact pre-start request")
	}
	return ctx.Err()
}
