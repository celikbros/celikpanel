//go:build linux

package dnsenginerecovery

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
)

type bindSwitchInverseFixture struct {
	evidence    SwitchEvidence
	native      BINDSwitchNativeState
	terminal    bool
	retired     bool
	failAt      string
	ownerEditAt string
	cancelAt    string
	cancel      context.CancelFunc
	calls       []string
}

func newBINDSwitchInverseFixture() *bindSwitchInverseFixture {
	return &bindSwitchInverseFixture{evidence: inactiveBINDSwitchEvidence(), native: BINDSwitchNativeNeedsRestore}
}

func (f *bindSwitchInverseFixture) ops() BINDSwitchInverseOps {
	call := func(name string) error {
		f.calls = append(f.calls, name)
		if f.ownerEditAt == name {
			f.evidence.Observation.SourceOwnership = SourceOwnershipDifferent
		}
		if f.cancelAt == name && f.cancel != nil {
			f.cancel()
		}
		if f.failAt == name {
			return errors.New("injected interruption")
		}
		return nil
	}
	return BINDSwitchInverseOps{
		Read: func(context.Context) (SwitchEvidence, bool, error) {
			if f.retired {
				return SwitchEvidence{}, false, nil
			}
			if err := call("read"); err != nil {
				return SwitchEvidence{}, false, err
			}
			return f.evidence, true, nil
		},
		ExcludeWorker: func(context.Context, SwitchEvidence) error { return call("worker") },
		AssessNative: func(context.Context, dnsengineartifact.SwitchJournalV1) (BINDSwitchNativeState, error) {
			if err := call("native-proof"); err != nil {
				return BINDSwitchNativeUnknown, err
			}
			if f.evidence.Observation.SourceOwnership != SourceOwnershipExact {
				return BINDSwitchNativeUnknown, errors.New("owner changed the source")
			}
			return f.native, nil
		},
		RestoreNative: func(context.Context, dnsengineartifact.SwitchJournalV1) error {
			if err := call("native-restore"); err != nil {
				return err
			}
			f.native = BINDSwitchNativeRestored
			f.evidence.Observation.SourceReceipt = SourceReceiptExact
			f.evidence.Observation.TargetReceipt = TargetReceiptDifferent
			return nil
		},
		WritePhase: func(_ context.Context, before, after dnsengineartifact.SwitchJournalV1) error {
			if err := call("phase"); err != nil {
				return err
			}
			if !reflect.DeepEqual(f.evidence.Journal, before) || after.Phase != dnsengineartifact.SwitchPhaseRolledBack {
				return errors.New("wrong BIND rollback checkpoint")
			}
			f.evidence.Journal = after
			f.evidence.Observation.Phase = after.Phase
			return nil
		},
		PublishFailed: func(context.Context, dnsengineartifact.SwitchJournalV1) error {
			if err := call("verdict"); err != nil {
				return err
			}
			f.terminal = true
			f.evidence.Observation.Status = EvidenceTerminalRolledBack
			return nil
		},
		RemoveJournal: func(context.Context, dnsengineartifact.SwitchJournalV1) error {
			if err := call("retire"); err != nil {
				return err
			}
			f.retired = true
			return nil
		},
	}
}

func TestInactiveBINDSwitchInverseOrdersNativeAndDurableEffects(t *testing.T) {
	f := newBINDSwitchInverseFixture()
	if err := CompleteInactiveBINDSwitchInverse(context.Background(), f.ops()); err != nil {
		t.Fatal(err)
	}
	if !f.retired || !f.terminal || f.native != BINDSwitchNativeRestored {
		t.Fatal("BIND inverse did not reach its exact terminal checkpoint")
	}
	indexes := map[string]int{}
	for i, name := range f.calls {
		if _, ok := indexes[name]; !ok {
			indexes[name] = i
		}
	}
	if !(indexes["native-restore"] < indexes["phase"] &&
		indexes["phase"] < indexes["verdict"] && indexes["verdict"] < indexes["retire"]) {
		t.Fatalf("unsafe BIND inverse effect ordering: %v", f.calls)
	}
}

func TestInactiveBINDSwitchInverseSameRequestResumesEachDurableCheckpoint(t *testing.T) {
	for _, failing := range []string{"native-restore", "phase", "verdict", "retire"} {
		t.Run(failing, func(t *testing.T) {
			f := newBINDSwitchInverseFixture()
			f.failAt = failing
			if err := CompleteInactiveBINDSwitchInverse(context.Background(), f.ops()); err == nil || f.retired {
				t.Fatal("failed BIND effect retired journal or lost error")
			}
			f.failAt, f.calls = "", nil
			if err := CompleteInactiveBINDSwitchInverse(context.Background(), f.ops()); err != nil {
				t.Fatal(err)
			}
			if !f.retired || !f.terminal {
				t.Fatal("same BIND request did not converge")
			}
			if failing == "verdict" || failing == "retire" {
				for _, name := range f.calls {
					if name == "native-restore" || name == "phase" {
						t.Fatalf("earlier durable effect repeated after %s: %v", failing, f.calls)
					}
				}
			}
		})
	}
}

func TestInactiveBINDSwitchInverseRejectsUnknownAndOwnerEditBeforeEffects(t *testing.T) {
	for _, editAt := range []string{"native-proof", "worker"} {
		t.Run(editAt, func(t *testing.T) {
			f := newBINDSwitchInverseFixture()
			f.ownerEditAt = editAt
			if err := CompleteInactiveBINDSwitchInverse(context.Background(), f.ops()); err == nil {
				t.Fatal("owner edit accepted")
			}
			for _, call := range f.calls {
				if call == "native-restore" || call == "phase" || call == "verdict" || call == "retire" {
					t.Fatalf("owner edit reached effect: %v", f.calls)
				}
			}
		})
	}
	f := newBINDSwitchInverseFixture()
	f.native = BINDSwitchNativeUnknown
	if err := CompleteInactiveBINDSwitchInverse(context.Background(), f.ops()); err == nil || f.retired {
		t.Fatal("unknown native state was accepted")
	}
}

func TestInactiveBINDSwitchInverseKeepsTerminalJournalAfterLateOwnerEdit(t *testing.T) {
	f := newBINDSwitchInverseFixture()
	f.ownerEditAt = "verdict"
	if err := CompleteInactiveBINDSwitchInverse(context.Background(), f.ops()); err == nil {
		t.Fatal("late owner edit accepted")
	}
	if !f.terminal || f.retired {
		t.Fatal("terminal verdict lost or journal retired after owner edit")
	}
}

func TestInactiveBINDSwitchInverseCancellationKeepsRetryableCheckpoint(t *testing.T) {
	for _, cut := range []string{"native-restore", "phase", "verdict"} {
		t.Run(cut, func(t *testing.T) {
			f := newBINDSwitchInverseFixture()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			f.cancelAt, f.cancel = cut, cancel
			if err := CompleteInactiveBINDSwitchInverse(ctx, f.ops()); !errors.Is(err, context.Canceled) || f.retired {
				t.Fatalf("cancellation at %s did not retain journal: %v", cut, err)
			}
			f.cancelAt, f.cancel = "", nil
			if err := CompleteInactiveBINDSwitchInverse(context.Background(), f.ops()); err != nil {
				t.Fatal(err)
			}
			if !f.retired {
				t.Fatal("same request did not resume")
			}
		})
	}
}
