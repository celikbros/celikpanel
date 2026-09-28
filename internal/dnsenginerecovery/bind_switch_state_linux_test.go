//go:build linux

package dnsenginerecovery

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
	"github.com/alicelik/celikpanel/internal/transport"
)

func bindStateRestoreFixture(t *testing.T) (dnsengineartifact.JournalPolicy, servicemutationledger.FileOwner, dnsengineartifact.SwitchJournalV1, []byte) {
	t.Helper()
	p, base := bindConfigClassifierFixture(t)
	after := base.InversePlan.ConfigAfter
	base.Schema = dnsengineartifact.SwitchJournalSchemaV1
	base.InversePlan = nil
	base.SourceEngine = transport.DNSEnginePowerDNS
	base.SourceEpoch = 1
	base.TargetEpoch = 2
	manifest, err := mutationpayload.CanonicalDNSEngineSwitchManifestWithPairIdentity(base.Mode, base.SourceEngine, base.TargetEngine, base.SourceEpoch, base.TargetEpoch, base.SourceRevision, base.Topology, "", "", "", "", "", base.Zones)
	if err != nil {
		t.Fatal(err)
	}
	base.ManifestQualifier = manifest.Qualifier
	base.SnapshotBytes = manifest.SnapshotBytes
	base.Zones = manifest.Zones
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	owner := servicemutationledger.FileOwner{UID: uint32(os.Getuid()), GID: uint32(os.Getgid())}
	p.StateUID, p.StateGID = owner.UID, owner.GID
	p.StatePath = filepath.Join(root, "dns-engine-state.json")
	source := dnsengineartifact.StateV1{Schema: dnsengineartifact.StateSchemaV1, Mode: "switch", Engine: transport.DNSEnginePowerDNS, EngineEpoch: 1, SourceRevision: 0, ManifestQualifier: manifest.Qualifier, MutationRequestID: strings.Repeat("d", 32), MutationOwnerID: strings.Repeat("e", 32)}
	sourceRaw, err := dnsengineartifact.CanonicalV1(source)
	if err != nil {
		t.Fatal(err)
	}
	base.StateBefore = dnsengineartifact.FileSnapshot{Path: p.StatePath, Exists: true, Mode: 0600, OwnerKnown: true, UID: owner.UID, GID: owner.GID, Data: sourceRaw, SHA256: dnsengineartifact.DigestBytes(sourceRaw)}
	base.SourceUnitsBefore = []dnsengineartifact.UnitSnapshot{{Name: "pdns.service", LoadState: "loaded", ActiveState: "active", UnitFileState: "enabled"}}
	j, err := p.BuildBINDSwitchInverseJournalV2(base, "apt", after)
	if err != nil {
		t.Fatal(err)
	}
	j.Phase = dnsengineartifact.SwitchPhaseRollingBack
	target := dnsengineartifact.StateV1{Schema: dnsengineartifact.StateSchemaV1, Mode: j.Mode, Engine: j.TargetEngine, EngineEpoch: j.TargetEpoch, Generation: j.TargetGeneration, SourceRevision: j.SourceRevision, ManifestQualifier: j.ManifestQualifier, MutationRequestID: j.MutationRequestID, MutationOwnerID: j.MutationOwnerID}
	raw, err := dnsengineartifact.CanonicalV1(target)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.StatePath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return p, owner, j, raw
}

func TestRestoreExactBINDSwitchSourceReceiptAndRetry(t *testing.T) {
	p, owner, j, _ := bindStateRestoreFixture(t)
	if err := RestoreExactBINDSwitchSourceReceipt(p, owner, j); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(p.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(p.StatePath)
	if err != nil || !bytes.Equal(got, j.StateBefore.Data) {
		t.Fatalf("source not restored: %v", err)
	}
	for _, phase := range []string{dnsengineartifact.SwitchPhaseRollingBack, dnsengineartifact.SwitchPhaseRolledBack} {
		j.Phase = phase
		if err := RestoreExactBINDSwitchSourceReceipt(p, owner, j); err != nil {
			t.Fatal(err)
		}
		after, err := os.Stat(p.StatePath)
		if err != nil || !os.SameFile(before, after) {
			t.Fatal("exact source retry replaced its inode")
		}
	}
}

func TestRestoreExactBINDSwitchSourceReceiptPreservesUnknown(t *testing.T) {
	for _, name := range []string{"owner-edit", "missing", "symlink", "hardlink", "unsafe-mode", "forward-phase", "rolled-back-target", "v1"} {
		t.Run(name, func(t *testing.T) {
			p, owner, j, raw := bindStateRestoreFixture(t)
			switch name {
			case "owner-edit":
				s, err := dnsengineartifact.DecodeV1(raw)
				if err != nil {
					t.Fatal(err)
				}
				s.MutationOwnerID = strings.Repeat("f", 32)
				raw, err = dnsengineartifact.CanonicalV1(s)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p.StatePath, raw, 0600); err != nil {
					t.Fatal(err)
				}
			case "missing":
				if err := os.Remove(p.StatePath); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				dest := filepath.Join(filepath.Dir(p.StatePath), "owner-state")
				if err := os.Rename(p.StatePath, dest); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(dest, p.StatePath); err != nil {
					t.Fatal(err)
				}
			case "hardlink":
				if err := os.Link(p.StatePath, filepath.Join(filepath.Dir(p.StatePath), "owner-state")); err != nil {
					t.Fatal(err)
				}
			case "unsafe-mode":
				if err := os.Chmod(p.StatePath, 0644); err != nil {
					t.Fatal(err)
				}
			case "forward-phase":
				j.Phase = dnsengineartifact.SwitchPhaseTargetVerified
			case "rolled-back-target":
				j.Phase = dnsengineartifact.SwitchPhaseRolledBack
			case "v1":
				j.Schema = dnsengineartifact.SwitchJournalSchemaV1
				j.InversePlan = nil
			}
			if err := RestoreExactBINDSwitchSourceReceipt(p, owner, j); err == nil {
				t.Fatal("unsafe or foreign state accepted")
			}
			if name == "missing" {
				if _, err := os.Lstat(p.StatePath); !os.IsNotExist(err) {
					t.Fatal("missing state recreated")
				}
				return
			}
			got, err := os.ReadFile(p.StatePath)
			if err != nil || !bytes.Equal(got, raw) {
				t.Fatal("rejected evidence changed")
			}
		})
	}
}
