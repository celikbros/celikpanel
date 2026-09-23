package dnsengineartifact

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func journalTestPolicy() JournalPolicy {
	return JournalPolicy{
		StatePath: "/var/lib/celikpanel-agent-private/dns-engine-state.json", RequireOwner: true,
		PDNSMainPath: "/etc/powerdns/pdns.conf", PDNSManagedPath: "/etc/powerdns/pdns.d/celikpanel.conf",
		PDNSClusterPath: "/etc/powerdns/pdns.d/celikpanel-cluster.conf", PDNSDatabasePath: "/var/lib/powerdns/pdns.sqlite3",
	}
}
func journalFixture(t testing.TB, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "switch-journal", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func TestSwitchJournalHistoricalProducers(t *testing.T) {
	raw, err := os.ReadFile("testdata/switch-journal/producer.json")
	if err != nil {
		t.Fatal(err)
	}
	var producer struct {
		Files  map[string]string `json:"files"`
		Export string            `json:"export_test_sha256"`
	}
	if err = json.Unmarshal(raw, &producer); err != nil {
		t.Fatal(err)
	}
	if len(producer.Files) != 3 {
		t.Fatal("historical journal evidence is incomplete")
	}
	driver, err := os.ReadFile("testdata/switch-journal/producer.go.txt")
	if err != nil || DigestBytes(driver) != producer.Export {
		t.Fatal("historical exporter changed", err)
	}
	policy := journalTestPolicy()
	for name, digest := range producer.Files {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("testdata", "switch-journal", name))
			if err != nil {
				t.Fatal(err)
			}
			if DigestBytes(raw) != digest {
				t.Fatal("historical producer bytes changed")
			}
			j, err := policy.DecodeSwitchJournal(raw)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := policy.EncodeSwitchJournal(j)
			if err != nil || !bytes.Equal(raw, encoded) {
				t.Fatal("historical bytes changed on round trip", err)
			}
			before := append([]byte(nil), j.StateBefore.Data...)
			for _, phase := range []string{SwitchPhaseIntent, SwitchPhaseTargetStaged, SwitchPhaseSourceStopped, SwitchPhaseTargetStarted, SwitchPhaseTargetVerified, SwitchPhaseCommitted, SwitchPhaseRollingBack, SwitchPhaseRolledBack} {
				j.Phase = phase
				next, err := policy.EncodeSwitchJournal(j)
				if err != nil {
					t.Fatal(phase, err)
				}
				decoded, err := policy.DecodeSwitchJournal(next)
				if err != nil || !bytes.Equal(decoded.StateBefore.Data, before) {
					t.Fatal("phase changed the frozen source", phase, err)
				}
			}
		})
	}
}
func TestSwitchJournalRejectsAmbiguousWireAndBeforeImages(t *testing.T) {
	policy := journalTestPolicy()
	raw := journalFixture(t, "alpha81-bind")
	for name, bad := range map[string][]byte{
		"leading-space":   append([]byte(" "), raw...),
		"trailing-json":   append(append([]byte(nil), raw...), []byte("{}")...),
		"duplicate-phase": bytes.Replace(raw, []byte(`"phase":"target-staged"`), []byte(`"phase":"intent","phase":"target-staged"`), 1),
		"unknown-field":   bytes.Replace(raw, []byte(`"phase":`), []byte(`"unreviewed":true,"phase":`), 1),
		"nil-slices":      bytes.Replace(raw, []byte(`"source_units_before":[]`), []byte(`"source_units_before":null`), 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := policy.DecodeSwitchJournal(bad); err == nil {
				t.Fatal("ambiguous bytes accepted")
			}
		})
	}
	tests := map[string]func(*SwitchJournalV1){
		"schema":         func(j *SwitchJournalV1) { j.Schema = "next" },
		"phase":          func(j *SwitchJournalV1) { j.Phase = "finished" },
		"identity":       func(j *SwitchJournalV1) { j.MutationOwnerID = "other" },
		"manifest":       func(j *SwitchJournalV1) { j.TargetEpoch++ },
		"hidden-source":  func(j *SwitchJournalV1) { j.StateBefore.Data = []byte("source") },
		"foreign-path":   func(j *SwitchJournalV1) { j.ConfigBefore[0].Path = "/etc/passwd" },
		"changed-bytes":  func(j *SwitchJournalV1) { j.ConfigBefore[0].Data = []byte("owner edit") },
		"missing-owner":  func(j *SwitchJournalV1) { j.ConfigBefore[0].OwnerKnown = false },
		"wrong-owner":    func(j *SwitchJournalV1) { j.ConfigBefore[0].UID = 42 },
		"wrong-mode":     func(j *SwitchJournalV1) { j.ConfigBefore[0].Mode = 0777 },
		"duplicate-unit": func(j *SwitchJournalV1) { j.TargetUnitsBefore[1] = j.TargetUnitsBefore[0] },
		"failed-unit":    func(j *SwitchJournalV1) { j.TargetUnitsBefore[0].ActiveState = "failed" },
		"foreign-unit":   func(j *SwitchJournalV1) { j.TargetUnitsBefore[0].Name = "sshd.service" },
		"hidden-pdns":    func(j *SwitchJournalV1) { j.PDNSLiveSize = 42 },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			j, err := policy.DecodeSwitchJournal(raw)
			if err != nil {
				t.Fatal(err)
			}
			mutate(&j)
			if _, err = policy.EncodeSwitchJournal(j); err == nil {
				t.Fatal("invalid journal accepted")
			}
		})
	}
}
func TestSwitchJournalFrozenSourceVersionAndPolicy(t *testing.T) {
	p := journalTestPolicy()
	raw := journalFixture(t, "alpha81-pdns-switch")
	j, err := p.DecodeSwitchJournal(raw)
	if err != nil {
		t.Fatal(err)
	}
	source, exists, err := SourceStateFromSwitchJournal(j)
	if err != nil || !exists {
		t.Fatal(err)
	}
	v2, err := CanonicalStateDocumentV2(source)
	if err != nil {
		t.Fatal(err)
	}
	j.StateBefore.Data = v2
	j.StateBefore.SHA256 = DigestBytes(v2)
	encoded, err := p.EncodeSwitchJournal(j)
	if err != nil {
		t.Fatal(err)
	}
	reread, err := p.DecodeSwitchJournal(encoded)
	if err != nil || !bytes.Equal(reread.StateBefore.Data, v2) {
		t.Fatal("frozen v2 bytes changed", err)
	}
	if bytes.Equal(encoded, raw) {
		t.Fatal("v2 before image was erased")
	}
	for name, change := range map[string]func(*JournalPolicy){
		"different-state-path":  func(p *JournalPolicy) { p.StatePath = "/var/lib/other/state.json" },
		"different-state-owner": func(p *JournalPolicy) { p.StateGID = 42 },
		"different-pdns-root":   func(p *JournalPolicy) { p.PDNSDatabasePath = "/var/lib/other/pdns.sqlite3" },
		"relative":              func(p *JournalPolicy) { p.StatePath = "state.json" },
		"traversal":             func(p *JournalPolicy) { p.StatePath = "/var/lib/../state.json" },
		"alias":                 func(p *JournalPolicy) { p.PDNSMainPath = p.PDNSManagedPath },
	} {
		t.Run(name, func(t *testing.T) {
			policy := p
			change(&policy)
			if _, err := policy.DecodeSwitchJournal(encoded); err == nil {
				t.Fatal("journal redefined the trusted host policy")
			}
		})
	}
	j.StateBefore.UID = 42
	if _, err = p.EncodeSwitchJournal(j); err == nil {
		t.Fatal("wrong source owner accepted")
	}
	j.StateBefore.UID = 0
	source.EngineEpoch++
	j.StateBefore.Data, err = CanonicalStateDocumentV2(source)
	if err != nil {
		t.Fatal(err)
	}
	j.StateBefore.SHA256 = DigestBytes(j.StateBefore.Data)
	if _, err = p.EncodeSwitchJournal(j); err == nil {
		t.Fatal("source acquisition drift accepted")
	}
}
func TestSwitchJournalPDNSRejectsOtherAuthority(t *testing.T) {
	p := journalTestPolicy()
	for name, change := range map[string]func(*SwitchJournalV1){
		"absent-managed": func(j *SwitchJournalV1) {
			for i := range j.ConfigBefore {
				if j.ConfigBefore[i].Path == p.PDNSManagedPath {
					j.ConfigBefore[i] = FileSnapshot{Path: p.PDNSManagedPath}
				}
			}
		},
		"other-running-engine": func(j *SwitchJournalV1) { j.TargetUnitsBefore[0].ActiveState = "active" },
		"stopped-target":       func(j *SwitchJournalV1) { j.TargetUnitsBefore[2].ActiveState = "inactive" },
		"switch-artifact":      func(j *SwitchJournalV1) { j.PDNSBackupSHA256 = strings.Repeat("c", 64); j.PDNSBackupSize = 4096 },
	} {
		t.Run(name, func(t *testing.T) {
			j, err := p.DecodeSwitchJournal(journalFixture(t, "alpha81-pdns-adopt"))
			if err != nil {
				t.Fatal(err)
			}
			change(&j)
			if _, err = p.EncodeSwitchJournal(j); err == nil {
				t.Fatal("different adoption authority accepted")
			}
		})
	}
}
func FuzzSwitchJournalDecode(f *testing.F) {
	p := journalTestPolicy()
	for _, name := range []string{"alpha81-bind", "alpha81-pdns-switch", "alpha81-pdns-adopt"} {
		f.Add(journalFixture(f, name))
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > 1<<20 {
			t.Skip()
		}
		j, err := p.DecodeSwitchJournal(raw)
		if err != nil {
			return
		}
		canonical, err := p.EncodeSwitchJournal(j)
		if err != nil || !bytes.Equal(raw, canonical) {
			t.Fatal("decoder accepted noncanonical evidence", err)
		}
	})
}

func TestProveFrozenSwitchSourceStateHistoricalJournals(t *testing.T) {
	policy := journalTestPolicy()
	for _, name := range []string{"alpha81-bind", "alpha81-pdns-switch", "alpha81-pdns-adopt"} {
		t.Run(name, func(t *testing.T) {
			j, err := policy.DecodeSwitchJournal(journalFixture(t, name))
			if err != nil {
				t.Fatal(err)
			}
			source, exists, err := SourceStateFromSwitchJournal(j)
			if err != nil {
				t.Fatal(err)
			}
			proved, err := ProveFrozenSwitchSourceState(j, source, exists)
			if err != nil || !proved {
				t.Fatalf("frozen source not recognized: proved=%v err=%v", proved, err)
			}
			proved, err = ProveFrozenSwitchSourceState(j, source, !exists)
			if err == nil && proved {
				t.Fatal("source presence mismatch proved")
			}
			if exists {
				foreign := source
				foreign.MutationRequestID = strings.Repeat("f", 32)
				proved, err = ProveFrozenSwitchSourceState(j, foreign, true)
				if err != nil || proved {
					t.Fatalf("foreign source proved: proved=%v err=%v", proved, err)
				}
				malformed := source
				malformed.Schema = "unknown"
				proved, err = ProveFrozenSwitchSourceState(j, malformed, true)
				if err == nil || proved {
					t.Fatalf("malformed observation proved: proved=%v err=%v", proved, err)
				}
			}
		})
	}
}
