//go:build linux

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
)

func testCanonicalBINDSwitchJournalV2(t *testing.T) dnsEngineSwitchJournal {
	t.Helper()
	path := filepath.Join("..", "..", "internal", "dnsengineartifact", "testdata", "switch-journal", "v2-bind-apt.json")
	wire, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	journal, err := decodeDNSEngineSwitchJournal(wire)
	if err != nil {
		t.Fatalf("agent cannot read the canonical v2 producer fixture: %v", err)
	}
	if journal.Schema != dnsengineartifact.SwitchJournalSchemaV2 ||
		journal.InversePlan == nil ||
		journal.InversePlan.Kind != dnsengineartifact.BINDSwitchInversePlanKindV2 {
		t.Fatal("canonical BIND inverse plan was not retained")
	}
	return journal
}

func TestAgentBINDSwitchJournalV2CheckpointPreservesFrozenPlan(t *testing.T) {
	intent := testCanonicalBINDSwitchJournalV2(t)
	var stored dnsEngineSwitchJournal
	var exists bool
	writes := 0
	read := func() (dnsEngineSwitchJournal, bool, error) { return stored, exists, nil }
	persist := func(wire []byte) error {
		var err error
		stored, err = decodeDNSEngineSwitchJournal(wire)
		exists = err == nil
		writes++
		return err
	}
	write := func(journal dnsEngineSwitchJournal) error {
		return writeDNSEngineSwitchJournalWithOps(journal, persist, read, nil)
	}
	if err := write(intent); err != nil {
		t.Fatal(err)
	}
	next := intent
	next.Phase = dnsSwitchPhaseRollingBack
	if err := write(next); err != nil {
		t.Fatal(err)
	}
	if !dnsengineartifact.SameImmutableBINDSwitchInversePlanV2(intent, stored) {
		t.Fatal("agent phase checkpoint changed the immutable inverse plan")
	}
	if writes != 2 {
		t.Fatalf("expected two durable checkpoints, got %d", writes)
	}

	downgraded := next
	downgraded.Schema = dnsengineartifact.SwitchJournalSchemaV1
	downgraded.InversePlan = nil
	if err := write(downgraded); err == nil || !strings.Contains(err.Error(), "changed before checkpoint") {
		t.Fatalf("v1 replacement of active v2 evidence was accepted: %v", err)
	}
	changed := next
	plan := *next.InversePlan
	changed.InversePlan = &plan
	changed.MutationOwnerID = strings.Repeat("d", 32)
	if err := write(changed); err == nil {
		t.Fatalf("changed v2 operation was accepted: %v", err)
	}
	if writes != 2 {
		t.Fatal("rejected replacement wrote a checkpoint")
	}
}

func TestAgentBINDSwitchJournalV2RequiresFreshIntentAndRefusesLegacyRollback(t *testing.T) {
	journal := testCanonicalBINDSwitchJournalV2(t)
	journal.Phase = dnsSwitchPhaseRollingBack
	writes := 0
	err := writeDNSEngineSwitchJournalWithOps(journal,
		func([]byte) error { writes++; return nil },
		func() (dnsEngineSwitchJournal, bool, error) { return dnsEngineSwitchJournal{}, false, nil },
		nil)
	if err == nil || !strings.Contains(err.Error(), "fresh intent") || writes != 0 {
		t.Fatalf("non-intent v2 journal was written: err=%v writes=%d", err, writes)
	}
	if err := rollbackDNSSwitchJournal(nil, journal); err == nil ||
		!strings.Contains(err.Error(), "independent inverse adapter") {
		t.Fatalf("legacy Agent rollback consumed v2 evidence: %v", err)
	}
	readErr := errors.New("journal unreadable")
	err = writeDNSEngineSwitchJournalWithOps(journal,
		func([]byte) error { writes++; return nil },
		func() (dnsEngineSwitchJournal, bool, error) { return dnsEngineSwitchJournal{}, false, readErr },
		nil)
	if !errors.Is(err, readErr) || writes != 0 {
		t.Fatalf("unreadable evidence was replaced: err=%v writes=%d", err, writes)
	}
}
