//go:build linux

package recoveryruntime

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/agentnativecontract"
	"github.com/alicelik/celikpanel/internal/bindconfig"
	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
)

func v4CompatibilityJournal(t *testing.T, statePath string) ([]byte, dnsengineartifact.JournalPolicy) {
	t.Helper()
	policy := dnsengineartifact.JournalPolicy{
		StatePath: statePath, StateUID: 0, StateGID: 0, RequireOwner: true,
		PDNSMainPath:     "/etc/powerdns/pdns.conf",
		PDNSManagedPath:  "/etc/powerdns/pdns.d/celikpanel.conf",
		PDNSClusterPath:  "/etc/powerdns/pdns.d/celikpanel-cluster.conf",
		PDNSDatabasePath: "/var/lib/powerdns/pdns.sqlite3",
	}
	raw, err := os.ReadFile(filepath.Join("..", "dnsengineartifact", "testdata", "switch-journal", "alpha81-pdns-switch.json"))
	if err != nil {
		t.Fatal(err)
	}
	raw = bytes.ReplaceAll(raw, []byte("/var/lib/celikpanel-agent-private/dns-engine-state.json"), []byte(statePath))
	base, err := policy.DecodeSwitchJournal(raw)
	if err != nil {
		t.Fatal(err)
	}
	base.TargetUnitsBefore[0].LoadState = "loaded"
	base.TargetUnitsBefore[0].UnitFileState = "disabled"
	state, exists, err := dnsengineartifact.SourceStateFromSwitchJournal(base)
	if err != nil || !exists {
		t.Fatalf("source state: exists=%v err=%v", exists, err)
	}
	after := append([]dnsengineartifact.FileSnapshot(nil), base.ConfigBefore...)
	for i := range after {
		after[i].Data = append([]byte(nil), after[i].Data...)
		if after[i].Path == policy.PDNSManagedPath {
			after[i].Exists = true
			after[i].Mode = 0o644
			after[i].OwnerKnown = true
			after[i].UID, after[i].GID = 0, 0
			after[i].Data = []byte("launch=gsqlite3\n")
		}
		if after[i].Exists {
			after[i].SHA256 = dnsengineartifact.DigestBytes(after[i].Data)
		}
	}
	local, err := bindconfig.ManagedZoneInclude("// source local config\n", "/var/cache/bind/celikpanel/current/zones.conf")
	if err != nil {
		t.Fatal(err)
	}
	source := dnsengineartifact.ManagedBINDSourceProofV4{
		Kind: dnsengineartifact.ManagedBINDSourceProofKindV4, HostLayout: "apt",
		Generation: state.Generation, EngineEpoch: state.EngineEpoch,
		ReceiptSHA256: strings.Repeat("e", 64),
		ConfigBefore: []dnsengineartifact.FileSnapshot{
			v4CompatSnapshot("/etc/bind/named.conf", []byte("include \"/etc/bind/named.conf.options\";\ninclude \"/etc/bind/named.conf.local\";\ninclude \"/etc/bind/named.conf.default-zones\";\n")),
			v4CompatSnapshot("/etc/bind/named.conf.default-zones", []byte("// defaults\n")),
			v4CompatSnapshot("/etc/bind/named.conf.local", []byte(local)),
			v4CompatSnapshot("/etc/bind/named.conf.options", []byte("// options\n")),
		},
	}
	candidate := dnsengineartifact.PDNSTargetCandidateProofV4{
		Path:   filepath.Join(filepath.Dir(statePath), ".celikpanel-switch-"+base.MutationRequestID+".sqlite3"),
		Device: 9, Inode: 11, Mode: 0o640, UID: 0, GID: 42, Size: 4096,
		SHA256: strings.Repeat("f", 64), NoSidecars: true,
	}
	journal, err := policy.BuildPDNSTargetInverseJournalV4(base, after, source, candidate)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := policy.EncodeSwitchJournal(journal)
	if err != nil {
		t.Fatal(err)
	}
	return encoded, policy
}

func v4CompatSnapshot(path string, data []byte) dnsengineartifact.FileSnapshot {
	return dnsengineartifact.FileSnapshot{
		Path: path, Exists: true, Mode: 0o644, OwnerKnown: true, UID: 0, GID: 42,
		Data: data, SHA256: dnsengineartifact.DigestBytes(data),
	}
}

func TestDNSApplicationCompatibilityRefusesActiveV4WithoutChangingEvidence(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("protected root-owned fixture")
	}
	for _, contractPresent := range []bool{false, true} {
		t.Run(map[bool]string{false: "historical-target", true: "current-target"}[contractPresent], func(t *testing.T) {
			root, err := os.MkdirTemp("/run", "celikpanel-v4-compat-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(root)
			bin, stateDir := filepath.Join(root, "bin"), filepath.Join(root, "private")
			for _, dir := range []string{bin, stateDir} {
				if err := os.Mkdir(dir, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			binary := []byte("target agent fixture")
			if err := os.WriteFile(filepath.Join(bin, "agent"), binary, 0o755); err != nil {
				t.Fatal(err)
			}
			if contractPresent {
				contract, err := agentnativecontract.New(binary, strings.Repeat("a", 40))
				if err != nil {
					t.Fatal(err)
				}
				encoded, err := agentnativecontract.Encode(contract)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(bin, agentnativecontract.FileName), encoded, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			journalPath := filepath.Join(stateDir, "dns-engine-switch-journal.json")
			journal, _ := v4CompatibilityJournal(t, filepath.Join(stateDir, "dns-engine-state.json"))
			if err := os.WriteFile(journalPath, journal, 0o600); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(journalPath)
			if err != nil {
				t.Fatal(err)
			}
			called := false
			err = checkDNSApplicationCompatibility(bin, stateDir, func() { called = true })
			if err != errDNSSwitchJournalApplicationV4 {
				t.Fatalf("active V4 journal error=%v, want explicit V4 compatibility refusal", err)
			}
			if called {
				t.Fatal("target publication revalidation ran after V4 refusal")
			}
			after, err := os.ReadFile(journalPath)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("V4 evidence changed: %v", err)
			}
		})
	}
}

func TestPromotionRefusesActiveV4JournalReadOnly(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("protected root-owned fixture")
	}
	root, err := os.MkdirTemp("/run", "celikpanel-v4-promotion-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "dns-engine-switch-journal.json")
	journal, _ := v4CompatibilityJournal(t, filepath.Join(root, "dns-engine-state.json"))
	if err := os.WriteFile(path, journal, 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := refuseUnprovedDNSV2Promotion(root); err == nil || !strings.Contains(err.Error(), "v4 PowerDNS target journal") {
		t.Fatalf("active V4 journal promotion result=%v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("V4 journal changed: %v", err)
	}
}
