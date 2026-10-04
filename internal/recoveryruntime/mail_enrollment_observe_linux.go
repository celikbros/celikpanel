//go:build linux

package recoveryruntime

import (
	"bytes"
	"context"
	"path/filepath"

	"github.com/alicelik/celikpanel/internal/mailrenewalkit"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
)

// MailEnrollmentObservation is historical execution evidence for one accepted
// request. Published does not assert current timer activity or mail readiness.
// Absence never authorizes dispatch: a handoff may not have admitted its ledger yet.
type MailEnrollmentObservation struct {
	Found      bool
	Identity   servicemutationledger.MailEnrollmentIdentity
	Generation string
	State      string
}

// ObserveMailEnrollment needs neither a running Agent nor native mutation locks.
// It reads only the common ledger and immutable scope; no manager initialization,
// systemd command, artifact repair, lease renewal or worker dispatch is possible.
func ObserveMailEnrollment(ctx context.Context, ledgerPath string, owner servicemutationledger.FileOwner, journals, requestID, ownerID, generation string) (MailEnrollmentObservation, error) {
	return observeMailEnrollmentAt(ctx, ledgerPath, owner, journals, requestID, ownerID, generation, nil)
}
func observeMailEnrollmentAt(ctx context.Context, ledgerPath string, owner servicemutationledger.FileOwner, journals, requestID, ownerID, generation string, afterRead func()) (MailEnrollmentObservation, error) {
	empty := MailEnrollmentObservation{}
	if ctx == nil || !servicemutationledger.ValidIdentity(requestID) || !servicemutationledger.ValidIdentity(ownerID) || !ValidDigest(generation) {
		return empty, fail(ReasonUnsupported)
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	read := func() ([]byte, error) {
		raw, found, err := servicemutationledger.ReadFile(ledgerPath, servicemutationledger.MaxSize, owner)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, fail(ReasonChanged)
		}
		return raw, nil
	}
	raw, err := read()
	if err != nil {
		return empty, err
	}
	ledger, err := servicemutationledger.Decode(raw)
	if err != nil {
		return empty, err
	}
	result := empty
	var evidence *runtimeState
	if ledger.Jobs[requestID] != nil {
		id, state, err := servicemutationledger.RecordedMailEnrollment(&ledger, requestID)
		if err != nil || id.OwnerID != ownerID {
			return empty, servicemutationledger.ErrMailEnrollment
		}
		scope, pinned, err := openRecordedMailEnrollmentScope(journals, requestID)
		if err != nil {
			return empty, err
		}
		evidence = pinned
		defer evidence.close()
		scopeRaw, err := promotionJSON(scope)
		if err != nil || Digest(scopeRaw) != id.ScopeSHA256 || scope.Target != generation {
			return empty, fail(ReasonChanged)
		}
		result = MailEnrollmentObservation{true, id, generation, state}
	}
	if afterRead != nil {
		afterRead()
	}
	// Concurrent publication is unknown, never a fabricated composite result. A
	// later poll can observe it; this read cannot restart or compensate anything.
	again, err := read()
	if err != nil {
		return empty, err
	}
	if !bytes.Equal(raw, again) {
		return empty, fail(ReasonChanged)
	}
	if evidence != nil {
		if err = evidence.revalidate(); err != nil {
			return empty, err
		}
	}
	if err = ctx.Err(); err != nil {
		return empty, err
	}
	return result, nil
}

// Shared by observation and the locked executor. This checks immutable identity,
// not present native files: restoration may already have removed those files.
func openRecordedMailEnrollmentScope(journals, requestID string) (scope mailEnrollmentScope, state *runtimeState, resultErr error) {
	if !servicemutationledger.ValidIdentity(requestID) || !filepath.IsAbs(journals) || filepath.Clean(journals) != journals {
		return scope, nil, fail(ReasonUnsupported)
	}
	state = promotionState()
	defer func() {
		if resultErr != nil {
			state.close()
		}
	}()
	parent, err := state.openPath(journals)
	if err != nil {
		return scope, state, err
	}
	parent.exactMode = 0700
	if err = state.verifyDirectory(parent); err != nil {
		return scope, state, err
	}
	read := func(suffix string) ([]byte, error) {
		f, err := state.openFile(parent, requestID+suffix, 0600, mailrenewalkit.MaxTransitionSize)
		if err != nil {
			return nil, asReadError(err)
		}
		if err = refusePromotionXattrs(f); err != nil {
			return nil, err
		}
		raw, err := f.readBounded()
		if err == nil {
			f.digest = Digest(raw)
		}
		return raw, err
	}
	capture, err := read(".json")
	if err != nil {
		return scope, state, err
	}
	files, err := read(".files.json")
	if err != nil {
		return scope, state, err
	}
	var before mailCaptureRecord
	var plan mailFilesRecord
	if err = decodePromotion(capture, &before); err != nil {
		return scope, state, err
	}
	if err = decodePromotion(files, &plan); err != nil {
		return scope, state, err
	}
	if before.Schema != mailCaptureSchema || before.Contract.Schema != mailrenewalkit.TransitionSchema || before.Contract.OperationID != requestID || !ValidDigest(before.Contract.Target) || plan.Schema != mailFilesSchema || plan.CaptureSHA256 != Digest(capture) {
		return scope, state, fail(ReasonInvalidManifest)
	}
	scope = mailEnrollmentScope{mailEnrollmentSchema, requestID, Digest(capture), Digest(files), before.Contract.Target}
	return scope, state, state.revalidate()
}
