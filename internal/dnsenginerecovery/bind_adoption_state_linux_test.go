//go:build linux

package dnsenginerecovery

import (
	"bytes"
	"github.com/alicelik/celikpanel/internal/bindconfig"
	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func adoptionConfigSnap(path string, mode, gid uint32, data []byte) dnsengineartifact.FileSnapshot {
	return dnsengineartifact.FileSnapshot{Path: path, Exists: true, Mode: mode, OwnerKnown: true, GID: gid, Data: data, SHA256: dnsengineartifact.DigestBytes(data)}
}
func runningBINDJournalFixture(t *testing.T) (dnsengineartifact.JournalPolicy, dnsengineartifact.SwitchJournalV1) {
	t.Helper()
	policy, j := bindConfigClassifierFixture(t)
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	policy.StatePath = filepath.Join(root, "dns-engine-state.json")
	policy.StateUID, policy.StateGID = uint32(os.Getuid()), uint32(os.Getgid())
	j.StateBefore = dnsengineartifact.FileSnapshot{Path: policy.StatePath}
	j.Schema = dnsengineartifact.SwitchJournalSchemaV1
	j.InversePlan = nil
	j.TargetUnitsBefore = []dnsengineartifact.UnitSnapshot{
		{Name: "bind9.service", LoadState: "loaded", ActiveState: "active", UnitFileState: "enabled"},
		{Name: "named.service", LoadState: "loaded", ActiveState: "active", UnitFileState: "enabled"},
	}
	local := []byte("zone \"owner.example\" { type master; file \"/etc/bind/db.owner\"; };\n")
	options := []byte("options { directory \"/var/cache/bind\"; dnssec-validation auto; };\n")
	j.ConfigBefore = []dnsengineartifact.FileSnapshot{
		adoptionConfigSnap("/etc/bind/named.conf.local", 0o644, 42, local),
		adoptionConfigSnap("/etc/bind/named.conf.options", 0o644, 42, options),
	}
	main := []byte("include \"/etc/bind/named.conf.options\";\ninclude \"/etc/bind/named.conf.local\";\ninclude \"/etc/bind/named.conf.root-hints\";\n")
	leaf := []byte("zone \".\" { type hint; file \"/usr/share/dns/root.hints\"; };\n")
	unchanged := []dnsengineartifact.FileSnapshot{adoptionConfigSnap("/etc/bind/named.conf", 0o644, 42, main), adoptionConfigSnap("/etc/bind/named.conf.root-hints", 0o644, 42, leaf)}
	managed, e := bindconfig.ManagedZoneInclude(string(local), "/var/cache/bind/celikpanel/current/zones.conf")
	if e != nil {
		t.Fatal(e)
	}
	after := []dnsengineartifact.FileSnapshot{adoptionConfigSnap("/etc/bind/named.conf.local", 0o644, 42, []byte(managed)), j.ConfigBefore[1]}
	proof := dnsengineartifact.BINDAdoptionSourceProofV1{
		Kind: dnsengineartifact.BINDAdoptionSourceProofKindV1,
		Files: []dnsengineartifact.BINDAdoptionSourceFileV1{
			{Path: "/etc/bind/db.owner", SHA256: dnsengineartifact.DigestBytes([]byte("owner")), Size: 5, Mode: 0o644, UID: 0, GID: 42, Device: 1, Inode: 2},
			{Path: "/usr/share/dns/root.hints", SHA256: dnsengineartifact.DigestBytes([]byte("hints")), Size: 5, Mode: 0o644, UID: 0, GID: 0, Device: 1, Inode: 3},
		},
		Zones: []dnsengineartifact.BINDAdoptionSourceZoneV1{
			{Name: "owner.example", Class: "IN", Type: "master", File: "/etc/bind/db.owner", SOASerial: 7},
			{Name: ".", Class: "IN", Type: "hint", File: "/usr/share/dns/root.hints"},
		},
	}
	result, e := policy.BuildBINDSwitchInverseJournalV2(j, "apt", after, dnsengineartifact.BINDSwitchSourceProofV2{BINDUnchangedConfig: unchanged, SourceBIND: &proof})
	if e != nil {
		t.Fatal(e)
	}
	return policy, result
}

func TestBINDAdoptionStateRemovalRequiresExactTarget(t *testing.T) {
	for _, edit := range []string{"exact", "owner-edit", "symlink", "hardlink", "unsafe-mode", "rolled-back-target"} {
		t.Run(edit, func(t *testing.T) {
			policy, j := runningBINDJournalFixture(t)
			j.Phase = dnsengineartifact.SwitchPhaseRollingBack
			owner := servicemutationledger.FileOwner{UID: policy.StateUID, GID: policy.StateGID}
			target := dnsengineartifact.StateV1{Schema: dnsengineartifact.StateSchemaV1, Mode: j.Mode, Engine: j.TargetEngine, EngineEpoch: j.TargetEpoch, Generation: j.TargetGeneration, SourceRevision: j.SourceRevision, ManifestQualifier: j.ManifestQualifier, MutationRequestID: j.MutationRequestID, MutationOwnerID: j.MutationOwnerID}
			if edit == "owner-edit" {
				target.MutationOwnerID = strings.Repeat("f", 32)
			}
			raw, err := dnsengineartifact.CanonicalV1(target)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(policy.StatePath, raw, 0600); err != nil {
				t.Fatal(err)
			}
			switch edit {
			case "symlink":
				renamed := policy.StatePath + ".original"
				if err := os.Rename(policy.StatePath, renamed); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(renamed, policy.StatePath); err != nil {
					t.Fatal(err)
				}
			case "hardlink":
				if err := os.Link(policy.StatePath, policy.StatePath+".other"); err != nil {
					t.Fatal(err)
				}
			case "unsafe-mode":
				if err := os.Chmod(policy.StatePath, 0666); err != nil {
					t.Fatal(err)
				}
			case "rolled-back-target":
				j.Phase = dnsengineartifact.SwitchPhaseRolledBack
			}
			err = RemoveExactBINDAdoptionTargetReceipt(policy, owner, j)
			if edit == "exact" {
				if err != nil {
					t.Fatal(err)
				}
				if _, err := os.Lstat(policy.StatePath); !os.IsNotExist(err) {
					t.Fatal("exact target remained")
				}
				if err := RemoveExactBINDAdoptionTargetReceipt(policy, owner, j); err != nil {
					t.Fatal("absent retry:", err)
				}
			} else {
				if err == nil {
					t.Fatal("foreign receipt removed")
				}
				current, err := os.ReadFile(policy.StatePath)
				if err != nil || !bytes.Equal(current, raw) {
					t.Fatal("refusal changed owner evidence")
				}
			}
		})
	}
}
