//go:build linux

package dnsenginerecovery

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
	"github.com/alicelik/celikpanel/internal/transport"
)

func adoptionStateRemovalFixture(t *testing.T) (dnsengineartifact.JournalPolicy, servicemutationledger.FileOwner, dnsengineartifact.SwitchJournalV1, dnsengineartifact.StateV1) {
	t.Helper()
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	owner := servicemutationledger.FileOwner{UID: uint32(os.Getuid()), GID: uint32(os.Getgid())}
	policy := dnsengineartifact.JournalPolicy{
		StatePath: "/var/lib/celikpanel-agent-private/dns-engine-state.json",
		StateUID:  owner.UID, StateGID: owner.GID, RequireOwner: true,
		PDNSMainPath:     "/etc/powerdns/pdns.conf",
		PDNSManagedPath:  "/etc/powerdns/pdns.d/celikpanel.conf",
		PDNSClusterPath:  "/etc/powerdns/pdns.d/celikpanel-cluster.conf",
		PDNSDatabasePath: "/var/lib/powerdns/pdns.sqlite3",
	}
	raw, err := os.ReadFile(filepath.Join("..", "dnsengineartifact", "testdata", "switch-journal", "alpha81-pdns-adopt.json"))
	if err != nil {
		t.Fatal(err)
	}
	journal, err := policy.DecodeSwitchJournal(raw)
	if err != nil {
		t.Fatal(err)
	}
	policy.StatePath = filepath.Join(root, "dns-engine-state.json")
	journal.StateBefore.Path = policy.StatePath
	journal.Phase = dnsengineartifact.SwitchPhaseRollingBack
	if err := policy.ValidateSwitchJournal(journal); err != nil {
		t.Fatal(err)
	}
	state := dnsengineartifact.StateV1{
		Schema:            dnsengineartifact.StateSchemaV1,
		Mode:              transport.DNSEngineSwitchModeAdopt,
		Engine:            transport.DNSEnginePowerDNS,
		EngineEpoch:       journal.TargetEpoch,
		SourceRevision:    journal.SourceRevision,
		ManifestQualifier: journal.ManifestQualifier,
		MutationRequestID: journal.MutationRequestID,
		MutationOwnerID:   journal.MutationOwnerID,
	}
	return policy, owner, journal, state
}

func TestRemoveExactPDNSAdoptionTargetReceipt(t *testing.T) {
	policy, owner, journal, state := adoptionStateRemovalFixture(t)
	raw, err := dnsengineartifact.CanonicalV1(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(policy.StatePath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RemoveExactPDNSAdoptionTargetReceipt(policy, owner, journal); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(policy.StatePath); !os.IsNotExist(err) {
		t.Fatalf("target receipt retained after exact removal: %v", err)
	}
	// A retry can see the already-removed state; native source proof remains
	// the caller's separate responsibility.
	journal.Phase = dnsengineartifact.SwitchPhaseRolledBack
	if err := RemoveExactPDNSAdoptionTargetReceipt(policy, owner, journal); err != nil {
		t.Fatal(err)
	}
}

func TestRemoveExactPDNSAdoptionTargetReceiptPreservesChangedOrUnsafeState(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*dnsengineartifact.StateV1)
	}{
		{"other-owner", func(s *dnsengineartifact.StateV1) { s.MutationOwnerID = strings.Repeat("c", 32) }},
		{"other-request", func(s *dnsengineartifact.StateV1) { s.MutationRequestID = strings.Repeat("c", 32) }},
		{"other-epoch", func(s *dnsengineartifact.StateV1) { s.EngineEpoch++ }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			policy, owner, journal, state := adoptionStateRemovalFixture(t)
			tc.change(&state)
			raw, err := dnsengineartifact.CanonicalV1(state)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(policy.StatePath, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := RemoveExactPDNSAdoptionTargetReceipt(policy, owner, journal); err == nil {
				t.Fatal("foreign or owner-edited state was removed")
			}
			after, err := os.ReadFile(policy.StatePath)
			if err != nil || string(after) != string(raw) {
				t.Fatal("rejected state was modified")
			}
		})
	}
	t.Run("symlink", func(t *testing.T) {
		policy, owner, journal, state := adoptionStateRemovalFixture(t)
		raw, err := dnsengineartifact.CanonicalV1(state)
		if err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(filepath.Dir(policy.StatePath), "owner-state")
		if err := os.WriteFile(target, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, policy.StatePath); err != nil {
			t.Fatal(err)
		}
		if err := RemoveExactPDNSAdoptionTargetReceipt(policy, owner, journal); err == nil {
			t.Fatal("symlinked state was accepted")
		}
		if _, err := os.Lstat(target); err != nil {
			t.Fatal("owner state was modified")
		}
	})
	t.Run("wrong-phase", func(t *testing.T) {
		policy, owner, journal, state := adoptionStateRemovalFixture(t)
		raw, err := dnsengineartifact.CanonicalV1(state)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(policy.StatePath, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		journal.Phase = dnsengineartifact.SwitchPhaseCommitted
		if err := RemoveExactPDNSAdoptionTargetReceipt(policy, owner, journal); err == nil {
			t.Fatal("committed target was removed without an inverse checkpoint")
		}
		if _, err := os.Lstat(policy.StatePath); err != nil {
			t.Fatal("target state was removed")
		}
	})
}
