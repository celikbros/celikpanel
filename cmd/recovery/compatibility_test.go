package main

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestRecoveryCompatibilityUsesOnlyExistingReadOnlyModes(t *testing.T) {
	for _, test := range []struct {
		mode string
		want []compatibilityCommand
	}{
		{"--normal", []compatibilityCommand{{"panel_database_check", "panel-checker", []string{"--check-service-operations-idle-wal-aware"}}, {"agent_ledger_check", "agent-checker", []string{"--check-service-mutation-idle"}}}},
		{"--bootstrap-pre-ledger", []compatibilityCommand{{"panel_database_check", "panel-checker", []string{"--check-pre-ledger-service-operations-idle-wal-aware"}}, {"agent_ledger_check", "agent-checker", []string{"--check-pre-ledger-service-mutation-idle"}}}},
		{"--bootstrap-schema17", []compatibilityCommand{{"panel_database_check", "schema17-bridge", []string{"check", "--db", "/var/lib/celikpanel/celikpanel.db"}}, {"agent_ledger_check", "agent-checker", []string{"--check-pre-ledger-service-mutation-idle"}}}},
	} {
		t.Run(test.mode, func(t *testing.T) {
			got, err := recoveryCompatibilityCommands(test.mode)
			if err != nil || !reflect.DeepEqual(got, test.want) {
				t.Fatalf("commands=%#v err=%v", got, err)
			}
		})
	}
	for _, mode := range []string{"", "normal", "--normal=true", "--normal ", " --normal", "--normal\n", "--initial", "--restore", "--normal --db /other"} {
		if got, err := recoveryCompatibilityCommands(mode); err == nil || got != nil {
			t.Fatalf("unsupported mode accepted: %q", mode)
		}
	}
}

type compatibilityTestRuntime struct {
	events     *[]string
	checks     int
	failAt     int
	closeError bool
}

func (runtime *compatibilityTestRuntime) Revalidate() error {
	*runtime.events = append(*runtime.events, "validate")
	runtime.checks++
	if runtime.checks == runtime.failAt {
		return errors.New("private runtime metadata")
	}
	return nil
}

func (runtime *compatibilityTestRuntime) Close() error {
	*runtime.events = append(*runtime.events, "close")
	if runtime.closeError {
		return errors.New("private close error")
	}
	return nil
}

func compatibilityTestDependencies(t *testing.T, events *[]string) (compatibilityDependencies, *compatibilityTestRuntime) {
	t.Helper()
	selected := &compatibilityTestRuntime{events: events}
	deps := compatibilityDependencies{
		euid: func() int { return 0 },
		boundary: func(fd int) error {
			if fd != 9 {
				t.Fatalf("unexpected transaction descriptor: %d", fd)
			}
			*events = append(*events, "boundary")
			return nil
		},
		resolve: func() (string, compatibilityRuntime, error) {
			*events = append(*events, "resolve")
			return filepath.FromSlash("/verified-selected-kit"), selected, nil
		},
		run: func(ctx context.Context, path string, args, environment []string) (string, error) {
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) <= 0 || time.Until(deadline) > compatibilityCheckTimeout {
				t.Fatal("checker execution has no bounded deadline")
			}
			if path != filepath.Join("/verified-selected-kit", "bin", filepath.Base(path)) {
				t.Fatalf("did not select a kit binary: %s", path)
			}
			if !reflect.DeepEqual(environment, recoveryCompatibilityEnvironment()) {
				t.Fatal("checker received an alternate environment")
			}
			*events = append(*events, "run:"+filepath.Base(path))
			return "", nil
		},
		sleep: func(delay time.Duration) {
			if delay != compatibilityCheckRetryDelay {
				t.Fatalf("unbounded retry delay: %s", delay)
			}
			*events = append(*events, "sleep")
		},
	}
	return deps, selected
}

func TestRecoveryCompatibilityRevalidatesKitAndNoMarkerBoundaryAroundEveryCheck(t *testing.T) {
	var events []string
	deps, _ := compatibilityTestDependencies(t, &events)
	if err := verifyRecoveryCompatibility("--normal", deps); err != nil {
		t.Fatal(err)
	}
	want := []string{"boundary", "resolve", "validate", "boundary", "run:panel-checker", "validate", "boundary", "validate", "boundary", "run:agent-checker", "validate", "boundary", "close"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events=%v", events)
	}
}

func TestRecoveryCompatibilityRefusesUnknownWithoutRunningAnotherCheck(t *testing.T) {
	steps := map[string]string{"non-root": "owner", "unsupported-mode": "mode", "lock-or-marker": "release_boundary",
		"resolve": "runtime_selection", "before-first": "runtime_revalidation", "after-first": "runtime_revalidation",
		"before-second": "runtime_revalidation", "after-second": "runtime_revalidation", "boundary-after-first": "release_boundary",
		"panel-rejected": "panel_database_check", "agent-rejected": "agent_ledger_check", "close": "runtime_revalidation"}
	for _, fail := range []string{"non-root", "unsupported-mode", "lock-or-marker", "resolve", "before-first", "after-first", "before-second", "after-second", "boundary-after-first", "panel-rejected", "agent-rejected", "close"} {
		t.Run(fail, func(t *testing.T) {
			var events []string
			deps, selected := compatibilityTestDependencies(t, &events)
			mode, maxRuns := "--normal", 0
			switch fail {
			case "non-root":
				deps.euid = func() int { return 1000 }
			case "unsupported-mode":
				mode = "--normal --restore"
			case "lock-or-marker":
				deps.boundary = func(int) error { return errors.New("private native marker") }
			case "resolve":
				deps.resolve = func() (string, compatibilityRuntime, error) { return "", nil, errors.New("private selection") }
			case "before-first":
				selected.failAt = 1
			case "after-first":
				selected.failAt, maxRuns = 2, 1
			case "before-second":
				selected.failAt, maxRuns = 3, 1
			case "after-second":
				selected.failAt, maxRuns = 4, 2
			case "boundary-after-first":
				maxRuns = 1
				original, count := deps.boundary, 0
				deps.boundary = func(fd int) error {
					count++
					if count == 3 {
						return errors.New("new private transaction marker")
					}
					return original(fd)
				}
			case "panel-rejected", "agent-rejected":
				// A rejecting checker is read once more after the bounded pause.
				target := "panel-checker"
				maxRuns = 2
				if fail == "agent-rejected" {
					target, maxRuns = "agent-checker", 3
				}
				original := deps.run
				deps.run = func(ctx context.Context, path string, args, env []string) (string, error) {
					_, _ = original(ctx, path, args, env)
					if filepath.Base(path) == target {
						return "", errors.New("private DB/ledger refusal")
					}
					return "", nil
				}
			case "close":
				selected.closeError, maxRuns = true, 2
			}
			err := verifyRecoveryCompatibility(mode, deps)
			if !errors.Is(err, errRecoveryCompatibility) || strings.Contains(err.Error(), "private") ||
				!strings.HasPrefix(err.Error(), "step="+steps[fail]+": ") {
				t.Fatalf("expected bounded typed compatibility refusal, got %v", err)
			}
			runs := 0
			for _, event := range events {
				if strings.HasPrefix(event, "run:") {
					runs++
				}
			}
			if runs != maxRuns {
				t.Fatalf("unexpected executions after refusal: %v", events)
			}
			if (fail == "non-root" || fail == "unsupported-mode") && len(events) != 0 {
				t.Fatalf("performed observations before root/argument admission: %v", events)
			}
		})
	}
}

func TestRecoveryCompatibilityIgnoresCallerEnvironment(t *testing.T) {
	for _, key := range []string{"PATH", "HOME", "TMPDIR", "CELIKPANEL_DATA_DIR", "CELIKPANEL_AGENT_STATE_DIR", "CELIKPANEL_MUTATION_LOCK", "CELIKPANEL_MUTATION_LOCK_FD", "LD_PRELOAD", "CELIKPANEL_RECOVERY_RUNTIME_ROOT"} {
		t.Setenv(key, "poisoned-alternate-value")
	}
	env := recoveryCompatibilityEnvironment()
	want := []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "HOME=/root", "USER=root", "LOGNAME=root", "SHELL=/bin/bash", "LANG=C", "LC_ALL=C", "CELIKPANEL_DATA_DIR=/var/lib/celikpanel", "CELIKPANEL_AGENT_STATE_DIR=/var/lib/celikpanel-agent-private", "CELIKPANEL_MUTATION_LOCK=/run/celikpanel/service-mutation.lock"}
	if !reflect.DeepEqual(env, want) {
		t.Fatalf("environment=%v", env)
	}
	env[0] = "modified"
	if !reflect.DeepEqual(recoveryCompatibilityEnvironment(), want) {
		t.Fatal("caller can mutate the next environment")
	}
}

// F1 (upd3): the refusal names the step and the checker's first diagnostic
// line, bounded and printable; a transient refusal is read once more after the
// bounded pause and then admits; a timed-out check is not run again.
func TestRecoveryCompatibilityKeepsTypedCauseAndRereadsOnce(t *testing.T) {
	var events []string
	deps, _ := compatibilityTestDependencies(t, &events)
	original, panelRuns := deps.run, 0
	deps.run = func(ctx context.Context, path string, args, env []string) (string, error) {
		_, _ = original(ctx, path, args, env)
		if filepath.Base(path) == "panel-checker" {
			panelRuns++
			if panelRuns == 1 {
				return "Recovery database check: service operations are not idle: SQLite sidecar -wal changed after pinning\nsecond line\n", errors.New("exit status 1")
			}
		}
		return "", nil
	}
	if err := verifyRecoveryCompatibility("--normal", deps); err != nil {
		t.Fatalf("transient refusal was not re-read: %v", err)
	}
	want := []string{"boundary", "resolve", "validate", "boundary", "run:panel-checker", "validate", "boundary",
		"sleep", "validate", "boundary", "run:panel-checker", "validate", "boundary",
		"validate", "boundary", "run:agent-checker", "validate", "boundary", "close"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events=%v", events)
	}

	events = nil
	deps, _ = compatibilityTestDependencies(t, &events)
	deps.run = func(ctx context.Context, path string, args, env []string) (string, error) {
		if filepath.Base(path) == "agent-checker" {
			return "\x1b\r\n  Service mutation idle check: service mutation state is not idle: the host package manager is active " + strings.Repeat("x", 400) + "\n", errors.New("exit status 1")
		}
		return "", nil
	}
	err := verifyRecoveryCompatibility("--normal", deps)
	message := ""
	if err != nil {
		message = err.Error()
	}
	wantPrefix := "step=agent_ledger_check: Service mutation idle check: service mutation state is not idle: the host package manager is active x"
	if !errors.Is(err, errRecoveryCompatibility) || !strings.HasPrefix(message, wantPrefix) ||
		len(message) != len("step=agent_ledger_check: ")+240 || strings.ContainsAny(message, "\x1b\n") {
		t.Fatalf("typed cause lost or unbounded: %q", message)
	}

	events = nil
	deps, _ = compatibilityTestDependencies(t, &events)
	runs := 0
	deps.run = func(ctx context.Context, path string, args, env []string) (string, error) {
		runs++
		<-ctx.Done()
		return "", ctx.Err()
	}
	done := make(chan error, 1)
	go func() { done <- verifyRecoveryCompatibilityWithTimeout("--normal", deps, 20*time.Millisecond) }()
	select {
	case err = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed-out check was not bounded")
	}
	if runs != 1 || err == nil || err.Error() != "step=panel_database_check: the read-only check did not finish within its time limit" {
		t.Fatalf("timeout retried or untyped: runs=%d err=%v", runs, err)
	}
}
