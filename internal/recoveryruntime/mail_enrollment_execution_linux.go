//go:build linux

package recoveryruntime

import (
	"context"
	"path/filepath"

	"github.com/alicelik/celikpanel/internal/hostmutationlock"
	"github.com/alicelik/celikpanel/internal/mailrenewalkit"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
)

// MailEnrollmentNative is the closed native command surface for the accepted
// enrollment. Implementations may not accept an arbitrary executable or unit for
// mutations. It is separate from an Agent RPC worker's expiring execution lease.
type MailEnrollmentNative interface {
	ObserveUnit(context.Context, string) ([]byte, error)
	Reload(context.Context) error
	StartTimer(context.Context) error
	StopTimer(context.Context) error
}

// MailEnrollmentBinding is supplied only by trusted owner-operation admission.
// VerifyAuthority must revalidate authenticated source and the exact persisted
// owner intent. Merely finding a prepared kit or old receipt grants no authority.
// The caller holds the existing release fd9 and host fd8 for the whole call.
type MailEnrollmentBinding struct {
	LedgerPath      string
	Owner           servicemutationledger.FileOwner
	OwnerID         string
	HostLock        string
	HostOwner       hostmutationlock.Owner
	VerifyAuthority func() error
	Native          MailEnrollmentNative
}

// PreparedMailEnrollment carries one immutable prepared scope. Native paths are
// fixed; the caller chooses only an already-protected journal directory. This
// adapter does not create durable owner identity, acquire locks, or admit work.
type PreparedMailEnrollment struct {
	scope   mailEnrollmentScope
	paths   mailCapturePaths
	binding MailEnrollmentBinding
}

func nativeMailEnrollmentPaths(journals string) mailCapturePaths {
	return mailCapturePaths{MailRenewalHookPath, "/etc/systemd/system", mailrenewalkit.InstalledRoot, journals, transactionPath}
}

// OpenPreparedMailEnrollment opens exact prepared evidence without changing it.
// The serialized scope is the same canonical v1 record used by the executor and
// shared ledger qualifier. Authority is independently rechecked on every use.
func OpenPreparedMailEnrollment(ctx context.Context, scopeRaw []byte, journals string, binding MailEnrollmentBinding) (*PreparedMailEnrollment, error) {
	return openPreparedMailEnrollmentAt(ctx, scopeRaw, nativeMailEnrollmentPaths(journals), binding)
}
func openPreparedMailEnrollmentAt(ctx context.Context, scopeRaw []byte, paths mailCapturePaths, binding MailEnrollmentBinding) (*PreparedMailEnrollment, error) {
	var scope mailEnrollmentScope
	if ctx == nil || binding.VerifyAuthority == nil || binding.Native == nil || !servicemutationledger.ValidIdentity(binding.OwnerID) || !filepath.IsAbs(paths.journals) || filepath.Clean(paths.journals) != paths.journals {
		return nil, fail(ReasonUnsupported)
	}
	if len(scopeRaw) == 0 || len(scopeRaw) > 2048 {
		return nil, fail(ReasonInvalidManifest)
	}
	if err := decodePromotion(scopeRaw, &scope); err != nil {
		return nil, err
	}
	if scope.Schema != mailEnrollmentSchema || !validPromotionNonce(scope.Operation) || !ValidDigest(scope.Target) || !ValidDigest(scope.CaptureSHA256) || !ValidDigest(scope.FilesSHA256) {
		return nil, fail(ReasonInvalidManifest)
	}
	execution := &PreparedMailEnrollment{scope, paths, binding}
	if err := execution.verifyBoundary(ctx); err != nil {
		return nil, err
	}
	c, err := openMailFilesContext(scope.Operation, scope.CaptureSHA256, 9, paths)
	if err != nil {
		return nil, err
	}
	defer c.close()
	raw, found, err := c.read(scope.Operation + ".files.json")
	if err != nil {
		return nil, err
	}
	if !found || Digest(raw) != scope.FilesSHA256 || c.capture.Contract.Target != scope.Target {
		return nil, fail(ReasonChanged)
	}
	var plan mailFilesRecord
	if err = decodePromotion(raw, &plan); err != nil {
		return nil, err
	}
	if err = plan.validate(c); err != nil {
		return nil, err
	}
	if err = c.revalidate(); err != nil {
		return nil, err
	}
	if err = execution.verifyBoundary(ctx); err != nil {
		return nil, err
	}
	return execution, nil
}
func (e *PreparedMailEnrollment) verifyBoundary(ctx context.Context) error {
	if e == nil || ctx == nil || e.binding.VerifyAuthority == nil {
		return fail(ReasonUnsupported)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := verifyEnrollmentLock(e.paths.transaction, 9); err != nil {
		return err
	}
	if err := hostmutationlock.VerifyInherited(e.binding.HostLock, 8, e.binding.HostOwner); err != nil {
		return err
	}
	return e.binding.VerifyAuthority()
}
func (e *PreparedMailEnrollment) Identity() servicemutationledger.MailEnrollmentIdentity {
	if e == nil {
		return servicemutationledger.MailEnrollmentIdentity{}
	}
	raw, _ := promotionJSON(e.scope)
	return servicemutationledger.MailEnrollmentIdentity{RequestID: e.scope.Operation, OwnerID: e.binding.OwnerID, ScopeSHA256: Digest(raw)}
}
func (e *PreparedMailEnrollment) commands() mailEnrollmentCommands {
	return mailEnrollmentCommands{loaded: mailLoadedCommands{observe: e.binding.Native.ObserveUnit, reload: e.binding.Native.Reload}, activity: mailActivityCommands{observe: e.binding.Native.ObserveUnit, startTimer: e.binding.Native.StartTimer, stopTimer: e.binding.Native.StopTimer}}
}
func (e *PreparedMailEnrollment) guard(ctx context.Context) mailEnrollmentGuard {
	return mailEnrollmentGuard{HostLock: e.binding.HostLock, HostOwner: e.binding.HostOwner, Verify: func(scope mailEnrollmentScope) error {
		if scope != e.scope {
			return fail(ReasonChanged)
		}
		return e.verifyBoundary(ctx)
	}}
}
func (e *PreparedMailEnrollment) reservation() mailEnrollmentReservation {
	return mailEnrollmentReservation{Path: e.binding.LedgerPath, Owner: e.binding.Owner, OwnerID: e.binding.OwnerID}
}
func (e *PreparedMailEnrollment) Resume(ctx context.Context, direction string) error {
	if err := e.verifyBoundary(ctx); err != nil {
		return err
	}
	return runReservedMailEnrollmentAt(ctx, e.scope, direction, e.paths, e.guard(ctx), e.reservation(), e.commands(), nil)
}
func (e *PreparedMailEnrollment) Verify(ctx context.Context, direction string) error {
	if err := e.verifyBoundary(ctx); err != nil {
		return err
	}
	return verifyReservedMailEnrollmentAt(ctx, e.scope, direction, e.paths, e.guard(ctx), e.reservation(), e.commands().loaded)
}
