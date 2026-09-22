// Package mailrenewalintent binds one unattended renewal to the observed prior
// selection before mutation admission. Decoding evidence grants no authority.
package mailrenewalintent

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/alicelik/celikpanel/internal/mailhostartifact"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
)

const Schema = "celikpanel-mail-renewal-before/v1"
const MaxSize = 4096

type Before struct {
	Schema                  string                   `json:"schema"`
	RequestID               string                   `json:"request_id"`
	OwnerID                 string                   `json:"owner_id"`
	BuildCommit             string                   `json:"build_commit"`
	Qualifier               string                   `json:"qualifier"`
	Pending                 mailhostartifact.Pending `json:"pending"`
	PreviousReceipt         mailhostartifact.Receipt `json:"previous_receipt"`
	PreviousSelectionSHA256 string                   `json:"previous_selection_sha256"`
}

// Identity preserves the existing v1 operation identity. Cross-build adoption
// remains explicit: a caller cannot replace a retained build or source leaf.
func Identity(domain, build string, leafDER []byte) (request, owner, qualifier string, err error) {
	commitment, err := mutationpayload.CanonicalMailHostCertificate(domain, "renewal@celikpanel.invalid", build)
	if err != nil || len(leafDER) == 0 {
		return "", "", "", errors.New("invalid mail renewal source identity")
	}
	digest := sha256.Sum256(append([]byte("mail-host-renewal/v1/"+domain+"/"+build+"/"), leafDER...))
	return hex.EncodeToString(digest[:16]), hex.EncodeToString(digest[16:]), commitment.Qualifier, nil
}

func New(build string, leafDER []byte, previous mailhostartifact.Receipt, selectionSHA string) (Before, error) {
	id, owner, qualifier, err := Identity(previous.Domain, build, leafDER)
	if err != nil {
		return Before{}, err
	}
	result := Before{Schema: Schema, RequestID: id, OwnerID: owner, BuildCommit: build, Qualifier: qualifier, Pending: mailhostartifact.Pending{Lineage: mailhostartifact.LineageName(previous.Domain), LeafSHA256: mailhostartifact.LeafSHA256(leafDER)}, PreviousReceipt: previous, PreviousSelectionSHA256: selectionSHA}
	if err = Validate(result); err != nil {
		return Before{}, err
	}
	return result, nil
}
func validID(value string) bool {
	if len(value) != 32 {
		return false
	}
	raw, e := hex.DecodeString(value)
	return e == nil && hex.EncodeToString(raw) == value
}
func Validate(value Before) error {
	if value.Schema != Schema || !validID(value.RequestID) || !validID(value.OwnerID) {
		return errors.New("invalid renewal before-image identity")
	}
	if e := mailhostartifact.ValidateReceipt(value.PreviousReceipt); e != nil {
		return e
	}
	if _, e := mailhostartifact.CanonicalPending(value.Pending); e != nil {
		return e
	}
	if e := mailhostartifact.ValidateLeafSHA256(value.PreviousSelectionSHA256); e != nil {
		return e
	}
	commitment, e := mutationpayload.CanonicalMailHostCertificate(value.PreviousReceipt.Domain, "renewal@celikpanel.invalid", value.BuildCommit)
	if e != nil || commitment.Qualifier != value.Qualifier || value.Pending.Lineage != mailhostartifact.LineageName(value.PreviousReceipt.Domain) || value.PreviousReceipt.LeafSHA256 == value.Pending.LeafSHA256 || value.PreviousReceipt.RequestID == value.RequestID {
		return errors.New("renewal before-image scope differs")
	}
	return nil
}
func Canonical(value Before) ([]byte, error) {
	if e := Validate(value); e != nil {
		return nil, e
	}
	raw, e := json.Marshal(value)
	if e != nil {
		return nil, e
	}
	return append(raw, '\n'), nil
}
func Decode(raw []byte) (Before, error) {
	var result Before
	if len(raw) == 0 || len(raw) > MaxSize || json.Unmarshal(raw, &result) != nil {
		return Before{}, errors.New("invalid renewal before-image encoding")
	}
	canonical, e := Canonical(result)
	if e != nil {
		return Before{}, e
	}
	if !bytes.Equal(raw, canonical) {
		return Before{}, errors.New("noncanonical renewal before-image")
	}
	return result, nil
}

// VerifySource separately recomputes opaque request/owner IDs from actual source
// DER. A syntactically valid persisted ID never substitutes for that observation.
func VerifySource(value Before, build string, leafDER []byte) error {
	if e := Validate(value); e != nil {
		return e
	}
	id, owner, qualifier, e := Identity(value.PreviousReceipt.Domain, build, leafDER)
	if e != nil || build != value.BuildCommit || id != value.RequestID || owner != value.OwnerID || qualifier != value.Qualifier || mailhostartifact.LeafSHA256(leafDER) != value.Pending.LeafSHA256 {
		return errors.New("renewal before-image source changed")
	}
	return nil
}
