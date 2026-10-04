package dnsenginerecovery

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/transport"
)

func TestPlanNativeInverseUsesFrozenJournalShape(t *testing.T) {
	policy, _, _, _ := inspectionFixture(t)
	for _, tc := range []struct {
		fixture string
		want    NativeInverseKind
	}{
		{"alpha81-bind.json", NativeInverseBINDSwitch},
		{"alpha81-pdns-adopt.json", NativeInversePDNSAdoption},
		{"alpha81-pdns-switch.json", NativeInversePDNSSwitch},
	} {
		t.Run(tc.fixture, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("..", "dnsengineartifact", "testdata", "switch-journal", tc.fixture))
			if err != nil {
				t.Fatal(err)
			}
			journal, err := policy.DecodeSwitchJournal(raw)
			if err != nil {
				t.Fatal(err)
			}
			got, err := PlanNativeInverse(journal)
			if err != nil || got != tc.want {
				t.Fatalf("inverse = %q, %v; want %q", got, err, tc.want)
			}
			if tc.fixture != "alpha81-bind.json" {
				return
			}
			for i := range journal.TargetUnitsBefore {
				journal.TargetUnitsBefore[i].LoadState = "loaded"
				journal.TargetUnitsBefore[i].ActiveState = "active"
				journal.TargetUnitsBefore[i].UnitFileState = "enabled"
			}
			if err := policy.ValidateSwitchJournal(journal); err != nil {
				t.Fatal(err)
			}
			got, err = PlanNativeInverse(journal)
			if err != nil || got != NativeInverseBINDRunningAdoption {
				t.Fatalf("running takeover = %q, %v", got, err)
			}
			journal.TargetUnitsBefore[0].ActiveState = "inactive"
			if _, err = PlanNativeInverse(journal); err == nil {
				t.Fatal("contradictory loaded BIND aliases selected an inverse")
			}
			journal.TargetUnitsBefore[0].ActiveState = "active"
			journal.ManifestQualifier = strings.Repeat("f", len(journal.ManifestQualifier))
			if _, err = PlanNativeInverse(journal); err == nil {
				t.Fatal("manifest mismatch selected an inverse")
			}
		})
	}
}

func beforeDecisionBINDSwitchJournal(phase string) dnsengineartifact.SwitchJournalV1 {
	return dnsengineartifact.SwitchJournalV1{
		Schema: dnsengineartifact.SwitchJournalSchemaV2,
		InversePlan: &dnsengineartifact.BINDSwitchInversePlanV2{
			Kind:                dnsengineartifact.BINDSwitchInversePlanKindV2,
			SourcePDNS:          &dnsengineartifact.PDNSSourceProofV2{Kind: dnsengineartifact.PDNSSourceProofKindV1},
			BINDUnchangedConfig: []dnsengineartifact.FileSnapshot{{Path: "/etc/bind/named.conf", Exists: true}, {Path: "/etc/bind/named.conf.default-zones", Exists: true}},
		},
		Phase: phase, Mode: transport.DNSEngineSwitchModeSwitch,
		SourceEngine: transport.DNSEnginePowerDNS, TargetEngine: transport.DNSEngineBIND,
		Topology:          transport.DNSTopologyStandalone,
		StateBefore:       dnsengineartifact.FileSnapshot{Exists: true},
		SourceUnitsBefore: []dnsengineartifact.UnitSnapshot{{Name: "pdns.service", LoadState: "loaded", ActiveState: "active", UnitFileState: "enabled"}},
		TargetUnitsBefore: []dnsengineartifact.UnitSnapshot{
			{Name: "bind9.service", LoadState: "not-found", ActiveState: "inactive"},
			{Name: "named.service", LoadState: "not-found", ActiveState: "inactive"},
		},
	}
}

// The before-decision classification is the recover-dns-bind-switch journal
// shape at a forward phase; it never overlaps the admitted rollback phases.
func TestBINDSwitchBeforeRollbackDecisionJournalIsPhaseBounded(t *testing.T) {
	for _, tc := range []struct {
		phase                  string
		before, neverStarted   bool
		admittedInverseJournal bool
	}{
		{dnsengineartifact.SwitchPhaseIntent, true, true, false},
		{dnsengineartifact.SwitchPhaseTargetStaged, true, true, false},
		{dnsengineartifact.SwitchPhaseSourceStopped, true, true, false},
		// target-started records that BIND was started: never the pre-start class.
		{dnsengineartifact.SwitchPhaseTargetStarted, true, false, false},
		{dnsengineartifact.SwitchPhaseTargetVerified, false, false, false},
		{dnsengineartifact.SwitchPhaseCommitted, false, false, false},
		{dnsengineartifact.SwitchPhaseRollingBack, false, false, true},
		{dnsengineartifact.SwitchPhaseRolledBack, false, false, true},
	} {
		j := beforeDecisionBINDSwitchJournal(tc.phase)
		if got := BINDSwitchBeforeRollbackDecisionJournal(j); got != tc.before {
			t.Fatalf("%s: before decision = %v", tc.phase, got)
		}
		if got := BINDSwitchNeverStartedBeforeDecisionJournal(j); got != tc.neverStarted {
			t.Fatalf("%s: never-started before decision = %v", tc.phase, got)
		}
		if got := InactiveBINDSwitchInverseJournal(j) == nil; got != tc.admittedInverseJournal {
			t.Fatalf("%s: inverse journal admitted = %v", tc.phase, got)
		}
		if got := BINDSwitchNeverStartedTargetJournal(j); got != tc.admittedInverseJournal {
			t.Fatalf("%s: rollback never-started class = %v", tc.phase, got)
		}
	}
	// A BIND that existed before the switch is outside the never-started class.
	j := beforeDecisionBINDSwitchJournal(dnsengineartifact.SwitchPhaseTargetStaged)
	j.TargetUnitsBefore[1] = dnsengineartifact.UnitSnapshot{Name: "named.service", LoadState: "loaded", ActiveState: "inactive", UnitFileState: "disabled"}
	if !BINDSwitchBeforeRollbackDecisionJournal(j) || BINDSwitchNeverStartedBeforeDecisionJournal(j) {
		t.Fatal("preexisting BIND target entered the never-started class")
	}
	// Other shapes (paired, BIND source, missing inverse plan) are excluded.
	for _, change := range []func(*dnsengineartifact.SwitchJournalV1){
		func(j *dnsengineartifact.SwitchJournalV1) { j.Topology = transport.DNSTopologyPaired },
		func(j *dnsengineartifact.SwitchJournalV1) { j.SourceEngine = transport.DNSEngineBIND },
		func(j *dnsengineartifact.SwitchJournalV1) { j.InversePlan = nil },
		func(j *dnsengineartifact.SwitchJournalV1) { j.Schema = dnsengineartifact.SwitchJournalSchemaV1 },
	} {
		j := beforeDecisionBINDSwitchJournal(dnsengineartifact.SwitchPhaseIntent)
		change(&j)
		if BINDSwitchBeforeRollbackDecisionJournal(j) || BINDSwitchNeverStartedBeforeDecisionJournal(j) {
			t.Fatalf("foreign journal shape classified as before decision: %+v", j)
		}
	}
}
