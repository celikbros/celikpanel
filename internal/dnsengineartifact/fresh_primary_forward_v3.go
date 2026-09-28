package dnsengineartifact

import (
	"errors"

	"github.com/alicelik/celikpanel/internal/binddns"
	"github.com/alicelik/celikpanel/internal/pdnsnative"
	"github.com/alicelik/celikpanel/internal/transport"
)

// FreshPrimaryForwardPlanV3 is a pure proposal, not mutation authority. The
// caller must independently pin the daemon/cgroup, main+WAL+SHM identity,
// selected binary, ledger worker and local/peer DNS before each checkpoint.
// Committed deliberately retains the journal: retirement is not admitted.
type FreshPrimaryForwardPlanV3 struct {
	Action       string
	NextJournal  SwitchJournalV1
	DesiredState *StateV1
}

const (
	FreshPrimaryCheckpointStartedV3   = "checkpoint-started"
	FreshPrimaryCheckpointNativeV3    = "checkpoint-native"
	FreshPrimaryPublishStateV3        = "publish-state"
	FreshPrimaryCheckpointVerifiedV3  = "checkpoint-verified"
	FreshPrimaryCheckpointCommittedV3 = "checkpoint-committed"
	FreshPrimaryRetainCommittedV3     = "retain-committed"
)

func FreshPrimaryTargetStateV3(j SwitchJournalV1) (StateV1, error) {
	if j.Schema != SwitchJournalSchemaV3 || j.PDNSFreshPlan == nil || j.PDNSFreshPlan.Native == nil {
		return StateV1{}, errors.New("v3 native receipt is absent")
	}
	state := StateV1{
		Schema: StateSchemaV1, Mode: j.Mode, Engine: transport.DNSEnginePowerDNS,
		EngineEpoch: j.TargetEpoch, PairRole: j.PairRole, PairLocalIP: j.LocalIP, PairPeerIP: j.PeerIP,
		PrimaryCatalogSerial: j.PDNSFreshPlan.Native.Observed.NativeSerial,
		SourceRevision:       j.SourceRevision, ManifestQualifier: j.ManifestQualifier,
		MutationRequestID: j.MutationRequestID, MutationOwnerID: j.MutationOwnerID,
		NativeCatalogV3: NativeCatalogDebian49V3,
	}
	if err := ValidateNativeCatalogStateV3(state); err != nil {
		return StateV1{}, err
	}
	if !ExactSwitchTargetStateV1(state, j) {
		return StateV1{}, errors.New("v3 target state differs from frozen journal")
	}
	return state, nil
}

func PlanObservedFreshPrimaryForwardV3(policy JournalPolicy, j SwitchJournalV1, id SwitchIdentity,
	live pdnsnative.Snapshot, current *StateV1, serviceVerified, authorityVerified bool) (FreshPrimaryForwardPlanV3, error) {
	empty := FreshPrimaryForwardPlanV3{}
	if id.Validate() != nil || policy.ValidateSwitchJournal(j) != nil ||
		j.Schema != SwitchJournalSchemaV3 || j.PDNSFreshPlan == nil || j.PDNSFreshPlan.Staged == nil ||
		j.MutationRequestID != id.RequestID || j.MutationOwnerID != id.OwnerID ||
		j.TargetEngine != id.Target || j.ManifestQualifier != id.Qualifier || !serviceVerified {
		return empty, errors.New("v3 forward proposal lacks exact request and independently verified service")
	}
	catalog, err := binddns.CatalogDomain(j.LocalIP)
	if err != nil {
		return empty, err
	}
	if j.PDNSFreshPlan.Native == nil {
		if current != nil {
			return empty, errors.New("v3 state appeared before native receipt")
		}
		if _, err := pdnsnative.ObserveFreshPrimaryCatalogTransition(*j.PDNSFreshPlan.Staged, live, catalog, j.PrimaryCatalogSerial); err != nil {
			return empty, err
		}
		next := j
		switch j.Phase {
		case SwitchPhaseTargetEnableIntent:
			next.Phase = SwitchPhaseTargetStarted
			return FreshPrimaryForwardPlanV3{Action: FreshPrimaryCheckpointStartedV3, NextJournal: next}, nil
		case SwitchPhaseTargetStarted:
			next, err = policy.AttachPDNSFreshNativeObservationV3(j, catalog, live)
			if err != nil {
				return empty, err
			}
			return FreshPrimaryForwardPlanV3{Action: FreshPrimaryCheckpointNativeV3, NextJournal: next}, nil
		default:
			return empty, errors.New("v3 unrecorded target has an unsupported forward phase")
		}
	}
	if err := pdnsnative.VerifyRecordedFreshPrimaryCatalogTransition(*j.PDNSFreshPlan.Staged, live, catalog, j.PrimaryCatalogSerial, *j.PDNSFreshPlan.Native); err != nil {
		return empty, err
	}
	desired, err := FreshPrimaryTargetStateV3(j)
	if err != nil {
		return empty, err
	}
	if current != nil && *current != desired {
		return empty, errors.New("v3 current state is foreign")
	}
	if !authorityVerified {
		return empty, errors.New("v3 local and peer authority is not verified")
	}
	switch j.Phase {
	case SwitchPhaseTargetStarted:
		if current == nil {
			return FreshPrimaryForwardPlanV3{Action: FreshPrimaryPublishStateV3, NextJournal: j, DesiredState: &desired}, nil
		}
		next := j
		next.Phase = SwitchPhaseTargetVerified
		return FreshPrimaryForwardPlanV3{Action: FreshPrimaryCheckpointVerifiedV3, NextJournal: next}, nil
	case SwitchPhaseTargetVerified:
		if current == nil {
			return empty, errors.New("v3 verified target lacks state")
		}
		next := j
		next.Phase = SwitchPhaseCommitted
		return FreshPrimaryForwardPlanV3{Action: FreshPrimaryCheckpointCommittedV3, NextJournal: next}, nil
	case SwitchPhaseCommitted:
		if current == nil {
			return empty, errors.New("v3 committed target lacks state")
		}
		return FreshPrimaryForwardPlanV3{Action: FreshPrimaryRetainCommittedV3, NextJournal: j}, nil
	default:
		return empty, errors.New("v3 recorded target has an unsupported forward phase")
	}
}
