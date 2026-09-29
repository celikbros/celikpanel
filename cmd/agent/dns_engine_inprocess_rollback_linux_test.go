//go:build linux

package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/alicelik/celikpanel/internal/binddns"
)

// stageGateTestBINDGeneration writes one verified immutable generation into
// root the way the publisher stages it (root-owned, read-only tree).
func stageGateTestBINDGeneration(t *testing.T, root string, epoch int64) string {
	t.Helper()
	generation, err := binddns.RenderManifest(root, binddns.Manifest{EngineEpoch: epoch})
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "generations", generation.ID)
	if err := os.MkdirAll(filepath.Join(dir, "zones"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "receipt.json"), generation.Receipt, 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "zones.conf"), generation.Config, 0o444); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(dir, "zones"), dir} {
		if err := os.Chmod(path, 0o555); err != nil {
			t.Fatal(err)
		}
	}
	return generation.ID
}

// The BIND switch and the running-BIND adoption hand a failed forward step to
// binddns.Publisher.Switch through runBINDSwitchWithRollbackGate. With the
// real publisher, a verified target recorded durably keeps its pointer and runs
// no inverse; the publisher restores a pointer or runs the inverse only after
// rolling-back is durable; an undecided rollback leaves everything in place.
func TestBINDSwitchRollbackGateWithRealPublisher(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("the BIND generation publisher requires root-owned trees")
	}
	for _, hadPrevious := range []bool{false, true} {
		for _, test := range []struct {
			name          string
			faults        map[string]string
			wantInverse   bool
			wantHandoff   bool
			wantForward   bool
			wantDiskPhase string
		}{
			{
				name:        "verified write durable but reported failed",
				faults:      map[string]string{dnsSwitchPhaseTargetVerified: journalFaultAfterDurable},
				wantHandoff: true, wantForward: true, wantDiskPhase: dnsSwitchPhaseTargetVerified,
			},
			{
				name:        "verified write not durable",
				faults:      map[string]string{dnsSwitchPhaseTargetVerified: journalFaultBeforeDurable},
				wantInverse: true, wantDiskPhase: dnsSwitchPhaseRolledBack,
			},
			{
				name: "verified and rolling-back writes not durable",
				faults: map[string]string{
					dnsSwitchPhaseTargetVerified: journalFaultBeforeDurable,
					dnsSwitchPhaseRollingBack:    journalFaultBeforeDurable,
				},
				wantHandoff: true, wantDiskPhase: dnsSwitchPhaseTargetStarted,
			},
			{
				name: "rolling-back write durable but reported failed",
				faults: map[string]string{
					dnsSwitchPhaseTargetVerified: journalFaultBeforeDurable,
					dnsSwitchPhaseRollingBack:    journalFaultAfterDurable,
				},
				wantInverse: true, wantDiskPhase: dnsSwitchPhaseRolledBack,
			},
			{
				name:        "earlier checkpoint durable but reported failed",
				faults:      map[string]string{dnsSwitchPhaseTargetStarted: journalFaultAfterDurable},
				wantInverse: true, wantDiskPhase: dnsSwitchPhaseRolledBack,
			},
		} {
			name := "first/" + test.name
			if hadPrevious {
				name = "previous/" + test.name
			}
			t.Run(name, func(t *testing.T) {
				root := t.TempDir()
				target := stageGateTestBINDGeneration(t, root, 2)
				previous := ""
				if hadPrevious {
					previous = stageGateTestBINDGeneration(t, root, 1)
					if err := os.Symlink("generations/"+previous, filepath.Join(root, "current")); err != nil {
						t.Fatal(err)
					}
				}
				publisher, err := binddns.NewOSPublisher(root)
				if err != nil {
					t.Fatal(err)
				}
				pointer := func() string {
					current, exists, err := publisher.Current()
					if err != nil {
						t.Fatal(err)
					}
					if !exists {
						return ""
					}
					return current
				}
				journal := testBINDSwitchJournal(t)
				journal.Phase = dnsSwitchPhaseIntent
				journal.TargetGeneration = target
				journal.PreviousGeneration, journal.HadPrevious = previous, hadPrevious
				store := newFaultJournalStore(journal, test.faults)
				store.onWrite = func(next dnsEngineSwitchJournal) {
					// The rollback decision is written while the pointer still
					// selects the target: no pointer effect precedes it.
					if next.Phase == dnsSwitchPhaseRollingBack && pointer() != target {
						t.Fatalf("pointer changed to %q before the rollback decision was written", pointer())
					}
				}
				forward := func(context.Context) error {
					for _, phase := range []string{
						dnsSwitchPhaseTargetStaged, dnsSwitchPhaseSourceStopped,
						dnsSwitchPhaseTargetStarted, dnsSwitchPhaseTargetVerified,
					} {
						journal.Phase = phase
						if err := store.write(journal); err != nil {
							return err
						}
					}
					return nil
				}
				inverse := 0
				rollbackAndJournal := func(context.Context) error {
					return runBINDRollbackWithJournal(&journal, bindSwitchRollbackJournalOps{
						read: store.read, write: store.write,
						rollback: func() error {
							inverse++
							if store.journal.Phase != dnsSwitchPhaseRollingBack {
								t.Fatalf("inverse ran at disk phase %s", store.journal.Phase)
							}
							// A first generation keeps its pointer until the
							// inverse succeeded; a prior one is selected again
							// before the restored unit serves it.
							want := target
							if hadPrevious {
								want = previous
							}
							if got := pointer(); got != want {
								t.Fatalf("pointer during inverse=%q want %q", got, want)
							}
							return nil
						},
						verify: func() error { return nil },
					})
				}
				err = runBINDSwitchWithRollbackGate(
					context.Background(),
					func(ctx context.Context, apply, recoverEmpty func(context.Context) error) error {
						return publisher.Switch(ctx, target, apply, recoverEmpty)
					},
					&journal, store.ops(), forward, rollbackAndJournal,
				)
				if err == nil {
					t.Fatal("failed forward step returned success")
				}
				if (inverse == 1) != test.wantInverse || inverse > 1 {
					t.Fatalf("inverse calls=%d want inverse=%v err=%v", inverse, test.wantInverse, err)
				}
				if test.wantHandoff {
					requireHandoff(t, err, test.wantForward)
				}
				if store.journal.Phase != test.wantDiskPhase {
					t.Fatalf("disk phase=%s want %s (persisted %v)", store.journal.Phase, test.wantDiskPhase, store.persisted)
				}
				wantPointer := target
				if test.wantInverse {
					wantPointer = previous
				}
				if got := pointer(); got != wantPointer {
					t.Fatalf("pointer after switch=%q want %q", got, wantPointer)
				}
			})
		}
	}
}
