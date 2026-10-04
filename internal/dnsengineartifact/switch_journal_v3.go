package dnsengineartifact

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"path/filepath"

	"github.com/alicelik/celikpanel/internal/pdnsnative"
	"github.com/alicelik/celikpanel/internal/transport"
)

const PDNSFreshPrimaryPlanKindV3 = "pdns-fresh-paired-primary/debian-4.9/v1"

// PDNSFreshPrimaryPlanV3 freezes the exact prestart candidate before the
// first DNS effect. Native is appended only after the daemon's poststart SQL
// and file observations have been attributed and durably checked. A missing
// Native receipt cannot authorize a poststart inverse.
type PDNSFreshPrimaryPlanV3 struct {
	Kind                string                         `json:"kind"`
	ConfigAfter         []FileSnapshot                 `json:"config_after"`
	Candidate           *PDNSTargetCandidateProofV4    `json:"candidate,omitempty"`
	Staged              *pdnsnative.Snapshot           `json:"staged,omitempty"`
	StagedLogicalSHA256 string                         `json:"staged_logical_sha256"`
	IntentDigest        string                         `json:"intent_digest"`
	StagedDigest        string                         `json:"staged_digest,omitempty"`
	Native              *pdnsnative.RecordedTransition `json:"native,omitempty"`
	NativeDigest        string                         `json:"native_digest,omitempty"`
}

// The guarded package install leaves its newly owned service mask in place
// until activation. The producer separately proves that the mask was absent
// before installation; the frozen V3 journal may retain either exact preimage.
func validFreshPDNSTargetUnitBeforeV3(unit UnitSnapshot) bool {
	if unit.Name != "pdns.service" || unit.ActiveState != "inactive" {
		return false
	}
	return (unit.LoadState == "loaded" && unit.UnitFileState == "disabled") ||
		(unit.LoadState == "masked" && unit.UnitFileState == "masked")
}

func (policy JournalPolicy) ValidatePDNSFreshPrimaryPlanV3(journal SwitchJournalV1) error {
	plan := journal.PDNSFreshPlan
	if journal.Schema != SwitchJournalSchemaV3 || plan == nil ||
		plan.Kind != PDNSFreshPrimaryPlanKindV3 ||
		journal.Mode != transport.DNSEngineSwitchModeSwitch ||
		journal.SourceEngine != "" || journal.SourceEpoch != 0 ||
		journal.TargetEngine != transport.DNSEnginePowerDNS ||
		journal.Topology != transport.DNSTopologyPaired ||
		journal.PairRole != transport.DNSPairRolePrimary ||
		journal.PrimaryCatalogSerial != 1 || journal.StateBefore.Exists ||
		journal.PDNSBackupSHA256 != "" || journal.PDNSBackupSize != 0 ||
		len(journal.SourceUnitsBefore) != 0 ||
		len(journal.TargetUnitsBefore) != 1 ||
		!validFreshPDNSTargetUnitBeforeV3(journal.TargetUnitsBefore[0]) ||
		journal.PDNSLiveSHA256 != "" || journal.PDNSLiveSize != 0 {
		return errors.New("v3 journal is not a fresh paired PowerDNS primary")
	}
	if err := policy.ValidatePDNSConfigSnapshotSet(plan.ConfigAfter); err != nil {
		return errors.New("v3 prepared PowerDNS config is incomplete")
	}
	if len(plan.ConfigAfter) != len(journal.ConfigBefore) {
		return errors.New("v3 prepared PowerDNS config differs from its preimage paths")
	}
	for i, before := range journal.ConfigBefore {
		after := plan.ConfigAfter[i]
		if before.Path != after.Path {
			return errors.New("v3 prepared PowerDNS config path changed")
		}
	}
	if !plan.ConfigAfter[1].Exists || !plan.ConfigAfter[2].Exists {
		return errors.New("v3 paired PowerDNS config after-image is absent")
	}
	if filepath.Dir(policy.StatePath) == filepath.Dir(policy.PDNSDatabasePath) ||
		journal.PDNSCandidatePath != policy.pdnsCandidatePathV4(journal.MutationRequestID) {
		return errors.New("v3 PowerDNS candidate path is not private and frozen")
	}
	intentDigest, err := pdnsFreshIntentDigestV3(journal)
	if err != nil || intentDigest != plan.IntentDigest {
		return errors.New("v3 fresh PowerDNS intent is not frozen")
	}
	if plan.Candidate == nil {
		if plan.Staged != nil || plan.StagedLogicalSHA256 != "" || plan.StagedDigest != "" ||
			plan.Native != nil || plan.NativeDigest != "" {
			return errors.New("v3 unstaged intent contains hidden target evidence")
		}
		switch journal.Phase {
		case SwitchPhaseIntent, SwitchPhaseRollingBack, SwitchPhaseRolledBack:
			return nil
		default:
			return errors.New("v3 target effect precedes a durable staged candidate")
		}
	}
	candidate := plan.Candidate
	if candidate.Path != journal.PDNSCandidatePath ||
		candidate.Device == 0 || candidate.Inode == 0 ||
		candidate.Size == 0 || !candidate.NoSidecars ||
		(candidate.Mode != 0o600 && candidate.Mode != 0o640) ||
		candidate.UID > 1<<31-1 || candidate.GID > 1<<31-1 ||
		!ValidGeneration(candidate.SHA256) || plan.Staged == nil {
		return errors.New("v3 frozen PowerDNS candidate identity is invalid")
	}
	stagedHash, err := pdnsnative.LogicalSHA256(*plan.Staged)
	if err != nil || stagedHash != plan.StagedLogicalSHA256 {
		return errors.New("v3 staged PowerDNS SQL commitment is invalid")
	}
	stagedDigest, err := pdnsFreshStagedDigestV3(journal)
	if err != nil || stagedDigest != plan.StagedDigest {
		return errors.New("v3 staged PowerDNS candidate is not frozen")
	}
	if journal.Phase == SwitchPhaseIntent || journal.Phase == SwitchPhaseSourceStopped {
		return errors.New("v3 staged fresh primary has an invalid phase")
	}
	if plan.Native != nil {
		switch journal.Phase {
		case SwitchPhaseTargetStarted, SwitchPhaseTargetVerified, SwitchPhaseCommitted:
		default:
			return errors.New("v3 native observation precedes PowerDNS start")
		}
	}
	if plan.Native == nil {
		if plan.NativeDigest != "" || journal.Phase == SwitchPhaseTargetVerified || journal.Phase == SwitchPhaseCommitted {
			return errors.New("v3 verified target lacks durable native observation")
		}
		return nil
	}
	observed := plan.Native.Observed
	if observed.SourceSerial != journal.PrimaryCatalogSerial ||
		observed.NativeSerial <= observed.SourceSerial ||
		!ValidGeneration(plan.Native.LogicalSHA256) ||
		!ValidGeneration(plan.NativeDigest) {
		return errors.New("v3 native PowerDNS observation is invalid")
	}
	catalogHash, err := base64.StdEncoding.DecodeString(observed.CatalogHash)
	if err != nil || len(catalogHash) != 32 {
		return errors.New("v3 PowerDNS catalog hash is invalid")
	}
	nativeDigest, err := pdnsFreshNativeDigestV3(journal)
	if err != nil || nativeDigest != plan.NativeDigest {
		return errors.New("v3 native PowerDNS observation is not frozen")
	}
	return nil
}

func pdnsFreshIntentDigestV3(journal SwitchJournalV1) (string, error) {
	if journal.PDNSFreshPlan == nil {
		return "", errors.New("v3 plan is absent")
	}
	frozen := journal
	plan := *journal.PDNSFreshPlan
	plan.IntentDigest = ""
	plan.Candidate = nil
	plan.Staged = nil
	plan.StagedLogicalSHA256 = ""
	plan.StagedDigest = ""
	plan.Native = nil
	plan.NativeDigest = ""
	frozen.PDNSFreshPlan = &plan
	frozen.Phase = ""
	canonicalV3Collections(&frozen)
	raw, err := json.Marshal(frozen)
	if err != nil {
		return "", err
	}
	return DigestBytes(raw), nil
}

func pdnsFreshStagedDigestV3(journal SwitchJournalV1) (string, error) {
	if journal.PDNSFreshPlan == nil || journal.PDNSFreshPlan.Candidate == nil ||
		journal.PDNSFreshPlan.Staged == nil {
		return "", errors.New("v3 staged plan is absent")
	}
	frozen := journal
	plan := *journal.PDNSFreshPlan
	plan.StagedDigest = ""
	plan.Native = nil
	plan.NativeDigest = ""
	frozen.PDNSFreshPlan = &plan
	frozen.Phase = ""
	canonicalV3Collections(&frozen)
	raw, err := json.Marshal(frozen)
	if err != nil {
		return "", err
	}
	return DigestBytes(raw), nil
}

func pdnsFreshNativeDigestV3(journal SwitchJournalV1) (string, error) {
	if journal.PDNSFreshPlan == nil || journal.PDNSFreshPlan.Native == nil {
		return "", errors.New("v3 native plan is absent")
	}
	frozen := journal
	plan := *journal.PDNSFreshPlan
	plan.NativeDigest = ""
	frozen.PDNSFreshPlan = &plan
	frozen.Phase = ""
	canonicalV3Collections(&frozen)
	raw, err := json.Marshal(frozen)
	if err != nil {
		return "", err
	}
	return DigestBytes(raw), nil
}

func canonicalV3Collections(journal *SwitchJournalV1) {
	if journal.Zones == nil {
		journal.Zones = []transport.DNSEngineSwitchZoneSnapshot{}
	}
	if journal.ConfigBefore == nil {
		journal.ConfigBefore = []FileSnapshot{}
	}
	if journal.TargetUnitsBefore == nil {
		journal.TargetUnitsBefore = []UnitSnapshot{}
	}
	if journal.SourceUnitsBefore == nil {
		journal.SourceUnitsBefore = []UnitSnapshot{}
	}
}
