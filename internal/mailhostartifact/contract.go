// Package mailhostartifact defines the frozen mail certificate evidence format.
// It has no Agent, database, license, service manager or filesystem dependency.
package mailhostartifact

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
)

const (
	Directory      = "/etc/ssl/celikpanel/_mail/host"
	ReceiptName    = ".mail-host-certificate-receipt.json"
	ReceiptSchema  = "mail-host-certificate-receipt/v1"
	ReceiptMaxSize = 1024
	PendingMaxSize = 512
)

type Receipt struct {
	Schema     string `json:"schema"`
	RequestID  string `json:"request_id"`
	Qualifier  string `json:"qualifier"`
	Domain     string `json:"domain"`
	LeafSHA256 string `json:"leaf_sha256"`
}

var receiptDomain = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$`)
var qualifierPattern = regexp.MustCompile(`^mhc1:[0-9a-f]{64}$`)

// ValidReceiptDomain preserves v1 historical parsing; enrollment separately
// requires the caller's canonical FQDN validation. Parsing grants no authority.
func ValidReceiptDomain(domain string) bool { return receiptDomain.MatchString(domain) }
func ValidQualifier(value string) bool      { return qualifierPattern.MatchString(value) }
func validRequestID(value string) bool {
	if len(value) != 32 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
func LeafSHA256(der []byte) string { v := sha256.Sum256(der); return hex.EncodeToString(v[:]) }
func ValidateLeafSHA256(value string) error {
	if len(value) != 64 || strings.ToLower(value) != value {
		return errors.New("invalid panel certificate leaf SHA-256")
	}
	raw, err := hex.DecodeString(value)
	if err != nil || len(raw) != sha256.Size {
		return errors.New("invalid panel certificate leaf SHA-256")
	}
	return nil
}

// LineageName preserves the historical deterministic name; it does not validate
// or authorize a domain, path, renewal or publication.
func LineageName(domain string) string {
	v := sha256.Sum256([]byte(domain))
	return "celikpanel-mail-" + hex.EncodeToString(v[:12])
}

func NewReceipt(
	requestID, qualifier, domain string,
	leafDER []byte,
) (Receipt, error) {
	receipt := Receipt{
		Schema:     ReceiptSchema,
		RequestID:  requestID,
		Qualifier:  qualifier,
		Domain:     domain,
		LeafSHA256: LeafSHA256(leafDER),
	}
	if err := ValidateReceipt(receipt); err != nil {
		return Receipt{}, err
	}
	return receipt, nil
}

func ValidateReceipt(receipt Receipt) error {
	if receipt.Schema != ReceiptSchema ||
		!validRequestID(receipt.RequestID) ||
		!ValidQualifier(receipt.Qualifier) ||
		!ValidReceiptDomain(receipt.Domain) ||
		receipt.Domain != strings.ToLower(strings.TrimSpace(receipt.Domain)) {
		return errors.New("invalid mail host certificate issue receipt identity")
	}
	if err := ValidateLeafSHA256(receipt.LeafSHA256); err != nil {
		return err
	}
	return nil
}

func CanonicalReceipt(
	receipt Receipt,
) ([]byte, error) {
	if err := ValidateReceipt(receipt); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(receipt)
	if err != nil {
		return nil, fmt.Errorf("encode mail host certificate issue receipt: %w", err)
	}
	raw = append(raw, '\n')
	if len(raw) > ReceiptMaxSize {
		return nil, errors.New("mail host certificate issue receipt exceeds size limit")
	}
	return raw, nil
}

func DecodeReceipt(raw []byte) (Receipt, error) {
	if len(raw) == 0 || len(raw) > ReceiptMaxSize {
		return Receipt{}, errors.New(
			"mail host certificate issue receipt has invalid size",
		)
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	var receipt Receipt
	if err := decoder.Decode(&receipt); err != nil {
		return Receipt{}, fmt.Errorf(
			"decode mail host certificate issue receipt: %w", err,
		)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("multiple JSON values")
		}
		return Receipt{}, fmt.Errorf(
			"decode mail host certificate issue receipt trailer: %w", err,
		)
	}
	canonical, err := CanonicalReceipt(receipt)
	if err != nil {
		return Receipt{}, err
	}
	if subtle.ConstantTimeCompare(raw, canonical) != 1 {
		return Receipt{}, errors.New(
			"mail host certificate issue receipt is not canonical JSON",
		)
	}
	return receipt, nil
}

type Pending struct {
	Lineage    string `json:"lineage"`
	LeafSHA256 string `json:"leaf_sha256"`
}

func DecodePending(raw []byte) (Pending, error) {
	var value Pending
	if len(raw) > PendingMaxSize || json.Unmarshal(raw, &value) != nil {
		return value, errors.New("invalid host renewal queue")
	}
	canonical, _ := json.Marshal(value)
	if !bytes.Equal(raw, canonical) {
		return value, errors.New("noncanonical host renewal queue")
	}
	suffix := strings.TrimPrefix(value.Lineage, "celikpanel-mail-")
	if suffix == value.Lineage || len(suffix) != 24 {
		return value, errors.New("invalid host renewal lineage")
	}
	decoded, err := hex.DecodeString(suffix)
	if err != nil || hex.EncodeToString(decoded) != suffix {
		return value, errors.New("invalid host renewal lineage")
	}
	if err := ValidateLeafSHA256(value.LeafSHA256); err != nil {
		return value, err
	}
	return value, nil
}

func CanonicalPending(value Pending) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	if _, err = DecodePending(raw); err != nil {
		return nil, err
	}
	return raw, nil
}
