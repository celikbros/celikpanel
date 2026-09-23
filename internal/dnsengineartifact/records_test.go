package dnsengineartifact

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

func legacyBytes(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "alpha81-"+name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestSeparatedRecordsPreserveActualHistoricalProducer(t *testing.T) {
	paths, err := filepath.Glob("testdata/alpha81-*.json")
	if err != nil || len(paths) != 7 {
		t.Fatalf("historical inputs: %v %v", paths, err)
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			original, err := DecodeV1(raw)
			if err != nil {
				t.Fatal(err)
			}
			a, p, err := SeparateV1(original)
			if err != nil {
				t.Fatal(err)
			}
			aRaw, err := CanonicalAcquisitionV1(a)
			if err != nil {
				t.Fatal(err)
			}
			pRaw, err := CanonicalPublicationV1(a, p)
			if err != nil {
				t.Fatal(err)
			}
			var aFields, pFields map[string]any
			if err := json.Unmarshal(aRaw, &aFields); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(pRaw, &pFields); err != nil {
				t.Fatal(err)
			}
			for _, field := range []string{"generation", "primary_catalog_serial", "acquisition_sha256"} {
				if _, exists := aFields[field]; exists {
					t.Fatalf("publication leaked into acquisition: %s", field)
				}
			}
			for _, field := range []string{"engine", "mode", "engine_epoch", "pair_role", "source_revision", "mutation_owner_id"} {
				if _, exists := pFields[field]; exists {
					t.Fatalf("authority copied into publication: %s", field)
				}
			}
			aDecoded, err := DecodeAcquisitionV1(aRaw)
			if err != nil || aDecoded != a {
				t.Fatalf("acquisition roundtrip: %v", err)
			}
			pDecoded, err := DecodePublicationV1(aDecoded, pRaw)
			if err != nil || pDecoded != p {
				t.Fatalf("publication roundtrip: %v", err)
			}
			combined, err := CombineV1(aDecoded, pDecoded)
			if err != nil {
				t.Fatal(err)
			}
			restored, err := CanonicalV1(combined)
			if err != nil || !bytes.Equal(raw, restored) {
				t.Fatalf("historical producer bytes changed: %v", err)
			}
		})
	}
}

func TestSeparatePublicationRetainsOneImmutableAcquisition(t *testing.T) {
	a, before, err := SeparateV1(fixture(t, "bind-acquisition"))
	if err != nil {
		t.Fatal(err)
	}
	frozen, err := CanonicalAcquisitionV1(a)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"bind-zone-add", "bind-zone-edit", "bind-zone-delete"} {
		nextA, next, err := SeparateV1(fixture(t, name))
		if err != nil {
			t.Fatal(err)
		}
		raw, err := CanonicalAcquisitionV1(nextA)
		if err != nil || !bytes.Equal(frozen, raw) || next.AcquisitionSHA256 != before.AcquisitionSHA256 {
			t.Fatalf("%s changed acquisition: %v", name, err)
		}
		if relation, err := ComparePublicationsV1(a, before, next); err != nil || relation != LaterBINDPublication {
			t.Fatalf("%s transition: %v", name, err)
		}
		before = next
	}
}

func TestSeparatedReadersRefuseAmbiguousEvidence(t *testing.T) {
	a, p, err := SeparateV1(fixture(t, "bind-zone-add"))
	if err != nil {
		t.Fatal(err)
	}
	aRaw, _ := CanonicalAcquisitionV1(a)
	pRaw, _ := CanonicalPublicationV1(a, p)
	plan, err := PlanLegacySeparationV1(legacyBytes(t, "bind-acquisition"), legacyBytes(t, "bind-zone-add"))
	if err != nil {
		t.Fatal(err)
	}
	planRaw, _ := CanonicalLegacySeparationV1(plan)
	for name, tc := range map[string]struct {
		data []byte
		read func([]byte) error
	}{
		"acquisition": {aRaw, func(b []byte) error { _, err := DecodeAcquisitionV1(b); return err }},
		"publication": {pRaw, func(b []byte) error { _, err := DecodePublicationV1(a, b); return err }},
		"separation":  {planRaw, func(b []byte) error { _, err := DecodeLegacySeparationV1(b); return err }},
	} {
		t.Run(name, func(t *testing.T) {
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(tc.data, &fields); err != nil {
				t.Fatal(err)
			}
			duplicate := bytes.Replace(tc.data, []byte(`"schema":`), append(append([]byte(`"schema":`), fields["schema"]...), []byte(`,"schema":`)...), 1)
			cases := [][]byte{
				nil, bytes.Repeat([]byte(" "), 4*recordLimit+1),
				bytes.TrimSuffix(tc.data, []byte("\n")),
				append([]byte(" "), tc.data...), append(bytes.Clone(tc.data), []byte("{}")...),
				bytes.Replace(tc.data, []byte(`"schema":`), []byte(`"unknown":0,"schema":`), 1),
				bytes.Replace(tc.data, []byte(`"schema":`), []byte(`"Schema":`), 1),
				bytes.Replace(tc.data, fields["schema"], []byte(`"unsupported/v99"`), 1),
				duplicate,
			}
			for i, input := range cases {
				if err := tc.read(input); err == nil {
					t.Fatalf("ambiguous case %d accepted", i)
				}
			}
		})
	}
}

func TestPublicationBindingRefusesAuthorityAndRoleConfusion(t *testing.T) {
	a, p, err := SeparateV1(fixture(t, "bind-zone-add"))
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*AcquisitionRecordV1){
		"owner":    func(a *AcquisitionRecordV1) { a.MutationOwnerID = strings.Repeat("a", 32) },
		"request":  func(a *AcquisitionRecordV1) { a.MutationRequestID = strings.Repeat("b", 32) },
		"epoch":    func(a *AcquisitionRecordV1) { a.EngineEpoch++ },
		"revision": func(a *AcquisitionRecordV1) { a.SourceRevision++ },
		"manifest": func(a *AcquisitionRecordV1) {
			a.ManifestQualifier = "dns-engine-switch/v1:sha256:" + strings.Repeat("c", 64)
		},
		"local":  func(a *AcquisitionRecordV1) { a.PairLocalIP = "192.0.2.5" },
		"peer":   func(a *AcquisitionRecordV1) { a.PairPeerIP = "192.0.2.6" },
		"role":   func(a *AcquisitionRecordV1) { a.PairRole = transport.DNSPairRoleSecondary },
		"engine": func(a *AcquisitionRecordV1) { a.Engine = transport.DNSEnginePowerDNS },
	} {
		t.Run(name, func(t *testing.T) {
			changed := a
			mutate(&changed)
			if _, err := CanonicalPublicationV1(changed, p); err == nil {
				t.Fatal("publication rebound to changed authority")
			}
			if _, err := CombineV1(changed, p); err == nil {
				t.Fatal("changed authority converted to legacy evidence")
			}
		})
	}
	// Validation remains necessary even when the supplied digest matches.
	broken := p
	broken.PrimaryCatalogSerial = 0
	if _, err := CanonicalPublicationV1(a, broken); err == nil {
		t.Fatal("primary without serial accepted")
	}
	broken = p
	broken.Generation = ""
	if _, err := CanonicalPublicationV1(a, broken); err == nil {
		t.Fatal("BIND without generation accepted")
	}
	secondary := fixture(t, "bind-acquisition")
	secondary.PairRole = transport.DNSPairRoleSecondary
	secondary.PrimaryCatalogSerial = 0
	sa, sp, err := SeparateV1(secondary)
	if err != nil {
		t.Fatal(err)
	}
	next := sp
	next.Generation = strings.Repeat("f", 64)
	if _, err := ComparePublicationsV1(sa, sp, next); err == nil {
		t.Fatal("secondary publication admitted")
	}
	if _, err := CanonicalAcquisitionV1(AcquisitionRecordV1{Schema: AcquisitionSchemaV1}); err == nil {
		t.Fatal("empty authority accepted")
	}
}

func FuzzSeparatedLegacyRoundtrip(f *testing.F) {
	for _, name := range []string{"bind-acquisition", "bind-zone-edit", "pdns-adopted", "pdns-standalone"} {
		raw, err := os.ReadFile("testdata/alpha81-" + name + ".json")
		if err != nil {
			f.Fatal(err)
		}
		f.Add(raw)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		state, err := DecodeV1(data)
		if err != nil {
			return
		}
		a, p, err := SeparateV1(state)
		if err != nil {
			t.Fatal(err)
		}
		joined, err := CombineV1(a, p)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := CanonicalV1(joined)
		if err != nil || !bytes.Equal(data, raw) {
			t.Fatalf("lossy conversion: %v", err)
		}
	})
}
