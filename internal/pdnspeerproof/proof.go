// Package pdnspeerproof domain-separates a PowerDNS native deletion challenge
// from the existing BIND inspector. It shares the reviewed contextual checks,
// but neither wire document nor replay digest can be accepted as BIND evidence.
package pdnspeerproof

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/alicelik/celikpanel/internal/dnspeerproof"
)

const (
	RequestSchemaV1  = "celikpanel-pdns-peer-deletion-request/v1"
	ResponseSchemaV1 = "celikpanel-pdns-peer-deletion-observation/v1"
	maxDocumentSize  = 4096
)

type RequestV1 dnspeerproof.RequestV1
type ResponseV1 dnspeerproof.ResponseV1
type PeerAuthentication = dnspeerproof.PeerAuthentication
type Verified = dnspeerproof.Verified
type ConsumeOnce = dnspeerproof.ConsumeOnce

func (r RequestV1) Validate() error {
	if r.Schema != RequestSchemaV1 {
		return errors.New("invalid PowerDNS request schema")
	}
	base := dnspeerproof.RequestV1(r)
	base.Schema = dnspeerproof.RequestSchemaV1
	return base.Validate()
}
func (r ResponseV1) Validate() error {
	if r.Schema != ResponseSchemaV1 {
		return errors.New("invalid PowerDNS response schema")
	}
	base := dnspeerproof.ResponseV1(r)
	base.Schema = dnspeerproof.ResponseSchemaV1
	return base.Validate()
}
func DecodeRequest(raw []byte) (RequestV1, error) {
	var r RequestV1
	if !decodeCanonical(raw, &r) || r.Validate() != nil {
		return RequestV1{}, errors.New("invalid PowerDNS request")
	}
	return r, nil
}
func DecodeResponse(raw []byte) (ResponseV1, error) {
	var r ResponseV1
	if !decodeCanonical(raw, &r) || r.Validate() != nil {
		return ResponseV1{}, errors.New("invalid PowerDNS response")
	}
	return r, nil
}
func EncodeRequest(r RequestV1) ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(r)
}
func EncodeResponse(r ResponseV1) ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(r)
}
func decodeCanonical(raw []byte, out any) bool {
	if len(raw) == 0 || len(raw) > maxDocumentSize {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(out) != nil {
		return false
	}
	canonical, err := json.Marshal(out)
	return err == nil && bytes.Equal(raw, canonical)
}
func RequestSHA256(r RequestV1) (string, error) {
	raw, err := EncodeRequest(r)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
func CatalogMembersSHA256(primaryIP string, serial uint32, members []string) (string, error) {
	return dnspeerproof.CatalogMembersSHA256(primaryIP, serial, members)
}

// Verify retains the existing authenticated-peer, exact-context, time and
// durable consume-once requirements while consuming the PDNS wire digest.
func Verify(expected RequestV1, response ResponseV1, peer PeerAuthentication, now time.Time, consume ConsumeOnce) (Verified, error) {
	if expected.Validate() != nil || response.Validate() != nil || consume == nil {
		return Verified{}, errors.New("invalid PowerDNS proof")
	}
	pdnsDigest, err := RequestSHA256(expected)
	if err != nil || response.RequestSHA256 != pdnsDigest {
		return Verified{}, errors.New("PowerDNS request digest mismatch")
	}
	baseRequest := dnspeerproof.RequestV1(expected)
	baseRequest.Schema = dnspeerproof.RequestSchemaV1
	baseResponse := dnspeerproof.ResponseV1(response)
	baseResponse.Schema = dnspeerproof.ResponseSchemaV1
	bindDigest, err := dnspeerproof.RequestSHA256(baseRequest)
	if err != nil {
		return Verified{}, err
	}
	baseResponse.RequestSHA256 = bindDigest
	verified, err := dnspeerproof.Verify(baseRequest, baseResponse, peer, now, func(digest string) bool { return digest == bindDigest && consume(pdnsDigest) })
	if err != nil {
		return Verified{}, err
	}
	verified.RequestSHA256 = pdnsDigest
	return verified, nil
}
