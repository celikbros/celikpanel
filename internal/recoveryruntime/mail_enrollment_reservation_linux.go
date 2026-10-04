//go:build linux

package recoveryruntime

import (
	"context"
	"golang.org/x/sys/unix"
	"path/filepath"

	"github.com/alicelik/celikpanel/internal/hostmutationlock"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
)

// The numeric owner is established by the accepted outer enrollment, never
// inferred from whatever file happens to occupy the ledger path today.
type mailEnrollmentReservation struct {
	Path    string
	Owner   servicemutationledger.FileOwner
	OwnerID string
}

func (r mailEnrollmentReservation) verify(scope mailEnrollmentScope, direction string, terminal bool) error {
	if !filepath.IsAbs(r.Path) || filepath.Clean(r.Path) != r.Path || filepath.Base(r.Path) != "service-mutations.json" ||
		(direction != "forward" && direction != "rollback") {
		return fail(ReasonUnsupported)
	}
	raw, found, err := servicemutationledger.ReadFile(r.Path, servicemutationledger.MaxSize, r.Owner)
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
	scopeRaw, err := promotionJSON(scope)
	if err != nil {
		return err
	}
	identity := servicemutationledger.MailEnrollmentIdentity{RequestID: scope.Operation, OwnerID: r.OwnerID, ScopeSHA256: Digest(scopeRaw)}
	state, err := servicemutationledger.MailEnrollmentState(&ledger, identity)
	if err != nil {
		return err
	}
	if state == direction && ledger.ActiveRequestID == scope.Operation {
		return nil
	}
	if terminal && ledger.ActiveRequestID == "" &&
		(direction == "forward" && state == servicemutationledger.MailEnrollmentPublished || direction == "rollback" && state == servicemutationledger.MailEnrollmentRestored) {
		return nil
	}
	return servicemutationledger.ErrMailEnrollment
}

// Composition boundary: every native step/command rechecks the same durable
// reservation as well as current source/owner authority and both real locks.
// No result, including an already-published native receipt, closes the ledger
// here; the outer writer must re-observe the terminal native result before its
// exact terminal publication. This remains a private dispatcher primitive.
func runReservedMailEnrollmentAt(ctx context.Context, scope mailEnrollmentScope, direction string, paths mailCapturePaths, guard mailEnrollmentGuard, reservation mailEnrollmentReservation, commands mailEnrollmentCommands, checkpoint func(string)) error {
	bound, err := bindMailEnrollmentReservation(ctx, scope, direction, paths, guard, reservation, false)
	if err != nil {
		return err
	}
	return runMailEnrollmentAt(ctx, scope, direction, paths, bound, commands, checkpoint)
}

// A terminal ledger retry is an observer only. It cannot re-enter the composite
// mutation path merely because a historical native receipt exists.
func verifyReservedMailEnrollmentAt(ctx context.Context, scope mailEnrollmentScope, direction string, paths mailCapturePaths, guard mailEnrollmentGuard, reservation mailEnrollmentReservation, commands mailLoadedCommands) error {
	bound, err := bindMailEnrollmentReservation(ctx, scope, direction, paths, guard, reservation, true)
	if err != nil {
		return err
	}
	return finishMailEnrollmentAt(ctx, scope, direction, paths, func() error { return bound.Verify(scope) }, commands, nil, true)
}

func bindMailEnrollmentReservation(ctx context.Context, scope mailEnrollmentScope, direction string, paths mailCapturePaths, guard mailEnrollmentGuard, reservation mailEnrollmentReservation, terminal bool) (mailEnrollmentGuard, error) {
	if guard.Verify == nil || ctx == nil {
		return mailEnrollmentGuard{}, fail(ReasonUnsupported)
	}
	outer := guard.Verify
	boundary := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := verifyEnrollmentLock(paths.transaction, 9); err != nil {
			return err
		}
		if err := hostmutationlock.VerifyInherited(guard.HostLock, 8, guard.HostOwner); err != nil {
			return err
		}
		return outer(scope)
	}
	if err := boundary(); err != nil {
		return mailEnrollmentGuard{}, err
	}
	if err := reservation.verify(scope, direction, terminal); err != nil {
		return mailEnrollmentGuard{}, err
	}
	var fileBefore, parentBefore unix.Stat_t
	if unix.Lstat(reservation.Path, &fileBefore) != nil || unix.Lstat(filepath.Dir(reservation.Path), &parentBefore) != nil {
		return mailEnrollmentGuard{}, fail(ReasonReadFailed)
	}
	guard.Verify = func(current mailEnrollmentScope) error {
		if current != scope {
			return fail(ReasonChanged)
		}
		if err := boundary(); err != nil {
			return err
		}
		var fileNow, parentNow unix.Stat_t
		if unix.Lstat(reservation.Path, &fileNow) != nil || unix.Lstat(filepath.Dir(reservation.Path), &parentNow) != nil ||
			!sameFile(fileBefore, fileNow) || parentBefore.Dev != parentNow.Dev || parentBefore.Ino != parentNow.Ino || parentBefore.Mode != parentNow.Mode || parentBefore.Uid != parentNow.Uid || parentBefore.Gid != parentNow.Gid {
			return fail(ReasonChanged)
		}
		return reservation.verify(current, direction, terminal)
	}
	return guard, nil
}
