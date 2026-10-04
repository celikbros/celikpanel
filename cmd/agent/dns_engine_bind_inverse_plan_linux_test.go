//go:build linux

package main

import (
	"context"
	"errors"
	"testing"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/hostplatform"
	"github.com/alicelik/celikpanel/internal/transport"
)

func TestBINDRollbackRefusesSourceChangeBeforeServiceCommands(t *testing.T) {
	refused := errors.New("source database changed by owner")
	called := 0
	err := rollbackBINDActivation(context.Background(), "/nonexistent-systemctl", bindConfigMutation{}, dnsFileSnapshot{}, nil, nil, false, func(context.Context) error { called++; return refused })
	if !errors.Is(err, refused) || called != 1 {
		t.Fatalf("source refusal lost before native mutations: calls=%d err=%v", called, err)
	}
}

func TestBINDIndependentIntentScopePreservesLegacyBranches(t *testing.T) {
	base := dnsEngineSwitchJournal{
		Schema: dnsengineartifact.SwitchJournalSchemaV1, Mode: transport.DNSEngineSwitchModeSwitch,
		SourceEngine: transport.DNSEnginePowerDNS, TargetEngine: transport.DNSEngineBIND,
		Topology: transport.DNSTopologyStandalone, StateBefore: dnsFileSnapshot{Exists: true},
		TargetUnitsBefore: []dnsengineartifact.UnitSnapshot{{Name: "named.service", ActiveState: "inactive"}, {Name: "bind9.service", ActiveState: "inactive"}},
		SourceUnitsBefore: []dnsengineartifact.UnitSnapshot{{Name: "pdns.service", ActiveState: "active"}},
	}
	profile := hostplatform.Profile{PackageManager: hostplatform.PackageManagerAPT}
	if !requiresBINDIndependentSourceProof(profile, base) {
		t.Fatal("managed standalone switch omitted its source proof")
	}
	for _, name := range []string{"paired", "unmanaged", "running-bind", "inactive-source", "missing-alias", "non-apt"} {
		t.Run(name, func(t *testing.T) {
			journal := base
			candidate := profile
			journal.TargetUnitsBefore = append([]dnsengineartifact.UnitSnapshot(nil), base.TargetUnitsBefore...)
			journal.SourceUnitsBefore = append([]dnsengineartifact.UnitSnapshot(nil), base.SourceUnitsBefore...)
			switch name {
			case "paired":
				journal.Topology = transport.DNSTopologyPaired
			case "unmanaged":
				journal.StateBefore.Exists = false
			case "running-bind":
				journal.TargetUnitsBefore[0].ActiveState = "active"
			case "inactive-source":
				journal.SourceUnitsBefore[0].ActiveState = "inactive"
			case "missing-alias":
				journal.TargetUnitsBefore = journal.TargetUnitsBefore[:1]
			case "non-apt":
				candidate.PackageManager = ""
			}
			if requiresBINDIndependentSourceProof(candidate, journal) {
				t.Fatal("unsupported native inverse was selected")
			}
			got, err := prepareBINDIndependentInverseJournal(nil, candidate, journal, bindConfigMutation{})
			if err != nil || got.Schema != journal.Schema || got.InversePlan != nil {
				t.Fatalf("legacy branch was rewritten: %v", err)
			}
		})
	}
}

func TestBINDInverseProofCannotBeInventedForLegacyV2(t *testing.T) {
	journal := testCanonicalBINDSwitchJournalV2(t)
	if err := verifyBINDIndependentSourceProof(context.Background(), journal); err == nil {
		t.Fatal("v2 without source preimage was silently accepted")
	}
}
func TestBINDIndependentIntentBindsAcceptedAndFrozenSourceBeforeJournal(t *testing.T) {
	journal := dnsEngineSwitchJournal{Schema: dnsengineartifact.SwitchJournalSchemaV2}
	var steps []string
	journalPublished := false
	accepted := func() error {
		steps = append(steps, "accepted")
		if journalPublished {
			return errors.New("ordinary PowerDNS readiness rejects an active switch journal")
		}
		return nil
	}
	frozen := func() error {
		steps = append(steps, "frozen")
		return nil
	}
	publish := func() error {
		steps = append(steps, "publish")
		journalPublished = true
		return nil
	}
	if err := publishBINDIntentAfterIndependentSourceProof(journal, accepted, frozen, publish); err != nil {
		t.Fatal(err)
	}
	if !journalPublished || len(steps) != 3 || steps[0] != "accepted" || steps[1] != "frozen" || steps[2] != "publish" {
		t.Fatalf("intent publication crossed the source proof boundary: %v", steps)
	}
	steps = nil
	journalPublished = false
	refused := errors.New("accepted manifest differs from frozen PowerDNS database")
	if err := publishBINDIntentAfterIndependentSourceProof(journal,
		func() error { return refused }, frozen, publish,
	); !errors.Is(err, refused) || journalPublished || len(steps) != 0 {
		t.Fatalf("failed accepted projection published intent: steps=%v err=%v", steps, err)
	}
	if err := publishBINDIntentAfterIndependentSourceProof(journal, accepted,
		func() error { return refused }, publish,
	); !errors.Is(err, refused) || journalPublished {
		t.Fatalf("changed frozen source published intent: err=%v", err)
	}
}
