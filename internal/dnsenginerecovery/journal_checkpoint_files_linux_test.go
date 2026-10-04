//go:build linux

package dnsenginerecovery

import (
	"context"
	"errors"
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

func TestReconcileResumesExactJournalAfterInterruptedIndependentCheckpoint(t *testing.T) {
	policy, owner, journal, path := rollbackCheckpointFixture(t)
	id := dnsengineartifact.SwitchIdentity{RequestID: journal.MutationRequestID, OwnerID: journal.MutationOwnerID, Target: journal.TargetEngine, Qualifier: journal.ManifestQualifier}
	read := func(context.Context) (dnsengineartifact.SwitchJournalV1, bool, error) {
		raw, exists, err := servicemutationledger.ReadFile(path, dnsengineartifact.SwitchJournalLimit, owner)
		if err != nil || !exists {
			return dnsengineartifact.SwitchJournalV1{}, exists, err
		}
		current, err := policy.DecodeSwitchJournal(raw)
		return current, err == nil, err
	}
	failInverse := true
	calls := 0
	ops := Operations{
		Read:              read,
		ProveFinalized:    func(context.Context, dnsengineartifact.SwitchIdentity) (bool, error) { return false, nil },
		VerifyTarget:      func(context.Context, dnsengineartifact.SwitchJournalV1) error { return errors.New("target unverified") },
		ProveTargetAbsent: func(context.Context, dnsengineartifact.SwitchJournalV1) (bool, error) { return true, nil },
		Write: func(_ context.Context, before, after dnsengineartifact.SwitchJournalV1) error {
			return ReplaceRollbackJournalPhase(policy, owner, before, after)
		},
		Inverse: func(_ context.Context, current dnsengineartifact.SwitchJournalV1) error {
			calls++
			if current.Phase != dnsengineartifact.SwitchPhaseRollingBack {
				t.Fatal("inverse has no durable intent")
			}
			if failInverse {
				return errors.New("native inverse unknown")
			}
			return nil
		},
	}
	if outcome, err := Reconcile(context.Background(), policy, id, ops); err == nil || outcome != OutcomeAbsent {
		t.Fatalf("interrupted inverse accepted: %s %v", outcome, err)
	}
	current, exists, err := read(context.Background())
	if err != nil || !exists || current.Phase != dnsengineartifact.SwitchPhaseRollingBack {
		t.Fatalf("interrupted inverse lost checkpoint: %s %v %v", current.Phase, exists, err)
	}
	failInverse = false
	if outcome, err := Reconcile(context.Background(), policy, id, ops); err != nil || outcome != OutcomeRolledBack || calls != 2 {
		t.Fatalf("same operation did not resume: %s %v calls=%d", outcome, err, calls)
	}
	current, exists, err = read(context.Background())
	if err != nil || !exists || current.Phase != dnsengineartifact.SwitchPhaseRolledBack {
		t.Fatalf("terminal checkpoint missing: %s %v %v", current.Phase, exists, err)
	}
}

func TestRemoveExactRollbackJournalRequiresTerminalExactCheckpoint(t *testing.T) {
	policy, owner, before, path := rollbackCheckpointFixture(t)
	if err := RemoveExactRollbackJournal(policy, owner, before); err == nil {
		t.Fatal("nonterminal rollback journal was retired")
	}
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
	foreign := rolled
	foreign.MutationOwnerID = "other"
	if err := RemoveExactRollbackJournal(policy, owner, foreign); err == nil {
		t.Fatal("foreign journal retired")
	}
	if err := RemoveExactRollbackJournal(policy, owner, rolled); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("terminal journal still present: %v", err)
	}
	if err := RemoveExactRollbackJournal(policy, owner, rolled); err == nil {
		t.Fatal("missing journal treated as a second successful cleanup")
	}
}

func TestRollbackJournalPhaseV4EnableIntentIsNarrow(t *testing.T) {
	for _, tc := range []struct {
		schema, before, after string
		want                  bool
	}{
		{dnsengineartifact.SwitchJournalSchemaV4, dnsengineartifact.SwitchPhaseTargetEnableIntent, dnsengineartifact.SwitchPhaseRollingBackTargetEnable, true},
		{dnsengineartifact.SwitchJournalSchemaV4, dnsengineartifact.SwitchPhaseRollingBackTargetEnable, dnsengineartifact.SwitchPhaseRolledBack, true},
		{dnsengineartifact.SwitchJournalSchemaV4, dnsengineartifact.SwitchPhaseTargetEnableIntent, dnsengineartifact.SwitchPhaseRollingBack, false},
		{dnsengineartifact.SwitchJournalSchemaV4, dnsengineartifact.SwitchPhaseRollingBackTargetEnable, dnsengineartifact.SwitchPhaseRollingBack, false},
		{dnsengineartifact.SwitchJournalSchemaV4, dnsengineartifact.SwitchPhaseTargetStarted, dnsengineartifact.SwitchPhaseRollingBackTargetEnable, false},
		{dnsengineartifact.SwitchJournalSchemaV1, dnsengineartifact.SwitchPhaseTargetEnableIntent, dnsengineartifact.SwitchPhaseRollingBackTargetEnable, false},
	} {
		before := dnsengineartifact.SwitchJournalV1{Schema: tc.schema, Phase: tc.before}
		after := before
		after.Phase = tc.after
		if got := allowedRollbackJournalPhase(before, after); got != tc.want {
			t.Fatalf("%s %s -> %s allowed=%t, want %t", tc.schema, tc.before, tc.after, got, tc.want)
		}
	}
}
