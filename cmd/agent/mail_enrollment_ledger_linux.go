//go:build linux

package main

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"

	"github.com/alicelik/celikpanel/internal/agentnativecontract"
	"github.com/alicelik/celikpanel/internal/recoveryruntime"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
)

// New admission through this boundary is not exposed through RPC or the renewal
// hook. The independent CLI can only resume an existing exact reservation. The
// setup dispatcher must supply authenticated source and accepted owner intent;
// native result proofs remain mandatory. An Agent declaration alone is not
// authorization. No historical release may be retroactively certified.
type mailEnrollmentAuthority struct {
	identity     servicemutationledger.MailEnrollmentIdentity
	agent        *recoveryruntime.CompatibleMailAgent
	verifyIntent func() error
	verifyResult func() error
}

// writeMailEnrollmentReservation uses the existing durable ledger writer, not a
// second pending marker. The caller holds release fd9 then host fd8 for its whole
// native operation. This writer additionally takes the common publication lock.
// No lease timer, generic orphan path or polling request can clear a reservation.
func writeMailEnrollmentReservation(stateDir, hostPath string, authority mailEnrollmentAuthority, next string) error {
	return writeMailEnrollmentReservationAt(stateDir, hostPath, authority, next, func() error { return recoveryruntime.VerifyPreflightBoundary(9) }, nil)
}

func writeMailEnrollmentReservationAt(stateDir, hostPath string, authority mailEnrollmentAuthority, next string, verifyRelease func() error, fault func(string) error) error {
	if authority.agent == nil || authority.verifyIntent == nil || verifyRelease == nil ||
		!filepath.IsAbs(stateDir) || filepath.Clean(stateDir) != stateDir ||
		!filepath.IsAbs(hostPath) || filepath.Clean(hostPath) != hostPath {
		return servicemutationledger.ErrMailEnrollment
	}
	terminal := next == servicemutationledger.MailEnrollmentPublished || next == servicemutationledger.MailEnrollmentRestored
	if terminal && authority.verifyResult == nil {
		return servicemutationledger.ErrMailEnrollment
	}
	verify := func() error {
		if err := verifyRelease(); err != nil {
			return err
		}
		if err := verifyInheritedServiceMutationFileLockFD(hostPath, 8); err != nil {
			return err
		}
		if authority.agent.Contract.MailEnrollmentPolicy != agentnativecontract.MailEnrollmentPolicy {
			return agentnativecontract.ErrContract
		}
		if err := authority.agent.Revalidate(); err != nil {
			return err
		}
		return authority.verifyIntent()
	}
	if err := verify(); err != nil {
		return err
	}
	publication, err := acquireExistingServiceMutationFileLock(serviceMutationLedgerPublicationLockFile(hostPath))
	if err != nil {
		return err
	}
	defer publication.Close()
	var directory, original unix.Stat_t
	ledgerPath := filepath.Join(stateDir, serviceMutationLedgerFileName)
	if unix.Lstat(stateDir, &directory) != nil || unix.Lstat(ledgerPath, &original) != nil {
		return servicemutationledger.ErrMailEnrollment
	}
	outerVerify := verify
	verify = func() error {
		if err := outerVerify(); err != nil {
			return err
		}
		if err := verifyInheritedServiceMutationFileLockFD(serviceMutationLedgerPublicationLockFile(hostPath), int(publication.file.Fd())); err != nil {
			return err
		}
		var named unix.Stat_t
		if unix.Lstat(stateDir, &named) != nil || named.Dev != directory.Dev || named.Ino != directory.Ino || named.Mode != directory.Mode || named.Uid != directory.Uid || named.Gid != directory.Gid {
			return servicemutationledger.ErrMailEnrollment
		}
		return nil
	}
	verifyOriginal := func() error {
		var named unix.Stat_t
		if unix.Lstat(ledgerPath, &named) != nil || named.Dev != original.Dev || named.Ino != original.Ino || named.Mode != original.Mode || named.Uid != original.Uid || named.Gid != original.Gid || named.Nlink != original.Nlink || named.Size != original.Size || named.Mtim != original.Mtim || named.Ctim != original.Ctim {
			return servicemutationledger.ErrMailEnrollment
		}
		return verify()
	}
	// Never construct the general manager: its startup recovery has different
	// authority. Read the established canonical ledger without initializing it.
	manager := &serviceMutationManager{ledgerPath: filepath.Join(stateDir, serviceMutationLedgerFileName), lockPath: hostPath, now: func() time.Time { return time.Now().UTC() }}
	if err = manager.load(); err != nil {
		return err
	}
	if err = verifyOriginal(); err != nil {
		return err
	}
	before, err := encodeServiceMutationLedger(&manager.ledger)
	if err != nil {
		return err
	}
	// Reuse the common writer's exact canonical temporary-file cleanup. Unknown
	// or incomplete stages remain refusal; they never become accepted operations.
	if err = cleanupAbandonedServiceMutationWriteStages(stateDir); err != nil {
		return err
	}
	if err = manager.observeRetainedMutationEvidenceLocked(); err != nil {
		return err
	}
	for _, job := range manager.ledger.Jobs {
		if job.Status == serviceMutationStatusPending {
			return errMailRenewalRecoveryRequired
		}
	}
	state, stateErr := servicemutationledger.MailEnrollmentState(&manager.ledger, authority.identity)
	var candidate servicemutationledger.Ledger
	now := manager.now()
	switch {
	case next == servicemutationledger.MailEnrollmentForward && stateErr != nil:
		busy, e := packageManagerMutationBusy()
		if e != nil {
			return e
		}
		if busy {
			return errServiceMutationHostBusy
		}
		candidate, err = servicemutationledger.AdmitMailEnrollment(&manager.ledger, authority.identity, now)
	case stateErr != nil:
		return stateErr
	case state == next:
		if err = verifyOriginal(); err != nil {
			return err
		}
		// Exact retry syncs the visible reservation after an uncertain prior sync;
		// it never changes bytes. Terminal retries still prove today's native state.
		if err = verify(); err != nil {
			return err
		}
		if err = syncServiceMutationDirectory(manager.ledgerPath); err != nil {
			return err
		}
		if err = verify(); err != nil {
			return err
		}
		if terminal {
			return authority.verifyResult()
		}
		return nil
	default:
		candidate, err = servicemutationledger.AdvanceMailEnrollment(&manager.ledger, authority.identity, next, now)
	}
	if err != nil {
		return err
	}
	if terminal {
		if err = authority.verifyResult(); err != nil {
			return err
		}
	}
	// Keep the complete retained ledger. Disable common history pruning and
	// refuse the size limit before any write rather than discarding evidence.
	if _, err = encodeServiceMutationLedger(&candidate); err != nil {
		return err
	}
	manager.ledger = candidate
	manager.retainAllHistory = true
	manager.writeFault = func(point string) error {
		if fault != nil {
			if e := fault(point); e != nil {
				return e
			}
		}
		if e := verify(); e != nil {
			return e
		}
		if point == serviceMutationWriteFaultBeforeRename {
			if e := verifyOriginal(); e != nil {
				return e
			}
			raw, found, e := readSecureServiceMutationLedger(manager.ledgerPath, serviceMutationLedgerMaxSize)
			if e != nil || !found || !bytes.Equal(raw, before) {
				return errors.Join(servicemutationledger.ErrMailEnrollment, e)
			}
			if terminal {
				return authority.verifyResult()
			}
		}
		return nil
	}
	if err = manager.writeProtectedLocked(authority.identity.RequestID); err != nil {
		return fmt.Errorf("persist native mail enrollment reservation: %w", err)
	}
	if err = verify(); err != nil {
		return err
	}
	// A failed directory sync must never release native work just because the
	// rename is visible. Only the writer's complete success reaches this readback.
	got, err := manager.loadLedgerFromDisk()
	if err != nil {
		return err
	}
	actual, err := servicemutationledger.MailEnrollmentState(&got, authority.identity)
	if err != nil || actual != next {
		return servicemutationledger.ErrMailEnrollment
	}
	return nil
}
