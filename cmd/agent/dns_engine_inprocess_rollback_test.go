package main

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

const (
	journalFaultBeforeDurable = "before"
	journalFaultAfterDurable  = "after"
)

// faultJournalStore is a journal "disk" whose writer can fail either before
// the checkpoint becomes durable (nothing changes on disk) or after it did (the
// new journal is on disk, but the write still reports an error - a failure
// reported after the rename, or a failed directory sync or readback).
type faultJournalStore struct {
	journal   dnsEngineSwitchJournal
	exists    bool
	faults    map[string]string
	readErr   error
	persisted []string
	onWrite   func(dnsEngineSwitchJournal)
}

func newFaultJournalStore(initial dnsEngineSwitchJournal, faults map[string]string) *faultJournalStore {
	return &faultJournalStore{journal: initial, exists: true, faults: faults}
}

func (store *faultJournalStore) read() (dnsEngineSwitchJournal, bool, error) {
	if store.readErr != nil {
		return dnsEngineSwitchJournal{}, false, store.readErr
	}
	return store.journal, store.exists, nil
}

func (store *faultJournalStore) write(journal dnsEngineSwitchJournal) error {
	if store.onWrite != nil {
		store.onWrite(journal)
	}
	switch store.faults[journal.Phase] {
	case journalFaultBeforeDurable:
		return errors.New("injected journal write failure before phase " + journal.Phase + " became durable")
	case journalFaultAfterDurable:
		store.journal, store.exists = journal, true
		store.persisted = append(store.persisted, journal.Phase)
		return errors.New("injected journal write failure reported after phase " + journal.Phase + " became durable")
	}
	store.journal, store.exists = journal, true
	store.persisted = append(store.persisted, journal.Phase)
	return nil
}

func (store *faultJournalStore) ops() dnsSwitchInProcessJournalOps {
	return dnsSwitchInProcessJournalOps{read: store.read, write: store.write}
}

func requireHandoff(t *testing.T, err error, forward bool) {
	t.Helper()
	var handoff *dnsSwitchInProcessHandoffError
	if !errors.As(err, &handoff) {
		t.Fatalf("error is not an in-process hand-off: %v", err)
	}
	if handoff.forward != forward {
		t.Fatalf("hand-off forward=%v, want %v: %v", handoff.forward, forward, err)
	}
}

func TestBINDRollbackGateRunsNoInverseWithoutDurableDecision(t *testing.T) {
	for _, test := range []struct {
		name          string
		memoryPhase   string
		diskPhase     string
		faults        map[string]string
		readErr       error
		wantInverse   bool
		wantForward   bool
		wantHandoff   bool
		wantDiskPhase string
	}{
		{
			name:        "durable target-verified never rolls back",
			memoryPhase: dnsSwitchPhaseTargetVerified, diskPhase: dnsSwitchPhaseTargetVerified,
			wantForward: true, wantHandoff: true, wantDiskPhase: dnsSwitchPhaseTargetVerified,
		},
		{
			name:        "durable committed never rolls back",
			memoryPhase: dnsSwitchPhaseTargetVerified, diskPhase: dnsSwitchPhaseCommitted,
			wantForward: true, wantHandoff: true, wantDiskPhase: dnsSwitchPhaseCommitted,
		},
		{
			name:        "rolling-back write fails before it is durable",
			memoryPhase: dnsSwitchPhaseTargetStarted, diskPhase: dnsSwitchPhaseTargetStarted,
			faults:      map[string]string{dnsSwitchPhaseRollingBack: journalFaultBeforeDurable},
			wantHandoff: true, wantDiskPhase: dnsSwitchPhaseTargetStarted,
		},
		{
			name:        "rolling-back write fails after it is durable",
			memoryPhase: dnsSwitchPhaseTargetStarted, diskPhase: dnsSwitchPhaseTargetStarted,
			faults:      map[string]string{dnsSwitchPhaseRollingBack: journalFaultAfterDurable},
			wantInverse: true, wantDiskPhase: dnsSwitchPhaseRolledBack,
		},
		{
			name:        "journal cannot be read back",
			memoryPhase: dnsSwitchPhaseTargetStarted, diskPhase: dnsSwitchPhaseTargetStarted,
			readErr:     errors.New("injected journal read failure"),
			wantHandoff: true, wantDiskPhase: dnsSwitchPhaseTargetStarted,
		},
		{
			name:        "memory is ahead of an earlier durable phase",
			memoryPhase: dnsSwitchPhaseTargetVerified, diskPhase: dnsSwitchPhaseTargetStarted,
			wantInverse: true, wantDiskPhase: dnsSwitchPhaseRolledBack,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			journal := testBINDSwitchJournal(t)
			disk := journal
			disk.Phase = test.diskPhase
			store := newFaultJournalStore(disk, test.faults)
			store.readErr = test.readErr
			journal.Phase = test.memoryPhase
			inverse := 0
			err := runBINDRollbackWithJournal(&journal, bindSwitchRollbackJournalOps{
				read: store.read, write: store.write,
				rollback: func() error {
					inverse++
					if store.journal.Phase != dnsSwitchPhaseRollingBack {
						t.Fatalf("inverse ran while the journal on disk was at %s", store.journal.Phase)
					}
					return nil
				},
				verify: func() error { return nil },
			})
			if (inverse == 1) != test.wantInverse || inverse > 1 {
				t.Fatalf("inverse calls=%d want inverse=%v err=%v", inverse, test.wantInverse, err)
			}
			if test.wantHandoff {
				requireHandoff(t, err, test.wantForward)
			} else if err != nil {
				t.Fatalf("durable rollback returned %v", err)
			}
			if store.journal.Phase != test.wantDiskPhase {
				t.Fatalf("disk phase=%s want %s", store.journal.Phase, test.wantDiskPhase)
			}
		})
	}
}

// The real checkpoint writer reports an after-write failure although the
// journal is durable; the gate reads it back and goes forward.
func TestBINDRollbackGateReadsBackRealCheckpointWriterFailure(t *testing.T) {
	journal := testBINDSwitchJournal(t)
	journal.Phase = dnsSwitchPhaseTargetStarted
	var disk []byte
	read := func() (dnsEngineSwitchJournal, bool, error) {
		if disk == nil {
			return dnsEngineSwitchJournal{}, false, nil
		}
		decoded, err := decodeDNSEngineSwitchJournal(disk)
		return decoded, err == nil, err
	}
	write := func(next dnsEngineSwitchJournal) error {
		return writeDNSEngineSwitchJournalWithOps(
			next,
			func(encoded []byte) error { disk = append([]byte(nil), encoded...); return nil },
			read,
			func(point string, observed dnsEngineSwitchJournal) error {
				if point == dnsEngineSwitchJournalFaultAfterWrite && observed.Phase == dnsSwitchPhaseTargetVerified {
					return errors.New("injected failure after the durable target-verified rename")
				}
				return nil
			},
		)
	}
	if err := write(journal); err != nil {
		t.Fatal(err)
	}
	journal.Phase = dnsSwitchPhaseTargetVerified
	if err := write(journal); err == nil {
		t.Fatal("checkpoint writer did not report the injected after-write failure")
	}
	inverse := 0
	err := runBINDRollbackWithJournal(&journal, bindSwitchRollbackJournalOps{
		read: read, write: write,
		rollback: func() error { inverse++; return nil },
		verify:   func() error { return nil },
	})
	requireHandoff(t, err, true)
	durable, exists, readErr := read()
	if inverse != 0 || readErr != nil || !exists || durable.Phase != dnsSwitchPhaseTargetVerified {
		t.Fatalf("inverse=%d durable=%s exists=%v err=%v", inverse, durable.Phase, exists, readErr)
	}
}

// pdnsGateTestJournal is a PowerDNS switch or adoption journal shape for the
// gate: the gate compares frozen content and phase only.
func pdnsGateTestJournal(mode string, phase string) dnsEngineSwitchJournal {
	return dnsEngineSwitchJournal{
		Schema: dnsEngineSwitchJournalSchema, Phase: phase, Mode: mode,
		MutationRequestID: strings.Repeat("c", 32), MutationOwnerID: strings.Repeat("d", 32),
		TargetEngine: transport.DNSEnginePowerDNS, TargetEpoch: 1,
	}
}

// Each PowerDNS driver runs its in-process rollback through
// runGatedDNSSwitchRollback with its own gate: the PowerDNS switch and the
// paired-secondary reconfiguration use decideDNSSwitchInProcessRollback, the
// adoption transitionPDNSAdoptionJournalToRollback. After a failed
// target-verified write each driver calls its rollback, so the gate decides.
func TestPDNSDriverRollbackGatesAfterVerifiedWriteFailure(t *testing.T) {
	gates := []struct {
		driver string
		mode   string
		prior  string
		decide func(*faultJournalStore) func(dnsEngineSwitchJournal, error) (dnsEngineSwitchJournal, error)
	}{
		{
			driver: "pdns-switch and reconfigure", mode: transport.DNSEngineSwitchModeSwitch,
			prior: dnsSwitchPhaseTargetStarted,
			decide: func(store *faultJournalStore) func(dnsEngineSwitchJournal, error) (dnsEngineSwitchJournal, error) {
				return func(current dnsEngineSwitchJournal, cause error) (dnsEngineSwitchJournal, error) {
					return decideDNSSwitchInProcessRollback(current, store.ops(), cause)
				}
			},
		},
		{
			driver: "pdns-adopt", mode: transport.DNSEngineSwitchModeAdopt,
			prior: dnsSwitchPhaseIntent,
			decide: func(store *faultJournalStore) func(dnsEngineSwitchJournal, error) (dnsEngineSwitchJournal, error) {
				return func(current dnsEngineSwitchJournal, cause error) (dnsEngineSwitchJournal, error) {
					return transitionPDNSAdoptionJournalToRollback(current, store.read, store.write, cause)
				}
			},
		},
	}
	cases := []struct {
		name          string
		faults        map[string]string
		wantInverse   bool
		wantForward   bool
		wantHandoff   bool
		wantDiskPhase func(prior string) string
	}{
		{
			name:        "verified write failed after it was durable",
			faults:      map[string]string{dnsSwitchPhaseTargetVerified: journalFaultAfterDurable},
			wantForward: true, wantHandoff: true,
			wantDiskPhase: func(string) string { return dnsSwitchPhaseTargetVerified },
		},
		{
			name:          "verified write failed before it was durable",
			faults:        map[string]string{dnsSwitchPhaseTargetVerified: journalFaultBeforeDurable},
			wantInverse:   true,
			wantDiskPhase: func(string) string { return dnsSwitchPhaseRolledBack },
		},
		{
			name: "rolling-back write also failed before it was durable",
			faults: map[string]string{
				dnsSwitchPhaseTargetVerified: journalFaultBeforeDurable,
				dnsSwitchPhaseRollingBack:    journalFaultBeforeDurable,
			},
			wantHandoff:   true,
			wantDiskPhase: func(prior string) string { return prior },
		},
		{
			name: "rolling-back write failed after it was durable",
			faults: map[string]string{
				dnsSwitchPhaseTargetVerified: journalFaultBeforeDurable,
				dnsSwitchPhaseRollingBack:    journalFaultAfterDurable,
			},
			wantInverse:   true,
			wantDiskPhase: func(string) string { return dnsSwitchPhaseRolledBack },
		},
	}
	for _, gate := range gates {
		for _, test := range cases {
			t.Run(gate.driver+"/"+test.name, func(t *testing.T) {
				journal := pdnsGateTestJournal(gate.mode, gate.prior)
				store := newFaultJournalStore(journal, test.faults)
				journal.Phase = dnsSwitchPhaseTargetVerified
				writeErr := store.write(journal)
				if writeErr == nil {
					t.Fatal("injected target-verified write did not fail")
				}
				inverse := 0
				err := runGatedDNSSwitchRollback(&journal, writeErr, gatedDNSSwitchRollbackOps{
					decide: gate.decide(store),
					inverse: func(decided dnsEngineSwitchJournal) error {
						inverse++
						if store.journal.Phase != dnsSwitchPhaseRollingBack || decided.Phase != dnsSwitchPhaseRollingBack {
							t.Fatalf("inverse ran at disk phase %s, decided %s", store.journal.Phase, decided.Phase)
						}
						return nil
					},
					write: store.write,
				})
				if (inverse == 1) != test.wantInverse || inverse > 1 {
					t.Fatalf("inverse calls=%d want inverse=%v err=%v", inverse, test.wantInverse, err)
				}
				if test.wantHandoff {
					requireHandoff(t, err, test.wantForward)
				}
				if !errors.Is(err, writeErr) {
					t.Fatalf("rollback error lost its cause: %v", err)
				}
				if want := test.wantDiskPhase(gate.prior); store.journal.Phase != want {
					t.Fatalf("disk phase=%s want %s (persisted %v)", store.journal.Phase, want, store.persisted)
				}
			})
		}
	}
}

func TestPDNSAdoptionGateRefusesForeignOrUnreadableJournal(t *testing.T) {
	journal := pdnsGateTestJournal(transport.DNSEngineSwitchModeAdopt, dnsSwitchPhaseIntent)
	foreign := journal
	foreign.MutationOwnerID = strings.Repeat("e", 32)
	for name, store := range map[string]*faultJournalStore{
		"foreign":    newFaultJournalStore(foreign, nil),
		"unreadable": {readErr: errors.New("injected read failure")},
		"absent":     {},
	} {
		t.Run(name, func(t *testing.T) {
			inverse := 0
			err := runGatedDNSSwitchRollback(&journal, errors.New("cause"), gatedDNSSwitchRollbackOps{
				decide: func(current dnsEngineSwitchJournal, cause error) (dnsEngineSwitchJournal, error) {
					return transitionPDNSAdoptionJournalToRollback(current, store.read, store.write, cause)
				},
				inverse: func(dnsEngineSwitchJournal) error { inverse++; return nil },
				write:   store.write,
			})
			requireHandoff(t, err, false)
			if inverse != 0 || len(store.persisted) != 0 {
				t.Fatalf("inverse=%d persisted=%v", inverse, store.persisted)
			}
		})
	}
}

// A stand-in switch function that runs its inverse whenever apply fails; the
// real binddns.Publisher is exercised in the Linux test next to this file.
func TestBINDSwitchRollbackGateHandsOffWithoutPublisherEffects(t *testing.T) {
	journal := testBINDSwitchJournal(t)
	journal.Phase = dnsSwitchPhaseIntent
	store := newFaultJournalStore(journal, map[string]string{
		dnsSwitchPhaseTargetVerified: journalFaultBeforeDurable,
		dnsSwitchPhaseRollingBack:    journalFaultBeforeDurable,
	})
	publisherInverse := 0
	switchFn := func(ctx context.Context, apply, recoverEmpty func(context.Context) error) error {
		if err := apply(ctx); err != nil {
			publisherInverse++
			return errors.Join(err, recoverEmpty(ctx))
		}
		return nil
	}
	forward := func(context.Context) error {
		for _, phase := range []string{dnsSwitchPhaseTargetStaged, dnsSwitchPhaseSourceStopped, dnsSwitchPhaseTargetStarted, dnsSwitchPhaseTargetVerified} {
			journal.Phase = phase
			if err := store.write(journal); err != nil {
				return err
			}
		}
		return nil
	}
	rollback := func(context.Context) error {
		t.Fatal("rollback ran without a durable decision")
		return nil
	}
	err := runBINDSwitchWithRollbackGate(context.Background(), switchFn, &journal, store.ops(), forward, rollback)
	requireHandoff(t, err, false)
	if publisherInverse != 0 || store.journal.Phase != dnsSwitchPhaseTargetStarted ||
		!reflect.DeepEqual(store.persisted, []string{dnsSwitchPhaseTargetStaged, dnsSwitchPhaseSourceStopped, dnsSwitchPhaseTargetStarted}) {
		t.Fatalf("publisher inverse=%d disk=%s persisted=%v", publisherInverse, store.journal.Phase, store.persisted)
	}
}
