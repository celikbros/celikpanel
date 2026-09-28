package dnsengineartifact

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/alicelik/celikpanel/internal/bindconfig"
	"path/filepath"
	"reflect"

	"github.com/alicelik/celikpanel/internal/transport"
)

const PDNSTargetInversePlanKindV4 = "pdns-standalone-target/v1"
const ManagedBINDSourceProofKindV4 = "managed-bind-source/v1"

// pdnsCandidatePathV4 is rooted in the trusted agent state parent. The native
// adapter must prove that parent is root-controlled before any file mutation.
func (policy JournalPolicy) pdnsCandidatePathV4(requestID string) string {
	return filepath.Join(filepath.Dir(policy.StatePath), ".celikpanel-switch-"+requestID+".sqlite3")
}

type ManagedBINDSourceProofV4 struct {
	Kind          string         `json:"kind"`
	HostLayout    string         `json:"host_layout"`
	Generation    string         `json:"generation"`
	EngineEpoch   int64          `json:"engine_epoch"`
	ReceiptSHA256 string         `json:"receipt_sha256"`
	ConfigBefore  []FileSnapshot `json:"config_before"`
}

// PDNSTargetCandidateProofV4 identifies the staged main SQLite file. The
// runtime must independently prove the inode, contents and sidecar absence;
// this record alone does not authorize a rename, unlink or restore.
type PDNSTargetCandidateProofV4 struct {
	Path       string `json:"path"`
	Device     uint64 `json:"device"`
	Inode      uint64 `json:"inode"`
	Mode       uint32 `json:"mode"`
	UID        uint32 `json:"uid"`
	GID        uint32 `json:"gid"`
	Size       uint64 `json:"size"`
	SHA256     string `json:"sha256"`
	NoSidecars bool   `json:"no_sidecars"`
}

// PDNSTargetInversePlanV4 has two commitments. Digest freezes the original
// intent, including the candidate identity, before any DNS config is changed.
// StagedDigest records the later target-staged phase after config is applied.
type PDNSTargetInversePlanV4 struct {
	Kind         string                      `json:"kind"`
	ConfigAfter  []FileSnapshot              `json:"config_after"`
	SourceBIND   *ManagedBINDSourceProofV4   `json:"source_bind"`
	Digest       string                      `json:"digest"`
	Candidate    *PDNSTargetCandidateProofV4 `json:"candidate,omitempty"`
	StagedDigest string                      `json:"staged_digest,omitempty"`
}

func (policy JournalPolicy) BuildPDNSTargetInverseJournalV4(base SwitchJournalV1, configAfter []FileSnapshot, sourceProof ManagedBINDSourceProofV4, candidateProof PDNSTargetCandidateProofV4) (SwitchJournalV1, error) {
	if base.Schema != SwitchJournalSchemaV1 || base.Phase != SwitchPhaseIntent || base.InversePlan != nil || base.PDNSTargetPlan != nil {
		return SwitchJournalV1{}, errors.New("v4 PowerDNS target plan requires a fresh v1 intent")
	}
	raw, err := policy.EncodeSwitchJournal(base)
	if err != nil {
		return SwitchJournalV1{}, err
	}
	journal, err := policy.DecodeSwitchJournal(raw)
	if err != nil {
		return SwitchJournalV1{}, err
	}
	journal.Schema = SwitchJournalSchemaV4
	journal.PDNSCandidatePath = policy.pdnsCandidatePathV4(journal.MutationRequestID)
	journal.PDNSTargetPlan = &PDNSTargetInversePlanV4{
		Kind:        PDNSTargetInversePlanKindV4,
		ConfigAfter: cloneFileSnapshotsV2(configAfter),
		SourceBIND:  &sourceProof,
		Candidate:   &candidateProof,
	}
	journal.PDNSTargetPlan.SourceBIND.ConfigBefore = cloneFileSnapshotsV2(sourceProof.ConfigBefore)
	digest, err := pdnsTargetIntentDigestV4(journal)
	if err != nil {
		return SwitchJournalV1{}, err
	}
	journal.PDNSTargetPlan.Digest = digest
	if err := policy.ValidateSwitchJournal(journal); err != nil {
		return SwitchJournalV1{}, err
	}
	return journal, nil
}

func (policy JournalPolicy) AttachPDNSTargetCandidateV4(intent SwitchJournalV1, proof PDNSTargetCandidateProofV4) (SwitchJournalV1, error) {
	if intent.Schema != SwitchJournalSchemaV4 || intent.Phase != SwitchPhaseIntent ||
		intent.PDNSTargetPlan == nil || intent.PDNSTargetPlan.Candidate == nil ||
		!reflect.DeepEqual(*intent.PDNSTargetPlan.Candidate, proof) {
		return SwitchJournalV1{}, errors.New("v4 staged candidate must match frozen intent")
	}
	raw, err := policy.EncodeSwitchJournal(intent)
	if err != nil {
		return SwitchJournalV1{}, err
	}
	journal, err := policy.DecodeSwitchJournal(raw)
	if err != nil {
		return SwitchJournalV1{}, err
	}
	journal.Phase = SwitchPhaseTargetStaged
	digest, err := pdnsTargetStagedDigestV4(journal)
	if err != nil {
		return SwitchJournalV1{}, err
	}
	journal.PDNSTargetPlan.StagedDigest = digest
	if err := policy.ValidateSwitchJournal(journal); err != nil {
		return SwitchJournalV1{}, err
	}
	return journal, nil
}

func (policy JournalPolicy) ValidatePDNSTargetInversePlanV4(journal SwitchJournalV1) error {
	if filepath.Dir(policy.StatePath) == filepath.Dir(policy.PDNSDatabasePath) {
		return errors.New("v4 PowerDNS candidate parent must be separate from the daemon-writable database parent")
	}
	plan := journal.PDNSTargetPlan
	if journal.Schema != SwitchJournalSchemaV4 || plan == nil || plan.Kind != PDNSTargetInversePlanKindV4 ||
		journal.InversePlan != nil || journal.Mode != transport.DNSEngineSwitchModeSwitch ||
		journal.SourceEngine != transport.DNSEngineBIND || journal.TargetEngine != transport.DNSEnginePowerDNS ||
		journal.Topology != transport.DNSTopologyStandalone || journal.PairRole != "" ||
		journal.LocalIP != "" || journal.LocalNS != "" || journal.PeerIP != "" || journal.PeerNS != "" ||
		journal.PrimaryCatalogSerial != 0 ||
		journal.PDNSBackupSHA256 != "" || journal.PDNSBackupSize != 0 ||
		len(journal.TargetUnitsBefore) != 1 || journal.TargetUnitsBefore[0].ActiveState != "inactive" {
		return errors.New("v4 PowerDNS target plan has unsupported standalone scope")
	}
	if len(journal.SourceUnitsBefore) != 2 || !UnitSnapshotNamesEqual(journal.SourceUnitsBefore, []string{"bind9.service", "named.service"}) || journal.SourceUnitsBefore[0].LoadState != "loaded" || journal.SourceUnitsBefore[1].LoadState != "loaded" || journal.SourceUnitsBefore[1].ActiveState != "active" || journal.TargetUnitsBefore[0].LoadState != "loaded" || journal.TargetUnitsBefore[0].UnitFileState != "disabled" {
		return errors.New("v4 PowerDNS target requires installed inactive PowerDNS and the running named.service BIND source")
	}
	source := plan.SourceBIND
	if source == nil || source.Kind != ManagedBINDSourceProofKindV4 ||
		source.HostLayout != "apt" || !ValidGeneration(source.Generation) ||
		!ValidGeneration(source.ReceiptSHA256) {
		return errors.New("v4 managed BIND source proof is absent or invalid")
	}
	state, exists, err := SourceStateFromSwitchJournal(journal)
	if err != nil || !exists || state.Generation != source.Generation || state.EngineEpoch != source.EngineEpoch {
		return errors.Join(errors.New("v4 managed BIND generation differs from frozen state"), err)
	}
	if len(source.ConfigBefore) != 4 || source.ConfigBefore[0].Path != "/etc/bind/named.conf" {
		return errors.New("v4 managed BIND source main config is absent")
	}
	leaf, err := bindconfig.DebianInverseMainLeaf(string(source.ConfigBefore[0].Data))
	if err != nil {
		return fmt.Errorf("v4 managed BIND main config includes: %w", err)
	}
	wantSource := []string{"/etc/bind/named.conf", leaf, "/etc/bind/named.conf.local", "/etc/bind/named.conf.options"}
	if len(source.ConfigBefore) != len(wantSource) {
		return errors.New("v4 managed BIND config proof is incomplete")
	}
	for i, snapshot := range source.ConfigBefore {
		if snapshot.Path != wantSource[i] || !snapshot.Exists ||
			snapshot.Mode != 0o644 || !snapshot.OwnerKnown ||
			snapshot.UID != 0 || snapshot.GID > uint32(1<<31-1) {
			return errors.New("v4 managed BIND config proof has an unsafe path or owner")
		}
		if err := ValidateFileSnapshotIntegrity(snapshot); err != nil {
			return fmt.Errorf("v4 managed BIND source config: %w", err)
		}
	}
	if err := policy.ValidatePDNSConfigSnapshotSet(plan.ConfigAfter); err != nil {
		return fmt.Errorf("v4 PowerDNS prepared config: %w", err)
	}
	for i, before := range journal.ConfigBefore {
		after := plan.ConfigAfter[i]
		if before.Path != after.Path ||
			(before.Exists && after.Exists &&
				(!before.OwnerKnown || !after.OwnerKnown ||
					before.Mode != after.Mode || before.UID != after.UID || before.GID != after.GID)) {
			return errors.New("v4 PowerDNS prepared config has an unsupported present-file metadata transition")
		}
	}
	managedPrepared := false
	for _, snapshot := range plan.ConfigAfter {
		if snapshot.Path == policy.PDNSManagedPath && snapshot.Exists {
			managedPrepared = true
		}
	}
	if !managedPrepared {
		return errors.New("v4 PowerDNS managed config after-image is absent")
	}
	if err := bindconfig.VerifyDebianInverseNoIncludes(string(source.ConfigBefore[1].Data)); err != nil {
		return err
	}
	if err := bindconfig.VerifyDebianInverseManagedLeaf(string(source.ConfigBefore[2].Data), "/var/cache/bind/celikpanel/current/zones.conf"); err != nil {
		return err
	}
	if err := bindconfig.VerifyDebianInverseNoIncludes(string(source.ConfigBefore[3].Data)); err != nil {
		return err
	}
	if !ValidGeneration(plan.Digest) {
		return errors.New("v4 PowerDNS intent digest is invalid")
	}
	wantIntent, err := pdnsTargetIntentDigestV4(journal)
	if err != nil || wantIntent != plan.Digest {
		return errors.Join(errors.New("v4 PowerDNS intent is not bound to the frozen journal"), err)
	}
	candidate := plan.Candidate
	if candidate == nil || candidate.Path != policy.pdnsCandidatePathV4(journal.MutationRequestID) ||
		candidate.Device == 0 || candidate.Inode == 0 || candidate.Size == 0 ||
		(candidate.Mode != 0o600 && candidate.Mode != 0o640) ||
		candidate.UID > uint32(1<<31-1) || candidate.GID > uint32(1<<31-1) ||
		!ValidGeneration(candidate.SHA256) || !candidate.NoSidecars {
		return errors.New("v4 PowerDNS candidate identity is invalid")
	}
	if journal.Phase == SwitchPhaseIntent || (plan.StagedDigest == "" &&
		(journal.Phase == SwitchPhaseRollingBack || journal.Phase == SwitchPhaseRolledBack)) {
		if plan.StagedDigest != "" {
			return errors.New("v4 PowerDNS intent cannot claim a staged candidate")
		}
		return nil
	}
	if !ValidGeneration(plan.StagedDigest) {
		return errors.New("v4 PowerDNS staged digest is absent")
	}
	wantStage, err := pdnsTargetStagedDigestV4(journal)
	if err != nil || wantStage != plan.StagedDigest {
		return errors.Join(errors.New("v4 PowerDNS staged candidate is not bound to the frozen journal"), err)
	}
	return nil
}

func pdnsTargetIntentDigestV4(journal SwitchJournalV1) (string, error) {
	if journal.PDNSTargetPlan == nil {
		return "", errors.New("v4 PowerDNS plan is absent")
	}
	frozen := journal
	plan := *journal.PDNSTargetPlan
	plan.Digest = ""
	plan.StagedDigest = ""
	frozen.PDNSTargetPlan = &plan
	frozen.Phase = ""
	raw, err := json.Marshal(frozen)
	if err != nil {
		return "", err
	}
	return DigestBytes(raw), nil
}

func pdnsTargetStagedDigestV4(journal SwitchJournalV1) (string, error) {
	if journal.PDNSTargetPlan == nil || journal.PDNSTargetPlan.Candidate == nil {
		return "", errors.New("v4 PowerDNS staged candidate is absent")
	}
	frozen := journal
	plan := *journal.PDNSTargetPlan
	plan.StagedDigest = ""
	frozen.PDNSTargetPlan = &plan
	frozen.Phase = ""
	raw, err := json.Marshal(frozen)
	if err != nil {
		return "", err
	}
	return DigestBytes(raw), nil
}

// SameImmutablePDNSTargetInversePlanV4 accepts only the staged digest addition
// at intent -> target-staged, then phase-only transitions.
func SameImmutablePDNSTargetInversePlanV4(before, after SwitchJournalV1) bool {
	if before.Schema != SwitchJournalSchemaV4 || after.Schema != SwitchJournalSchemaV4 ||
		before.PDNSTargetPlan == nil || after.PDNSTargetPlan == nil {
		return false
	}
	if before.Phase == SwitchPhaseIntent && after.Phase == SwitchPhaseTargetStaged {
		return before.Phase == SwitchPhaseIntent && after.Phase == SwitchPhaseTargetStaged &&
			before.PDNSTargetPlan.Digest == after.PDNSTargetPlan.Digest &&
			before.PDNSTargetPlan.StagedDigest == "" && ValidGeneration(after.PDNSTargetPlan.StagedDigest) &&
			samePDNSTargetBaseV4(before, after)
	}
	return before.PDNSTargetPlan.Candidate != nil && after.PDNSTargetPlan.Candidate != nil &&
		before.PDNSTargetPlan.StagedDigest == after.PDNSTargetPlan.StagedDigest &&
		samePDNSTargetBaseV4(before, after)
}

func samePDNSTargetBaseV4(before, after SwitchJournalV1) bool {
	intentToStaged := before.Phase == SwitchPhaseIntent && after.Phase == SwitchPhaseTargetStaged
	after.Phase = before.Phase
	if intentToStaged {
		plan := *after.PDNSTargetPlan
		plan.StagedDigest = ""
		after.PDNSTargetPlan = &plan
	}
	return reflect.DeepEqual(before, after)
}

// ValidPDNSTargetForwardPhaseTransitionV4 requires the staged V4 producer to
// persist enable intent before starting PowerDNS. Rollback has a separate
// phase-only writer and cannot be produced by this forward path.
func ValidPDNSTargetForwardPhaseTransitionV4(before, after SwitchJournalV1) bool {
	if before.Schema != SwitchJournalSchemaV4 || after.Schema != SwitchJournalSchemaV4 {
		return false
	}
	if before.Phase == after.Phase {
		switch before.Phase {
		case SwitchPhaseIntent, SwitchPhaseTargetStaged, SwitchPhaseSourceStopped,
			SwitchPhaseTargetEnableIntent, SwitchPhaseTargetStarted, SwitchPhaseTargetVerified,
			SwitchPhaseCommitted:
			return true
		default:
			return false
		}
	}
	switch before.Phase {
	case SwitchPhaseIntent:
		return after.Phase == SwitchPhaseTargetStaged
	case SwitchPhaseTargetStaged:
		return after.Phase == SwitchPhaseSourceStopped
	case SwitchPhaseSourceStopped:
		return after.Phase == SwitchPhaseTargetEnableIntent
	case SwitchPhaseTargetEnableIntent:
		return after.Phase == SwitchPhaseTargetStarted
	case SwitchPhaseTargetStarted:
		return after.Phase == SwitchPhaseTargetVerified
	case SwitchPhaseTargetVerified:
		return after.Phase == SwitchPhaseCommitted
	default:
		return false
	}
}
