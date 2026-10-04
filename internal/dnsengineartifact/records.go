package dnsengineartifact

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/alicelik/celikpanel/internal/transport"
)

const (
	AcquisitionSchemaV1 = "celikpanel-dns-engine-acquisition/v1"
	PublicationSchemaV1 = "celikpanel-dns-engine-publication/v1"
	recordLimit         = 64 << 10
)

// AcquisitionRecordV1 is immutable for an engine tenure. It has no generation,
// catalog serial, observation, or health fields. Its digest binds publications;
// that content binding is not a signature or permission to mutate native DNS.
type AcquisitionRecordV1 struct {
	Schema string `json:"schema"`
	AcquisitionV1
}

// PublicationRecordV1 binds configuration evidence to one exact acquisition.
// Its generation/catalog fields do not establish the daemon's current state.
type PublicationRecordV1 struct {
	Schema            string `json:"schema"`
	AcquisitionSHA256 string `json:"acquisition_sha256"`
	PublicationV1
}

func CanonicalAcquisitionV1(record AcquisitionRecordV1) ([]byte, error) {
	if record.Schema != AcquisitionSchemaV1 {
		return nil, errors.New("unsupported DNS acquisition schema")
	}
	if err := validateAcquisition(record.AcquisitionV1); err != nil {
		return nil, err
	}
	return canonicalRecord(record)
}

func DecodeAcquisitionV1(data []byte) (AcquisitionRecordV1, error) {
	var record AcquisitionRecordV1
	err := decodeCanonicalRecord(data, recordLimit, &record, func() ([]byte, error) {
		return CanonicalAcquisitionV1(record)
	})
	if err != nil {
		return AcquisitionRecordV1{}, err
	}
	return record, nil
}

func acquisitionDigest(record AcquisitionRecordV1) (string, error) {
	data, err := CanonicalAcquisitionV1(record)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func CanonicalPublicationV1(acquisition AcquisitionRecordV1, record PublicationRecordV1) ([]byte, error) {
	digest, err := acquisitionDigest(acquisition)
	if err != nil {
		return nil, err
	}
	if record.Schema != PublicationSchemaV1 || record.AcquisitionSHA256 != digest {
		return nil, errors.New("DNS publication schema or acquisition binding differs")
	}
	if err := validatePublication(acquisition.AcquisitionV1, record.PublicationV1); err != nil {
		return nil, err
	}
	return canonicalRecord(record)
}

func DecodePublicationV1(acquisition AcquisitionRecordV1, data []byte) (PublicationRecordV1, error) {
	var record PublicationRecordV1
	err := decodeCanonicalRecord(data, recordLimit, &record, func() ([]byte, error) {
		return CanonicalPublicationV1(acquisition, record)
	})
	if err != nil {
		return PublicationRecordV1{}, err
	}
	return record, nil
}

// SeparateV1 is a lossless conversion, not migration admission. In particular it
// does not prove a legacy ownership/current pair, native tree, lock, or journal.
func SeparateV1(state StateV1) (AcquisitionRecordV1, PublicationRecordV1, error) {
	acquisition, publication, err := SplitV1(state)
	if err != nil {
		return AcquisitionRecordV1{}, PublicationRecordV1{}, err
	}
	a := AcquisitionRecordV1{Schema: AcquisitionSchemaV1, AcquisitionV1: acquisition}
	digest, err := acquisitionDigest(a)
	if err != nil {
		return AcquisitionRecordV1{}, PublicationRecordV1{}, err
	}
	return a, PublicationRecordV1{
		Schema: PublicationSchemaV1, AcquisitionSHA256: digest, PublicationV1: publication,
	}, nil
}

// CombineV1 preserves the legacy canonical producer's exact field order and
// omission rules. This cannot authorize downgrading an installed application.
func CombineV1(acquisition AcquisitionRecordV1, publication PublicationRecordV1) (StateV1, error) {
	if _, err := CanonicalPublicationV1(acquisition, publication); err != nil {
		return StateV1{}, err
	}
	a, p := acquisition.AcquisitionV1, publication.PublicationV1
	state := StateV1{
		Schema: StateSchemaV1, Mode: a.Mode, Engine: a.Engine, EngineEpoch: a.EngineEpoch,
		PairRole: a.PairRole, PairLocalIP: a.PairLocalIP, PairPeerIP: a.PairPeerIP,
		SourceRevision: a.SourceRevision, ManifestQualifier: a.ManifestQualifier,
		MutationRequestID: a.MutationRequestID, MutationOwnerID: a.MutationOwnerID,
		Generation: p.Generation, PrimaryCatalogSerial: p.PrimaryCatalogSerial,
		NativeCatalogV3: a.NativeCatalogV3,
	}
	if err := ValidateV1(state); err != nil {
		return StateV1{}, err
	}
	return state, nil
}

// ComparePublicationsV1 only admits evolution within one immutable acquisition.
// A LaterBINDPublication result still requires the existing caller's native
// generation proof. Neither result can replace exact frozen snapshot equality.
func ComparePublicationsV1(acquisition AcquisitionRecordV1, before, after PublicationRecordV1) (Relationship, error) {
	if _, err := CanonicalPublicationV1(acquisition, before); err != nil {
		return 0, err
	}
	if _, err := CanonicalPublicationV1(acquisition, after); err != nil {
		return 0, err
	}
	if before == after {
		return SamePublication, nil
	}
	if acquisition.Engine != transport.DNSEngineBIND ||
		acquisition.PairRole == transport.DNSPairRoleSecondary ||
		before.Generation == after.Generation || after.PrimaryCatalogSerial < before.PrimaryCatalogSerial {
		return 0, errors.New("DNS engine publication is not a supported continuation of its acquisition")
	}
	return LaterBINDPublication, nil
}

func canonicalRecord(record any) ([]byte, error) {
	data, err := json.Marshal(record)
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// Canonical equality also rejects duplicate keys, alternate field casing,
// whitespace and omitted required zero-valued fields that JSON alone accepts.
func decodeCanonicalRecord(data []byte, limit int, target any, canonical func() ([]byte, error)) error {
	if len(data) == 0 || len(data) > limit {
		return errors.New("DNS evidence has an invalid size")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode DNS evidence: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return errors.New("DNS evidence contains trailing JSON")
	}
	encoded, err := canonical()
	if err != nil {
		return err
	}
	if !bytes.Equal(data, encoded) {
		return errors.New("DNS evidence is not canonical JSON")
	}
	return nil
}
