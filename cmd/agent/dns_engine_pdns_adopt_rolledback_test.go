package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/dnsenginerecovery"
	"github.com/alicelik/celikpanel/internal/transport"
)

func rolledBackAdoptionTestJournal(schema string) dnsEngineSwitchJournal {
	return dnsEngineSwitchJournal{
		Schema: schema, Mode: transport.DNSEngineSwitchModeAdopt,
		Phase:             dnsSwitchPhaseRolledBack,
		MutationRequestID: strings.Repeat("c", 32),
		MutationOwnerID:   strings.Repeat("d", 32),
		TargetEngine:      transport.DNSEnginePowerDNS,
	}
}

// Item 3: a V1 PowerDNS adoption journal already at rolled-back is re-proved
// by the restarted Agent with the rolling-back checks and without any effect.
// V2 stays with the owner recovery command.
func TestPDNSAdoptionRollbackStageAtRolledBackIsReproofOnly(t *testing.T) {
	rolling := rolledBackAdoptionTestJournal(dnsengineartifact.SwitchJournalSchemaV1)
	rolling.Phase = dnsSwitchPhaseRollingBack
	if stage, restore, err := pdnsAdoptionRollbackStage(rolling); err != nil ||
		stage != pdnsAdoptionEvidenceRollback || !restore {
		t.Fatalf("rolling-back stage=%v restore=%v err=%v", stage, restore, err)
	}
	rolled := rolledBackAdoptionTestJournal(dnsengineartifact.SwitchJournalSchemaV1)
	if stage, restore, err := pdnsAdoptionRollbackStage(rolled); err != nil ||
		stage != pdnsAdoptionEvidenceRolledBack || restore {
		t.Fatalf("rolled-back V1 stage=%v restore=%v err=%v", stage, restore, err)
	}
	v2 := rolledBackAdoptionTestJournal(dnsengineartifact.SwitchJournalSchemaV2)
	if _, _, err := pdnsAdoptionRollbackStage(v2); err == nil ||
		!strings.Contains(err.Error(), "only the owner recovery command") {
		t.Fatalf("rolled-back V2 adoption reached the Agent's re-proof: %v", err)
	}
}

// The Agent's rollback sequence at rolled-back: configs are proved and the
// restored source is verified with the rolled-back stage, and the state
// receipt is never written. At rolling-back the receipt is restored first.
func TestPDNSAdoptionRolledBackReproofWritesNothing(t *testing.T) {
	for _, tc := range []struct {
		phase     string
		verifyErr error
		want      string
	}{
		{dnsSwitchPhaseRolledBack, nil, "prove,verify-rolled-back"},
		{dnsSwitchPhaseRolledBack, errors.New("owner PowerDNS changed"), "prove,verify-rolled-back"},
		{dnsSwitchPhaseRollingBack, nil, "prove,write-state,verify-rolling-back"},
	} {
		journal := rolledBackAdoptionTestJournal(dnsengineartifact.SwitchJournalSchemaV1)
		journal.Phase = tc.phase
		var steps []string
		ops, err := pdnsAdoptionRollbackOps(journal,
			func(context.Context) error { steps = append(steps, "prove"); return nil },
			func() error { steps = append(steps, "write-state"); return nil },
			func(_ context.Context, stage pdnsAdoptionEvidenceStage) error {
				steps = append(steps, "verify-"+map[pdnsAdoptionEvidenceStage]string{
					pdnsAdoptionEvidenceRollback: "rolling-back", pdnsAdoptionEvidenceRolledBack: "rolled-back",
				}[stage])
				return tc.verifyErr
			},
		)
		if err != nil {
			t.Fatal(err)
		}
		err = dnsenginerecovery.RollbackPDNSAdoption(context.Background(), ops)
		if got := strings.Join(steps, ","); got != tc.want {
			t.Fatalf("%s: steps=%s, want %s", tc.phase, got, tc.want)
		}
		if (err == nil) != (tc.verifyErr == nil) {
			t.Fatalf("%s: verification result lost: %v", tc.phase, err)
		}
	}
	if _, err := pdnsAdoptionRollbackOps(rolledBackAdoptionTestJournal(dnsengineartifact.SwitchJournalSchemaV2),
		func(context.Context) error { return nil }, func() error { return nil },
		func(context.Context, pdnsAdoptionEvidenceStage) error { return nil },
	); err == nil {
		t.Fatal("V2 rolled-back adoption built Agent rollback operations")
	}
}

func TestPDNSAdoptionRolledBackBindingIsExactAndV1Only(t *testing.T) {
	journal := rolledBackAdoptionTestJournal(dnsengineartifact.SwitchJournalSchemaV1)
	exactState := dnsEngineStateReceipt{
		Schema: dnsEngineStateSchema, Mode: journal.Mode,
		Engine: journal.TargetEngine, EngineEpoch: 1,
		MutationRequestID: journal.MutationRequestID, MutationOwnerID: journal.MutationOwnerID,
	}
	if err := validatePDNSAdoptionTransactionBinding(
		journal, journal, true, dnsEngineStateReceipt{}, false, pdnsAdoptionEvidenceRolledBack,
	); err != nil {
		t.Fatalf("exact rolled-back V1 adoption rejected: %v", err)
	}
	for _, tc := range []struct {
		name          string
		expected      dnsEngineSwitchJournal
		actual        dnsEngineSwitchJournal
		journalExists bool
		stateExists   bool
		stage         pdnsAdoptionEvidenceStage
	}{
		{"target receipt still present", journal, journal, true, true, pdnsAdoptionEvidenceRolledBack},
		{"journal retired", journal, dnsEngineSwitchJournal{}, false, false, pdnsAdoptionEvidenceRolledBack},
		{"different journal", journal, func() dnsEngineSwitchJournal {
			other := journal
			other.MutationOwnerID = strings.Repeat("e", 32)
			return other
		}(), true, false, pdnsAdoptionEvidenceRolledBack},
		{"V2 journal", func() dnsEngineSwitchJournal {
			v2 := journal
			v2.Schema = dnsengineartifact.SwitchJournalSchemaV2
			return v2
		}(), func() dnsEngineSwitchJournal {
			v2 := journal
			v2.Schema = dnsengineartifact.SwitchJournalSchemaV2
			return v2
		}(), true, false, pdnsAdoptionEvidenceRolledBack},
		{"rolling-back under rolled-back stage", func() dnsEngineSwitchJournal {
			rolling := journal
			rolling.Phase = dnsSwitchPhaseRollingBack
			return rolling
		}(), func() dnsEngineSwitchJournal {
			rolling := journal
			rolling.Phase = dnsSwitchPhaseRollingBack
			return rolling
		}(), true, false, pdnsAdoptionEvidenceRolledBack},
		// The rolling-back stage itself is unchanged: it still refuses a
		// rolled-back journal.
		{"rolled-back under rolling-back stage", journal, journal, true, false, pdnsAdoptionEvidenceRollback},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := dnsEngineStateReceipt{}
			if tc.stateExists {
				state = exactState
			}
			if err := validatePDNSAdoptionTransactionBinding(
				tc.expected, tc.actual, tc.journalExists, state, tc.stateExists, tc.stage,
			); err == nil {
				t.Fatal("inexact rolled-back adoption binding was accepted")
			}
		})
	}
}
