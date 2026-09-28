//go:build linux

package dnsenginerecovery

import (
	"os"
	"reflect"
	"testing"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
)

func bindConfigClassifierFixture(t *testing.T) (dnsengineartifact.JournalPolicy, dnsengineartifact.SwitchJournalV1) {
	t.Helper()
	p := dnsengineartifact.JournalPolicy{
		StatePath: "/var/lib/celikpanel-agent-private/dns-engine-state.json", RequireOwner: true,
		PDNSMainPath: "/etc/powerdns/pdns.conf", PDNSManagedPath: "/etc/powerdns/pdns.d/celikpanel.conf",
		PDNSClusterPath: "/etc/powerdns/pdns.d/celikpanel-cluster.conf", PDNSDatabasePath: "/var/lib/powerdns/pdns.sqlite3",
	}
	raw, err := os.ReadFile("../dnsengineartifact/testdata/switch-journal/alpha81-bind.json")
	if err != nil {
		t.Fatal(err)
	}
	base, err := p.DecodeSwitchJournal(raw)
	if err != nil {
		t.Fatal(err)
	}
	base.Phase = dnsengineartifact.SwitchPhaseIntent
	base.ConfigBefore = []dnsengineartifact.FileSnapshot{
		{Path: "/etc/bind/named.conf.local", Exists: true, Mode: 0o644, OwnerKnown: true, GID: 42, Data: []byte("before local"), SHA256: dnsengineartifact.DigestBytes([]byte("before local"))},
		{Path: "/etc/bind/named.conf.options", Exists: true, Mode: 0o644, OwnerKnown: true, GID: 42, Data: []byte("before options"), SHA256: dnsengineartifact.DigestBytes([]byte("before options"))},
	}
	after := append([]dnsengineartifact.FileSnapshot(nil), base.ConfigBefore...)
	for i := range after {
		after[i].Data = []byte("managed after")
		after[i].SHA256 = dnsengineartifact.DigestBytes(after[i].Data)
	}
	j, err := p.BuildBINDSwitchInverseJournalV2(base, "apt", after)
	if err != nil {
		t.Fatal(err)
	}
	return p, j
}

func TestClassifyBINDSwitchConfigFilesV2(t *testing.T) {
	p, j := bindConfigClassifierFixture(t)
	cases := []struct {
		name    string
		current []dnsengineartifact.FileSnapshot
		want    []BINDConfigFileState
		fail    bool
	}{
		{"before", append([]dnsengineartifact.FileSnapshot(nil), j.ConfigBefore...), []BINDConfigFileState{BINDConfigFileBefore, BINDConfigFileBefore}, false},
		{"after", append([]dnsengineartifact.FileSnapshot(nil), j.InversePlan.ConfigAfter...), []BINDConfigFileState{BINDConfigFileAfter, BINDConfigFileAfter}, false},
		{"mixed-exact", []dnsengineartifact.FileSnapshot{j.InversePlan.ConfigAfter[0], j.ConfigBefore[1]}, []BINDConfigFileState{BINDConfigFileAfter, BINDConfigFileBefore}, false},
		{"incomplete", []dnsengineartifact.FileSnapshot{j.ConfigBefore[0]}, nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ClassifyBINDSwitchConfigFilesV2(p, j, tc.current)
			if (err != nil) != tc.fail || (!tc.fail && !reflect.DeepEqual(got, tc.want)) {
				t.Fatalf("states=%v error=%v", got, err)
			}
		})
	}
	for _, tc := range []struct {
		name string
		edit func(*dnsengineartifact.FileSnapshot)
	}{
		{"partial-bytes", func(s *dnsengineartifact.FileSnapshot) {
			s.Data = []byte("managed")
			s.SHA256 = dnsengineartifact.DigestBytes(s.Data)
		}},
		{"digest-mismatch", func(s *dnsengineartifact.FileSnapshot) { s.Data = []byte("owner edit") }},
		{"owner", func(s *dnsengineartifact.FileSnapshot) { s.GID++ }},
		{"mode", func(s *dnsengineartifact.FileSnapshot) { s.Mode = 0o600 }},
		{"path", func(s *dnsengineartifact.FileSnapshot) { s.Path = "/etc/passwd" }},
		{"absent", func(s *dnsengineartifact.FileSnapshot) { s.Exists = false }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			current := append([]dnsengineartifact.FileSnapshot(nil), j.ConfigBefore...)
			tc.edit(&current[0])
			got, err := ClassifyBINDSwitchConfigFilesV2(p, j, current)
			if err != nil || got[0] != BINDConfigFileUnknown || got[1] != BINDConfigFileBefore {
				t.Fatalf("states=%v error=%v", got, err)
			}
		})
	}
	t.Run("invalid-manifest-refused", func(t *testing.T) {
		broken := j
		broken.ManifestQualifier = "invalid"
		if _, err := ClassifyBINDSwitchConfigFilesV2(p, broken, broken.ConfigBefore); err == nil {
			t.Fatal("journal with invalid manifest passed complete validation")
		}
	})
	t.Run("v1-refused", func(t *testing.T) {
		j.Schema = dnsengineartifact.SwitchJournalSchemaV1
		j.InversePlan = nil
		if _, err := ClassifyBINDSwitchConfigFilesV2(p, j, j.ConfigBefore); err == nil {
			t.Fatal("v1 lacks frozen after evidence")
		}
	})
}
