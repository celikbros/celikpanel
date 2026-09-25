package main

import (
	"context"
	"errors"

	"github.com/alicelik/celikpanel/internal/dnsenginerecovery"
)

// A newly activated BIND target must be inactive, with dead SubState and zero
// systemd MainPID/ControlPID, before its configuration preimage is restored.
// This does not prove an empty cgroup or exclude an independent owner restart.
func verifyBINDTargetStoppedBeforeConfigRestoreWithOps(
	ctx context.Context,
	inspectUnit func(context.Context) (bindInstallUnitState, error),
	inspectProcesses func(context.Context) (dnsUnitProcesses, error),
) error {
	if inspectUnit == nil || inspectProcesses == nil {
		return errors.New("BIND target stop proof requires native unit and process observers")
	}
	return dnsenginerecovery.VerifyStoppedUnit(ctx, "named.service",
		func(proofCtx context.Context) (dnsenginerecovery.StoppedUnitObservation, error) {
			unit, err := inspectUnit(proofCtx)
			if err != nil {
				return dnsenginerecovery.StoppedUnitObservation{}, err
			}
			processes, err := inspectProcesses(proofCtx)
			if err != nil {
				return dnsenginerecovery.StoppedUnitObservation{}, err
			}
			return dnsenginerecovery.StoppedUnitObservation{
				Name: unit.name, LoadState: unit.loadState,
				ActiveState: unit.activeState, UnitFileState: unit.unitFileState,
				MainPID: processes.MainPID, ControlPID: processes.ControlPID,
				SubState: processes.SubState,
			}, nil
		})
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
