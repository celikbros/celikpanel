package mailrenewalintent

import (
	"bytes"
	"encoding/json"
	"github.com/alicelik/celikpanel/internal/mailhostartifact"
	"os"
	"strings"
	"testing"
)

func fixture(t *testing.T) (Before, []byte) {
	t.Helper()
	previous, e := mailhostartifact.NewReceipt(strings.Repeat("a", 32), "mhc1:"+strings.Repeat("b", 64), "mail.example.test", []byte("old leaf"))
	if e != nil {
		t.Fatal(e)
	}
	leaf := []byte("new leaf")
	v, e := New(strings.Repeat("c", 40), leaf, previous, strings.Repeat("d", 64))
	if e != nil {
		t.Fatal(e)
	}
	return v, leaf
}
func TestBeforeRoundTrip(t *testing.T) {
	v, leaf := fixture(t)
	raw, e := Canonical(v)
	if e != nil {
		t.Fatal(e)
	}
	got, e := Decode(raw)
	if e != nil || got != v {
		t.Fatal("roundtrip", e)
	}
	if e = VerifySource(got, v.BuildCommit, leaf); e != nil {
		t.Fatal(e)
	}
	if bytes.Contains(raw, leaf) {
		t.Fatal("source bytes persisted")
	}
}
func TestBeforeRejectsAmbiguityAndForeignScope(t *testing.T) {
	v, leaf := fixture(t)
	raw, _ := Canonical(v)
	for name, bad := range map[string][]byte{"unknown": bytes.Replace(raw, []byte(`{"schema"`), []byte(`{"extra":true,"schema"`), 1), "duplicate": bytes.Replace(raw, []byte(`{"schema"`), []byte(`{"owner_id":"bad","schema"`), 1), "version": bytes.Replace(raw, []byte(Schema), []byte("celikpanel-mail-renewal-before/v2"), 1), "trailer": append(append([]byte{}, raw...), []byte("{}")...), "newline": bytes.TrimSuffix(raw, []byte("\n")), "null": []byte("null"), "oversize": bytes.Repeat([]byte(" "), MaxSize+1)} {
		t.Run(name, func(t *testing.T) {
			if _, e := Decode(bad); e == nil {
				t.Fatal("ambiguous evidence accepted")
			}
		})
	}
	for name, alter := range map[string]func(*Before){"owner": func(x *Before) { x.OwnerID = "bad" }, "lineage": func(x *Before) { x.Pending.Lineage = mailhostartifact.LineageName("other.test") }, "same-leaf": func(x *Before) { x.Pending.LeafSHA256 = x.PreviousReceipt.LeafSHA256 }, "qualifier": func(x *Before) { x.Qualifier = "mhc1:" + strings.Repeat("a", 64) }, "prior-selection": func(x *Before) { x.PreviousSelectionSHA256 = "" }} {
		t.Run(name, func(t *testing.T) {
			x := v
			alter(&x)
			if _, e := Canonical(x); e == nil {
				t.Fatal("foreign scope accepted")
			}
		})
	}
	for name, alter := range map[string]func(*Before){"request": func(x *Before) { x.RequestID = strings.Repeat("f", 32) }, "owner": func(x *Before) { x.OwnerID = strings.Repeat("e", 32) }, "leaf": func(x *Before) { x.Pending.LeafSHA256 = strings.Repeat("e", 64) }} {
		t.Run("source-"+name, func(t *testing.T) {
			x := v
			alter(&x)
			if e := VerifySource(x, v.BuildCommit, leaf); e == nil {
				t.Fatal("source mismatch accepted")
			}
		})
	}
	if VerifySource(v, strings.Repeat("e", 40), leaf) == nil || VerifySource(v, v.BuildCommit, []byte("another leaf")) == nil {
		t.Fatal("cross-build/source adoption accepted")
	}
}

func TestIdentityMatchesActualPriorNativeProducer(t *testing.T) {
	leaf, e := os.ReadFile("testdata/native-be-leaf.der")
	if e != nil {
		t.Fatal(e)
	}
	if mailhostartifact.LeafSHA256(leaf) != "65a3ae295898b4339f99f4c1b7afeb1f72a8277983b70e4fc098cabc79e6347d" {
		t.Fatal("native leaf changed")
	}
	raw, e := os.ReadFile("testdata/native-be-identity.json")
	if e != nil {
		t.Fatal(e)
	}
	var fixture struct {
		Job struct {
			Request   string `json:"request_id"`
			Owner     string `json:"owner_id"`
			Domain    string `json:"target"`
			Qualifier string `json:"package_name"`
		} `json:"job"`
	}
	if e = json.Unmarshal(raw, &fixture); e != nil {
		t.Fatal(e)
	}
	id, owner, qualifier, e := Identity(fixture.Job.Domain, "bbd81cc2d108fd0d79fd7fba6b144571bc6800e3", leaf)
	if e != nil || id != fixture.Job.Request || owner != fixture.Job.Owner || qualifier != fixture.Job.Qualifier {
		t.Fatal("prior native identity changed", id, owner, qualifier, e)
	}
}
