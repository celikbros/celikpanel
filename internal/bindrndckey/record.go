// Package bindrndckey holds the product's rule for BIND's rndc control key:
// the provenance record the Agent writes when it installs BIND, the typed
// reason for an rndc call that fails because the control channel has no usable
// key, and the rollback removal that deletes only a key this product created
// and nobody changed since.
//
// Some BIND packages (Arch `bind`) create no rndc key; Debian's `bind9`
// creates /etc/bind/rndc.key at package configuration. A product-installed
// BIND must be able to answer `rndc zonestatus`, so the fresh install
// generates the key with the native `rndc-confgen -a` only when neither the
// package nor the owner provided one. The owner may replace it at any time;
// the product never rewrites it, and removing the panel leaves it in place.
//
// bindrndckey paketi, BIND'in rndc denetim anahtarı için ürün kuralını tutar:
// Agent'ın BIND kurarken yazdığı köken kaydı, denetim kanalında kullanılabilir
// anahtar olmadığı için başarısız olan rndc çağrısının türlü nedeni ve geri
// almada yalnız bu ürünün oluşturduğu ve o zamandan beri kimsenin değiştirmediği
// anahtarı silen kural. Arch `bind` paketi anahtar oluşturmaz; Debian `bind9`
// /etc/bind/rndc.key dosyasını oluşturur. Ürün anahtarı yalnız paket ya da
// sahip sağlamadığında yerel `rndc-confgen -a` ile üretir; sahip onu her zaman
// değiştirebilir; ürün onu yeniden yazmaz; panelin kaldırılması onu yerinde
// bırakır.
package bindrndckey

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
)

// RecordSchema names the provenance record. It is a companion of the BIND
// install-ownership receipt, kept in its own file so the receipt's wire format
// (read byte-exactly by older Agents, the owner recovery kit and the native
// kill-matrix probe) does not change.
const RecordSchema = "celikpanel-dns-engine-bind-rndc-key/v1"

// RecordFileName is the record's name in the Agent's service mutation state
// directory, next to dns-engine-install-ownership-bind.json.
const RecordFileName = "dns-engine-bind-rndc-key.json"

const (
	// ProvenanceProductCreated: this product ran rndc-confgen -a; SHA256 is
	// the content it created.
	ProvenanceProductCreated = "product_created"
	// ProvenanceOwnerOrPackage: the product created nothing; Basis says what
	// it found.
	ProvenanceOwnerOrPackage = "owner_or_package_provided"
)

const (
	// BasisKeyPresent: the default key file already existed.
	BasisKeyPresent = "key_present"
	// BasisRNDCConfPresent: rndc.conf exists beside it; rndc reads that first.
	BasisRNDCConfPresent = "rndc_conf_present"
	// BasisControlsStatement: the owner configured named's control channel.
	BasisControlsStatement = "controls_statement"
)

// Default key paths compiled into the two certified BIND builds: Arch's
// sysconfdir is /etc, Debian's /etc/bind.
const (
	PacmanKeyPath = "/etc/rndc.key"
	APTKeyPath    = "/etc/bind/rndc.key"
)

// Record is bound to the install-ownership receipt's exact transaction.
type Record struct {
	Schema            string `json:"schema"`
	Path              string `json:"path"`
	Provenance        string `json:"provenance"`
	SHA256            string `json:"sha256,omitempty"`
	Basis             string `json:"basis,omitempty"`
	ManifestQualifier string `json:"manifest_qualifier"`
	MutationRequestID string `json:"mutation_request_id"`
	MutationOwnerID   string `json:"mutation_owner_id"`
}

var sha256Hex = regexp.MustCompile(`^[0-9a-f]{64}$`)

// KnownKeyPath reports one of the certified default key paths.
func KnownKeyPath(path string) bool {
	return path == PacmanKeyPath || path == APTKeyPath
}

// Validate refuses any record this product never writes.
func (r Record) Validate() error {
	if r.Schema != RecordSchema || !KnownKeyPath(r.Path) ||
		!mutationpayload.ValidDNSEngineSwitchQualifier(r.ManifestQualifier) ||
		!servicemutationledger.ValidIdentity(r.MutationRequestID) ||
		!servicemutationledger.ValidIdentity(r.MutationOwnerID) {
		return errors.New("BIND rndc key record identity is invalid")
	}
	switch r.Provenance {
	case ProvenanceProductCreated:
		if !sha256Hex.MatchString(r.SHA256) || r.Basis != "" {
			return errors.New("product-created BIND rndc key record needs exactly a content hash")
		}
	case ProvenanceOwnerOrPackage:
		if r.SHA256 != "" {
			return errors.New("owner-provided BIND rndc key record must not carry a hash")
		}
		switch r.Basis {
		case BasisKeyPresent, BasisRNDCConfPresent, BasisControlsStatement:
		default:
			return errors.New("owner-provided BIND rndc key record has no valid basis")
		}
	default:
		return errors.New("BIND rndc key record provenance is invalid")
	}
	return nil
}

// SameTransaction reports whether the record names exactly this transaction.
func (r Record) SameTransaction(qualifier, requestID, ownerID string) bool {
	return r.ManifestQualifier == qualifier && r.MutationRequestID == requestID &&
		r.MutationOwnerID == ownerID
}

// Encode returns the canonical bytes (compact JSON and a newline).
func Encode(r Record) ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(r)
	if err != nil {
		return nil, fmt.Errorf("encode BIND rndc key record: %w", err)
	}
	return append(encoded, '\n'), nil
}

// Decode accepts only the canonical encoding of a valid record.
func Decode(data []byte) (Record, error) {
	if len(data) == 0 || len(data) > 4<<10 {
		return Record{}, errors.New("BIND rndc key record has an invalid size")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var record Record
	if err := decoder.Decode(&record); err != nil {
		return Record{}, fmt.Errorf("decode BIND rndc key record: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return Record{}, errors.New("BIND rndc key record contains trailing JSON")
	}
	canonical, err := Encode(record)
	if err != nil {
		return Record{}, err
	}
	if !bytes.Equal(canonical, data) {
		return Record{}, errors.New("BIND rndc key record is not canonical JSON")
	}
	return record, nil
}
