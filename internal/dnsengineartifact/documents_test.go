package dnsengineartifact

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
)

func TestDNSDocumentsReadMixedHistoricalAndSeparatedRoles(t *testing.T) {
	owner, current := legacyBytes(t, "bind-acquisition"), legacyBytes(t, "bind-zone-add")
	plan, err := PlanLegacySeparationV1(owner, current)
	if err != nil {
		t.Fatal(err)
	}
	newOwner, newCurrent, err := SeparationDocumentsV2(plan)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range [][]byte{owner, newOwner} {
		for _, p := range [][]byte{current, newCurrent} {
			before, isNew, err := DecodeOwnershipDocument(a)
			if err != nil || isNew != bytes.Equal(a, newOwner) {
				t.Fatal(isNew, err)
			}
			after, isNew, err := DecodeStateDocument(p)
			if err != nil || isNew != bytes.Equal(p, newCurrent) {
				t.Fatal(isNew, err)
			}
			relationship, err := CompareV1(before, after)
			if err != nil || relationship != LaterBINDPublication {
				t.Fatal(relationship, err)
			}
			restoredBefore, _ := CanonicalV1(before)
			restoredAfter, _ := CanonicalV1(after)
			if !bytes.Equal(restoredBefore, owner) || !bytes.Equal(restoredAfter, current) {
				t.Fatal("historical projection changed")
			}
		}
	}
	if _, _, err := DecodeOwnershipDocument(newCurrent); err == nil {
		t.Fatal("current publication became an ownership checkpoint")
	}
	if _, _, err := DecodeStateDocument(newOwner); err == nil {
		t.Fatal("ownership checkpoint became current publication")
	}
	for _, raw := range [][]byte{newOwner, newCurrent} {
		if _, err := DecodeV1(raw); err == nil {
			t.Fatal("historical reader unexpectedly accepts new format")
		}
	}
}

func TestDNSDocumentsRejectAmbiguousOrUnboundInputs(t *testing.T) {
	source := fixture(t, "bind-zone-add")
	raw, err := CanonicalStateDocumentV2(source)
	if err != nil {
		t.Fatal(err)
	}
	var doc documentV2
	if err = json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	doc.Publication.AcquisitionSHA256 = "bad"
	broken, _ := json.Marshal(doc)
	broken = append(broken, '\n')
	for _, input := range [][]byte{
		nil, broken,
		bytes.Replace(raw, []byte(StateDocumentSchemaV2), []byte("unknown"), 1),
		bytes.Replace(raw, []byte(`"acquisition":`), []byte(`"extra":0,"acquisition":`), 1),
		bytes.Replace(raw, []byte(`"schema":`), []byte(`"schema":"ignored","schema":`), 1),
		append(bytes.Clone(raw), ' '),
	} {
		if _, _, err := DecodeStateDocument(input); err == nil {
			t.Fatal("ambiguous document accepted")
		}
	}
	for _, name := range []string{"bind-acquisition", "bind-zone-add", "bind-zone-edit", "bind-zone-delete", "bind-standalone", "pdns-standalone", "pdns-adopted"} {
		state := fixture(t, name)
		for _, ownership := range []bool{false, true} {
			var out []byte
			var err error
			var decoded StateV1
			if ownership {
				out, err = CanonicalOwnershipDocumentV2(state)
				if err == nil {
					decoded, _, err = DecodeOwnershipDocument(out)
				}
			} else {
				out, err = CanonicalStateDocumentV2(state)
				if err == nil {
					decoded, _, err = DecodeStateDocument(out)
				}
			}
			if err != nil || decoded != state {
				t.Fatalf("%s: %v", name, err)
			}
		}
	}
}

func TestDNSDocumentsGoldenCrossLanguage(t *testing.T) {
	for _, name := range []string{"bind-zone-add", "pdns-adopted"} {
		state := fixture(t, name)
		for _, role := range []string{"state", "ownership"} {
			var got []byte
			var err error
			if role == "state" {
				got, err = CanonicalStateDocumentV2(state)
			} else {
				got, err = CanonicalOwnershipDocumentV2(state)
			}
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile("testdata/v2-" + name + "-" + role + ".json")
			if err != nil || !bytes.Equal(got, want) {
				t.Fatal("cross-language fixture differs", name, role, err)
			}
		}
	}
}
