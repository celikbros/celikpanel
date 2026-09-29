package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

// Native trial pdns-switch__target-staged__after-write (2026-09-29): a first
// PowerDNS install killed at target-staged left pdns.service under the package
// guard's persistent mask and never started. Rollback must accept that
// never-served target instead of retaining the journal as unknown.
type freshSourceRollbackRun struct {
	steps     []string
	writes    []string
	listeners int
	err       error
	journal   dnsEngineSwitchJournal
}

func runFreshSourcePDNSRollback(
	t *testing.T,
	journal dnsEngineSwitchJournal,
	observed bindInstallUnitState,
	processes dnsUnitProcesses,
	listener error,
) freshSourceRollbackRun {
	t.Helper()
	var run freshSourceRollbackRun
	record := func(step string) func() error {
		return func() error { run.steps = append(run.steps, step); return nil }
	}
	run.err = runDNSSwitchRecoveryRollbackWithJournal(&journal, dnsSwitchRecoveryRollbackOps{
		write: func(next dnsEngineSwitchJournal) error {
			run.writes = append(run.writes, next.Phase)
			return nil
		},
		rollback: func(current dnsEngineSwitchJournal) error {
			stopTarget, verifyStopped := pdnsSwitchRollbackTargetOps(
				current,
				pdnsRollbackStoppedProofOps{
					inspectUnit: func(context.Context) (bindInstallUnitState, error) {
						return observed, nil
					},
					inspectProcesses: func(context.Context) (dnsUnitProcesses, error) {
						return processes, nil
					},
					inspectCgroup: func(context.Context) error { return nil },
					inspectPublicDNSListeners: func(context.Context) error {
						run.listeners++
						return listener
					},
				},
				func(context.Context) error { return record("stop-target")() },
			)
			return rollbackPDNSSwitchWithOps(context.Background(), pdnsSwitchRollbackOps{
				stopTarget: stopTarget,
				verifyStopped: func(ctx context.Context) error {
					run.steps = append(run.steps, "verify-stopped")
					return verifyStopped(ctx)
				},
				restorePDNSDatabaseSnapshot: record("restore-database"),
				restoreConfigs:              record("restore-configs"),
				restoreState:                record("restore-state"),
				restoreTarget:               func(context.Context) error { return record("restore-target")() },
				restoreSource:               func(context.Context) error { return record("restore-source")() },
			})
		},
	})
	run.journal = journal
	return run
}

func freshPDNSRollbackJournal(phase string, target dnsUnitSnapshot) dnsEngineSwitchJournal {
	return dnsEngineSwitchJournal{
		Schema: dnsEngineSwitchJournalSchema, Phase: phase,
		Mode:         transport.DNSEngineSwitchModeSwitch,
		TargetEngine: transport.DNSEnginePowerDNS, TargetEpoch: 1,
		Topology:          transport.DNSTopologyStandalone,
		TargetUnitsBefore: []dnsUnitSnapshot{target},
		SourceUnitsBefore: []dnsUnitSnapshot{},
	}
}

func TestFreshSourcePDNSRollbackProceedsFromNeverStartedTarget(t *testing.T) {
	maskedSnapshot := dnsUnitSnapshot{Name: "pdns.service", LoadState: "masked", ActiveState: "inactive", UnitFileState: "masked"}
	notFoundSnapshot := dnsUnitSnapshot{Name: "pdns.service", LoadState: "not-found", ActiveState: "inactive"}
	dead := dnsUnitProcesses{SubState: "dead"}
	full := "verify-stopped,restore-database,restore-configs,restore-state,restore-target,restore-source"
	for _, tc := range []struct {
		name      string
		journal   dnsEngineSwitchJournal
		observed  bindInstallUnitState
		wantSteps string
	}{
		{
			name:      "target-staged-package-guard-mask",
			journal:   freshPDNSRollbackJournal(dnsSwitchPhaseTargetStaged, maskedSnapshot),
			observed:  bindInstallUnitState{name: "pdns.service", loadState: "masked", activeState: "inactive", unitFileState: "masked"},
			wantSteps: "stop-target," + full,
		},
		{
			name:      "intent-unit-not-found",
			journal:   freshPDNSRollbackJournal(dnsSwitchPhaseIntent, notFoundSnapshot),
			observed:  bindInstallUnitState{name: "pdns.service", loadState: "not-found", activeState: "inactive"},
			wantSteps: full,
		},
		{
			name:      "source-stopped-unmasked-before-start",
			journal:   freshPDNSRollbackJournal(dnsSwitchPhaseSourceStopped, maskedSnapshot),
			observed:  bindInstallUnitState{name: "pdns.service", loadState: "loaded", activeState: "inactive", unitFileState: "disabled"},
			wantSteps: "stop-target," + full,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			run := runFreshSourcePDNSRollback(t, tc.journal, tc.observed, dead, nil)
			if run.err != nil {
				t.Fatalf("fresh-source rollback refused a never-started target: %v", run.err)
			}
			if got := strings.Join(run.steps, ","); got != tc.wantSteps {
				t.Fatalf("rollback steps = %s, want %s", got, tc.wantSteps)
			}
			if got := strings.Join(run.writes, ","); got != dnsSwitchPhaseRollingBack+","+dnsSwitchPhaseRolledBack ||
				run.journal.Phase != dnsSwitchPhaseRolledBack {
				t.Fatalf("journal writes = %s phase=%s", got, run.journal.Phase)
			}
			if run.listeners != 2 {
				t.Fatalf("port-53 listener proofs = %d, want two", run.listeners)
			}
		})
	}

	refusals := []struct {
		name      string
		journal   dnsEngineSwitchJournal
		observed  bindInstallUnitState
		processes dnsUnitProcesses
		listener  error
		want      string
		listeners int
	}{
		{
			name: "bind-source-keeps-loaded-requirement",
			journal: func() dnsEngineSwitchJournal {
				j := freshPDNSRollbackJournal(dnsSwitchPhaseTargetStaged, maskedSnapshot)
				j.SourceEngine, j.SourceEpoch = transport.DNSEngineBIND, 1
				return j
			}(),
			observed:  bindInstallUnitState{name: "pdns.service", loadState: "masked", activeState: "inactive", unitFileState: "masked"},
			processes: dead,
			want:      "DNS target is not a loaded unit",
		},
		{
			name: "bind-source-epoch-only-keeps-loaded-requirement",
			journal: func() dnsEngineSwitchJournal {
				j := freshPDNSRollbackJournal(dnsSwitchPhaseIntent, notFoundSnapshot)
				j.SourceEpoch = 3
				return j
			}(),
			observed:  bindInstallUnitState{name: "pdns.service", loadState: "not-found", activeState: "inactive"},
			processes: dead,
			want:      "DNS target is not a loaded unit",
		},
		{
			name:      "fresh-public-listener",
			journal:   freshPDNSRollbackJournal(dnsSwitchPhaseTargetStaged, maskedSnapshot),
			observed:  bindInstallUnitState{name: "pdns.service", loadState: "masked", activeState: "inactive", unitFileState: "masked"},
			processes: dead,
			listener:  errors.New("a public port-53 listener is present"),
			want:      "public port-53 listener",
			listeners: 1,
		},
		{
			name:      "fresh-target-still-running",
			journal:   freshPDNSRollbackJournal(dnsSwitchPhaseTargetStarted, maskedSnapshot),
			observed:  bindInstallUnitState{name: "pdns.service", loadState: "loaded", activeState: "active", unitFileState: "enabled"},
			processes: dnsUnitProcesses{SubState: "running", MainPID: 42},
			want:      "inactive/dead",
		},
		{
			name:      "fresh-runtime-mask",
			journal:   freshPDNSRollbackJournal(dnsSwitchPhaseTargetStaged, maskedSnapshot),
			observed:  bindInstallUnitState{name: "pdns.service", loadState: "masked", activeState: "inactive", unitFileState: "masked-runtime"},
			processes: dead,
			want:      "neither absent, persistently masked nor loaded",
		},
	}
	for _, tc := range refusals {
		t.Run("refuse/"+tc.name, func(t *testing.T) {
			run := runFreshSourcePDNSRollback(t, tc.journal, tc.observed, tc.processes, tc.listener)
			if run.err == nil || !strings.Contains(run.err.Error(), tc.want) {
				t.Fatalf("rollback err = %v, want %q", run.err, tc.want)
			}
			for _, step := range run.steps {
				if strings.HasPrefix(step, "restore-") {
					t.Fatalf("restore ran after a refused stopped proof: %v", run.steps)
				}
			}
			if strings.Join(run.writes, ",") != dnsSwitchPhaseRollingBack ||
				run.journal.Phase != dnsSwitchPhaseRollingBack {
				t.Fatalf("refused rollback did not retain rolling-back: writes=%v phase=%s", run.writes, run.journal.Phase)
			}
			if run.listeners != tc.listeners {
				t.Fatalf("listener proofs = %d, want %d", run.listeners, tc.listeners)
			}
		})
	}
	t.Run("source-journal-stops-not-found-target", func(t *testing.T) {
		journal := freshPDNSRollbackJournal(dnsSwitchPhaseIntent, notFoundSnapshot)
		journal.SourceEngine, journal.SourceEpoch = transport.DNSEngineBIND, 1
		run := runFreshSourcePDNSRollback(t, journal,
			bindInstallUnitState{name: "pdns.service", loadState: "not-found", activeState: "inactive"}, dead, nil)
		if len(run.steps) == 0 || run.steps[0] != "stop-target" {
			t.Fatalf("source-present rollback skipped its stop: %v", run.steps)
		}
	})
}

func TestFreshSourceBINDRollbackTargetStopProof(t *testing.T) {
	dead := func(context.Context) (dnsUnitProcesses, error) { return dnsUnitProcesses{SubState: "dead"}, nil }
	emptyCgroup := func(context.Context) error { return nil }
	for _, unit := range []bindInstallUnitState{
		{name: "named.service", loadState: "not-found", activeState: "inactive"},
		{name: "named.service", loadState: "masked", activeState: "inactive", unitFileState: "masked"},
		{name: "named.service", loadState: "loaded", activeState: "inactive", unitFileState: "disabled"},
	} {
		t.Run(unit.loadState, func(t *testing.T) {
			listeners := 0
			if err := verifyBINDTargetStoppedBeforeConfigRestoreForSourceWithOps(
				context.Background(), true,
				func(context.Context) (bindInstallUnitState, error) { return unit, nil },
				dead, emptyCgroup,
				func(context.Context) error { listeners++; return nil },
			); err != nil || listeners != 2 {
				t.Fatalf("fresh BIND stopped proof err=%v listener proofs=%d", err, listeners)
			}
			if unit.loadState == "loaded" {
				return
			}
			if err := verifyBINDTargetStoppedBeforeConfigRestoreForSourceWithOps(
				context.Background(), false,
				func(context.Context) (bindInstallUnitState, error) { return unit, nil },
				dead, emptyCgroup, nil,
			); err == nil || err.Error() != "DNS target is not a loaded unit" {
				t.Fatalf("source-present BIND proof accepted %s: %v", unit.loadState, err)
			}
		})
	}
	masked := bindInstallUnitState{name: "named.service", loadState: "masked", activeState: "inactive", unitFileState: "masked"}
	if err := verifyBINDTargetStoppedBeforeConfigRestoreForSourceWithOps(
		context.Background(), true,
		func(context.Context) (bindInstallUnitState, error) { return masked, nil },
		dead, emptyCgroup,
		func(context.Context) error { return errors.New("a public port-53 listener is present") },
	); err == nil {
		t.Fatal("fresh BIND proof accepted a public port-53 listener")
	}
	if err := verifyBINDTargetStoppedBeforeConfigRestoreForSourceWithOps(
		context.Background(), true,
		func(context.Context) (bindInstallUnitState, error) { return masked, nil },
		dead, emptyCgroup, nil,
	); err == nil {
		t.Fatal("fresh BIND proof ran without a listener observer")
	}
	for _, journal := range []dnsEngineSwitchJournal{
		{SourceEngine: "", SourceEpoch: 0},
		{SourceEngine: transport.DNSEnginePowerDNS, SourceEpoch: 2},
		{SourceEngine: "", SourceEpoch: 1},
	} {
		want := journal.SourceEngine == "" && journal.SourceEpoch == 0
		if got := dnsSwitchJournalHasEmptySource(journal); got != want {
			t.Fatalf("empty source for %q/%d = %t", journal.SourceEngine, journal.SourceEpoch, got)
		}
	}
}
