package main

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/dnsenginerecovery"
	"github.com/alicelik/celikpanel/internal/transport"
)

func unrecordedBINDTargetState(journal dnsEngineSwitchJournal) dnsEngineStateReceipt {
	return dnsEngineStateReceipt{
		Schema: dnsEngineStateSchema, Mode: journal.Mode,
		Engine: transport.DNSEngineBIND, EngineEpoch: journal.TargetEpoch,
		Generation: journal.TargetGeneration, SourceRevision: journal.SourceRevision,
		ManifestQualifier: journal.ManifestQualifier,
		MutationRequestID: journal.MutationRequestID, MutationOwnerID: journal.MutationOwnerID,
	}
}

type fakeUnrecordedBINDHost struct {
	pointer    string
	pointerErr error
	anchor     bool
	calls      int
	logs       []string
}

func (host *fakeUnrecordedBINDHost) ops() func() (unrecordedBINDTargetOps, error) {
	return func() (unrecordedBINDTargetOps, error) {
		host.calls++
		return unrecordedBINDTargetOps{
			pointerPath: testBINDPointerPath,
			current: func() (string, bool, error) {
				return host.pointer, host.pointer != "", host.pointerErr
			},
			anchorIncludesPointer: func() (bool, error) { return host.anchor, nil },
			logf: func(format string, arguments ...any) {
				host.logs = append(host.logs, fmt.Sprintf(format, arguments...))
			},
		}, nil
	}
}

func withPDNSSource(journal dnsEngineSwitchJournal) dnsEngineSwitchJournal {
	journal.SourceEngine = transport.DNSEnginePowerDNS
	journal.SourceEpoch = 1
	journal.StateBefore.Exists = true
	return journal
}

// A journal before target-verified whose records name the BIND target and whose
// pointer is missing: a first install is rolled back to no DNS engine through
// the ordinary decision; anything with a source is refused with the observation
// and the owner's next step, and never rolled back.
func TestUnrecordedBINDTargetWithoutPointerAdmission(t *testing.T) {
	for _, test := range []struct {
		name        string
		mutate      func(dnsEngineSwitchJournal) dnsEngineSwitchJournal
		state       func(dnsEngineSwitchJournal) (dnsEngineStateReceipt, bool)
		host        fakeUnrecordedBINDHost
		wantAdmit   bool
		wantRefusal bool
		wantHost    bool
	}{
		{
			name:      "first install, pointer missing",
			host:      fakeUnrecordedBINDHost{anchor: true},
			wantAdmit: true, wantHost: true,
		},
		{
			name:        "PowerDNS source, pointer missing",
			mutate:      withPDNSSource,
			host:        fakeUnrecordedBINDHost{anchor: true},
			wantRefusal: true, wantHost: true,
		},
		{
			name: "running BIND before the switch",
			mutate: func(j dnsEngineSwitchJournal) dnsEngineSwitchJournal {
				j.TargetUnitsBefore = []dnsUnitSnapshot{{Name: "named.service", LoadState: "loaded", ActiveState: "active", UnitFileState: "enabled"}}
				return j
			},
			host:        fakeUnrecordedBINDHost{},
			wantRefusal: true, wantHost: true,
		},
		{
			name: "prior generation existed",
			mutate: func(j dnsEngineSwitchJournal) dnsEngineSwitchJournal {
				j.HadPrevious, j.PreviousGeneration = true, strings.Repeat("e", 64)
				return j
			},
			host:        fakeUnrecordedBINDHost{},
			wantRefusal: true, wantHost: true,
		},
		{
			name:     "pointer still selects the target",
			host:     fakeUnrecordedBINDHost{pointer: strings.Repeat("c", 64)},
			wantHost: true,
		},
		{
			name:     "pointer unreadable",
			host:     fakeUnrecordedBINDHost{pointerErr: errors.New("not a root-owned symlink")},
			wantHost: true,
		},
		{
			name: "records do not name the target",
			state: func(j dnsEngineSwitchJournal) (dnsEngineStateReceipt, bool) {
				state := unrecordedBINDTargetState(j)
				state.Generation = strings.Repeat("f", 64)
				return state, true
			},
		},
		{
			name:  "records absent",
			state: func(dnsEngineSwitchJournal) (dnsEngineStateReceipt, bool) { return dnsEngineStateReceipt{}, false },
		},
		{
			name: "target already recorded verified",
			mutate: func(j dnsEngineSwitchJournal) dnsEngineSwitchJournal {
				j.Phase = dnsSwitchPhaseTargetVerified
				return j
			},
		},
		{
			name: "rollback already decided",
			mutate: func(j dnsEngineSwitchJournal) dnsEngineSwitchJournal {
				j.Phase = dnsSwitchPhaseRollingBack
				return j
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			journal := testBINDSwitchJournal(t)
			journal.Phase = dnsSwitchPhaseTargetStarted
			if test.mutate != nil {
				journal = test.mutate(journal)
			}
			state, stateExists := unrecordedBINDTargetState(journal), true
			if test.state != nil {
				state, stateExists = test.state(journal)
			}
			host := test.host
			admitted, err := admitUnrecordedBINDTargetWithoutPointer(journal, state, stateExists, host.ops())
			if admitted != test.wantAdmit || (host.calls > 0) != test.wantHost {
				t.Fatalf("admitted=%v host calls=%d err=%v", admitted, host.calls, err)
			}
			var refusal *bindUnrecordedTargetRefusal
			if errors.As(err, &refusal) != test.wantRefusal || (!test.wantRefusal && err != nil) {
				t.Fatalf("refusal=%v err=%v", test.wantRefusal, err)
			}
			if test.wantAdmit && (len(host.logs) != 1 || !strings.Contains(host.logs[0], "rolls it back to no DNS engine")) {
				t.Fatalf("admission log = %q", host.logs)
			}
			if !test.wantAdmit && len(host.logs) != 0 {
				t.Fatalf("refused shape logged an admission: %q", host.logs)
			}
			if test.wantRefusal {
				text := err.Error()
				for _, want := range []string{
					journal.MutationRequestID, testBINDPointerPath, "before its BIND target was recorded as verified",
					"does not restore the pointer", "recovery dns-switch-status --quiesced", "contacts support",
					"no owner recovery command applies",
				} {
					if !strings.Contains(text, want) {
						t.Errorf("refusal %q lacks %q", text, want)
					}
				}
				if strings.Contains(text, "cannot start after a reboot") != host.anchor {
					t.Errorf("reboot claim does not follow the anchor observation: %q", text)
				}
				message := releasedDNSSwitchUnknownMessage(fmt.Errorf("DNS engine target absence could not be proved: %w", errors.Join(errors.New("open current"), err)))
				if message != refusal.ledgerMessage() || len(message) > 512 {
					t.Fatalf("ledger message (%d bytes) = %q", len(message), message)
				}
			}
		})
	}
}

// Through the shared decision: the admitted first install records its
// rollback decision before the inverse; the refused shape writes nothing.
func TestUnrecordedBINDTargetReconcileWritesDecisionBeforeInverse(t *testing.T) {
	for _, withSource := range []bool{false, true} {
		t.Run(fmt.Sprintf("prior-state=%v", withSource), func(t *testing.T) {
			journal := testBINDSwitchJournal(t)
			journal.Phase = dnsSwitchPhaseTargetStarted
			if withSource {
				// A prior generation: not the first-install shape.
				journal.HadPrevious, journal.PreviousGeneration = true, strings.Repeat("e", 64)
			}
			host := &fakeUnrecordedBINDHost{anchor: true}
			var steps []string
			ops := dnsenginerecovery.Operations{
				Read: func(context.Context) (dnsengineartifact.SwitchJournalV1, bool, error) { return journal, true, nil },
				ProveFinalized: func(context.Context, dnsengineartifact.SwitchIdentity) (bool, error) {
					return false, nil
				},
				VerifyTarget: func(context.Context, dnsengineartifact.SwitchJournalV1) error {
					steps = append(steps, "verify")
					return errors.New("open current: file does not exist")
				},
				ProveTargetAbsent: func(_ context.Context, observed dnsengineartifact.SwitchJournalV1) (bool, error) {
					steps = append(steps, "absence")
					return admitUnrecordedBINDTargetWithoutPointer(observed, unrecordedBINDTargetState(observed), true, host.ops())
				},
				Write: func(_ context.Context, _, next dnsengineartifact.SwitchJournalV1) error {
					steps = append(steps, "write "+next.Phase)
					journal = next
					return nil
				},
				Inverse: func(context.Context, dnsengineartifact.SwitchJournalV1) error {
					steps = append(steps, "inverse")
					return nil
				},
				RepairVerifiedTarget: func(context.Context, dnsengineartifact.SwitchJournalV1) (bool, error) {
					t.Fatal("an unrecorded target is never repaired")
					return false, nil
				},
			}
			id := dnsengineartifact.SwitchIdentity{
				RequestID: journal.MutationRequestID, OwnerID: journal.MutationOwnerID,
				Target: journal.TargetEngine, Qualifier: journal.ManifestQualifier,
			}
			outcome, err := dnsenginerecovery.Reconcile(context.Background(), dnsJournalPolicy(), id, ops)
			if withSource {
				var refusal *bindUnrecordedTargetRefusal
				if err == nil || !errors.As(err, &refusal) || outcome != dnsenginerecovery.OutcomeAbsent ||
					!reflect.DeepEqual(steps, []string{"verify", "absence"}) {
					t.Fatalf("outcome=%s err=%v steps=%v", outcome, err, steps)
				}
				return
			}
			want := []string{"verify", "absence", "write " + dnsSwitchPhaseRollingBack, "inverse", "write " + dnsSwitchPhaseRolledBack}
			if err != nil || outcome != dnsenginerecovery.OutcomeRolledBack || !reflect.DeepEqual(steps, want) {
				t.Fatalf("outcome=%s err=%v steps=%v", outcome, err, steps)
			}
		})
	}
}
