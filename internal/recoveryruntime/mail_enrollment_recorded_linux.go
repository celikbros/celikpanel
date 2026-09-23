//go:build linux

package recoveryruntime

import (
	"context"
	"github.com/alicelik/celikpanel/internal/agentnativecontract"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
)

// OpenRecordedMailEnrollment recovers the immutable scope from its existing
// capture and file plan, checked against the accepted common ledger digest.
// It does not prepare new evidence, create owner identity, select another request
// or switch direction. Both inherited locks and trusted source remain mandatory.
func OpenRecordedMailEnrollment(ctx context.Context, requestID, journals string, agent *CompatibleMailAgent, binding MailEnrollmentBinding) (*PreparedMailEnrollment, string, error) {
	return openRecordedMailEnrollmentAt(ctx, requestID, nativeMailEnrollmentPaths(journals), agent, binding)
}
func openRecordedMailEnrollmentAt(ctx context.Context, requestID string, paths mailCapturePaths, agent *CompatibleMailAgent, binding MailEnrollmentBinding) (*PreparedMailEnrollment, string, error) {
	if agent == nil || binding.VerifyAuthority == nil {
		return nil, "", fail(ReasonUnsupported)
	}
	originalAuthority := binding.VerifyAuthority
	binding.VerifyAuthority = func() error {
		if err := agent.Revalidate(); err != nil {
			return err
		}
		if agent.Contract.MailEnrollmentPolicy != agentnativecontract.MailEnrollmentPolicy || !ValidDigest(agent.Contract.MailRenewalGeneration) {
			return fail(ReasonUnsupported)
		}
		return originalAuthority()
	}
	return openRecordedMailEnrollmentSourceAt(ctx, requestID, paths, agent.Contract.MailRenewalGeneration, binding)
}

// OpenRetainedMailEnrollment consumes only an existing admission. The immutable
// scope digest in that admission binds the retained kit, so ordinary management
// files are unnecessary. It cannot create a reservation or reconstruct evidence.
func OpenRetainedMailEnrollment(ctx context.Context, requestID, journals string, helper *RetainedMailEnrollmentHelper, binding MailEnrollmentBinding) (*PreparedMailEnrollment, string, error) {
	return openRetainedMailEnrollmentAt(ctx, requestID, nativeMailEnrollmentPaths(journals), helper, binding)
}
func openRetainedMailEnrollmentAt(ctx context.Context, requestID string, paths mailCapturePaths, helper *RetainedMailEnrollmentHelper, binding MailEnrollmentBinding) (*PreparedMailEnrollment, string, error) {
	if helper == nil || binding.VerifyAuthority == nil {
		return nil, "", fail(ReasonUnsupported)
	}
	originalAuthority := binding.VerifyAuthority
	binding.VerifyAuthority = func() error {
		if err := helper.Revalidate(); err != nil {
			return err
		}
		return originalAuthority()
	}
	return openRecordedMailEnrollmentSourceAt(ctx, requestID, paths, helper.Generation(), binding)
}
func openRecordedMailEnrollmentSourceAt(ctx context.Context, requestID string, paths mailCapturePaths, generation string, binding MailEnrollmentBinding) (*PreparedMailEnrollment, string, error) {
	if ctx == nil || !ValidDigest(generation) || binding.VerifyAuthority == nil || binding.Native == nil || !servicemutationledger.ValidIdentity(requestID) {
		return nil, "", fail(ReasonUnsupported)
	}
	boundary := func() error { return (&PreparedMailEnrollment{paths: paths, binding: binding}).verifyBoundary(ctx) }
	if err := boundary(); err != nil {
		return nil, "", err
	}
	read := func() (servicemutationledger.MailEnrollmentIdentity, string, error) {
		raw, found, err := servicemutationledger.ReadFile(binding.LedgerPath, servicemutationledger.MaxSize, binding.Owner)
		if err != nil {
			return servicemutationledger.MailEnrollmentIdentity{}, "", err
		}
		if !found {
			return servicemutationledger.MailEnrollmentIdentity{}, "", servicemutationledger.ErrMailEnrollment
		}
		ledger, err := servicemutationledger.Decode(raw)
		if err != nil {
			return servicemutationledger.MailEnrollmentIdentity{}, "", err
		}
		return servicemutationledger.RecordedMailEnrollment(&ledger, requestID)
	}
	identity, state, err := read()
	if err != nil {
		return nil, "", err
	}
	if binding.OwnerID != "" && binding.OwnerID != identity.OwnerID {
		return nil, "", servicemutationledger.ErrMailEnrollment
	}
	binding.OwnerID = identity.OwnerID
	direction, terminal := "forward", servicemutationledger.MailEnrollmentPublished
	if state == servicemutationledger.MailEnrollmentRollback || state == servicemutationledger.MailEnrollmentRestored {
		direction, terminal = "rollback", servicemutationledger.MailEnrollmentRestored
	}
	sourceAuthority := binding.VerifyAuthority
	binding.VerifyAuthority = func() error {
		if err := sourceAuthority(); err != nil {
			return err
		}
		current, status, err := read()
		if err != nil {
			return err
		}
		// The executor may acknowledge this result, but cannot reverse a newer owner
		// rollback decision or resurrect a deleted/closed/replaced reservation.
		if current != identity || (status != state && !((state == direction) && status == terminal)) {
			return servicemutationledger.ErrMailEnrollment
		}
		return nil
	}
	scope, journal, err := openRecordedMailEnrollmentScope(paths.journals, requestID)
	if err != nil {
		return nil, "", err
	}
	defer journal.close()
	raw, err := promotionJSON(scope)
	if err != nil {
		return nil, "", err
	}
	if Digest(raw) != identity.ScopeSHA256 || scope.Target != generation {
		return nil, "", fail(ReasonChanged)
	}
	prepared, err := openPreparedMailEnrollmentAt(ctx, raw, paths, binding)
	if err != nil {
		return nil, "", err
	}
	if err = journal.revalidate(); err != nil {
		return nil, "", err
	}
	if err = boundary(); err != nil {
		return nil, "", err
	}
	return prepared, direction, nil
}
