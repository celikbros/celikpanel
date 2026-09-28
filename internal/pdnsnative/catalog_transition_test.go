package pdnsnative

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func measuredFreshPair(t *testing.T) (Snapshot, Snapshot) {
	t.Helper()
	path := filepath.Join("..", "..", "deploy", "e2e", "dns-kill-matrix", "evidence", "pdns-master-bind-20260928", "debian-primary.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var observation struct {
		Staged struct {
			SQLite Snapshot `json:"sqlite"`
		} `json:"staged"`
		Running struct {
			SQLite Snapshot `json:"sqlite"`
		} `json:"running"`
	}
	if err := json.Unmarshal(raw, &observation); err != nil {
		t.Fatal(err)
	}
	return observation.Staged.SQLite, observation.Running.SQLite
}

func cloneSnapshot(t *testing.T, source Snapshot) Snapshot {
	t.Helper()
	raw, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	var copy Snapshot
	if err := json.Unmarshal(raw, &copy); err != nil {
		t.Fatal(err)
	}
	return copy
}

func TestMeasuredFreshPrimaryNativeCatalogTransition(t *testing.T) {
	staged, running := measuredFreshPair(t)
	proof, err := VerifyFreshPrimaryCatalogTransition(staged, running, "catalog-c000020a.celikpanel.invalid", 1)
	if err != nil {
		t.Fatal(err)
	}
	if proof.SourceSerial != 1 || proof.NativeSerial != 1790542951 || proof.CatalogHash == "" {
		t.Fatalf("unexpected native proof: %+v", proof)
	}
	bad := []struct {
		name   string
		change func(*Snapshot)
	}{
		{"foreign table row", func(s *Snapshot) { s.Tables["supermasters"] = [][]any{{"192.0.2.55", "other.test", "foreign"}} }},
		{"foreign catalog row", func(s *Snapshot) { s.Tables["records"][1][4] = "owner.invalid" }},
		{"member type drift", func(s *Snapshot) { s.Tables["domains"][1][4] = "NATIVE" }},
		{"member last check drift", func(s *Snapshot) { s.Tables["domains"][1][5] = float64(9) }},
		{"foreign metadata", func(s *Snapshot) {
			s.Tables["domainmetadata"] = append(s.Tables["domainmetadata"], []any{2., 1., "ALSO", "value"})
		}},
		{"arbitrary SOA RDATA", func(s *Snapshot) {
			s.Tables["records"][len(s.Tables["records"])-1][4] = "foreign invalid 1790542951 60 30 3600 30"
		}},
		{"schema change", func(s *Snapshot) {
			s.Schema = append(s.Schema, [4]string{"table", "foreign", "foreign", "CREATE TABLE foreign (x INT)"})
		}},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			mutated := cloneSnapshot(t, running)
			tc.change(&mutated)
			if _, err := VerifyFreshPrimaryCatalogTransition(staged, mutated, "catalog-c000020a.celikpanel.invalid", 1); err == nil {
				t.Fatal("accepted foreign native SQL mutation")
			}
		})
	}
}
