package dnsengineartifact

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

func fixture(t *testing.T, name string) StateV1 {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "alpha81-"+name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	s, err := DecodeV1(raw)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestHistoricalProducerBytesAndRoles(t *testing.T) {
	raw, err := os.ReadFile("testdata/producer.json")
	if err != nil {
		t.Fatal(err)
	}
	var origin struct {
		Source string            `json:"source_commit"`
		Files  map[string]string `json:"files"`
	}
	if err = json.Unmarshal(raw, &origin); err != nil {
		t.Fatal(err)
	}
	if origin.Source != "45dfc265bfd7997e9a0e0d39b0ebf60a57f58c00" || len(origin.Files) != 7 {
		t.Fatal("historical producer evidence differs")
	}
	for name, want := range origin.Files {
		raw, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(raw)
		if hex.EncodeToString(digest[:]) != want {
			t.Fatal("historical producer bytes changed")
		}
		s, err := DecodeV1(raw)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		out, err := CanonicalV1(s)
		if err != nil || !bytes.Equal(out, raw) {
			t.Fatalf("historical snapshot rewritten: %s %v", name, err)
		}
		if relation, err := CompareV1(s, s); err != nil || relation != SamePublication {
			t.Fatalf("exact %s: %v", name, err)
		}
	}
	owner := fixture(t, "bind-acquisition")
	previous := owner
	for _, name := range []string{"bind-zone-add", "bind-zone-edit", "bind-zone-delete"} {
		next := fixture(t, name)
		for _, before := range []StateV1{owner, previous} {
			relation, err := CompareV1(before, next)
			if err != nil || relation != LaterBINDPublication {
				t.Fatalf("%s: %v", name, err)
			}
			acquisition, _, err := SplitV1(before)
			if err != nil {
				t.Fatal(err)
			}
			after, _, err := SplitV1(next)
			if err != nil || acquisition != after {
				t.Fatalf("publication changed acquisition: %s", name)
			}
		}
		previous = next
	}
}

func TestEveryWireFieldHasAnExplicitRole(t *testing.T) {
	// Adding a field to the shared producer cannot silently exclude its authority
	// from relationship checks: it must first acquire an explicit contract role.
	wire := reflect.TypeOf(StateV1{})
	acquisition := reflect.TypeOf(AcquisitionV1{})
	publication := reflect.TypeOf(PublicationV1{})
	for i := 0; i < wire.NumField(); i++ {
		name := wire.Field(i).Name
		if name == "Schema" {
			continue
		}
		_, a := acquisition.FieldByName(name)
		_, p := publication.FieldByName(name)
		if a == p {
			t.Fatalf("field %s has missing or ambiguous evidence role", name)
		}
	}
	if wire.NumField() != 1+acquisition.NumField()+publication.NumField() {
		t.Fatal("role fields and wire fields differ")
	}
}

func TestAcquisitionChangesAreNeverPublicationAdvances(t *testing.T) {
	owner := fixture(t, "bind-acquisition")
	state := fixture(t, "bind-zone-add")
	mutations := map[string]func(*StateV1){
		"schema":                     func(s *StateV1) { s.Schema = "unknown" },
		"mode":                       func(s *StateV1) { s.Mode = "adopt" },
		"engine":                     func(s *StateV1) { s.Engine = transport.DNSEnginePowerDNS; s.Generation = "" },
		"epoch":                      func(s *StateV1) { s.EngineEpoch++ },
		"role":                       func(s *StateV1) { s.PairRole = transport.DNSPairRoleSecondary; s.PrimaryCatalogSerial = 0 },
		"local":                      func(s *StateV1) { s.PairLocalIP = "192.0.2.10" },
		"peer":                       func(s *StateV1) { s.PairPeerIP = "192.0.2.20" },
		"revision":                   func(s *StateV1) { s.SourceRevision++ },
		"qualifier":                  func(s *StateV1) { s.ManifestQualifier = "dns-engine-switch/v1:sha256:" + strings.Repeat("a", 64) },
		"request":                    func(s *StateV1) { s.MutationRequestID = strings.Repeat("a", 32) },
		"owner":                      func(s *StateV1) { s.MutationOwnerID = strings.Repeat("b", 32) },
		"same-generation-new-serial": func(s *StateV1) { s.Generation = owner.Generation },
	}
	for name, change := range mutations {
		t.Run(name, func(t *testing.T) {
			next := state
			change(&next)
			if _, err := CompareV1(owner, next); err == nil {
				t.Fatal("authority drift accepted")
			}
		})
	}
	newer := owner
	newer.PrimaryCatalogSerial = 3
	if _, err := CompareV1(newer, state); err == nil {
		t.Fatal("catalog regression accepted")
	}
	secondary := owner
	secondary.PairRole = transport.DNSPairRoleSecondary
	secondary.PrimaryCatalogSerial = 0
	other := secondary
	other.Generation = state.Generation
	if _, err := CompareV1(secondary, other); err == nil {
		t.Fatal("secondary publication authority invented")
	}
	invalid := StateV1{}
	if _, err := CompareV1(invalid, invalid); err == nil {
		t.Fatal("equal invalid records accepted")
	}
}

func TestWireReaderRefusesAmbiguousOrUnrecognizedState(t *testing.T) {
	raw, err := CanonicalV1(fixture(t, "bind-zone-add"))
	if err != nil {
		t.Fatal(err)
	}
	cases := [][]byte{nil, bytes.Repeat([]byte(" "), (64<<10)+1), append([]byte(" "), raw...), append(bytes.Clone(raw), []byte("{}")...), bytes.TrimSuffix(raw, []byte("\n")), bytes.Replace(raw, []byte(`"schema":`), []byte(`"unknown":0,"schema":`), 1), bytes.Replace(raw, []byte(`"engine_epoch":1`), []byte(`"engine_epoch":1,"engine_epoch":1`), 1), bytes.Replace(raw, []byte(StateSchemaV1), []byte("celikpanel-dns-engine-state/v2"), 1)}
	for i, data := range cases {
		if _, err := DecodeV1(data); err == nil {
			t.Fatalf("ambiguous wire input %d accepted", i)
		}
	}
}
