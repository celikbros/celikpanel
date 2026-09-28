package dnsengineartifact

import (
	"encoding/json"
	"errors"
)

// Documents keep their component schemas together in one atomically replaceable
// file. They have no external artifact dependency that a historical snapshot
// could omit. Ownership retains its frozen publication checkpoint; current state
// has a separately evolving publication bound to the same acquisition.
const (
	StateDocumentSchemaV2     = "celikpanel-dns-engine-state/v2"
	OwnershipDocumentSchemaV2 = "celikpanel-dns-engine-ownership/v2"
)

type documentV2 struct {
	Schema      string              `json:"schema"`
	Acquisition AcquisitionRecordV1 `json:"acquisition"`
	Publication PublicationRecordV1 `json:"publication"`
}

func CanonicalStateDocumentV2(state StateV1) ([]byte, error) {
	if state.NativeCatalogV3 != "" {
		return CanonicalStateDocumentV3(state)
	}
	return canonicalDocumentV2(StateDocumentSchemaV2, state)
}

func CanonicalOwnershipDocumentV2(state StateV1) ([]byte, error) {
	if state.NativeCatalogV3 != "" {
		return CanonicalOwnershipDocumentV3(state)
	}
	return canonicalDocumentV2(OwnershipDocumentSchemaV2, state)
}

func canonicalDocumentV2(schema string, state StateV1) ([]byte, error) {
	if state.NativeCatalogV3 != "" {
		return nil, errors.New("native PowerDNS marker requires state-v3 document")
	}
	a, p, err := SeparateV1(state)
	if err != nil {
		return nil, err
	}
	return canonicalRecord(documentV2{Schema: schema, Acquisition: a, Publication: p})
}

// DecodeStateDocument and DecodeOwnershipDocument return the validated semantic
// v1 projection and whether the file uses separated records. Legacy bytes are
// never normalized or rewritten by a read. Cross-role v2 documents are refused.
func DecodeStateDocument(data []byte) (StateV1, bool, error) {
	var header struct {
		Schema string `json:"schema"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return StateV1{}, false, err
	}
	if header.Schema == StateDocumentSchemaV3 {
		state, err := DecodeStateDocumentV3(data)
		return state, err == nil, err
	}
	return decodeDocument(data, StateDocumentSchemaV2)
}
func DecodeOwnershipDocument(data []byte) (StateV1, bool, error) {
	var header struct {
		Schema string `json:"schema"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return StateV1{}, false, err
	}
	if header.Schema == OwnershipDocumentSchemaV3 {
		state, err := DecodeOwnershipDocumentV3(data)
		return state, err == nil, err
	}
	return decodeDocument(data, OwnershipDocumentSchemaV2)
}
func decodeDocument(data []byte, schema string) (StateV1, bool, error) {
	if len(data) == 0 || len(data) > recordLimit {
		return StateV1{}, false, errors.New("DNS document has an invalid size")
	}
	var header struct {
		Schema string `json:"schema"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return StateV1{}, false, err
	}
	if header.Schema == StateSchemaV1 {
		state, err := DecodeV1(data)
		return state, false, err
	}
	if header.Schema != schema {
		return StateV1{}, false, errors.New("unsupported DNS document schema or role")
	}
	var document documentV2
	var state StateV1
	err := decodeCanonicalRecord(data, recordLimit, &document, func() ([]byte, error) {
		var err error
		state, err = CombineV1(document.Acquisition, document.Publication)
		if err != nil {
			return nil, err
		}
		return canonicalDocumentV2(schema, state)
	})
	if err != nil {
		return StateV1{}, false, err
	}
	return state, true, nil
}

// SeparationDocumentsV2 assembles a validated legacy proposal into two complete
// documents. The caller publishes them only inside an accepted operation with
// exact source/checkpoint protection. A mixed legacy/v2 pair remains readable;
// a changed acquisition is refused regardless of representation.
func SeparationDocumentsV2(plan LegacySeparationV1) (ownership, current []byte, err error) {
	if err = validateLegacySeparationV1(plan); err != nil {
		return nil, nil, err
	}
	before, err := CombineV1(plan.Acquisition, plan.OwnershipPublication)
	if err != nil {
		return nil, nil, err
	}
	after, err := CombineV1(plan.Acquisition, plan.CurrentPublication)
	if err != nil {
		return nil, nil, err
	}
	ownership, err = CanonicalOwnershipDocumentV2(before)
	if err != nil {
		return nil, nil, err
	}
	current, err = CanonicalStateDocumentV2(after)
	if err != nil {
		return nil, nil, err
	}
	return ownership, current, nil
}
