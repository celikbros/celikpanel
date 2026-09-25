//go:build linux

package dnsenginerecovery

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
)

func rollbackCheckpointFixture(t *testing.T) (dnsengineartifact.JournalPolicy, servicemutationledger.FileOwner, dnsengineartifact.SwitchJournalV1, string) {
	t.Helper()
	policy, journal, _ := switchFixture(t)
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	owner := servicemutationledger.FileOwner{UID: uint32(os.Getuid()), GID: uint32(os.Getgid())}
	policy.StatePath = filepath.Join(root, "dns-engine-state.json")
	policy.StateUID, policy.StateGID = owner.UID, owner.GID
	journal.StateBefore.Path = policy.StatePath
	path := filepath.Join(root, "dns-engine-switch-journal.json")
	journal.Phase = dnsengineartifact.SwitchPhaseTargetStarted
	raw, err := policy.EncodeSwitchJournal(journal)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return policy, owner, journal, path
}

func TestReplaceRollbackJournalPhasePublishesOnlyExactForwardCheckpoints(t *testing.T) {
	policy, owner, before, path := rollbackCheckpointFixture(t)
	rolling := before
	rolling.Phase = dnsengineartifact.SwitchPhaseRollingBack
	if err := ReplaceRollbackJournalPhase(policy, owner, before, rolling); err != nil {
		t.Fatal(err)
	}
	rolled := rolling
	rolled.Phase = dnsengineartifact.SwitchPhaseRolledBack
	if err := ReplaceRollbackJournalPhase(policy, owner, rolling, rolled); err != nil {
		t.Fatal(err)
	}
	raw, exists, err := servicemutationledger.ReadFile(path, dnsengineartifact.SwitchJournalLimit, owner)
	if err != nil || !exists {
		t.Fatalf("checkpoint unreadable: %v %v", exists, err)
	}
	got, err := policy.DecodeSwitchJournal(raw)
	if err != nil || got.Phase != dnsengineartifact.SwitchPhaseRolledBack {
		t.Fatalf("wrong checkpoint phase: %s %v", got.Phase, err)
	}
	if err := ReplaceRollbackJournalPhase(policy, owner, before, rolling); err == nil {
		t.Fatal("stale preimage overwrote completed rollback")
	}
}

func TestReplaceRollbackJournalPhaseRefusesForeignOwnerEvidenceAndBackwardsPhase(t *testing.T) {
	for _, name := range []string{"different owner", "changed identity", "committed", "nonrollback", "missing"} {
		t.Run(name, func(t *testing.T) {
			p, o, b, file := rollbackCheckpointFixture(t)
			n := b
			n.Phase = dnsengineartifact.SwitchPhaseRollingBack
			switch name {
			case "different owner":
				o.GID++
			case "changed identity":
				n.MutationOwnerID = "other"
			case "committed":
				b.Phase = dnsengineartifact.SwitchPhaseCommitted
			case "nonrollback":
				n.Phase = dnsengineartifact.SwitchPhaseTargetVerified
			case "missing":
				if err := os.Remove(file); err != nil {
					t.Fatal(err)
				}
			}
			if err := ReplaceRollbackJournalPhase(p, o, b, n); err == nil {
				t.Fatal("unsafe checkpoint accepted")
			}
		})
	}
}
