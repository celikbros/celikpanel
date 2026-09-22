//go:build linux

package main

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/alicelik/celikpanel/internal/recoveryruntime"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
)

type nativeMailEnrollmentExecution interface {
	Identity() servicemutationledger.MailEnrollmentIdentity
	Resume(context.Context, string) error
	Verify(context.Context, string) error
}

// executePreparedMailEnrollment joins the actual common writer to the native
// executor. It runs only within accepted owner-operation dispatch; no periodic
// observer, generic RPC or CLI admits a new operation through this function.
func executePreparedMailEnrollment(ctx context.Context, stateDir, hostPath string, authority mailEnrollmentAuthority, execution *recoveryruntime.PreparedMailEnrollment, direction string) error {
	if execution == nil {
		return servicemutationledger.ErrMailEnrollment
	}
	return executeMailEnrollmentAt(ctx, stateDir, hostPath, authority, execution, direction, func() error { return recoveryruntime.VerifyPreflightBoundary(9) }, nil)
}
func executeMailEnrollmentAt(ctx context.Context, stateDir, hostPath string, authority mailEnrollmentAuthority, execution nativeMailEnrollmentExecution, direction string, verifyRelease func() error, fault func(string) error) error {
	if ctx == nil || execution == nil || execution.Identity() != authority.identity || authority.verifyIntent == nil || verifyRelease == nil || (direction != "forward" && direction != "rollback") {
		return servicemutationledger.ErrMailEnrollment
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := verifyRelease(); err != nil {
		return err
	}
	if err := verifyInheritedServiceMutationFileLockFD(hostPath, 8); err != nil {
		return err
	}
	if err := authority.verifyIntent(); err != nil {
		return err
	}
	raw, found, err := readSecureServiceMutationLedger(filepath.Join(stateDir, serviceMutationLedgerFileName), serviceMutationLedgerMaxSize)
	if err != nil {
		return err
	}
	if !found {
		return servicemutationledger.ErrMailEnrollment
	}
	ledger, err := decodeServiceMutationLedger(raw)
	if err != nil {
		return err
	}
	state, err := servicemutationledger.MailEnrollmentState(&ledger, authority.identity)
	// A mismatched existing request is not absence and must never be replaced.
	if err != nil && ledger.Jobs[authority.identity.RequestID] != nil {
		return err
	}
	if err != nil && direction != "forward" {
		return servicemutationledger.ErrMailEnrollment
	}
	terminal := servicemutationledger.MailEnrollmentPublished
	if direction == "rollback" {
		terminal = servicemutationledger.MailEnrollmentRestored
	}
	// Fresh native verification is part of both terminal publication and exact
	// terminal retry. Never trust a caller's precomputed success callback.
	authority.verifyResult = func() error {
		if e := ctx.Err(); e != nil {
			return e
		}
		return execution.Verify(ctx, direction)
	}
	originalIntent := authority.verifyIntent
	authority.verifyIntent = func() error {
		if e := ctx.Err(); e != nil {
			return e
		}
		return originalIntent()
	}
	write := func(next string) error {
		return writeMailEnrollmentReservationAt(stateDir, hostPath, authority, next, verifyRelease, fault)
	}
	if state == servicemutationledger.MailEnrollmentPublished || state == servicemutationledger.MailEnrollmentRestored {
		if state != terminal {
			return servicemutationledger.ErrMailEnrollment
		}
		return write(terminal) // read-only native proof; no executor replay
	}
	if err = write(direction); err != nil {
		return err
	} // durable fence precedes native work
	if err = execution.Resume(ctx, direction); err != nil {
		return err
	} // retain exact reservation
	if err = execution.Verify(ctx, direction); err != nil {
		return err
	}
	if err = write(terminal); err != nil {
		return errors.Join(errors.New("native enrollment is not acknowledged; resume the same recorded operation"), err)
	}
	return nil
}
