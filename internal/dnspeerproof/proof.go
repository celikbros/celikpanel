package dnspeerproof

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"sort"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/binddns"
	"github.com/alicelik/celikpanel/internal/hostname"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
)

const (
	RequestSchemaV1  = "celikpanel-bind-peer-deletion-request/v1"
	ResponseSchemaV1 = "celikpanel-bind-peer-deletion-observation/v1"
	DefaultView      = "_default"
	MaxLifetime      = 2 * time.Minute
	MaxClockSkew     = 5 * time.Second
	maxDocumentSize  = 4096
)

// RequestV1 is minted for one inspection attempt after the caller has checked
// the exact accepted deletion and source-bound primary/secondary catalog pair.
// A later attempt must use a new cryptographically random 32-byte nonce.
type RequestV1 struct {
	Schema               string `json:"schema"`
	MutationRequestID    string `json:"mutation_request_id"`
	MutationOwnerID      string `json:"mutation_owner_id"`
	DeletionGeneration   int64  `json:"deletion_generation"`
	DeletionQualifier    string `json:"deletion_qualifier"`
	PrimaryIP            string `json:"primary_ip"`
	PeerIP               string `json:"peer_ip"`
	PeerIdentitySHA256   string `json:"peer_identity_sha256"`
	CatalogName          string `json:"catalog_name"`
	CatalogSerial        uint32 `json:"catalog_serial"`
	CatalogMembersSHA256 string `json:"catalog_members_sha256"`
	DeletedZone          string `json:"deleted_zone"`
	View                 string `json:"view"`
	Nonce                string `json:"nonce"`
	Attempt              uint64 `json:"attempt"`
	IssuedAtUnix         int64  `json:"issued_at_unix"`
	ExpiresAtUnix        int64  `json:"expires_at_unix"`
}

// ResponseV1 is an inspector assertion, not authority by itself. A caller must
// authenticate the peer independently and supply that identity to Verify.
// NativeState=unloaded means the inspector checked its own BIND loaded-zone
// state for the exact zone/view. REFUSED or NoTransfer alone is never unloaded.
type ResponseV1 struct {
	Schema               string `json:"schema"`
	RequestSHA256        string `json:"request_sha256"`
	Nonce                string `json:"nonce"`
	Attempt              uint64 `json:"attempt"`
	PrimaryIP            string `json:"primary_ip"`
	PeerIP               string `json:"peer_ip"`
	CatalogName          string `json:"catalog_name"`
	CatalogSerial        uint32 `json:"catalog_serial"`
	CatalogMembersSHA256 string `json:"catalog_members_sha256"`
	DeletedZone          string `json:"deleted_zone"`
	View                 string `json:"view"`
	CatalogState         string `json:"catalog_state"` // transferred, stale, unknown
	MemberState          string `json:"member_state"`  // absent, present, unknown
	NativeState          string `json:"native_state"`  // unloaded, loaded, unknown
	ObservedAtUnix       int64  `json:"observed_at_unix"`
}

// PeerAuthentication must be derived from an authenticated inspector channel,
// never from fields in ResponseV1. A PIN or trusted peer credential must bind
// IdentitySHA256 to the reviewed secondary; IP alone is not authentication.
type PeerAuthentication struct {
	Established    bool
	IdentitySHA256 string
	PeerIP         string
}

// ConsumeOnce must atomically and durably consume this request digest. Returning
// false includes replay and storage failure; implementations fail closed. The
// package does no I/O and supplies no in-memory substitute for durable replay.
type ConsumeOnce func(requestSHA256 string) bool

type Verified struct {
	RequestSHA256  string
	RequestID      string
	OwnerID        string
	DeletedZone    string
	ObservedAtUnix int64
}

type Code string

const (
	CodeInvalid         Code = "invalid_proof"
	CodeUnauthenticated Code = "peer_unauthenticated"
	CodeExpired         Code = "proof_expired"
	CodeMismatch        Code = "proof_context_mismatch"
	CodeUnverified      Code = "native_deletion_unverified"
	CodeReplay          Code = "proof_replayed_or_unrecorded"
)

type ValidationError struct{ Code Code }

func (e ValidationError) Error() string { return string(e.Code) }

func fail(code Code) error { return ValidationError{Code: code} }

func canonicalIPv4(value string) bool {
	ip := net.ParseIP(value)
	return ip != nil && ip.To4() != nil && ip.String() == value && ip.IsGlobalUnicast()
}

func hexOfBytes(value string, count int) bool {
	if len(value) != count*2 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func canonicalName(value string) bool {
	name, err := hostname.CanonicalFQDN(value)
	return err == nil && name == value
}

func (r RequestV1) Validate() error {
	if r.Schema != RequestSchemaV1 ||
		!servicemutationledger.ValidIdentity(r.MutationRequestID) ||
		!servicemutationledger.ValidIdentity(r.MutationOwnerID) ||
		r.DeletionGeneration < 0 ||
		!mutationpayload.ValidDNSZoneSyncV3Qualifier(r.DeletionQualifier) ||
		!canonicalIPv4(r.PrimaryIP) || !canonicalIPv4(r.PeerIP) ||
		r.PrimaryIP == r.PeerIP || !hexOfBytes(r.PeerIdentitySHA256, 32) ||
		r.CatalogSerial == 0 || !hexOfBytes(r.CatalogMembersSHA256, 32) ||
		!canonicalName(r.DeletedZone) || r.DeletedZone == r.CatalogName ||
		r.View != DefaultView || !hexOfBytes(r.Nonce, 32) || r.Attempt == 0 ||
		r.IssuedAtUnix <= 0 || r.ExpiresAtUnix <= r.IssuedAtUnix ||
		r.ExpiresAtUnix-r.IssuedAtUnix > int64(MaxLifetime/time.Second) {
		return fail(CodeInvalid)
	}
	catalog, err := binddns.CatalogDomain(r.PrimaryIP)
	if err != nil || catalog != r.CatalogName {
		return fail(CodeInvalid)
	}
	return nil
}

func EncodeRequest(r RequestV1) ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(r)
}

func DecodeRequest(raw []byte) (RequestV1, error) {
	var r RequestV1
	if len(raw) == 0 || len(raw) > maxDocumentSize || decodeCanonical(raw, &r) != nil || r.Validate() != nil {
		return RequestV1{}, fail(CodeInvalid)
	}
	return r, nil
}

func (r ResponseV1) Validate() error {
	if r.Schema != ResponseSchemaV1 || !hexOfBytes(r.RequestSHA256, 32) ||
		!hexOfBytes(r.Nonce, 32) || r.Attempt == 0 ||
		!canonicalIPv4(r.PrimaryIP) || !canonicalIPv4(r.PeerIP) ||
		r.PrimaryIP == r.PeerIP || !canonicalName(r.CatalogName) ||
		r.CatalogSerial == 0 || !hexOfBytes(r.CatalogMembersSHA256, 32) ||
		!canonicalName(r.DeletedZone) || r.DeletedZone == r.CatalogName ||
		r.View != DefaultView || r.ObservedAtUnix <= 0 {
		return fail(CodeInvalid)
	}
	switch r.CatalogState {
	case "transferred", "stale", "unknown":
	default:
		return fail(CodeInvalid)
	}
	switch r.MemberState {
	case "absent", "present", "unknown":
	default:
		return fail(CodeInvalid)
	}
	switch r.NativeState {
	case "unloaded", "loaded", "unknown":
	default:
		return fail(CodeInvalid)
	}
	return nil
}

func EncodeResponse(r ResponseV1) ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(r)
}

func DecodeResponse(raw []byte) (ResponseV1, error) {
	var r ResponseV1
	if len(raw) == 0 || len(raw) > maxDocumentSize || decodeCanonical(raw, &r) != nil || r.Validate() != nil {
		return ResponseV1{}, fail(CodeInvalid)
	}
	return r, nil
}

func decodeCanonical(raw []byte, out any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return err
	}
	canonical, err := json.Marshal(out)
	if err != nil || !bytes.Equal(raw, canonical) {
		return errors.New("noncanonical proof document")
	}
	return nil
}

func RequestSHA256(request RequestV1) (string, error) {
	raw, err := EncodeRequest(request)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// CatalogMembersSHA256 is a semantic digest of the exact transferred catalog
// member set and serial, independent of BIND's local zone-file presentation.
// The inspector must derive this from its native transferred catalog state.
func CatalogMembersSHA256(primaryIP string, serial uint32, members []string) (string, error) {
	catalog, err := binddns.CatalogDomain(primaryIP)
	if err != nil || serial == 0 || len(members) > 65536 {
		return "", fail(CodeInvalid)
	}
	frozen := append([]string(nil), members...)
	sort.Strings(frozen)
	for index, member := range frozen {
		if !canonicalName(member) || member == catalog || (index > 0 && member == frozen[index-1]) {
			return "", fail(CodeInvalid)
		}
	}
	h := sha256.New()
	writeFrame(h, []byte("celikpanel-bind-peer-catalog-members/v1"))
	writeFrame(h, []byte(catalog))
	var number [4]byte
	binary.BigEndian.PutUint32(number[:], serial)
	_, _ = h.Write(number[:])
	binary.BigEndian.PutUint32(number[:], uint32(len(frozen)))
	_, _ = h.Write(number[:])
	for _, member := range frozen {
		writeFrame(h, []byte(member))
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

type hashWriter interface{ Write([]byte) (int, error) }

func writeFrame(h hashWriter, value []byte) {
	var size [4]byte
	binary.BigEndian.PutUint32(size[:], uint32(len(value)))
	_, _ = h.Write(size[:])
	_, _ = h.Write(value)
}

// Verify accepts only a fresh native-unloaded result for this exact request.
// The supplied authentication and consume callback are external trust inputs.
// A caller must also retain the existing source-bound catalog and NoTransfer
// checks and recheck the current accepted operation before terminal success.
func Verify(expected RequestV1, response ResponseV1, peer PeerAuthentication, now time.Time, consume ConsumeOnce) (Verified, error) {
	if expected.Validate() != nil || response.Validate() != nil {
		return Verified{}, fail(CodeInvalid)
	}
	if !peer.Established || peer.IdentitySHA256 != expected.PeerIdentitySHA256 || peer.PeerIP != expected.PeerIP {
		return Verified{}, fail(CodeUnauthenticated)
	}
	seconds := now.Unix()
	if seconds < expected.IssuedAtUnix-int64(MaxClockSkew/time.Second) ||
		seconds > expected.ExpiresAtUnix ||
		response.ObservedAtUnix < expected.IssuedAtUnix ||
		response.ObservedAtUnix > seconds+int64(MaxClockSkew/time.Second) ||
		response.ObservedAtUnix > expected.ExpiresAtUnix {
		return Verified{}, fail(CodeExpired)
	}
	digest, _ := RequestSHA256(expected)
	if response.RequestSHA256 != digest || response.Nonce != expected.Nonce ||
		response.Attempt != expected.Attempt || response.PrimaryIP != expected.PrimaryIP ||
		response.PeerIP != expected.PeerIP || response.CatalogName != expected.CatalogName ||
		response.CatalogSerial != expected.CatalogSerial ||
		response.CatalogMembersSHA256 != expected.CatalogMembersSHA256 ||
		response.DeletedZone != expected.DeletedZone || response.View != expected.View {
		return Verified{}, fail(CodeMismatch)
	}
	if response.CatalogState != "transferred" || response.MemberState != "absent" ||
		response.NativeState != "unloaded" {
		return Verified{}, fail(CodeUnverified)
	}
	if consume == nil || !consume(digest) {
		return Verified{}, fail(CodeReplay)
	}
	return Verified{digest, expected.MutationRequestID, expected.MutationOwnerID, expected.DeletedZone, response.ObservedAtUnix}, nil
}

func IsCode(err error, code Code) bool {
	var validation ValidationError
	return errors.As(err, &validation) && validation.Code == code
}
