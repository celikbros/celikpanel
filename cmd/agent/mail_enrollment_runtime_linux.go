//go:build linux

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/alicelik/celikpanel/internal/hostingpath"
	"github.com/alicelik/celikpanel/internal/mailrenewalruntime"
	"github.com/alicelik/celikpanel/internal/recoveryruntime"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
)

// An explicit recorded continuation may restore volatile exclusion after reboot.
// New enrollment still requires the existing initialized runtime. A missing
// durable ledger, retained helper, group or accepted scope is never repaired.
func prepareRecordedMailEnrollmentRuntime(ctx context.Context, accepted []string, releaseFD int) error {
	if ctx == nil || !validMailEnrollmentWorkerArgs(accepted) {
		return servicemutationledger.ErrMailEnrollment
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := os.Lstat("/run/celikpanel"); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := recoveryruntime.VerifyHeldPreflightBoundary(releaseFD); err != nil {
		return err
	}
	proof, err := inspectRunningRetainedMailEnrollmentHelper()
	if err != nil {
		return err
	}
	defer proof.Close()
	owner := servicemutationledger.FileOwner{UID: serviceMutationRequiredOwnerUID, GID: serviceMutationRequiredOwnerGID}
	ledgerPath := filepath.Join(hostingpath.ServiceMutationStateRoot(), serviceMutationLedgerFileName)
	readIdentity := func() (servicemutationledger.MailEnrollmentIdentity, error) {
		raw, found, err := servicemutationledger.ReadFile(ledgerPath, servicemutationledger.MaxSize, owner)
		if err != nil || !found {
			return servicemutationledger.MailEnrollmentIdentity{}, errors.Join(servicemutationledger.ErrMailEnrollment, err)
		}
		return recordedMailEnrollmentRuntimeIdentity(raw, accepted, proof.Generation())
	}
	id, err := readIdentity()
	if err != nil {
		return err
	}
	verify := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := recoveryruntime.VerifyHeldPreflightBoundary(releaseFD); err != nil {
			return err
		}
		if err := proof.Revalidate(); err != nil {
			return err
		}
		current, err := readIdentity()
		if err != nil {
			return err
		}
		if current != id {
			return servicemutationledger.ErrMailEnrollment
		}
		observed, err := recoveryruntime.ObserveMailEnrollment(ctx, ledgerPath, owner, mailEnrollmentJournalRoot, id.RequestID, id.OwnerID, proof.Generation())
		if err != nil {
			return err
		}
		if !observed.Found || observed.Identity != id {
			return servicemutationledger.ErrMailEnrollment
		}
		return nil
	}
	return mailrenewalruntime.RestoreEnrollmentLocks(owner.GID, verify)
}

// Read-only selection: absence, conflicting ownership and another active
// operation cannot authorize restoring this request's volatile runtime.
func recordedMailEnrollmentRuntimeIdentity(raw []byte, accepted []string, generation string) (servicemutationledger.MailEnrollmentIdentity, error) {
	empty := servicemutationledger.MailEnrollmentIdentity{}
	if !validMailEnrollmentWorkerArgs(accepted) || !recoveryruntime.ValidDigest(generation) {
		return empty, servicemutationledger.ErrMailEnrollment
	}
	ledger, err := servicemutationledger.Decode(raw)
	if err != nil {
		return empty, err
	}
	id, _, err := servicemutationledger.RecordedMailEnrollment(&ledger, accepted[0])
	if err != nil || ledger.ActiveRequestID != "" && ledger.ActiveRequestID != accepted[0] {
		return empty, servicemutationledger.ErrMailEnrollment
	}
	if len(accepted) == 3 && (accepted[1] != id.OwnerID || accepted[2] != generation) {
		return empty, servicemutationledger.ErrMailEnrollment
	}
	return id, nil
}
