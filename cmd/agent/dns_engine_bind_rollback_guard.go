package main

import (
	"context"
	"errors"
)

// A newly activated BIND target must be inactive, with dead SubState and zero
// systemd MainPID/ControlPID, before its configuration preimage is restored.
// This does not prove an empty cgroup or exclude an independent owner restart.
func verifyBINDTargetStoppedBeforeConfigRestoreWithOps(
	ctx context.Context,
	inspectUnit func(context.Context) (bindInstallUnitState, error),
	inspectProcesses func(context.Context) (dnsUnitProcesses, error),
) error {
	if ctx == nil || inspectUnit == nil || inspectProcesses == nil {
		return errors.New("BIND target stop proof requires native unit and process observers")
	}
	observe := func() (bindInstallUnitState, dnsUnitProcesses, error) {
		if err := ctx.Err(); err != nil {
			return bindInstallUnitState{}, dnsUnitProcesses{}, err
		}
		unit, err := inspectUnit(ctx)
		if err != nil {
			return bindInstallUnitState{}, dnsUnitProcesses{}, err
		}
		if unit.name != "named.service" || unit.activeState != "inactive" {
			return bindInstallUnitState{}, dnsUnitProcesses{},
				errors.New("BIND target is not an inactive named.service")
		}
		processes, err := inspectProcesses(ctx)
		if err != nil {
			return bindInstallUnitState{}, dnsUnitProcesses{}, err
		}
		if err := verifyDNSUnitProcessesStopped(processes); err != nil {
			return bindInstallUnitState{}, dnsUnitProcesses{}, err
		}
		return unit, processes, nil
	}
	beforeUnit, beforeProcesses, err := observe()
	if err != nil {
		return err
	}
	afterUnit, afterProcesses, err := observe()
	if err != nil {
		return err
	}
	if beforeUnit != afterUnit || beforeProcesses != afterProcesses {
		return errors.New("BIND target unit or process changed during stopped proof")
	}
	return nil
}

func verifyBINDTargetStoppedBeforeConfigRestore(ctx context.Context, systemctl string) error {
	guard := dnsSystemdStateGuard(systemctl)
	return verifyBINDTargetStoppedBeforeConfigRestoreWithOps(
		ctx,
		func(proofCtx context.Context) (bindInstallUnitState, error) {
			return guard.inspect(proofCtx, "named.service")
		},
		func(proofCtx context.Context) (dnsUnitProcesses, error) {
			return inspectDNSUnitProcesses(proofCtx, systemctl, "named.service")
		},
	)
}
