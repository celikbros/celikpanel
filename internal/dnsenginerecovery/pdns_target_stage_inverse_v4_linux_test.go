//go:build linux

package dnsenginerecovery

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/transport"
)

type pdnsStageFixtureV4 struct {
	e       SwitchEvidence
	state   PDNSTargetStageState
	fail    string
	retired bool
	calls   []string
}

func newPDNSStageFixtureV4() *pdnsStageFixtureV4 {
	j := dnsengineartifact.SwitchJournalV1{
		Schema: dnsengineartifact.SwitchJournalSchemaV4,
		Mode:   transport.DNSEngineSwitchModeSwitch, SourceEngine: transport.DNSEngineBIND,
		TargetEngine: transport.DNSEnginePowerDNS, Topology: transport.DNSTopologyStandalone,
		Phase:             dnsengineartifact.SwitchPhaseRollingBack,
		MutationRequestID: strings.Repeat("a", 32), TargetGeneration: strings.Repeat("b", 64), TargetEpoch: 2,
		StateBefore:       dnsengineartifact.FileSnapshot{Exists: true},
		SourceUnitsBefore: []dnsengineartifact.UnitSnapshot{{Name: "bind9.service", ActiveState: "active"}, {Name: "named.service", ActiveState: "active"}},
		TargetUnitsBefore: []dnsengineartifact.UnitSnapshot{{Name: "pdns.service", ActiveState: "inactive"}},
		PDNSTargetPlan:    &dnsengineartifact.PDNSTargetInversePlanV4{Kind: dnsengineartifact.PDNSTargetInversePlanKindV4, Candidate: &dnsengineartifact.PDNSTargetCandidateProofV4{}},
	}
	o := EvidenceObservation{
		EvidenceSHA256: strings.Repeat("c", 64), Status: EvidenceActive, RequestID: j.MutationRequestID,
		Phase: j.Phase, SourceEngine: string(j.SourceEngine), TargetEngine: string(j.TargetEngine),
		TargetGeneration: j.TargetGeneration, TargetEpoch: j.TargetEpoch, InverseKind: NativeInversePDNSSwitch,
		SourceOwnership: SourceOwnershipExact, SourceReceipt: SourceReceiptExact, TargetReceipt: TargetReceiptDifferent,
	}
	return &pdnsStageFixtureV4{e: SwitchEvidence{Journal: j, Observation: o}, state: PDNSTargetStageNeedsRestore}
}

func (f *pdnsStageFixtureV4) ops() PDNSTargetStageInverseOps {
	call := func(name string) error {
		f.calls = append(f.calls, name)
		if f.fail == name {
			return errors.New("interrupted")
		}
		return nil
	}
	return PDNSTargetStageInverseOps{
		Read: func(context.Context) (SwitchEvidence, bool, error) {
			if f.retired {
				return SwitchEvidence{}, false, nil
			}
			return f.e, true, nil
		},
		ExcludeWorker: func(context.Context, SwitchEvidence) error { return call("worker") },
		AssessNative: func(context.Context, dnsengineartifact.SwitchJournalV1) (PDNSTargetStageState, error) {
			if err := call("native"); err != nil {
				return PDNSTargetStageUnknown, err
			}
			return f.state, nil
		},
		RestoreNative: func(context.Context, dnsengineartifact.SwitchJournalV1) error {
			if err := call("restore"); err != nil {
				return err
			}
			f.state = PDNSTargetStageRestored
			return nil
		},
		WritePhase: func(_ context.Context, before, after dnsengineartifact.SwitchJournalV1) error {
			if err := call("phase"); err != nil {
				return err
			}
			if !reflect.DeepEqual(f.e.Journal, before) || after.Phase != dnsengineartifact.SwitchPhaseRolledBack {
				return errors.New("wrong phase")
			}
			f.e.Journal = after
			f.e.Observation.Phase = after.Phase
			return nil
		},
		PublishFailed: func(context.Context, dnsengineartifact.SwitchJournalV1) error {
			if err := call("verdict"); err != nil {
				return err
			}
			f.e.Observation.Status = EvidenceTerminalRolledBack
			return nil
		},
		RemoveJournal: func(context.Context, dnsengineartifact.SwitchJournalV1) error {
			if err := call("retire"); err != nil {
				return err
			}
			f.retired = true
			return nil
		},
		VerifyTerminalWithoutJournal: func(_ context.Context, expected dnsengineartifact.SwitchJournalV1) error {
			if err := call("terminal-without-journal"); err != nil {
				return err
			}
			if !f.retired || f.e.Observation.Status != EvidenceTerminalRolledBack ||
				expected.MutationRequestID != f.e.Journal.MutationRequestID ||
				expected.MutationOwnerID != f.e.Journal.MutationOwnerID ||
				expected.ManifestQualifier != f.e.Journal.ManifestQualifier {
				return errors.New("same-request terminal ledger or journal absence unproved")
			}
			return nil
		},
	}
}

func TestStagedPDNSTargetV4OrdersNativeCheckpointVerdictAndRetirement(t *testing.T) {
	f := newPDNSStageFixtureV4()
	if err := CompleteStagedPDNSTargetInverseV4(context.Background(), f.ops()); err != nil {
		t.Fatal(err)
	}
	if !f.retired || f.state != PDNSTargetStageRestored {
		t.Fatal("rollback did not complete")
	}
	order := map[string]int{}
	for i, s := range f.calls {
		if _, ok := order[s]; !ok {
			order[s] = i
		}
	}
	if !(order["restore"] < order["phase"] && order["phase"] < order["verdict"] && order["verdict"] < order["retire"]) {
		t.Fatalf("unsafe order: %v", f.calls)
	}
}

func TestStagedPDNSTargetV4SameRequestResumesDurableCuts(t *testing.T) {
	for _, cut := range []string{"restore", "phase", "verdict", "retire"} {
		t.Run(cut, func(t *testing.T) {
			f := newPDNSStageFixtureV4()
			f.fail = cut
			if err := CompleteStagedPDNSTargetInverseV4(context.Background(), f.ops()); err == nil || f.retired {
				t.Fatalf("failed cut lost journal: %v", err)
			}
			f.fail = ""
			f.calls = nil
			if err := CompleteStagedPDNSTargetInverseV4(context.Background(), f.ops()); err != nil {
				t.Fatal(err)
			}
			if !f.retired {
				t.Fatal("same request did not converge")
			}
			if cut == "verdict" || cut == "retire" {
				for _, c := range f.calls {
					if c == "restore" || c == "phase" {
						t.Fatalf("replayed earlier effect after %s: %v", cut, f.calls)
					}
				}
			}
		})
	}
}

func TestStagedPDNSTargetV4RefusesUnknownIntentAndChangedEvidence(t *testing.T) {
	f := newPDNSStageFixtureV4()
	f.state = PDNSTargetStageUnknown
	if err := CompleteStagedPDNSTargetInverseV4(context.Background(), f.ops()); err == nil || f.retired {
		t.Fatal("unknown native state accepted")
	}
	f = newPDNSStageFixtureV4()
	f.e.Journal.PDNSTargetPlan.Candidate = nil
	if err := CompleteStagedPDNSTargetInverseV4(context.Background(), f.ops()); err == nil || len(f.calls) != 0 {
		t.Fatal("candidate-less intent reached native effect")
	}
	f = newPDNSStageFixtureV4()
	f.e.Observation.SourceOwnership = SourceOwnershipDifferent
	if err := CompleteStagedPDNSTargetInverseV4(context.Background(), f.ops()); err == nil || len(f.calls) != 0 {
		t.Fatal("changed BIND ownership reached native effect")
	}
}
func TestStagedPDNSTargetV4KeepsTerminalJournalAfterLateNativeDrift(t *testing.T) {
	f := newPDNSStageFixtureV4()
	ops := f.ops()
	previous := ops.PublishFailed
	ops.PublishFailed = func(ctx context.Context, journal dnsengineartifact.SwitchJournalV1) error {
		if err := previous(ctx, journal); err != nil {
			return err
		}
		f.state = PDNSTargetStageUnknown
		return nil
	}
	if err := CompleteStagedPDNSTargetInverseV4(context.Background(), ops); err == nil {
		t.Fatal("late native drift accepted")
	}
	if f.retired || f.e.Observation.Status != EvidenceTerminalRolledBack {
		t.Fatal("late drift retired journal or lost durable verdict")
	}
}
func TestStagedPDNSTargetV4RejectsNoopJournalRemoval(t *testing.T) {
	f := newPDNSStageFixtureV4()
	ops := f.ops()
	ops.RemoveJournal = func(context.Context, dnsengineartifact.SwitchJournalV1) error {
		f.calls = append(f.calls, "noop-retire")
		return nil
	}
	if err := CompleteStagedPDNSTargetInverseV4(context.Background(), ops); err == nil {
		t.Fatal("no-op journal removal was reported as success")
	}
	if f.retired || f.e.Observation.Status != EvidenceTerminalRolledBack {
		t.Fatal("no-op removal lost terminal verdict or retired the evidence")
	}
	for _, call := range f.calls {
		if call == "terminal-without-journal" {
			t.Fatal("ledger verifier ran while journal was still present")
		}
	}
}

func TestStagedPDNSTargetV4RequiresTerminalLedgerProofAfterRemoval(t *testing.T) {
	f := newPDNSStageFixtureV4()
	ops := f.ops()
	ops.VerifyTerminalWithoutJournal = func(context.Context, dnsengineartifact.SwitchJournalV1) error {
		return errors.New("terminal ledger unverified")
	}
	if err := CompleteStagedPDNSTargetInverseV4(context.Background(), ops); err == nil {
		t.Fatal("unverified terminal ledger was reported as success")
	}
	if !f.retired || f.e.Observation.Status != EvidenceTerminalRolledBack {
		t.Fatal("late proof failure did not retain the durable verdict")
	}
}
func TestExactDNSRollbackTerminalWithoutJournalRequiresAbsenceAndSameLedger(t *testing.T) {
	policy, owner, journal, now, journalPath, ledgerPath := rollbackVerdictFixture(t)
	if err := PublishExactDNSRollbackVerdict(policy, owner, journal, now); err != nil {
		t.Fatal(err)
	}
	if err := VerifyExactDNSRollbackTerminalWithoutJournal(policy, owner, journal); err == nil {
		t.Fatal("present journal accepted after terminal verdict")
	}
	if err := os.Remove(journalPath); err != nil {
		t.Fatal(err)
	}
	if err := VerifyExactDNSRollbackTerminalWithoutJournal(policy, owner, journal); err != nil {
		t.Fatal(err)
	}
	changed := journal
	changed.MutationRequestID = strings.Repeat("d", 32)
	if err := VerifyExactDNSRollbackTerminalWithoutJournal(policy, owner, changed); err == nil {
		t.Fatal("different request accepted")
	}
	if err := os.Remove(ledgerPath); err != nil {
		t.Fatal(err)
	}
	if err := VerifyExactDNSRollbackTerminalWithoutJournal(policy, owner, journal); err == nil {
		t.Fatal("missing terminal ledger accepted")
	}
}

func TestRenamedPDNSTargetV4UsesSameDurableInverse(t *testing.T) {
	f := newPDNSStageFixtureV4()
	f.state = PDNSTargetStageRenamed
	if err := CompleteStagedPDNSTargetInverseV4(context.Background(), f.ops()); err != nil || !f.retired || f.state != PDNSTargetStageRestored {
		t.Fatalf("exact renamed target did not reach terminal rollback: %v", err)
	}
	f = newPDNSStageFixtureV4()
	f.state = PDNSTargetStageRenamed
	f.fail = "restore"
	if err := CompleteStagedPDNSTargetInverseV4(context.Background(), f.ops()); err == nil || f.retired {
		t.Fatalf("interrupted rename inverse retired evidence: %v", err)
	}
	f.fail = ""
	if err := CompleteStagedPDNSTargetInverseV4(context.Background(), f.ops()); err != nil || !f.retired {
		t.Fatalf("same renamed request did not converge: %v", err)
	}
}

func TestEnabledPDNSTargetV4CompletesOnlyFromSpecialRollbackPhase(t *testing.T) {
	f := newPDNSStageFixtureV4()
	f.state = PDNSTargetStageRenamedEnabled
	f.e.Journal.Phase = dnsengineartifact.SwitchPhaseRollingBackTargetEnable
	f.e.Observation.Phase = f.e.Journal.Phase
	if err := CompleteStagedPDNSTargetInverseV4(context.Background(), f.ops()); err != nil || !f.retired {
		t.Fatalf("enable-intent inverse did not reach terminal verdict: %v", err)
	}
	f = newPDNSStageFixtureV4()
	f.state = PDNSTargetStageRenamedEnabled
	if err := CompleteStagedPDNSTargetInverseV4(context.Background(), f.ops()); err == nil {
		t.Fatal("enabled target without special rollback phase reached native inverse")
	}
}
