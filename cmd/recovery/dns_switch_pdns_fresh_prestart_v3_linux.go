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
	if len(args) != 3 || args[0] != ownerPDNSFreshPrestartV3Command || args[1] != "--request-id" ||
		!servicemutationledger.ValidIdentity(args[2]) {
		fmt.Fprintln(diagnostic, "Usage: recovery recover-dns-pdns-fresh-prestart --request-id <32 lowercase hex characters>")
		return exitUsage
	}
	if uid != 0 {
		fmt.Fprintln(diagnostic, "Run as root or authorized sudo; no DNS operation was started.")
		return exitNotOwner
	}
	runtime, err := recoveryruntime.VerifiedLauncherRuntime()
	if err == nil {
		defer runtime.Close()
		err = runtime.VerifyExecutingBinary()
	}
	if err == nil {
		err = completeInstalledFreshPDNSPrestartV3(context.Background(), args[2])
	}
	if err != nil {
		fmt.Fprintln(diagnostic, "Fresh PowerDNS prestart recovery did not reach a terminal verdict. Preserve the journal and ledger; inspect recovery dns-switch-status --quiesced --request-id "+args[2]+". A running or changed target requires forward reconciliation, not deletion. Reason: "+err.Error())
		return exitUnavailable
	}
	fmt.Fprintln(out, "The exact fresh PowerDNS prestart operation was restored and its rollback verdict recorded.")
	return exitOK
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
		if !present || e.Journal.MutationRequestID != request || e.Journal.Schema != dnsengineartifact.SwitchJournalSchemaV3 {
			return dnsenginerecovery.SwitchEvidence{}, errors.New("exact v3 request journal is absent or owned by another operation")
		}
		if err := excludeInstalledDNSInverseWorker(ctx, e); err != nil {
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
	if err := dnsenginerecovery.PublishExactDNSRollbackVerdict(policy, owner, j, time.Now().UTC()); err != nil {
		return err
	}
	if err := verifyLocks(); err != nil {
		return err
	}
	return dnsenginerecovery.RemoveExactRollbackJournal(policy, owner, j)
}
