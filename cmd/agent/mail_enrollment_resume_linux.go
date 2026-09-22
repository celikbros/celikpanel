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
// initialize a missing ledger. Initial setup and boot dispatch supply these locks.
func resumeIndependentMailEnrollment(ctx context.Context, requestID string) error {
	if !validMutationIdentity(requestID) || !mailRenewalOnlyBuild || os.Geteuid() != 0 {
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
	if err != nil || kit.Generation != agent.Contract.MailRenewalGeneration {
		return servicemutationledger.ErrMailEnrollment
	}
	verify := func() error {
		if err := agent.Revalidate(); err != nil {
			return err
		}
		if kit.Generation != agent.Contract.MailRenewalGeneration {
			return servicemutationledger.ErrMailEnrollment
		}
		return nil
	}
	state := hostingpath.ServiceMutationStateRoot()
	binding := recoveryruntime.MailEnrollmentBinding{
		LedgerPath: filepath.Join(state, serviceMutationLedgerFileName),
		Owner:      servicemutationledger.FileOwner{UID: serviceMutationRequiredOwnerUID, GID: serviceMutationRequiredOwnerGID},
		HostLock:   host, HostOwner: serviceMutationLockOwner(), VerifyAuthority: verify, Native: mailEnrollmentNativeHost{},
	}
	execution, direction, err := recoveryruntime.OpenRecordedMailEnrollment(ctx, requestID, mailEnrollmentJournalRoot, agent, binding)
	if err != nil {
		return err
	}
	// The recorded opener revalidates the same ledger selection on every native
	// boundary. The writer must keep that admission requirement too, so a missing
	// request can never fall through to executeMailEnrollmentAt's new-admission path.
	authority := mailEnrollmentAuthority{identity: execution.Identity(), agent: agent, verifyIntent: func() error { return execution.RevalidateAuthority(ctx) }}
	return executePreparedMailEnrollment(ctx, state, host, authority, execution, direction)
}
