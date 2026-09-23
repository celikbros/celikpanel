//go:build linux

package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/alicelik/celikpanel/internal/hostingpath"
	"github.com/alicelik/celikpanel/internal/mailrenewalkit"
	"github.com/alicelik/celikpanel/internal/recoveryruntime"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
)

const mailEnrollmentJournalRoot = "/var/lib/celikpanel-mail-renewal/enrollment"

// One-shot recorded-operation consumer. It requires the existing release fd9 and
// host fd8; it cannot acquire a new lease, prepare evidence, change direction or
// initialize a missing ledger. The detached worker supplies these locks.
func resumeIndependentMailEnrollment(ctx context.Context, requestID string) error {
	return runIndependentMailEnrollment(ctx, requestID, "", "")
}

// An explicit root-owner invocation supplies new intent. Recorded requests always
// take the recorded path; even a repeated start cannot recreate their scope.
func runIndependentMailEnrollment(ctx context.Context, requestID, ownerID, target string) error {
	return runIndependentMailEnrollmentMode(ctx, requestID, ownerID, target, false)
}

func runIndependentMailEnrollmentMode(ctx context.Context, requestID, ownerID, target string, automatic bool) error {
	if ctx == nil || automatic && (ownerID != "" || target != "") || !validMutationIdentity(requestID) || !mailRenewalOnlyBuild || os.Geteuid() != 0 ||
		(ownerID != "" && (!validMutationIdentity(ownerID) || !recoveryruntime.ValidDigest(target))) || (ownerID == "" && target != "") {
		return servicemutationledger.ErrMailEnrollment
	}
	gid, ok := lookupGroupID("celikpanel")
	if !ok || gid < 0 || uint32(gid) != serviceMutationRequiredOwnerGID {
		return errors.New("retained mail service group identity is unavailable")
	}
	const host = "/run/celikpanel/service-mutation.lock"
	if err := recoveryruntime.VerifyPreflightBoundary(9); err != nil {
		return err
	}
	if err := verifyInheritedServiceMutationFileLockFD(host, 8); err != nil {
		return err
	}
	agent, err := recoveryruntime.InspectCompatibleMailAgent("/opt/celikpanel/bin")
	if err != nil {
		return err
	}
	defer agent.Close()
	// The caller cannot substitute an unrelated consumer for the helper bound in
	// the accepted release. Reading this executable never executes an Agent probe.
	self, err := os.Open("/proc/self/exe")
	if err != nil {
		return err
	}
	raw, err := io.ReadAll(io.LimitReader(self, mailrenewalkit.MaxBinarySize+1))
	closeErr := self.Close()
	if err != nil || closeErr != nil {
		return errors.Join(err, closeErr)
	}
	kit, _, err := mailrenewalkit.Payload(raw)
	if err != nil || kit.Generation != agent.Contract.MailRenewalGeneration || (target != "" && target != kit.Generation) {
		return servicemutationledger.ErrMailEnrollment
	}
	verify := func() error {
		if err := agent.Revalidate(); err != nil {
			return err
		}
		if kit.Generation != agent.Contract.MailRenewalGeneration || (target != "" && target != kit.Generation) {
			return servicemutationledger.ErrMailEnrollment
		}
		return nil
	}
	state := hostingpath.ServiceMutationStateRoot()
	binding := recoveryruntime.MailEnrollmentBinding{
		LedgerPath: filepath.Join(state, serviceMutationLedgerFileName),
		OwnerID:    ownerID,
		Owner:      servicemutationledger.FileOwner{UID: serviceMutationRequiredOwnerUID, GID: serviceMutationRequiredOwnerGID},
		HostLock:   host, HostOwner: serviceMutationLockOwner(), VerifyAuthority: verify, Native: mailEnrollmentNativeHost{},
	}
	rawLedger, found, err := servicemutationledger.ReadFile(binding.LedgerPath, servicemutationledger.MaxSize, binding.Owner)
	if err != nil {
		return err
	}
	if !found {
		return servicemutationledger.ErrMailEnrollment
	}
	ledger, err := servicemutationledger.Decode(rawLedger)
	if err != nil {
		return err
	}
	if automatic {
		pending, err := mailEnrollmentBootPending(&ledger, requestID)
		if err != nil {
			return err
		}
		if !pending {
			return nil
		}
	}
	fresh, err := mailEnrollmentAdmission(&ledger, requestID, ownerID, target, kit.Generation)
	if err != nil {
		return err
	}
	var execution *recoveryruntime.PreparedMailEnrollment
	direction := servicemutationledger.MailEnrollmentForward
	if fresh {
		// Directory creation is explicit new owner work. Resume/status never repairs
		// missing evidence. Native publication still follows the common reservation.
		if err = recoveryruntime.PrepareMailEnrollmentJournal(9); err != nil {
			return err
		}
		execution, _, err = recoveryruntime.PrepareMailEnrollment(ctx, requestID, mailEnrollmentJournalRoot, agent, binding)
	} else {
		execution, direction, err = recoveryruntime.OpenRecordedMailEnrollment(ctx, requestID, mailEnrollmentJournalRoot, agent, binding)
	}
	if err != nil {
		return err
	}
	// The boot unit is armed before the first common reservation/native effect.
	// A power cut before reservation leaves only a harmless read-only boot probe;
	// the same explicit owner start may finish admission. Recorded automatic work
	// cannot arm units, admit new requests, reverse direction or replay terminals.
	_, existingState, _ := servicemutationledger.RecordedMailEnrollment(&ledger, requestID)
	if automatic {
		if err = execution.ClaimBootAttempt(ctx); err != nil {
			return err
		}
	} else if existingState != servicemutationledger.MailEnrollmentPublished && existingState != servicemutationledger.MailEnrollmentRestored {
		if err = execution.ArmBoot(ctx); err != nil {
			return err
		}
	}
	authority := mailEnrollmentAuthority{identity: execution.Identity(), agent: agent, verifyIntent: func() error { return execution.RevalidateAuthority(ctx) }}
	return executePreparedMailEnrollment(ctx, state, host, authority, execution, direction)
}

// Absence is new admission only with an explicit exact owner/target tuple. The
// whole canonical ledger is validated first; malformed/reused IDs never become
// absence, and terminal or inverse requests retain their recorded direction.
func mailEnrollmentAdmission(ledger *servicemutationledger.Ledger, requestID, ownerID, target, currentTarget string) (bool, error) {
	if !validMutationIdentity(requestID) || !recoveryruntime.ValidDigest(currentTarget) || servicemutationledger.Validate(ledger) != nil {
		return false, servicemutationledger.ErrMailEnrollment
	}
	if ownerID != "" && (!validMutationIdentity(ownerID) || target != currentTarget) || ownerID == "" && target != "" {
		return false, servicemutationledger.ErrMailEnrollment
	}
	if ledger.Jobs[requestID] != nil {
		id, _, err := servicemutationledger.RecordedMailEnrollment(ledger, requestID)
		if err != nil || ownerID != "" && id.OwnerID != ownerID {
			return false, servicemutationledger.ErrMailEnrollment
		}
		return false, nil
	}
	if ownerID == "" || ledger.ActiveRequestID != "" {
		return false, servicemutationledger.ErrMailEnrollment
	}
	for _, job := range ledger.Jobs {
		if job.Status == servicemutationledger.StatusPending {
			return false, servicemutationledger.ErrMailEnrollment
		}
	}
	return true, nil
}
