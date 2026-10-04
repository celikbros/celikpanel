//go:build linux

package recoveryruntime

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/alicelik/celikpanel/internal/agentnativecontract"
	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
)

func TestDNSApplicationCompatibilityPreservesNativeEvidence(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root descriptor fixture")
	}
	cases := []struct {
		name    string
		allowed bool
	}{
		{"absent-root", true}, {"empty", true}, {"legacy", true}, {"separated-compatible", true},
		{"mixed-compatible", true}, {"private-service-group", true},
		{"historical-agent", false}, {"old-policy", false}, {"changed-agent", false},
		{"unknown-schema", false}, {"wrong-role", false}, {"wrong-engine-path", false},
		{"file-mode", false}, {"file-link", false}, {"hardlink", false}, {"foreign-owner", false}, {"parent-mode", false},
		{"late-current-change", false}, {"late-current-replacement", false}, {"late-absent-file", false},
		{"late-parent-replacement", false}, {"late-target-change", false}, {"late-absent-root", false},
		{"v1-switch-old-agent", true}, {"v2-switch-compatible", true}, {"v2-switch-old-agent", false},
		{"v2-switch-historical-agent", false}, {"v2-switch-unknown-contract", false},
		{"v2-switch-corrupt", false}, {"v2-switch-symlink", false}, {"v2-switch-hardlink", false},
		{"late-v2-switch-change", false}, {"late-v2-switch-absent", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root, err := os.MkdirTemp("/run", "celikpanel-dns-compat-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(root)
			bin, private := filepath.Join(root, "bin"), filepath.Join(root, "private")
			for _, p := range []string{bin, private} {
				if err := os.Mkdir(p, 0700); err != nil {
					t.Fatal(err)
				}
			}
			put := func(path string, raw []byte, mode os.FileMode) {
				t.Helper()
				if err := os.WriteFile(path, raw, mode); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(path, mode); err != nil {
					t.Fatal(err)
				}
			}
			binary := []byte("target Agent bytes must only be read")
			put(filepath.Join(bin, "agent"), binary, 0755)
			c, err := agentnativecontract.New(binary, strings.Repeat("a", 40))
			if err != nil {
				t.Fatal(err)
			}
			c.DNSSwitchJournalPolicy = ""
			if tc.name == "old-policy" {
				c.DNSEvidencePolicy = ""
			}
			if tc.name == "v2-switch-compatible" || tc.name == "late-v2-switch-change" {
				c.DNSSwitchJournalPolicy = agentnativecontract.DNSSwitchJournalPolicy
			}

			contract, err := agentnativecontract.Encode(c)
			if err != nil {
				t.Fatal(err)
			}
			if tc.name == "v2-switch-unknown-contract" {
				contract = bytes.Replace(contract, []byte(`"dns_evidence_policy"`), []byte(`"dns_switch_journal_policy":"unsupported","dns_evidence_policy"`), 1)
			}
			if tc.name != "historical-agent" && tc.name != "v2-switch-historical-agent" {
				put(filepath.Join(bin, agentnativecontract.FileName), contract, 0644)
			}
			owner, err := os.ReadFile("../dnsengineartifact/testdata/alpha81-bind-acquisition.json")
			if err != nil {
				t.Fatal(err)
			}
			current, err := os.ReadFile("../dnsengineartifact/testdata/alpha81-bind-zone-add.json")
			if err != nil {
				t.Fatal(err)
			}
			plan, err := dnsengineartifact.PlanLegacySeparationV1(owner, current)
			if err != nil {
				t.Fatal(err)
			}
			newOwner, newCurrent, err := dnsengineartifact.SeparationDocumentsV2(plan)
			if err != nil {
				t.Fatal(err)
			}
			statePath, ownerPath := filepath.Join(private, "dns-engine-state.json"), filepath.Join(private, "dns-engine-ownership-bind.json")
			put(statePath, newCurrent, 0600)
			put(ownerPath, newOwner, 0600)
			journalPath := filepath.Join(private, "dns-engine-switch-journal.json")
			if strings.Contains(tc.name, "switch") {
				policy := dnsengineartifact.JournalPolicy{
					StatePath: statePath, StateUID: 0, StateGID: 0, RequireOwner: true,
					PDNSMainPath:     "/etc/powerdns/pdns.conf",
					PDNSManagedPath:  "/etc/powerdns/pdns.d/celikpanel.conf",
					PDNSClusterPath:  "/etc/powerdns/pdns.d/celikpanel-cluster.conf",
					PDNSDatabasePath: "/var/lib/powerdns/pdns.sqlite3",
				}
				fixture, err := os.ReadFile("../dnsengineartifact/testdata/switch-journal/alpha81-bind.json")
				if err != nil {
					t.Fatal(err)
				}
				journal, err := policy.DecodeSwitchJournal(bytes.Replace(fixture, []byte("/var/lib/celikpanel-agent-private/dns-engine-state.json"), []byte(statePath), 1))
				if err != nil {
					t.Fatal(err)
				}
				if tc.name != "v1-switch-old-agent" && tc.name != "late-v2-switch-absent" {
					journal.Phase = dnsengineartifact.SwitchPhaseIntent
					journal.ConfigBefore = []dnsengineartifact.FileSnapshot{
						{Path: "/etc/bind/named.conf.local", Exists: true, Mode: 0o644, OwnerKnown: true, GID: 42, Data: []byte("before local"), SHA256: dnsengineartifact.DigestBytes([]byte("before local"))},
						{Path: "/etc/bind/named.conf.options", Exists: true, Mode: 0o644, OwnerKnown: true, GID: 42, Data: []byte("before options"), SHA256: dnsengineartifact.DigestBytes([]byte("before options"))},
					}
					after := append([]dnsengineartifact.FileSnapshot(nil), journal.ConfigBefore...)
					for i := range after {
						after[i].Data = []byte("after")
						after[i].SHA256 = dnsengineartifact.DigestBytes(after[i].Data)
					}
					journal, err = policy.BuildBINDSwitchInverseJournalV2(journal, "apt", after)
					if err != nil {
						t.Fatal(err)
					}
				}
				encoded, err := policy.EncodeSwitchJournal(journal)
				if err != nil {
					t.Fatal(err)
				}
				if tc.name != "late-v2-switch-absent" {
					put(journalPath, encoded, 0600)
				}
				if tc.name == "v2-switch-corrupt" {
					put(journalPath, []byte("{}\n"), 0600)
				}
				if tc.name == "v2-switch-symlink" {
					if err := os.Remove(journalPath); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(statePath, journalPath); err != nil {
						t.Fatal(err)
					}
				}
				if tc.name == "v2-switch-hardlink" {
					if err := os.Link(journalPath, filepath.Join(root, "journal-hardlink")); err != nil {
						t.Fatal(err)
					}
				}
			}
			var late func()
			switch tc.name {
			case "absent-root", "late-absent-root":
				if err := os.RemoveAll(private); err != nil {
					t.Fatal(err)
				}
				if tc.name == "late-absent-root" {
					late = func() {
						if err := os.Mkdir(private, 0700); err != nil {
							t.Fatal(err)
						}
					}
				}
			case "empty":
				if err := os.Remove(statePath); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(ownerPath); err != nil {
					t.Fatal(err)
				}
			case "legacy":
				put(statePath, current, 0600)
				put(ownerPath, owner, 0600)
				if err := os.Remove(filepath.Join(bin, agentnativecontract.FileName)); err != nil {
					t.Fatal(err)
				}
			case "mixed-compatible":
				put(ownerPath, owner, 0600)
			case "private-service-group":
				for _, p := range []string{private, statePath, ownerPath} {
					if err := os.Chown(p, 0, 12345); err != nil {
						t.Fatal(err)
					}
				}
			case "changed-agent":
				put(filepath.Join(bin, "agent"), []byte("owner binary"), 0755)
			case "unknown-schema":
				put(statePath, []byte("{}\n"), 0600)
			case "wrong-role":
				put(ownerPath, newCurrent, 0600)
			case "wrong-engine-path":
				if err := os.Rename(ownerPath, filepath.Join(private, "dns-engine-ownership-pdns.json")); err != nil {
					t.Fatal(err)
				}
			case "file-mode":
				if err := os.Chmod(statePath, 0644); err != nil {
					t.Fatal(err)
				}
			case "file-link":
				if err := os.Remove(statePath); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(ownerPath, statePath); err != nil {
					t.Fatal(err)
				}
			case "hardlink":
				if err := os.Link(statePath, filepath.Join(root, "extra-link")); err != nil {
					t.Fatal(err)
				}
			case "foreign-owner":
				if err := os.Chown(statePath, 12345, 0); err != nil {
					t.Fatal(err)
				}
			case "parent-mode":
				if err := os.Chmod(private, 0755); err != nil {
					t.Fatal(err)
				}
			case "late-current-change":
				late = func() { put(statePath, current, 0600) }
			case "late-current-replacement":
				late = func() {
					stage := filepath.Join(private, "replacement")
					put(stage, newCurrent, 0600)
					if err := os.Rename(stage, statePath); err != nil {
						t.Fatal(err)
					}
				}
			case "late-absent-file":
				late = func() {
					put(filepath.Join(private, "dns-engine-ownership-pdns.json"), []byte("{}\n"), 0600)
				}
			case "late-parent-replacement":
				late = func() {
					if err := os.Rename(private, private+"-old"); err != nil {
						t.Fatal(err)
					}
					if err := os.Mkdir(private, 0700); err != nil {
						t.Fatal(err)
					}
				}
			case "late-v2-switch-change":
				late = func() { put(journalPath, []byte("{}\\n"), 0600) }
			case "late-v2-switch-absent":
				late = func() { put(journalPath, []byte("{}\\n"), 0600) }
			case "late-target-change":
				late = func() { put(filepath.Join(bin, "agent"), []byte("late owner binary"), 0755) }
			}
			err = checkDNSApplicationCompatibility(bin, private, late)
			if (err == nil) != tc.allowed {
				t.Fatalf("allowed=%v error=%v", tc.allowed, err)
			}
			// Successful read-only checks cannot normalize or rewrite retained evidence.
			if tc.name == "private-service-group" {
				info, err := os.Stat(statePath)
				if err != nil || info.Mode().Perm() != 0600 || info.Sys().(*syscall.Stat_t).Gid != 12345 {
					t.Fatal("metadata changed", err)
				}
				got, err := os.ReadFile(statePath)
				if err != nil || string(got) != string(newCurrent) {
					t.Fatal("evidence changed", err)
				}
			}
		})
	}
}

func TestBINDSourceJournalRequiresCompatibleApplication(t *testing.T) {
	for _, policy := range []string{"", "unsupported", agentnativecontract.DNSSwitchJournalPolicy, agentnativecontract.DNSSwitchSourceJournalPolicy, agentnativecontract.DNSSwitchAdoptionJournalPolicy} {
		for _, source := range []bool{false, true} {
			for _, adoption := range []bool{false, true} {
				want := false
				switch policy {
				case agentnativecontract.DNSSwitchJournalPolicy:
					want = !source && !adoption
				case agentnativecontract.DNSSwitchSourceJournalPolicy:
					want = !adoption
				case agentnativecontract.DNSSwitchAdoptionJournalPolicy:
					want = true
				}
				if got := supportsBINDJournalPolicy(policy, source, adoption); got != want {
					t.Fatalf("policy %q source %v adoption %v: got %v want %v", policy, source, adoption, got, want)
				}
			}
		}
	}
}
