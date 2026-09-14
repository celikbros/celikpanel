package main

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/alicelik/celikpanel/internal/recoveryruntime"
)

func TestRuntimePreparationClosedOwnerTuple(t *testing.T) {
	valid := []string{"prepare-runtime", "--source", filepath.Join(t.TempDir(), "recovery-runtime"), "--mode", "--normal", "--transaction-fd", "9"}
	for _, mode := range []string{"--normal", "--bootstrap-pre-ledger", "--bootstrap-schema17"} {
		args := append([]string{}, valid...)
		args[4] = mode
		called := false
		if code := dispatchRuntimePreparation(args, 0, func(source, gotMode string) error {
			called = true
			if source != valid[2] || gotMode != mode {
				t.Fatalf("changed tuple: %q %q", source, gotMode)
			}
			return nil
		}, func(string) {}); code != exitOK || !called {
			t.Fatal(code, called)
		}
	}
	invalid := [][]string{
		{"prepare-runtime"},
		{"prepare-runtime", "--source", "/verified/../elsewhere", "--mode", "--normal", "--transaction-fd", "9"},
		{"prepare-runtime", "--source", "relative", "--mode", "--normal", "--transaction-fd", "9"},
		{"prepare-runtime", "--source", "/verified", "--mode", "--force", "--transaction-fd", "9"},
		{"prepare-runtime", "--source", "/verified", "--mode", "--normal", "--transaction-fd", "8"},
		append(append([]string{}, valid...), "--force"),
	}
	for _, args := range invalid {
		if code := dispatchRuntimePreparation(args, 0, func(string, string) error { t.Fatal("invalid reached mutation"); return nil }, func(string) {}); code != exitUsage {
			t.Fatal(args, code)
		}
	}
	if code := dispatchRuntimePreparation(valid, 1000, func(string, string) error { t.Fatal("unprivileged mutation"); return nil }, func(string) {}); code != exitNotOwner {
		t.Fatal(code)
	}
	if code := dispatchRuntimePreparation(valid, 0, func(string, string) error { return errors.New("unknown") }, func(string) {}); code != exitUnavailable {
		t.Fatal(code)
	}
}

func TestRuntimePreparationAbsenceIsNotCorruption(t *testing.T) {
	for _, tc := range []struct {
		name     string
		selected error
		want     string
	}{
		{"first", recoveryruntime.ErrNotSelected, "enroll"},
		{"selected", nil, "promote"},
		{"corrupt", recoveryruntime.ErrUnavailable, ""},
		{"read_failure", errors.New("read failed"), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := []string{}
			err := prepareRuntimeWith("/verified/runtime", "--normal", runtimePreparationDependencies{
				boundary: func() error { calls = append(calls, "boundary"); return nil },
				selected: func() error { calls = append(calls, "selected"); return tc.selected },
				enroll:   func(string) error { calls = append(calls, "enroll"); return nil },
				promote:  func(string, string) error { calls = append(calls, "promote"); return nil },
			})
			want := []string{"boundary", "selected"}
			if tc.want != "" {
				want = append(want, tc.want)
			}
			if !reflect.DeepEqual(calls, want) || (err == nil) != (tc.want != "") {
				t.Fatal(calls, err)
			}
		})
	}
	if err := prepareRuntimeWith("/verified", "--normal", runtimePreparationDependencies{
		boundary: func() error { return errors.New("active transaction") },
		selected: func() error { t.Fatal("read past failed boundary"); return nil },
	}); err == nil {
		t.Fatal("failed boundary admitted")
	}
}

type fakeLauncherRuntime struct {
	same                bool
	args                []string
	closeErr, errorExec error
}

func (r *fakeLauncherRuntime) VerifyExecutingBinary() error {
	if r.same {
		return nil
	}
	return errors.New("different executable")
}
func (r *fakeLauncherRuntime) ExecSelected(args []string) error {
	r.args = append([]string{}, args...)
	return r.errorExec
}
func (r *fakeLauncherRuntime) Close() error { return r.closeErr }

func TestLauncherResumesOnlyExplicitRecovery(t *testing.T) {
	for _, command := range [][]string{
		{"recover"}, {"material-root", "--snapshot", "snapshot"},
		{"verify-material-support", "--layout", "snapshot-name-sha256-v1"},
		{"recover", "--force"},
	} {
		resumed := false
		runtime := &fakeLauncherRuntime{}
		deps := launcherDependencies{
			isEntry:  func() (bool, error) { return true, nil },
			pending:  func() (bool, error) { return true, nil },
			resume:   func() error { resumed = true; return nil },
			selected: func() (launcherRuntime, error) { return runtime, nil },
		}
		if err := dispatchLauncherWith(command, deps); err != nil {
			t.Fatal(err)
		}
		if resumed != (len(command) == 1 && command[0] == "recover") || !reflect.DeepEqual(runtime.args, command) {
			t.Fatal(command, resumed, runtime.args)
		}
	}
}
func TestLauncherUnknownOrBusyDoesNotDispatch(t *testing.T) {
	for _, failure := range []string{"entry", "pending", "resume", "selected"} {
		t.Run(failure, func(t *testing.T) {
			unknown := errors.New("unknown")
			runtime := &fakeLauncherRuntime{}
			deps := launcherDependencies{
				isEntry: func() (bool, error) {
					if failure == "entry" {
						return false, unknown
					}
					return true, nil
				},
				pending: func() (bool, error) {
					if failure == "pending" {
						return false, unknown
					}
					return true, nil
				},
				resume: func() error {
					if failure == "resume" {
						return unknown
					}
					return nil
				},
				selected: func() (launcherRuntime, error) {
					if failure == "selected" {
						return nil, unknown
					}
					return runtime, nil
				},
			}
			if err := dispatchLauncherWith([]string{"recover"}, deps); err == nil || runtime.args != nil {
				t.Fatal(err, runtime.args)
			}
		})
	}
}
func TestLauncherPreservesIndependentObservationAndAvoidsRecursion(t *testing.T) {
	for _, args := range [][]string{{"status", "--request-id", "id"}, {"version"}, {"enroll-runtime"}, {"prepare-runtime"}, nil} {
		if launcherDispatchCommand(args) {
			t.Fatal("observation or admission routed through selected executor", args)
		}
	}
	if err := dispatchLauncherWith([]string{"material-root"}, launcherDependencies{isEntry: func() (bool, error) { return false, nil }}); err != nil {
		t.Fatal(err)
	}
	runtime := &fakeLauncherRuntime{same: true}
	if err := dispatchLauncherWith([]string{"material-root"}, launcherDependencies{
		isEntry:  func() (bool, error) { return true, nil },
		selected: func() (launcherRuntime, error) { return runtime, nil },
	}); err != nil || runtime.args != nil {
		t.Fatal("same binary dispatched recursively", err, runtime.args)
	}
}
