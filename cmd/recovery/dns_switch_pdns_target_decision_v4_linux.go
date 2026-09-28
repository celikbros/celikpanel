//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/dnsenginerecovery"
)

type pdnsTargetRollbackDecisionOpsV4 struct {
	Read          func(context.Context) (dnsenginerecovery.SwitchEvidence, bool, error)
	ExcludeWorker func(context.Context, dnsenginerecovery.SwitchEvidence) error
	AssessNative  func(context.Context, dnsengineartifact.SwitchJournalV1) (dnsenginerecovery.PDNSTargetStageState, error)
	VerifyTarget  func(dnsengineartifact.SwitchJournalV1) error
	WritePhase    func(context.Context, dnsengineartifact.SwitchJournalV1, dnsengineartifact.SwitchJournalV1) error
	Continue      func(context.Context) error
}

// A durable phase-only rollback decision precedes any native inverse effect.
// The synthetic phase is used solely for the read-only assessor: it cannot
// mutate a journal or grant authority to a target that may already have run.
func decideAndCompletePDNSTargetRollbackV4(ctx context.Context, ops pdnsTargetRollbackDecisionOpsV4) error {
	if ctx == nil || ops.Read == nil || ops.ExcludeWorker == nil || ops.AssessNative == nil ||
		ops.VerifyTarget == nil || ops.WritePhase == nil || ops.Continue == nil {
		return errors.New("PowerDNS target decision requires complete protected operations")
	}
	read := func() (dnsenginerecovery.SwitchEvidence, error) {
		if err := ctx.Err(); err != nil {
			return dnsenginerecovery.SwitchEvidence{}, err
		}
		e, present, err := ops.Read(ctx)
		if err != nil || !present {
			return dnsenginerecovery.SwitchEvidence{}, errors.Join(errors.New("PowerDNS target decision evidence is absent or unreadable"), err)
		}
		return e, nil
	}
	first, err := read()
	if err != nil {
		return err
	}
	if first.Journal.Phase == dnsengineartifact.SwitchPhaseRollingBack || first.Journal.Phase == dnsengineartifact.SwitchPhaseRollingBackTargetEnable || first.Journal.Phase == dnsengineartifact.SwitchPhaseRolledBack {
		return ops.Continue(ctx)
	}
	if err := validatePreDecisionPDNSTargetV4(first); err != nil {
		return err
	}
	rollbackPhase := dnsengineartifact.SwitchPhaseRollingBack
	if first.Journal.Phase == dnsengineartifact.SwitchPhaseTargetEnableIntent {
		rollbackPhase = dnsengineartifact.SwitchPhaseRollingBackTargetEnable
	}
	firstNativeState := dnsenginerecovery.PDNSTargetStageUnknown
	prove := func(e dnsenginerecovery.SwitchEvidence) error {
		if err := ops.ExcludeWorker(ctx, e); err != nil {
			return fmt.Errorf("accepted DNS worker is not excluded: %w", err)
		}
		synthetic := e.Journal
		synthetic.Phase = rollbackPhase
		if err := ops.VerifyTarget(synthetic); err != nil {
			return fmt.Errorf("PowerDNS target is no longer the exact staged candidate: %w", err)
		}
		state, err := ops.AssessNative(ctx, synthetic)
		if err != nil || !preDecisionPDNSTargetNativeStateV4(e.Journal.Phase, state) {
			return errors.Join(errors.New("PowerDNS target native source is not the frozen pre-activation state"), err)
		}
		if firstNativeState == dnsenginerecovery.PDNSTargetStageUnknown {
			firstNativeState = state
		} else if state != firstNativeState {
			return errors.New("PowerDNS target file shape changed before rollback decision")
		}
		return nil
	}
	if err := prove(first); err != nil {
		return err
	}
	before, err := read()
	if err != nil || !reflect.DeepEqual(first, before) {
		return errors.Join(errors.New("PowerDNS target decision evidence changed before checkpoint"), err)
	}
	if err := prove(before); err != nil {
		return err
	}
	next := before.Journal
	next.Phase = rollbackPhase
	writeErr := ops.WritePhase(ctx, before.Journal, next)
	after, err := read()
	if err != nil || !samePDNSTargetDecisionEvidenceV4(before, after, rollbackPhase) {
		return errors.Join(errors.New("PowerDNS target rollback decision was not durably re-read"), writeErr, err)
	}
	// A persistence call may report an interruption after its atomic write. The
	// exact re-read checkpoint is the authority for replay, not its return code.
	if err := ops.ExcludeWorker(ctx, after); err != nil {
		return err
	}
	if err := ops.VerifyTarget(after.Journal); err != nil {
		return err
	}
	state, err := ops.AssessNative(ctx, after.Journal)
	if err != nil || state != firstNativeState {
		return errors.Join(errors.New("PowerDNS target changed after rollback decision"), err)
	}
	return ops.Continue(ctx)
}

func validatePreDecisionPDNSTargetV4(e dnsenginerecovery.SwitchEvidence) error {
	j := e.Journal
	if j.Schema != dnsengineartifact.SwitchJournalSchemaV4 || j.PDNSTargetPlan == nil || j.PDNSTargetPlan.Candidate == nil ||
		(j.Phase != dnsengineartifact.SwitchPhaseIntent && j.Phase != dnsengineartifact.SwitchPhaseTargetStaged && j.Phase != dnsengineartifact.SwitchPhaseSourceStopped && j.Phase != dnsengineartifact.SwitchPhaseTargetEnableIntent) {
		return errors.New("PowerDNS target phase has no bounded pre-activation rollback decision")
	}
	synthetic := e
	rollbackPhase := dnsengineartifact.SwitchPhaseRollingBack
	if j.Phase == dnsengineartifact.SwitchPhaseTargetEnableIntent {
		rollbackPhase = dnsengineartifact.SwitchPhaseRollingBackTargetEnable
	}
	synthetic.Journal.Phase = rollbackPhase
	synthetic.Observation.Phase = rollbackPhase
	if err := dnsenginerecovery.ValidateStagedPDNSTargetInverseEvidence(synthetic); err != nil {
		return err
	}
	return nil
}

func samePDNSTargetDecisionEvidenceV4(before, after dnsenginerecovery.SwitchEvidence, rollbackPhase string) bool {
	if after.Journal.Phase != rollbackPhase || after.Observation.Phase != after.Journal.Phase {
		return false
	}
	after.Journal.Phase = before.Journal.Phase
	after.Observation.Phase = before.Observation.Phase
	// The installed journal byte fingerprint changes with its phase. The
	// canonical accepted job, source receipt and native state must not.
	after.Observation.EvidenceSHA256 = before.Observation.EvidenceSHA256
	return reflect.DeepEqual(before, after)
}

func preDecisionPDNSTargetNativeStateV4(phase string, state dnsenginerecovery.PDNSTargetStageState) bool {
	switch phase {
	case dnsengineartifact.SwitchPhaseTargetEnableIntent:
		return state == dnsenginerecovery.PDNSTargetStageRenamed || state == dnsenginerecovery.PDNSTargetStageRenamedEnabled
	case dnsengineartifact.SwitchPhaseSourceStopped:
		return state == dnsenginerecovery.PDNSTargetStageNeedsRestore || state == dnsenginerecovery.PDNSTargetStageRenamed
	case dnsengineartifact.SwitchPhaseIntent, dnsengineartifact.SwitchPhaseTargetStaged:
		return state == dnsenginerecovery.PDNSTargetStageNeedsRestore
	default:
		return false
	}
}
