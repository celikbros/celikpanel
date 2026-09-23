//go:build linux

package main

import (
	"context"
	"path/filepath"

	"github.com/alicelik/celikpanel/internal/hostingpath"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
)

// Boot observation is not admission. Terminal history does not need the current
// Agent/helper pair and must not restore an owner-stopped native timer. Missing
// or malformed ledger evidence is unknown, never permission to initialize it.
func runIndependentMailEnrollmentBoot(ctx context.Context, requestID string) error {
	if ctx == nil || !validMutationIdentity(requestID) {
		return servicemutationledger.ErrMailEnrollment
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	gid, ok := lookupGroupID("celikpanel")
	if !ok || gid < 0 || uint32(gid) != serviceMutationRequiredOwnerGID {
		return servicemutationledger.ErrMailEnrollment
	}
	raw, found, err := servicemutationledger.ReadFile(filepath.Join(hostingpath.ServiceMutationStateRoot(), serviceMutationLedgerFileName), servicemutationledger.MaxSize, servicemutationledger.FileOwner{UID: serviceMutationRequiredOwnerUID, GID: serviceMutationRequiredOwnerGID})
	if err != nil {
		return err
	}
	if !found {
		return servicemutationledger.ErrMailEnrollment
	}
	ledger, err := servicemutationledger.Decode(raw)
	if err != nil {
		return err
	}
	pending, err := mailEnrollmentBootPending(&ledger, requestID)
	if err != nil || !pending {
		return err
	}
	return runIndependentMailEnrollmentWorkerMode(ctx, []string{requestID}, true)
}

func mailEnrollmentBootPending(ledger *servicemutationledger.Ledger, requestID string) (bool, error) {
	if !validMutationIdentity(requestID) || servicemutationledger.Validate(ledger) != nil {
		return false, servicemutationledger.ErrMailEnrollment
	}
	if ledger.Jobs[requestID] == nil {
		return false, nil
	} // power cut before common admission
	_, state, err := servicemutationledger.RecordedMailEnrollment(ledger, requestID)
	if err != nil {
		return false, err
	}
	switch state {
	case servicemutationledger.MailEnrollmentPublished, servicemutationledger.MailEnrollmentRestored:
		return false, nil
	case servicemutationledger.MailEnrollmentForward, servicemutationledger.MailEnrollmentRollback:
		if ledger.ActiveRequestID != requestID {
			return false, servicemutationledger.ErrMailEnrollment
		}
		return true, nil
	default:
		return false, servicemutationledger.ErrMailEnrollment
	}
}
