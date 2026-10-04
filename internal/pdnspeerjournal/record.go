package pdnspeerjournal

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/alicelik/celikpanel/internal/pdnspeerproof"
)

const (
	SchemaV1         = "celikpanel-pdns-peer-challenge-journal/v1"
	StateOutstanding = "outstanding"
	StateConsumed    = "consumed"
	MaxBytes         = 8192
)

// Path is a separate, agent-private sidecar. Missing storage is unknown for an
// active operation; no reader may reconstruct an outstanding challenge.
const Path = "/var/lib/celikpanel-agent-private/pdns-peer-challenge-v1.json"

type Code string

const (
	Missing  Code = "pdns_peer_challenge_missing"
	Unknown  Code = "pdns_peer_challenge_unknown"
	Mismatch Code = "pdns_peer_challenge_context_mismatch"
	Replay   Code = "pdns_peer_challenge_replayed"
)

type StateError struct{ Code Code }

func (e StateError) Error() string { return string(e.Code) }
func IsCode(err error, code Code) bool {
	var value StateError
	return errors.As(err, &value) && value.Code == code
}

// RecordV1 freezes the exact challenge and reviewed enrollment digest. A
// consumed record remains durable; it is never interpreted as another success.
type RecordV1 struct {
	Schema           string                  `json:"schema"`
	State            string                  `json:"state"`
	LedgerAttempt    uint64                  `json:"ledger_attempt"`
	EnrollmentSHA256 string                  `json:"enrollment_sha256"`
	RequestSHA256    string                  `json:"request_sha256"`
	Request          pdnspeerproof.RequestV1 `json:"request"`
}

func hex32(value string) bool {
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func (r RecordV1) Validate() error {
	if r.Schema != SchemaV1 || (r.State != StateOutstanding && r.State != StateConsumed) || r.LedgerAttempt == 0 ||
		!hex32(r.EnrollmentSHA256) || !hex32(r.RequestSHA256) {
		return StateError{Unknown}
	}
	digest, err := pdnspeerproof.RequestSHA256(r.Request)
	if err != nil || digest != r.RequestSHA256 {
		return StateError{Unknown}
	}
	return nil
}

func Encode(r RecordV1) ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(r)
	if err != nil || len(raw) > MaxBytes {
		return nil, StateError{Unknown}
	}
	return raw, nil
}

func Decode(raw []byte) (RecordV1, error) {
	var record RecordV1
	if len(raw) == 0 || len(raw) > MaxBytes {
		return RecordV1{}, StateError{Unknown}
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&record) != nil || record.Validate() != nil {
		return RecordV1{}, StateError{Unknown}
	}
	canonical, err := json.Marshal(record)
	if err != nil || !bytes.Equal(raw, canonical) {
		return RecordV1{}, StateError{Unknown}
	}
	return record, nil
}

// sameContext deliberately excludes nonce, attempt and issue/expiry time.
// Advancing catalog or enrollment state requires a new accepted operation,
// never an implicit retry of this one.
func sameContext(a, b RecordV1) bool {
	x, y := a.Request, b.Request
	return a.EnrollmentSHA256 == b.EnrollmentSHA256 &&
		x.MutationRequestID == y.MutationRequestID && x.MutationOwnerID == y.MutationOwnerID &&
		x.DeletionGeneration == y.DeletionGeneration && x.DeletionQualifier == y.DeletionQualifier &&
		x.PrimaryIP == y.PrimaryIP && x.PeerIP == y.PeerIP &&
		x.PeerIdentitySHA256 == y.PeerIdentitySHA256 && x.CatalogName == y.CatalogName &&
		x.CatalogSerial == y.CatalogSerial && x.CatalogMembersSHA256 == y.CatalogMembersSHA256 &&
		x.DeletedZone == y.DeletedZone && x.View == y.View
}
