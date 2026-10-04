package mailhostartifact

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func historical(t *testing.T, name string) []byte {
	t.Helper()
	v, e := os.ReadFile("testdata/" + name)
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func TestAlpha81ProducerBytesRemainExact(t *testing.T) {
	raw := historical(t, "alpha81-receipt.json")
	receipt, err := DecodeReceipt(raw)
	if err != nil {
		t.Fatal(err)
	}
	next, err := NewReceipt(strings.Repeat("a", 32), "mhc1:"+strings.Repeat("b", 64), "mail.example.test", []byte("historical leaf DER fixture"))
	if err != nil || next != receipt {
		t.Fatalf("receipt: %+v %v", next, err)
	}
	encoded, err := CanonicalReceipt(next)
	if err != nil || !bytes.Equal(encoded, raw) {
		t.Fatalf("historical receipt changed: %s %v", encoded, err)
	}
	pendingRaw := historical(t, "alpha81-pending.json")
	pending, err := DecodePending(pendingRaw)
	if err != nil {
		t.Fatal(err)
	}
	if pending.Lineage != LineageName(receipt.Domain) || pending.LeafSHA256 != receipt.LeafSHA256 {
		t.Fatal("historical lineage or leaf changed")
	}
	encoded, err = CanonicalPending(pending)
	if err != nil || !bytes.Equal(encoded, pendingRaw) {
		t.Fatalf("historical pending changed: %s %v", encoded, err)
	}
}
func TestReceiptRejectsAmbiguityAndChangedIdentity(t *testing.T) {
	raw := string(historical(t, "alpha81-receipt.json"))
	for name, bad := range map[string]string{
		"unknown-field": strings.Replace(raw, `{"schema"`, `{"extra":true,"schema"`, 1),
		"duplicate":     strings.Replace(raw, `{"schema"`, `{"domain":"other.test","schema"`, 1),
		"trailer":       raw + "{}", "prefix": " " + raw, "missing-newline": strings.TrimSuffix(raw, "\n"),
		"version":          strings.Replace(raw, "receipt/v1", "receipt/v2", 1),
		"uppercase-domain": strings.Replace(raw, "mail.example.test", "Mail.example.test", 1),
		"path-domain":      strings.Replace(raw, "mail.example.test", "../mail.example.test", 1),
		"other-purpose":    strings.Replace(raw, "mhc1:", "pci1:", 1),
		"uppercase-id":     strings.Replace(raw, strings.Repeat("a", 32), strings.Repeat("A", 32), 1),
		"unknown-leaf":     strings.Replace(raw, `"leaf_sha256":"0a`, `"leaf_sha256":"zz`, 1),
		"oversize":         strings.Repeat(" ", ReceiptMaxSize+1), "null": "null", "empty": "",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeReceipt([]byte(bad)); err == nil {
				t.Fatal("invalid receipt accepted")
			}
		})
	}
}
func TestPendingRejectsAlternateBytesAndInvalidLineage(t *testing.T) {
	raw := string(historical(t, "alpha81-pending.json"))
	for name, bad := range map[string]string{
		"newline": raw + "\n", "prefix": " " + raw, "trailer": raw + "{}", "null": "null",
		"duplicate":     strings.Replace(raw, `{"lineage"`, `{"lineage":"wrong","lineage"`, 1),
		"extra":         strings.Replace(raw, `{"lineage"`, `{"extra":true,"lineage"`, 1),
		"panel-lineage": strings.Replace(raw, "celikpanel-mail-", "celikpanel-", 1),
		"path":          strings.Replace(raw, "celikpanel-mail-", "../celikpanel-mail-", 1),
		"uppercase":     strings.Replace(raw, "6467aa", "6467AA", 1),
		"leaf":          strings.Replace(raw, `"leaf_sha256":"0a`, `"leaf_sha256":"zz`, 1),
		"oversize":      strings.Repeat(" ", PendingMaxSize+1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodePending([]byte(bad)); err == nil {
				t.Fatal("invalid pending accepted")
			}
		})
	}
	if _, err := CanonicalPending(Pending{Lineage: "arbitrary"}); err == nil {
		t.Fatal("invalid pending produced")
	}
}
