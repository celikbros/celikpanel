package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

type fakeBINDInstallUnit struct {
	loadState     string
	active        bool
	unitFileState string
	masked        bool // persistent mask
	runtimeMasked bool
}

type fakeBINDInstallSystemd struct {
	units    map[string]*fakeBINDInstallUnit
	commands []string
}

func newFakeBINDInstallSystemd(units map[string]*fakeBINDInstallUnit) *fakeBINDInstallSystemd {
	return &fakeBINDInstallSystemd{units: units}
}

func TestBINDSystemdStateInspectionRejectsAmbiguousOutput(t *testing.T) {
	canonical := "LoadState=loaded\nActiveState=inactive\nUnitFileState=disabled\n"
	for _, test := range []struct {
		name   string
		output string
	}{
		{name: "malformed", output: canonical + "warning\n"},
		{name: "unknown", output: canonical + "Evil=value\n"},
		{name: "duplicate", output: canonical + "LoadState=loaded\n"},
		{name: "leading-space", output: " LoadState=loaded\nActiveState=inactive\nUnitFileState=disabled\n"},
		{name: "trailing-space", output: "LoadState=loaded \nActiveState=inactive\nUnitFileState=disabled\n"},
		{name: "unicode-space", output: "\u00a0LoadState=loaded\nActiveState=inactive\nUnitFileState=disabled\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			guard := bindPackageInstallGuard{
				systemctl: "/usr/bin/systemctl",
				ops: bindInstallGuardOps{
					runSystemd: func(context.Context, string, ...string) ([]byte, error) {
						return []byte(test.output), nil
					},
				},
			}
			if _, err := guard.inspect(context.Background(), "named.service"); err == nil {
				t.Fatal("ambiguous systemctl state output was accepted")
			}
		})
	}
}

func (f *fakeBINDInstallSystemd) run(_ context.Context, executable string, args ...string) ([]byte, error) {
	if executable != "/usr/bin/systemctl" {
		return nil, fmt.Errorf("unexpected executable %q", executable)
	}
	f.commands = append(f.commands, strings.Join(args, " "))
	if len(args) < 2 {
		return nil, errors.New("incomplete systemctl command")
	}
	command := args[0]
	unitName := args[1]
	runtime := false
	if (command == "enable" || command == "mask" || command == "unmask") && args[1] == "--runtime" {
		if len(args) != 3 {
			return nil, errors.New("incomplete runtime systemctl command")
		}
		unitName = args[2]
		runtime = true
	}
	unit := f.units[unitName]
	if unit == nil {
		return nil, fmt.Errorf("unknown unit %q", unitName)
	}
	switch command {
	case "show":
		loadState := unit.loadState
		unitFileState := unit.unitFileState
		if unit.runtimeMasked {
			loadState = "masked"
			unitFileState = "masked-runtime"
		} else if unit.masked {
			loadState = "masked"
			unitFileState = "masked"
		}
		activeState := "inactive"
		if unit.active {
			activeState = "active"
		}
		return []byte(fmt.Sprintf(
			"LoadState=%s\nActiveState=%s\nUnitFileState=%s\n",
			loadState, activeState, unitFileState,
		)), nil
	case "mask":
		if runtime {
			unit.runtimeMasked = true
		} else {
			unit.masked = true
		}
		return nil, nil
	case "unmask":
		if runtime {
			unit.runtimeMasked = false
		} else {
			unit.masked = false
		}
		return nil, nil
	case "stop":
		unit.active = false
		return nil, nil
	case "reset-failed":
		unit.active = false
		return nil, nil
	case "start":
		if unit.masked || unit.runtimeMasked {
			return []byte("unit is masked"), errors.New("start refused")
		}
		unit.active = true
		return nil, nil
	case "disable":
		unit.unitFileState = "disabled"
		return nil, nil
	case "enable":
		unit.unitFileState = "enabled"
		if runtime {
			unit.unitFileState = "enabled-runtime"
		}
		return nil, nil
	default:
		return nil, fmt.Errorf("unexpected systemctl command %q", command)
	}
}

func fakeBINDInstallGuardOps(systemd *fakeBINDInstallSystemd, recoveries *int) bindInstallGuardOps {
	return bindInstallGuardOps{
		runSystemd: systemd.run,
		recoveryContext: func(context.Context) (context.Context, context.CancelFunc, error) {
			*recoveries++
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			return ctx, cancel, nil
		},
	}
}

func TestBINDPackageInstallMasksBothDistroUnitsBeforeFreshInstall(t *testing.T) {
	systemd := newFakeBINDInstallSystemd(map[string]*fakeBINDInstallUnit{
		"bind9.service": {loadState: "not-found", unitFileState: ""},
		"named.service": {loadState: "not-found", unitFileState: ""},
	})
	recoveries := 0
	installCalled := false
	output, err := installBINDPackagesWithGuardOps(
		context.Background(),
		"/usr/bin/systemctl",
		func() (string, error) {
			installCalled = true
			systemd.commands = append(systemd.commands, "PACKAGE_INSTALL")
			for _, unit := range systemd.units {
				unit.loadState = "loaded"
				unit.unitFileState = "enabled"
			}
			if _, startErr := systemd.run(context.Background(), "/usr/bin/systemctl", "start", "named.service"); startErr == nil {
				t.Fatal("package-maintainer start escaped the BIND mask")
			}
			return "installed bind", nil
		},
		fakeBINDInstallGuardOps(systemd, &recoveries),
	)
	if err != nil {
		t.Fatalf("guarded install failed: %v", err)
	}
	if output != "installed bind" || !installCalled || recoveries != 1 {
		t.Fatalf("output=%q installCalled=%v recoveries=%d", output, installCalled, recoveries)
	}
	packageIndex := commandIndex(systemd.commands, "PACKAGE_INSTALL")
	for _, command := range []string{"mask bind9.service", "mask named.service"} {
		index := commandIndex(systemd.commands, command)
		if index < 0 || index >= packageIndex {
			t.Fatalf("%q did not precede package install: %v", command, systemd.commands)
		}
	}
	for name, unit := range systemd.units {
		if !unit.masked || unit.runtimeMasked || unit.active {
			t.Errorf("%s terminal state persistent=%v runtime=%v active=%v, want exact persistent mask and stopped", name, unit.masked, unit.runtimeMasked, unit.active)
		}
	}
	for _, command := range systemd.commands {
		if strings.HasPrefix(command, "enable ") {
			t.Fatalf("generic BIND install enabled a unit: %v", systemd.commands)
		}
	}
}

func TestBINDPackageInstallFailureRestoresPriorUnitState(t *testing.T) {
	systemd := newFakeBINDInstallSystemd(map[string]*fakeBINDInstallUnit{
		"bind9.service": {loadState: "not-found", unitFileState: ""},
		"named.service": {loadState: "loaded", active: true, unitFileState: "enabled"},
	})
	recoveries := 0
	wantInstallErr := errors.New("apt transaction failed")
	_, err := installBINDPackagesWithGuardOps(
		context.Background(),
		"/usr/bin/systemctl",
		func() (string, error) {
			// Model a partial unpack and a package hook changing enablement under
			// the masks before apt reports failure.
			systemd.units["bind9.service"].loadState = "loaded"
			systemd.units["bind9.service"].unitFileState = "alias"
			systemd.units["named.service"].unitFileState = "disabled"
			return "partial apt output", wantInstallErr
		},
		fakeBINDInstallGuardOps(systemd, &recoveries),
	)
	if !errors.Is(err, wantInstallErr) {
		t.Fatalf("error=%v, want package failure", err)
	}
	if recoveries != 1 {
		t.Fatalf("recovery contexts=%d, want 1", recoveries)
	}
	bind9 := systemd.units["bind9.service"]
	if bind9.masked || bind9.runtimeMasked || bind9.active || bind9.unitFileState != "disabled" {
		t.Fatalf("partially unpacked bind9 state=%+v, want explicit unmasked+stopped+disabled compensation", bind9)
	}
	named := systemd.units["named.service"]
	if named.masked || named.runtimeMasked || !named.active || named.unitFileState != "enabled" {
		t.Fatalf("named rollback state=%+v, want original active+enabled state", named)
	}
	for _, command := range []string{"unmask named.service", "unmask bind9.service", "enable named.service", "start named.service"} {
		if commandIndex(systemd.commands, command) < 0 {
			t.Errorf("rollback command %q missing from %v", command, systemd.commands)
		}
	}
}

func TestBINDRestoreAbsentUnitAcceptsDisableRemovingUnitAlias(t *testing.T) {
	showCalls := 0
	disableCalls := 0
	guard := bindPackageInstallGuard{
		systemctl: "/usr/bin/systemctl",
		ops: bindInstallGuardOps{
			runSystemd: func(_ context.Context, executable string, args ...string) ([]byte, error) {
				if executable != "/usr/bin/systemctl" {
					return nil, fmt.Errorf("unexpected executable %q", executable)
				}
				if len(args) >= 2 && args[0] == "show" {
					showCalls++
					if showCalls == 1 {
						return []byte("LoadState=loaded\nActiveState=inactive\nUnitFileState=alias\n"), nil
					}
					return []byte("LoadState=not-found\nActiveState=inactive\nUnitFileState=\n"), nil
				}
				if len(args) == 2 && args[0] == "disable" && args[1] == "bind9.service" {
					disableCalls++
					return []byte("Removed unit alias"), nil
				}
				return nil, fmt.Errorf("unexpected systemctl args %q", args)
			},
		},
	}
	before := bindInstallUnitState{
		name:          "bind9.service",
		loadState:     "not-found",
		activeState:   "inactive",
		unitFileState: "",
	}

	if err := guard.restoreUnitFileState(context.Background(), before); err != nil {
		t.Fatalf("restore rejected exact absent readback after alias removal: %v", err)
	}
	if showCalls != 2 || disableCalls != 1 {
		t.Fatalf("show calls=%d disable calls=%d, want 2 and 1", showCalls, disableCalls)
	}
}

func TestBINDPackageInstallFailurePreservesPreexistingMask(t *testing.T) {
	systemd := newFakeBINDInstallSystemd(map[string]*fakeBINDInstallUnit{
		"bind9.service": {loadState: "loaded", unitFileState: "disabled", masked: true},
		"named.service": {loadState: "not-found", unitFileState: ""},
	})
	recoveries := 0
	_, err := installBINDPackagesWithGuardOps(
		context.Background(),
		"/usr/bin/systemctl",
		func() (string, error) { return "", errors.New("pacman failed") },
		fakeBINDInstallGuardOps(systemd, &recoveries),
	)
	if err == nil {
		t.Fatal("failed package transaction reported success")
	}
	if !systemd.units["bind9.service"].masked {
		t.Fatal("rollback removed a BIND mask it did not create")
	}
	if commandIndex(systemd.commands, "unmask bind9.service") >= 0 {
		t.Fatalf("preexisting mask was unmasked: %v", systemd.commands)
	}
}

func TestBINDPackageInstallFailureRestoresExactRuntimeMask(t *testing.T) {
	systemd := newFakeBINDInstallSystemd(map[string]*fakeBINDInstallUnit{
		"bind9.service": {loadState: "loaded", unitFileState: "disabled", runtimeMasked: true},
		"named.service": {loadState: "not-found", unitFileState: ""},
	})
	recoveries := 0
	wantErr := errors.New("apt failed")
	_, err := installBINDPackagesWithGuardOps(
		context.Background(),
		"/usr/bin/systemctl",
		func() (string, error) {
			// Simulate a hostile package hook replacing the runtime mask with a
			// persistent one; rollback must recover the exact original class.
			unit := systemd.units["bind9.service"]
			unit.runtimeMasked = false
			unit.masked = true
			return "partial", wantErr
		},
		fakeBINDInstallGuardOps(systemd, &recoveries),
	)
	if !errors.Is(err, wantErr) {
		t.Fatalf("error=%v, want package failure", err)
	}
	unit := systemd.units["bind9.service"]
	if unit.masked || !unit.runtimeMasked || unit.active {
		t.Fatalf("runtime mask rollback state=%+v, want exact masked-runtime+inactive", unit)
	}
	for _, command := range []string{"mask --runtime bind9.service", "unmask bind9.service"} {
		if commandIndex(systemd.commands, command) < 0 {
			t.Errorf("exact runtime mask recovery command %q missing: %v", command, systemd.commands)
		}
	}
}

func TestBINDPackageInstallSuccessSealUsesDetachedTrackedContext(t *testing.T) {
	systemd := newFakeBINDInstallSystemd(map[string]*fakeBINDInstallUnit{
		"bind9.service": {loadState: "not-found", unitFileState: ""},
		"named.service": {loadState: "not-found", unitFileState: ""},
	})
	requestCtx, cancelRequest := context.WithCancel(context.Background())
	recoveries := 0
	sealStarted := false
	ops := fakeBINDInstallGuardOps(systemd, &recoveries)
	baseRun := ops.runSystemd
	installReturned := false
	ops.runSystemd = func(ctx context.Context, executable string, args ...string) ([]byte, error) {
		if installReturned {
			sealStarted = true
			if ctx.Err() != nil {
				return nil, fmt.Errorf("seal inherited canceled request: %w", ctx.Err())
			}
			if _, ok := ctx.Deadline(); !ok {
				return nil, errors.New("seal context is not bounded")
			}
		}
		return baseRun(ctx, executable, args...)
	}
	_, err := installBINDPackagesWithGuardOps(
		requestCtx,
		"/usr/bin/systemctl",
		func() (string, error) {
			for _, unit := range systemd.units {
				unit.loadState = "loaded"
				unit.unitFileState = "enabled"
			}
			cancelRequest()
			installReturned = true
			return "installed", nil
		},
		ops,
	)
	if err != nil {
		t.Fatalf("detached success seal failed: %v", err)
	}
	if recoveries != 1 || !sealStarted {
		t.Fatalf("recoveries=%d sealStarted=%v, want tracked seal context", recoveries, sealStarted)
	}
}

func TestBINDPackageInstallSuccessRejectsUnprovenSeal(t *testing.T) {
	systemd := newFakeBINDInstallSystemd(map[string]*fakeBINDInstallUnit{
		"bind9.service": {loadState: "not-found", unitFileState: ""},
		"named.service": {loadState: "not-found", unitFileState: ""},
	})
	recoveries := 0
	ops := fakeBINDInstallGuardOps(systemd, &recoveries)
	baseRun := ops.runSystemd
	installReturned := false
	ops.runSystemd = func(ctx context.Context, executable string, args ...string) ([]byte, error) {
		output, err := baseRun(ctx, executable, args...)
		if installReturned && len(args) == 3 && args[0] == "unmask" && args[1] == "--runtime" {
			// Model state changing immediately after systemctl claimed success.
			systemd.units[args[2]].masked = false
		}
		return output, err
	}
	_, err := installBINDPackagesWithGuardOps(
		context.Background(),
		"/usr/bin/systemctl",
		func() (string, error) {
			for _, unit := range systemd.units {
				unit.loadState = "loaded"
				unit.unitFileState = "enabled"
			}
			installReturned = true
			return "installed", nil
		},
		ops,
	)
	if err == nil || !strings.Contains(err.Error(), "could not be left safely stopped") {
		t.Fatalf("error=%v, want exact seal proof failure", err)
	}
}

func TestBINDPackageInstallFailsClosedBeforePackageMutationOnUnstableState(t *testing.T) {
	systemd := newFakeBINDInstallSystemd(map[string]*fakeBINDInstallUnit{
		"bind9.service": {loadState: "loaded", unitFileState: "disabled"},
		"named.service": {loadState: "not-found", unitFileState: ""},
	})
	// An unknown/transitional state is emitted directly by this wrapper; the
	// fake's normal state machine intentionally exposes only stable states.
	run := systemd.run
	recoveries := 0
	installCalled := false
	ops := fakeBINDInstallGuardOps(systemd, &recoveries)
	ops.runSystemd = func(ctx context.Context, executable string, args ...string) ([]byte, error) {
		if len(args) > 1 && args[0] == "show" && args[1] == "bind9.service" {
			return []byte("LoadState=loaded\nActiveState=activating\nUnitFileState=disabled\n"), nil
		}
		return run(ctx, executable, args...)
	}
	_, err := installBINDPackagesWithGuardOps(
		context.Background(),
		"/usr/bin/systemctl",
		func() (string, error) {
			installCalled = true
			return "", nil
		},
		ops,
	)
	if err == nil || !strings.Contains(err.Error(), "unsupported unit state") {
		t.Fatalf("error=%v, want fail-closed state rejection", err)
	}
	if installCalled {
		t.Fatal("package manager ran after the BIND unit state became untrustworthy")
	}
}

func TestBINDPackageInstallRejectsUnrestorableUnitFileStatesBeforeMutation(t *testing.T) {
	for _, state := range []string{"alias", "linked", "linked-runtime", "static", "indirect", "generated", "transient"} {
		t.Run(state, func(t *testing.T) {
			systemd := newFakeBINDInstallSystemd(map[string]*fakeBINDInstallUnit{
				"bind9.service": {loadState: "loaded", unitFileState: state},
				"named.service": {loadState: "not-found", unitFileState: ""},
			})
			recoveries := 0
			installCalled := false
			_, err := installBINDPackagesWithGuardOps(
				context.Background(), "/usr/bin/systemctl",
				func() (string, error) { installCalled = true; return "", nil },
				fakeBINDInstallGuardOps(systemd, &recoveries),
			)
			if err == nil || !strings.Contains(err.Error(), "unsupported unit state") {
				t.Fatalf("state %q error=%v, want fail-closed rejection", state, err)
			}
			if installCalled {
				t.Fatalf("package manager ran for unrestorable state %q", state)
			}
		})
	}
}

func commandIndex(commands []string, want string) int {
	for index, command := range commands {
		if command == want {
			return index
		}
	}
	return -1
}
