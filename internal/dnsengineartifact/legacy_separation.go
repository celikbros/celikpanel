package dnsengineartifact

import (
	"bytes"
	"errors"
	"fmt"
)

const LegacySeparationSchemaV1 = "celikpanel-dns-engine-legacy-separation/v1"

// LegacySeparationV1 is a read-only, self-contained migration proposal. It
// retains BOTH exact legacy records, including the ownership record's frozen
// publication. Advancing current publication never rewrites that before-image.
// This is not an accepted mutation, checkpoint or proof of current native DNS.
// A future publisher must bind it to an owner operation and protected source
// identities under exclusion before changing any installed evidence.
type LegacySeparationV1 struct {
	Schema               string              `json:"schema"`
	LegacyOwnership      []byte              `json:"legacy_ownership"`
	LegacyState          []byte              `json:"legacy_state"`
	Acquisition          AcquisitionRecordV1 `json:"acquisition"`
	OwnershipPublication PublicationRecordV1 `json:"ownership_publication"`
	CurrentPublication   PublicationRecordV1 `json:"current_publication"`
}

// PlanLegacySeparationV1 refuses ambiguous input and unsupported relationships.
// It never normalizes source bytes, copies current publication onto ownership,
// or invents a missing source record.
func PlanLegacySeparationV1(ownership, current []byte) (LegacySeparationV1, error) {
	before, err := DecodeV1(ownership)
	if err != nil {
		return LegacySeparationV1{}, fmt.Errorf("legacy DNS ownership: %w", err)
	}
	after, err := DecodeV1(current)
	if err != nil {
		return LegacySeparationV1{}, fmt.Errorf("legacy DNS publication: %w", err)
	}
	acquisition, beforePublication, err := SeparateV1(before)
	if err != nil {
		return LegacySeparationV1{}, err
	}
	currentAcquisition, afterPublication, err := SeparateV1(after)
	if err != nil {
		return LegacySeparationV1{}, err
	}
	if acquisition != currentAcquisition {
		return LegacySeparationV1{}, errors.New("legacy DNS acquisition identity changed")
	}
	if _, err := ComparePublicationsV1(acquisition, beforePublication, afterPublication); err != nil {
		return LegacySeparationV1{}, err
	}
	return LegacySeparationV1{
		Schema:          LegacySeparationSchemaV1,
		LegacyOwnership: bytes.Clone(ownership), LegacyState: bytes.Clone(current),
		Acquisition: acquisition, OwnershipPublication: beforePublication, CurrentPublication: afterPublication,
	}, nil
}

func validateLegacySeparationV1(plan LegacySeparationV1) error {
	if plan.Schema != LegacySeparationSchemaV1 {
		return errors.New("unsupported DNS legacy separation schema")
	}
	expected, err := PlanLegacySeparationV1(plan.LegacyOwnership, plan.LegacyState)
	if err != nil {
		return err
	}
	if plan.Acquisition != expected.Acquisition || plan.OwnershipPublication != expected.OwnershipPublication ||
		plan.CurrentPublication != expected.CurrentPublication {
		return errors.New("separated DNS evidence differs from its retained legacy source")
	}
	return nil
}

func CanonicalLegacySeparationV1(plan LegacySeparationV1) ([]byte, error) {
	if err := validateLegacySeparationV1(plan); err != nil {
		return nil, err
	}
	return canonicalRecord(plan)
}

func DecodeLegacySeparationV1(data []byte) (LegacySeparationV1, error) {
	var plan LegacySeparationV1
	err := decodeCanonicalRecord(data, 4*recordLimit, &plan, func() ([]byte, error) {
		return CanonicalLegacySeparationV1(plan)
	})
	if err != nil {
		return LegacySeparationV1{}, err
	}
	return plan, nil
}

// VerifyLegacySeparationSourceV1 requires frozen source equality. A valid later
// zone publication requires a fresh plan; it cannot reuse this proposal.
func VerifyLegacySeparationSourceV1(plan LegacySeparationV1, ownership, current []byte) error {
	if err := validateLegacySeparationV1(plan); err != nil {
		return err
	}
	if !bytes.Equal(ownership, plan.LegacyOwnership) || !bytes.Equal(current, plan.LegacyState) {
		return errors.New("DNS legacy separation source changed; review a new proposal")
	}
	return nil
}

// LegacyBeforeImagesV1 returns independent copies only when every observed new
// artifact still exactly matches the proposal. A later publication or owner edit
// is not permission to restore old DNS evidence over newer work. File ownership,
// native-tree proofs, operation authority and atomic restoration remain the
// filesystem publisher/recovery executor's responsibility.
func LegacyBeforeImagesV1(plan LegacySeparationV1, acquisition, ownershipPublication, currentPublication []byte) ([]byte, []byte, error) {
	if err := validateLegacySeparationV1(plan); err != nil {
		return nil, nil, err
	}
	a, err := DecodeAcquisitionV1(acquisition)
	if err != nil {
		return nil, nil, err
	}
	before, err := DecodePublicationV1(a, ownershipPublication)
	if err != nil {
		return nil, nil, err
	}
	current, err := DecodePublicationV1(a, currentPublication)
	if err != nil {
		return nil, nil, err
	}
	if a != plan.Acquisition || before != plan.OwnershipPublication || current != plan.CurrentPublication {
		return nil, nil, errors.New("separated DNS evidence changed; preserve current state and recovery evidence")
	}
	return bytes.Clone(plan.LegacyOwnership), bytes.Clone(plan.LegacyState), nil
}
