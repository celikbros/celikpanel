package dnsengineartifact

import (
	"errors"
	"reflect"

	"github.com/alicelik/celikpanel/internal/pdnsnative"
)

// BuildPDNSFreshPrimaryJournalV3 creates an empty durable intent before the
// candidate is created. No service/config effect may precede its readback.
func (policy JournalPolicy) BuildPDNSFreshPrimaryJournalV3(base SwitchJournalV1, configAfter []FileSnapshot) (SwitchJournalV1, error) {
	if base.Schema != SwitchJournalSchemaV1 || base.Phase != SwitchPhaseIntent ||
		base.InversePlan != nil || base.PDNSTargetPlan != nil || base.PDNSFreshPlan != nil {
		return SwitchJournalV1{}, errors.New("v3 fresh PowerDNS plan requires a v1 intent")
	}
	if err := policy.ValidateSwitchJournal(base); err != nil {
		return SwitchJournalV1{}, err
	}
	journal := base
	journal.Schema = SwitchJournalSchemaV3
	journal.PDNSCandidatePath = policy.pdnsCandidatePathV4(base.MutationRequestID)
	journal.PDNSFreshPlan = &PDNSFreshPrimaryPlanV3{Kind: PDNSFreshPrimaryPlanKindV3, ConfigAfter: cloneFileSnapshotsV2(configAfter)}
	var err error
	journal.PDNSFreshPlan.IntentDigest, err = pdnsFreshIntentDigestV3(journal)
	if err != nil {
		return SwitchJournalV1{}, err
	}
	if err := policy.ValidateSwitchJournal(journal); err != nil {
		return SwitchJournalV1{}, err
	}
	return journal, nil
}

// StagePDNSFreshPrimaryCandidateV3 seals the candidate inode and full staged
// SQL after the intent is durable and before config, unit or live-DB effects.
func (policy JournalPolicy) StagePDNSFreshPrimaryCandidateV3(intent SwitchJournalV1, candidate PDNSTargetCandidateProofV4, staged pdnsnative.Snapshot) (SwitchJournalV1, error) {
	if intent.Schema != SwitchJournalSchemaV3 || intent.Phase != SwitchPhaseIntent ||
		intent.PDNSFreshPlan == nil || intent.PDNSFreshPlan.Candidate != nil {
		return SwitchJournalV1{}, errors.New("v3 staged candidate requires an empty durable intent")
	}
	if err := policy.ValidateSwitchJournal(intent); err != nil {
		return SwitchJournalV1{}, err
	}
	stagedHash, err := pdnsnative.LogicalSHA256(staged)
	if err != nil {
		return SwitchJournalV1{}, err
	}
	journal := intent
	journal.Phase = SwitchPhaseTargetStaged
	plan := *intent.PDNSFreshPlan
	plan.Candidate = &candidate
	plan.Staged = &staged
	plan.StagedLogicalSHA256 = stagedHash
	journal.PDNSFreshPlan = &plan
	plan.StagedDigest, err = pdnsFreshStagedDigestV3(journal)
	if err != nil {
		return SwitchJournalV1{}, err
	}
	if err := policy.ValidateSwitchJournal(journal); err != nil {
		return SwitchJournalV1{}, err
	}
	return journal, nil
}

// The caller proves daemon/version attribution and physical main/WAL/SHM
// identity before persisting this exact poststart observation.
func (policy JournalPolicy) AttachPDNSFreshNativeObservationV3(started SwitchJournalV1, catalog string, live pdnsnative.Snapshot) (SwitchJournalV1, error) {
	if started.Schema != SwitchJournalSchemaV3 || started.Phase != SwitchPhaseTargetStarted ||
		started.PDNSFreshPlan == nil || started.PDNSFreshPlan.Native != nil {
		return SwitchJournalV1{}, errors.New("v3 poststart observation requires an unobserved started target")
	}
	if err := policy.ValidateSwitchJournal(started); err != nil {
		return SwitchJournalV1{}, err
	}
	observed, err := pdnsnative.ObserveFreshPrimaryCatalogTransition(*started.PDNSFreshPlan.Staged, live, catalog, started.PrimaryCatalogSerial)
	if err != nil {
		return SwitchJournalV1{}, err
	}
	journal := started
	plan := *started.PDNSFreshPlan
	plan.Native = &observed
	journal.PDNSFreshPlan = &plan
	plan.NativeDigest, err = pdnsFreshNativeDigestV3(journal)
	if err != nil {
		return SwitchJournalV1{}, err
	}
	if err := policy.ValidateSwitchJournal(journal); err != nil {
		return SwitchJournalV1{}, err
	}
	return journal, nil
}

// Only intent -> staged may append candidate evidence, and only a same-phase
// started checkpoint may append native observation. All other fields freeze.
func SameImmutablePDNSFreshPrimaryPlanV3(before, after SwitchJournalV1) bool {
	if before.Schema != SwitchJournalSchemaV3 || after.Schema != SwitchJournalSchemaV3 ||
		before.PDNSFreshPlan == nil || after.PDNSFreshPlan == nil {
		return false
	}
	addStage := before.Phase == SwitchPhaseIntent && after.Phase == SwitchPhaseTargetStaged &&
		before.PDNSFreshPlan.Candidate == nil && after.PDNSFreshPlan.Candidate != nil
	addNative := before.Phase == SwitchPhaseTargetStarted && after.Phase == SwitchPhaseTargetStarted &&
		before.PDNSFreshPlan.Native == nil && after.PDNSFreshPlan.Native != nil
	if addStage || addNative {
		plan := *after.PDNSFreshPlan
		if addStage {
			plan.Candidate = nil
			plan.Staged = nil
			plan.StagedLogicalSHA256 = ""
			plan.StagedDigest = ""
		}
		if addNative {
			plan.Native = nil
			plan.NativeDigest = ""
		}
		after.PDNSFreshPlan = &plan
	}
	after.Phase = before.Phase
	return reflect.DeepEqual(before, after)
}

func ValidPDNSFreshPrimaryForwardPhaseTransitionV3(before, after SwitchJournalV1) bool {
	if before.Schema != SwitchJournalSchemaV3 || after.Schema != SwitchJournalSchemaV3 {
		return false
	}
	if before.Phase == after.Phase {
		switch before.Phase {
		case SwitchPhaseIntent, SwitchPhaseTargetStaged, SwitchPhaseTargetEnableIntent:
			return true
		case SwitchPhaseTargetStarted:
			return before.PDNSFreshPlan != nil && (before.PDNSFreshPlan.Native == nil || after.PDNSFreshPlan.Native != nil)
		case SwitchPhaseTargetVerified, SwitchPhaseCommitted:
			return before.PDNSFreshPlan != nil && before.PDNSFreshPlan.Native != nil
		default:
			return false
		}
	}
	switch before.Phase {
	case SwitchPhaseIntent:
		return after.Phase == SwitchPhaseTargetStaged
	case SwitchPhaseTargetStaged:
		return after.Phase == SwitchPhaseTargetEnableIntent
	case SwitchPhaseTargetEnableIntent:
		return after.Phase == SwitchPhaseTargetStarted
	case SwitchPhaseTargetStarted:
		return after.Phase == SwitchPhaseTargetVerified && before.PDNSFreshPlan != nil && before.PDNSFreshPlan.Native != nil
	case SwitchPhaseTargetVerified:
		return after.Phase == SwitchPhaseCommitted
	default:
		return false
	}
}
