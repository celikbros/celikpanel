package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// PowerDNS's catalog consumer records, in the options column of every member
// zone it creates, the unique label under which the member appears in the
// producer's catalog. Native evidence (PowerDNS 4.9.17-0+deb13u1, Debian 13,
// batch 5 cells c3 and c4, 2026-09-29) shows the exact bytes for a member whose
// catalog PTR owner is <label>.zones.<catalog>:
//
//	{"consumer": {"unique": "<label>."}}
//
// A consumed member row is therefore accepted with exactly two options
// values: empty (NULL or ''), or that object naming exactly the member's
// unique label in the peer catalog this operation read. Everything else -
// another key, a nested extra key, a duplicate, a group or coo property,
// another label - is refused as before, because it is not what this
// operation's consumer produced.
//
// PowerDNS'in katalog tüketicisi, oluşturduğu her üye bölgenin options
// sütununa üyenin üretici katalogdaki benzersiz etiketini yazar. Tüketilmiş
// üye satırı yalnız iki değerle kabul edilir: boş ya da tam olarak bu işlemin
// okuduğu katalogdaki etiketi adlandıran o nesne. Başka her şey reddedilir.

// pdnsConsumedMemberOptionsLimit bounds the column value before parsing; the
// accepted object for a 63-octet label is well below it.
const pdnsConsumedMemberOptionsLimit = 256

// validPDNSCatalogUniqueLabel accepts one lower-case DNS label of the shapes
// both known producers emit (56 hex or 32 base32hex characters).
func validPDNSCatalogUniqueLabel(label string) bool {
	if label == "" || len(label) > 63 {
		return false
	}
	for _, value := range []byte(label) {
		if (value < '0' || value > '9') && (value < 'a' || value > 'z') {
			return false
		}
	}
	return true
}

// verifyPDNSConsumedMemberOptions returns nil for an empty options value or
// for the exact consumer object naming uniqueLabel; any other value is
// refused with the reason.
func verifyPDNSConsumedMemberOptions(options, uniqueLabel string) error {
	if options == "" {
		return nil
	}
	if len(options) > pdnsConsumedMemberOptionsLimit {
		return errors.New("options value exceeds its bound")
	}
	if !validPDNSCatalogUniqueLabel(uniqueLabel) {
		return errors.New("options value is set but the member has no unique label in the peer catalog")
	}
	decoder := json.NewDecoder(strings.NewReader(options))
	expect := func(want json.Delim) error {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		if delim, ok := token.(json.Delim); !ok || delim != want {
			return fmt.Errorf("options value has %v where %q was expected", token, want)
		}
		return nil
	}
	key := func(want string) error {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		if name, ok := token.(string); !ok || name != want {
			return fmt.Errorf("options value has %v where the single key %q was expected", token, want)
		}
		return nil
	}
	if err := expect('{'); err != nil {
		return err
	}
	if err := key("consumer"); err != nil {
		return err
	}
	if err := expect('{'); err != nil {
		return err
	}
	if err := key("unique"); err != nil {
		return err
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	unique, ok := token.(string)
	if !ok {
		return errors.New("options consumer unique value is not a string")
	}
	if err := expect('}'); err != nil {
		return err
	}
	if err := expect('}'); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return errors.New("options value has content after its object")
	}
	if unique != uniqueLabel+"." {
		return fmt.Errorf("options consumer unique label %q is not the member's peer catalog label %q", unique, uniqueLabel+".")
	}
	return nil
}
