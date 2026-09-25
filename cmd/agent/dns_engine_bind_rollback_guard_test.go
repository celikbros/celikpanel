package main

import (
	"context"
	"errors"
	"testing"
)

func TestBINDRollbackTargetStopProof(t *testing.T) {
	stopped := bindInstallUnitState{name: "named.service", loadState: "loaded", activeState: "inactive", unitFileState: "disabled"}
	processes := dnsUnitProcesses{SubState: "dead"}
	calls := 0
	inspectUnit := func(context.Context) (bindInstallUnitState, error) {
		calls++
		return stopped, nil
	}
	inspectProcesses := func(context.Context) (dnsUnitProcesses, error) { return processes, nil }
	cgroupCalls := 0
	inspectCgroup := func(context.Context) error { cgroupCalls++; return nil }
	if err := verifyBINDTargetStoppedBeforeConfigRestoreWithOps(context.Background(), inspectUnit, inspectProcesses, inspectCgroup); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || cgroupCalls != 2 {
		t.Fatalf("unit/cgroup observations = %d/%d, want two each", calls, cgroupCalls)
	}
	for _, tc := range []struct {
		name      string
		unit      bindInstallUnitState
		processes dnsUnitProcesses
	}{
		{"still-active", bindInstallUnitState{name: "named.service", activeState: "active"}, processes},
		{"wrong-unit", bindInstallUnitState{name: "bind9.service", activeState: "inactive"}, processes},
		{"main-process", stopped, dnsUnitProcesses{MainPID: 42, SubState: "dead"}},
		{"control-process", stopped, dnsUnitProcesses{ControlPID: 43, SubState: "dead"}},
		{"not-dead", stopped, dnsUnitProcesses{SubState: "running"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := verifyBINDTargetStoppedBeforeConfigRestoreWithOps(
				context.Background(),
				func(context.Context) (bindInstallUnitState, error) { return tc.unit, nil },
				func(context.Context) (dnsUnitProcesses, error) { return tc.processes, nil },
				inspectCgroup,
			); err == nil {
				t.Fatal("unsafe target was accepted")
			}
		})
	}
	changed := 0
	if err := verifyBINDTargetStoppedBeforeConfigRestoreWithOps(
		context.Background(),
		func(context.Context) (bindInstallUnitState, error) {
			changed++
			next := stopped
			if changed == 2 {
				next.unitFileState = "enabled"
			}
			return next, nil
		},
		inspectProcesses,
		inspectCgroup,
	); err == nil {
		t.Fatal("changed unit was accepted")
	}
	seenProcesses := 0
	if err := verifyBINDTargetStoppedBeforeConfigRestoreWithOps(
		context.Background(),
		inspectUnit,
		func(context.Context) (dnsUnitProcesses, error) {
			seenProcesses++
			if seenProcesses == 2 {
				return dnsUnitProcesses{ControlPID: 44, SubState: "dead"}, nil
			}
			return processes, nil
		},
		inspectCgroup,
	); err == nil {
		t.Fatal("process appearing between reads was accepted")
	}
	cgroupReads := 0
	if err := verifyBINDTargetStoppedBeforeConfigRestoreWithOps(context.Background(), inspectUnit, inspectProcesses,
		func(context.Context) error {
			cgroupReads++
			if cgroupReads == 2 {
				return errors.New("cgroup populated")
			}
			return nil
		}); err == nil {
		t.Fatal("target cgroup becoming populated was accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := verifyBINDTargetStoppedBeforeConfigRestoreWithOps(ctx, inspectUnit, inspectProcesses, inspectCgroup); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled proof = %v", err)
	}
}
