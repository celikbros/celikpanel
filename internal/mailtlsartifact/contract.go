// Package mailtlsartifact owns the historical v1 mail TLS accepted-plan bytes.
// A valid plan describes accepted configuration; it does not prove present host
// convergence, acquire ownership or authorize a filesystem or service mutation.
package mailtlsartifact

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

const (
	Version           = 1
	MaxSize           = 2 << 20
	JournalFileName   = "mail-tls-sync-journal.json"
	CommittedFileName = "mail-tls-committed.json"
)

// Plan preserves the historical field order and omitted empty SNI representation.
type Plan struct {
	Version     int                      `json:"version"`
	RequestID   string                   `json:"request_id"`
	Qualifier   string                   `json:"qualifier"`
	ManagedRoot string                   `json:"managed_root"`
	Myhostname  string                   `json:"myhostname"`
	SNI         []transport.MailSNIEntry `json:"sni,omitempty"`
}

func EqualSNI(left, right []transport.MailSNIEntry) bool {
	leftRaw, leftErr := json.Marshal(left)
	rightRaw, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftRaw, rightRaw)
}

func Equal(left, right *Plan) bool {
	if left == nil || right == nil {
		return left == right
	}
	leftRaw, leftErr := Encode(left)
	rightRaw, rightErr := Encode(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftRaw, rightRaw)
}

func Validate(journal *Plan) error {
	if journal == nil || journal.Version != Version ||
		!validRequestID(journal.RequestID) ||
		!mutationpayload.ValidMailTLSSyncQualifier(journal.Qualifier) {
		return errors.New("mail TLS sync journal identity is invalid")
	}
	canonical, err := mutationpayload.CanonicalMailTLSSync(
		journal.ManagedRoot, journal.Myhostname, journal.SNI,
	)
	if err != nil || canonical.Qualifier != journal.Qualifier ||
		canonical.ManagedRoot != journal.ManagedRoot ||
		canonical.Myhostname != journal.Myhostname ||
		!EqualSNI(canonical.SNI, journal.SNI) {
		return errors.New("mail TLS sync journal payload is not canonical")
	}
	return nil
}

func Encode(journal *Plan) ([]byte, error) {
	if err := Validate(journal); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(journal)
	if err != nil {
		return nil, err
	}
	if len(raw) > MaxSize {
		return nil, errors.New("mail TLS sync journal exceeds the size limit")
	}
	return raw, nil
}

func Decode(raw []byte) (*Plan, error) {
	if len(raw) == 0 || len(raw) > MaxSize {
		return nil, errors.New("mail TLS sync journal has invalid size")
	}
	var journal Plan
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&journal); err != nil {
		return nil, fmt.Errorf("decode mail TLS sync journal: %w", err)
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("mail TLS sync journal contains trailing JSON")
	}
	canonical, err := Encode(&journal)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(raw, canonical) {
		return nil, errors.New("mail TLS sync journal is not canonical")
	}
	return &journal, nil
}

func validRequestID(value string) bool {
	if len(value) != 32 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
