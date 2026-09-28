package dnsengineartifact

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/alicelik/celikpanel/internal/bindconfig"
	"reflect"
	"strings"
)

const BINDSwitchInversePlanKindV2 = "bind-switch-config/v1"
const PDNSSourceProofKindV1 = "pdns-source/v1"
const BINDAdoptionSourceProofKindV1 = "bind-adoption-source/v1"

// BuildBINDSwitchInverseJournalV2 freezes the producer's exact intended BIND
// config before any native config write. It accepts only an already-valid v1
// intent, and never derives a v2 plan from a live, possibly mutated host.
func (policy JournalPolicy) BuildBINDSwitchInverseJournalV2(
	base SwitchJournalV1, hostLayout string, configAfter []FileSnapshot, sourceProof ...BINDSwitchSourceProofV2,
) (SwitchJournalV1, error) {
	if base.Schema != SwitchJournalSchemaV1 || base.Phase != SwitchPhaseIntent ||
		base.InversePlan != nil {
		return SwitchJournalV1{}, errors.New("v2 BIND inverse plan requires a fresh v1 intent")
	}
	raw, err := policy.EncodeSwitchJournal(base)
	if err != nil {
		return SwitchJournalV1{}, err
	}
	journal, err := policy.DecodeSwitchJournal(raw)
	if err != nil {
		return SwitchJournalV1{}, err
	}
	journal.Schema = SwitchJournalSchemaV2
	journal.InversePlan = &BINDSwitchInversePlanV2{
		Kind:        BINDSwitchInversePlanKindV2,
		HostLayout:  hostLayout,
		ConfigAfter: make([]FileSnapshot, len(configAfter)),
	}
	for i, snapshot := range configAfter {
		snapshot.Data = append([]byte(nil), snapshot.Data...)
		journal.InversePlan.ConfigAfter[i] = snapshot
	}
	if len(sourceProof) > 1 {
		return SwitchJournalV1{}, errors.New("at most one source proof is supported")
	}
	if len(sourceProof) == 1 {
		if sourceProof[0].SourcePDNS != nil {
			proof := *sourceProof[0].SourcePDNS
			proof.ConfigBefore = cloneFileSnapshotsV2(sourceProof[0].SourcePDNS.ConfigBefore)
			journal.InversePlan.SourcePDNS = &proof
		}
		if sourceProof[0].SourceBIND != nil {
			proof := *sourceProof[0].SourceBIND
			proof.Files = append([]BINDAdoptionSourceFileV1(nil), proof.Files...)
			proof.Zones = append([]BINDAdoptionSourceZoneV1(nil), proof.Zones...)
			journal.InversePlan.SourceBIND = &proof
		}
		journal.InversePlan.BINDUnchangedConfig = cloneFileSnapshotsV2(sourceProof[0].BINDUnchangedConfig)
	}
	digest, err := bindSwitchInversePlanDigestV2(journal)
	if err != nil {
		return SwitchJournalV1{}, err
	}
	journal.InversePlan.Digest = digest
	if err := policy.ValidateSwitchJournal(journal); err != nil {
		return SwitchJournalV1{}, err
	}
	return journal, nil
}

// ValidateBINDSwitchInversePlanV2 restricts both before and after evidence to
// the host policy's exact supported vendor paths and metadata. The plan digest
// covers the full journal except the mutable phase and digest itself.
func (policy JournalPolicy) ValidateBINDSwitchInversePlanV2(journal SwitchJournalV1) error {
	plan := journal.InversePlan
	if journal.Schema != SwitchJournalSchemaV2 || plan == nil ||
		plan.Kind != BINDSwitchInversePlanKindV2 ||
		journal.TargetEngine != "bind" ||
		journal.Mode != "switch" {
		return errors.New("v2 DNS journal has an unsupported BIND inverse plan")
	}
	var want []string
	var mode uint32
	switch plan.HostLayout {
	case "apt":
		want, mode = []string{"/etc/bind/named.conf.local", "/etc/bind/named.conf.options"}, 0o644
	case "pacman":
		want, mode = []string{"/etc/named.conf"}, 0o640
	default:
		return errors.New("v2 BIND inverse plan has an unsupported host layout")
	}
	if len(journal.ConfigBefore) != len(want) || len(plan.ConfigAfter) != len(want) ||
		!policy.ValidBINDConfigSnapshotSet(journal.ConfigBefore) ||
		!policy.ValidBINDConfigSnapshotSet(plan.ConfigAfter) {
		return errors.New("v2 BIND inverse config sets are incomplete")
	}
	for i, path := range want {
		before, after := journal.ConfigBefore[i], plan.ConfigAfter[i]
		if before.Path != path || after.Path != path ||
			!before.Exists || !after.Exists ||
			before.Mode != mode || after.Mode != mode ||
			!before.OwnerKnown || !after.OwnerKnown ||
			before.UID != 0 || after.UID != 0 ||
			before.GID != after.GID ||
			after.Mode != before.Mode {
			return errors.New("v2 BIND inverse config path or owner differs from the trusted layout")
		}
		if err := ValidateFileSnapshotIntegrity(after); err != nil {
			return fmt.Errorf("v2 BIND inverse prepared config: %w", err)
		}
	}
	if err := policy.validateBINDUnchangedConfigV2(journal); err != nil {
		return err
	}
	if plan.SourcePDNS != nil {
		if journal.SourceEngine != "pdns" || plan.SourcePDNS.Kind != PDNSSourceProofKindV1 ||
			policy.validatePDNSSourceProofV2(*plan.SourcePDNS) != nil {
			return errors.New("v2 BIND inverse PowerDNS source proof is invalid")
		}
	}
	if plan.SourceBIND != nil {
		if plan.SourcePDNS != nil || validateBINDAdoptionSourceProofV1(journal) != nil {
			return errors.New("v2 BIND adoption source proof is invalid")
		}
	}
	if !ValidGeneration(plan.Digest) {
		return errors.New("v2 BIND inverse plan digest is invalid")
	}
	wantDigest, err := bindSwitchInversePlanDigestV2(journal)
	if err != nil || plan.Digest != wantDigest {
		return errors.Join(errors.New("v2 BIND inverse plan is not bound to the frozen journal"), err)
	}
	return nil
}

func bindSwitchInversePlanDigestV2(journal SwitchJournalV1) (string, error) {
	if journal.InversePlan == nil {
		return "", errors.New("v2 BIND inverse plan is absent")
	}
	frozen := journal
	plan := *journal.InversePlan
	plan.Digest = ""
	frozen.InversePlan = &plan
	frozen.Phase = ""
	raw, err := json.Marshal(frozen)
	if err != nil {
		return "", fmt.Errorf("encode immutable BIND inverse plan: %w", err)
	}
	return DigestBytes(raw), nil
}

// SameImmutableBINDSwitchInversePlanV2 permits the single defined journal
// phase transition while rejecting changes to any frozen preimage or plan.
// Both inputs must have passed the strict v2 codec under the same host policy.
func SameImmutableBINDSwitchInversePlanV2(before, after SwitchJournalV1) bool {
	if before.Schema != SwitchJournalSchemaV2 || after.Schema != SwitchJournalSchemaV2 ||
		before.InversePlan == nil || after.InversePlan == nil {
		return false
	}
	after.Phase = before.Phase
	return reflect.DeepEqual(before, after)
}

func (policy JournalPolicy) validatePDNSSourceProofV2(proof PDNSSourceProofV2) error {
	if err := policy.ValidatePDNSConfigSnapshotSet(proof.ConfigBefore); err != nil {
		return err
	}
	db := proof.Database
	if db.Path != policy.PDNSDatabasePath || !ValidGeneration(db.LogicalSHA256) ||
		(db.Mode != 0o640 && db.Mode != 0o600) ||
		db.UID > uint32(1<<31-1) || db.GID > uint32(1<<31-1) ||
		db.Device == 0 || db.Inode == 0 || strings.ContainsAny(db.Path, "\x00\r\n") {
		return errors.New("PowerDNS source database identity is invalid")
	}
	return nil
}

// BINDSwitchSourceProofV2 groups optional source and unchanged target evidence.
type BINDSwitchSourceProofV2 struct {
	SourcePDNS          *PDNSSourceProofV2
	SourceBIND          *BINDAdoptionSourceProofV1
	BINDUnchangedConfig []FileSnapshot
}

func cloneFileSnapshotsV2(input []FileSnapshot) []FileSnapshot {
	if input == nil {
		return nil
	}
	cloned := make([]FileSnapshot, len(input))
	for i, snapshot := range input {
		snapshot.Data = append([]byte(nil), snapshot.Data...)
		cloned[i] = snapshot
	}
	return cloned
}

func (policy JournalPolicy) validateBINDUnchangedConfigV2(journal SwitchJournalV1) error {
	plan := journal.InversePlan
	if len(plan.BINDUnchangedConfig) == 0 {
		return nil
	}
	if plan.HostLayout != "apt" || (journal.SourceEngine != "pdns" && plan.SourceBIND == nil) || len(plan.BINDUnchangedConfig) != 2 {
		return errors.New("BIND inverse unchanged config envelope has unsupported scope")
	}
	leaf, err := bindconfig.DebianInverseMainLeaf(string(plan.BINDUnchangedConfig[0].Data))
	if err != nil {
		return err
	}
	want := []string{"/etc/bind/named.conf", leaf}
	for i, snapshot := range plan.BINDUnchangedConfig {
		if snapshot.Path != want[i] || !snapshot.Exists || snapshot.Mode != 0o644 || !snapshot.OwnerKnown || snapshot.UID != 0 || snapshot.GID > uint32(1<<31-1) {
			return errors.New("BIND inverse unchanged config has unsafe owner, path or mode")
		}
		if err := ValidateFileSnapshotIntegrity(snapshot); err != nil {
			return err
		}
	}
	if err := bindconfig.VerifyDebianInverseMainIncludes(string(plan.BINDUnchangedConfig[0].Data)); err != nil {
		return err
	}
	if err := bindconfig.VerifyDebianInverseNoIncludes(string(plan.BINDUnchangedConfig[1].Data)); err != nil {
		return err
	}
	if err := bindconfig.VerifyDebianInverseNoIncludes(string(journal.ConfigBefore[0].Data)); err != nil {
		return err
	}
	if err := bindconfig.VerifyDebianInverseNoIncludes(string(journal.ConfigBefore[1].Data)); err != nil {
		return err
	}
	if err := bindconfig.VerifyDebianInverseManagedLeaf(string(plan.ConfigAfter[0].Data), "/var/cache/bind/celikpanel/current/zones.conf"); err != nil {
		return err
	}
	return bindconfig.VerifyDebianInverseNoIncludes(string(plan.ConfigAfter[1].Data))
}
