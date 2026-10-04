//go:build linux

package dnsenginerecovery

import (
	"context"
	"errors"
	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"reflect"
	"testing"
)

func runningBINDInverseEvidence(t *testing.T) SwitchEvidence {
	_, j := runningBINDJournalFixture(t)
	j.Phase = dnsengineartifact.SwitchPhaseRollingBack
	return SwitchEvidence{Journal: j, Observation: EvidenceObservation{
		EvidenceSHA256: "secured-fingerprint", Status: EvidenceActive, RequestID: j.MutationRequestID,
		Phase: j.Phase, TargetEngine: "bind", TargetGeneration: j.TargetGeneration, TargetEpoch: j.TargetEpoch,
		InverseKind: NativeInverseBINDRunningAdoption, SourceOwnership: SourceOwnershipNotApplicable,
		TargetReceipt: TargetReceiptExact, SourceReceipt: SourceReceiptDifferent,
	}}
}
func TestRunningBINDInverseAdmissionRejectsWrongOwnershipAndScope(t *testing.T) {
	e := runningBINDInverseEvidence(t)
	if err := ValidateRunningBINDAdoptionInverseEvidence(e); err != nil {
		t.Fatal(err)
	}
	edits := []func(*SwitchEvidence){
		func(e *SwitchEvidence) { e.Journal.InversePlan.SourceBIND = nil },
		func(e *SwitchEvidence) { e.Journal.StateBefore.Exists = true },
		func(e *SwitchEvidence) { e.Journal.HadPrevious = true },
		func(e *SwitchEvidence) { e.Journal.TargetUnitsBefore[0].ActiveState = "inactive" },
		func(e *SwitchEvidence) { e.Journal.TargetUnitsBefore[0].UnitFileState = "disabled" },
		func(e *SwitchEvidence) {
			e.Journal.Phase = dnsengineartifact.SwitchPhaseTargetStarted
			e.Observation.Phase = e.Journal.Phase
		},
		func(e *SwitchEvidence) { e.Observation.SourceOwnership = SourceOwnershipExact },
		func(e *SwitchEvidence) { e.Observation.TargetReceipt = TargetReceiptDifferent },
		func(e *SwitchEvidence) { e.Observation.Status = EvidenceTerminalRolledBack },
	}
	for i, edit := range edits {
		e := runningBINDInverseEvidence(t)
		edit(&e)
		if err := ValidateRunningBINDAdoptionInverseEvidence(e); err == nil {
			t.Fatalf("unsafe shape %d accepted", i)
		}
	}
}

func TestRunningBINDInverseResumesEachExactCheckpoint(t *testing.T) {
	for _, cut := range []string{"restore", "phase", "verdict", "retire"} {
		t.Run(cut, func(t *testing.T) {
			evidence := runningBINDInverseEvidence(t)
			restored, retired := false, false
			calls := []string{}
			stop := cut
			effect := func(name string) error {
				calls = append(calls, name)
				if stop == name {
					return errors.New("interrupted")
				}
				return nil
			}
			ops := BINDSwitchInverseOps{
				Read:          func(context.Context) (SwitchEvidence, bool, error) { return evidence, !retired, nil },
				ExcludeWorker: func(context.Context, SwitchEvidence) error { return nil },
				AssessNative: func(context.Context, dnsengineartifact.SwitchJournalV1) (BINDSwitchNativeState, error) {
					if restored {
						return BINDSwitchNativeRestored, nil
					}
					return BINDSwitchNativeNeedsRestore, nil
				},
				RestoreNative: func(context.Context, dnsengineartifact.SwitchJournalV1) error {
					if err := effect("restore"); err != nil {
						return err
					}
					restored = true
					evidence.Observation.SourceReceipt = SourceReceiptMutualAbsence
					evidence.Observation.TargetReceipt = TargetReceiptAbsent
					return nil
				},
				WritePhase: func(_ context.Context, before, after dnsengineartifact.SwitchJournalV1) error {
					if err := effect("phase"); err != nil {
						return err
					}
					if !reflect.DeepEqual(before, evidence.Journal) {
						t.Fatal("checkpoint overwrote changed journal")
					}
					evidence.Journal = after
					evidence.Observation.Phase = after.Phase
					return nil
				},
				PublishFailed: func(context.Context, dnsengineartifact.SwitchJournalV1) error {
					if err := effect("verdict"); err != nil {
						return err
					}
					if !restored || evidence.Journal.Phase != dnsengineartifact.SwitchPhaseRolledBack {
						t.Fatal("premature terminal verdict")
					}
					evidence.Observation.Status = EvidenceTerminalRolledBack
					return nil
				},
				RemoveJournal: func(context.Context, dnsengineartifact.SwitchJournalV1) error {
					if err := effect("retire"); err != nil {
						return err
					}
					if evidence.Observation.Status != EvidenceTerminalRolledBack {
						t.Fatal("premature journal retirement")
					}
					retired = true
					return nil
				},
			}
			if err := CompleteRunningBINDAdoptionInverse(context.Background(), ops); err == nil || retired {
				t.Fatal("interruption lost retained evidence")
			}
			stop = ""
			calls = nil
			if err := CompleteRunningBINDAdoptionInverse(context.Background(), ops); err != nil {
				t.Fatal(err)
			}
			if !retired || !restored {
				t.Fatal("same request did not finish")
			}
			if cut == "verdict" || cut == "retire" {
				for _, call := range calls {
					if call == "restore" || call == "phase" {
						t.Fatal("native effect repeated after durable checkpoint")
					}
				}
			}
		})
	}
}
func TestRunningBINDInverseNativeUnknownCannotWriteVerdict(t *testing.T) {
	evidence := runningBINDInverseEvidence(t)
	effects := 0
	ops := BINDSwitchInverseOps{
		Read:          func(context.Context) (SwitchEvidence, bool, error) { return evidence, true, nil },
		ExcludeWorker: func(context.Context, SwitchEvidence) error { return nil },
		AssessNative: func(context.Context, dnsengineartifact.SwitchJournalV1) (BINDSwitchNativeState, error) {
			return BINDSwitchNativeUnknown, errors.New("owner zone edited at same serial")
		},
		RestoreNative: func(context.Context, dnsengineartifact.SwitchJournalV1) error { effects++; return nil },
		WritePhase: func(context.Context, dnsengineartifact.SwitchJournalV1, dnsengineartifact.SwitchJournalV1) error {
			effects++
			return nil
		},
		PublishFailed: func(context.Context, dnsengineartifact.SwitchJournalV1) error { effects++; return nil },
		RemoveJournal: func(context.Context, dnsengineartifact.SwitchJournalV1) error { effects++; return nil },
	}
	if err := CompleteRunningBINDAdoptionInverse(context.Background(), ops); err == nil || effects != 0 {
		t.Fatal("unknown source reached effects")
	}
}

func TestRunningBINDInverseMissingDebianAlias(t *testing.T) {
	e := runningBINDInverseEvidence(t)
	index := -1
	for i, u := range e.Journal.TargetUnitsBefore {
		if u.Name == "bind9.service" {
			index = i
		}
	}
	if index < 0 {
		t.Fatal("missing alias fixture")
	}
	e.Journal.TargetUnitsBefore[index] = dnsengineartifact.UnitSnapshot{Name: "bind9.service", LoadState: "not-found", ActiveState: "inactive"}
	if err := ValidateRunningBINDAdoptionInverseEvidence(e); err != nil {
		t.Fatal(err)
	}
	for _, u := range []dnsengineartifact.UnitSnapshot{
		{Name: "bind9.service", LoadState: "not-found", ActiveState: "active"},
		{Name: "bind9.service", LoadState: "not-found", ActiveState: "inactive", UnitFileState: "disabled"},
		{Name: "bind9.service", LoadState: "loaded", ActiveState: "inactive", UnitFileState: "enabled"},
	} {
		e.Journal.TargetUnitsBefore[index] = u
		if err := ValidateRunningBINDAdoptionInverseEvidence(e); err == nil {
			t.Fatalf("unsafe alias accepted: %+v", u)
		}
	}
}
