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
	masked        bool
}

type fakeBINDInstallSystemd struct {
	units    map[string]*fakeBINDInstallUnit
	commands []string
}

func newFakeBINDInstallSystemd(units map[string]*fakeBINDInstallUnit) *fakeBINDInstallSystemd {
	return &fakeBINDInstallSystemd{units: units}
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
	if command == "enable" && args[1] == "--runtime" {
		if len(args) != 3 {
			return nil, errors.New("incomplete runtime enable")
		}
		unitName = args[2]
	}
	unit := f.units[unitName]
	if unit == nil {
		return nil, fmt.Errorf("unknown unit %q", unitName)
	}
	switch command {
	case "show":
		loadState := unit.loadState
		unitFileState := unit.unitFileState
		if unit.masked {
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
		unit.masked = true
		return nil, nil
	case "unmask":
		unit.masked = false
		return nil, nil
	case "stop":
		unit.active = false
		return nil, nil
	case "reset-failed":
		unit.active = false
		return nil, nil
	case "start":
		if unit.masked {
			return []byte("unit is masked"), errors.New("start refused")
		}
		unit.active = true
		return nil, nil
	case "disable":
		unit.unitFileState = "disabled"
		return nil, nil
	case "enable":
		unit.unitFileState = "enabled"
		if len(args) == 3 && args[1] == "--runtime" {
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
	if output != "installed bind" || !installCalled || recoveries != 0 {
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
		if !unit.masked || unit.active {
			t.Errorf("%s terminal state masked=%v active=%v, want masked and stopped", name, unit.masked, unit.active)
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
	if bind9.masked || bind9.active || bind9.unitFileState == "enabled" || bind9.unitFileState == "enabled-runtime" {
		t.Fatalf("partially unpacked bind9 state=%+v, want unmasked, stopped and not enabled", bind9)
	}
	named := systemd.units["named.service"]
	if named.masked || !named.active || named.unitFileState != "enabled" {
		t.Fatalf("named rollback state=%+v, want original active+enabled state", named)
	}
	for _, command := range []string{"unmask named.service", "unmask bind9.service", "enable named.service", "start named.service"} {
		if commandIndex(systemd.commands, command) < 0 {
			t.Errorf("rollback command %q missing from %v", command, systemd.commands)
		}
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

func commandIndex(commands []string, want string) int {
	for index, command := range commands {
		if command == want {
			return index
		}
	}
	return -1
}
