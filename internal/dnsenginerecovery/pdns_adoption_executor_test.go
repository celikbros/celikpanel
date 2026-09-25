package dnsenginerecovery

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/transport"
)

type adoptionInverseFixture struct {
	journal      dnsengineartifact.SwitchJournalV1
	statePresent bool
	terminal     bool
	removed      bool
	failAt       string
	calls        []string
}

func newAdoptionInverseFixture() *adoptionInverseFixture {
	return &adoptionInverseFixture{
		journal: dnsengineartifact.SwitchJournalV1{
			Phase:             dnsengineartifact.SwitchPhaseRollingBack,
			Mode:              transport.DNSEngineSwitchModeAdopt,
			MutationRequestID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			MutationOwnerID:   "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			TargetEngine:      transport.DNSEnginePowerDNS,
		},
		statePresent: true,
	}
}

func (f *adoptionInverseFixture) ops() PDNSAdoptionInverseOps {
	call := func(name string) error {
		f.calls = append(f.calls, name)
		if f.failAt == name {
			return errors.New("injected interruption")
		}
		return nil
	}
	return PDNSAdoptionInverseOps{
		Read: func(context.Context) (SwitchEvidence, bool, error) {
			if f.removed {
				return SwitchEvidence{}, false, nil
			}
			if err := call("read"); err != nil {
				return SwitchEvidence{}, false, err
			}
			observed := EvidenceObservation{
				EvidenceSHA256:  "exact-secured-bytes",
				RequestID:       f.journal.MutationRequestID,
				SourceEngine:    string(f.journal.SourceEngine),
				TargetEngine:    string(f.journal.TargetEngine),
				TargetEpoch:     f.journal.TargetEpoch,
				Phase:           f.journal.Phase,
				InverseKind:     NativeInversePDNSAdoption,
				SourceOwnership: SourceOwnershipNotApplicable,
				Status:          EvidenceActive,
				TargetReceipt:   TargetReceiptAbsent,
				SourceReceipt:   SourceReceiptMutualAbsence,
			}
			if f.statePresent {
				observed.TargetReceipt = TargetReceiptExact
				observed.SourceReceipt = SourceReceiptDifferent
			}
			if f.terminal {
				observed.Status = EvidenceTerminalRolledBack
			}
			return SwitchEvidence{Journal: f.journal, Observation: observed}, true, nil
		},
		ExcludeWorker: func(context.Context, SwitchEvidence) error { return call("worker") },
		ProveNative:   func(context.Context, dnsengineartifact.SwitchJournalV1) error { return call("native") },
		RemoveState: func(context.Context, dnsengineartifact.SwitchJournalV1) error {
			if err := call("state"); err != nil {
				return err
			}
			f.statePresent = false
			return nil
		},
		WritePhase: func(_ context.Context, before, after dnsengineartifact.SwitchJournalV1) error {
			if err := call("phase"); err != nil {
				return err
			}
			if !reflect.DeepEqual(f.journal, before) || after.Phase != dnsengineartifact.SwitchPhaseRolledBack {
				return errors.New("wrong journal checkpoint")
			}
			f.journal = after
			return nil
		},
		PublishFailed: func(context.Context, dnsengineartifact.SwitchJournalV1) error {
			if err := call("ledger"); err != nil {
				return err
			}
			f.terminal = true
			return nil
		},
		RemoveJournal: func(context.Context, dnsengineartifact.SwitchJournalV1) error {
			if err := call("cleanup"); err != nil {
				return err
			}
			f.removed = true
			return nil
		},
	}
}

func TestCompletePDNSAdoptionInverseOrdersDurableEffects(t *testing.T) {
	fixture := newAdoptionInverseFixture()
	if err := CompletePDNSAdoptionInverse(context.Background(), fixture.ops()); err != nil {
		t.Fatal(err)
	}
	if !fixture.removed || fixture.statePresent || !fixture.terminal ||
		fixture.journal.Phase != dnsengineartifact.SwitchPhaseRolledBack {
		t.Fatal("adoption inverse did not reach the terminal state")
	}
	var state, phase, ledger, cleanup int
	for index, call := range fixture.calls {
		switch call {
		case "state":
			state = index
		case "phase":
			phase = index
		case "ledger":
			ledger = index
		case "cleanup":
			cleanup = index
		}
	}
	if !(state > 0 && state < phase && phase < ledger && ledger < cleanup) {
		t.Fatalf("unsafe inverse effect ordering: %v", fixture.calls)
	}
}

func TestCompletePDNSAdoptionInverseRetriesSameCheckpoint(t *testing.T) {
	for _, failing := range []string{"state", "phase", "ledger", "cleanup"} {
		t.Run(failing, func(t *testing.T) {
			fixture := newAdoptionInverseFixture()
			fixture.failAt = failing
			if err := CompletePDNSAdoptionInverse(context.Background(), fixture.ops()); err == nil {
				t.Fatal("injected interruption was ignored")
			}
			if fixture.removed {
				t.Fatal("journal retired after a failed effect")
			}
			fixture.failAt = ""
			fixture.calls = nil
			if err := CompletePDNSAdoptionInverse(context.Background(), fixture.ops()); err != nil {
				t.Fatal(err)
			}
			if !fixture.removed || !fixture.terminal {
				t.Fatal("same operation did not converge after retry")
			}
			if failing == "ledger" || failing == "cleanup" {
				for _, call := range fixture.calls {
					if call == "state" || call == "phase" {
						t.Fatalf("durable inverse effect repeated after %s: %v", failing, fixture.calls)
					}
				}
			}
		})
	}
}

func TestCompletePDNSAdoptionInverseRejectsUnsupportedOrChangedEvidence(t *testing.T) {
	for _, change := range []struct {
		name string
		edit func(*adoptionInverseFixture)
	}{
		{"not-rolling-back", func(f *adoptionInverseFixture) { f.journal.Phase = dnsengineartifact.SwitchPhaseIntent }},
		{"terminal-before-checkpoint", func(f *adoptionInverseFixture) {
			f.statePresent = false
			f.terminal = true
			f.journal.Phase = dnsengineartifact.SwitchPhaseRollingBack
		}},
		{"terminal-with-target-receipt", func(f *adoptionInverseFixture) {
			f.terminal = true
			f.journal.Phase = dnsengineartifact.SwitchPhaseRolledBack
		}},
	} {
		t.Run(change.name, func(t *testing.T) {
			fixture := newAdoptionInverseFixture()
			change.edit(fixture)
			if err := CompletePDNSAdoptionInverse(context.Background(), fixture.ops()); err == nil {
				t.Fatal("unsupported inverse evidence was accepted")
			}
			for _, call := range fixture.calls {
				if call == "state" || call == "phase" || call == "ledger" || call == "cleanup" {
					t.Fatalf("unsupported evidence reached a host effect: %v", fixture.calls)
				}
			}
		})
	}
}
