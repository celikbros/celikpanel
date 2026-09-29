package main

import (
	"context"
	"errors"

	"github.com/alicelik/celikpanel/internal/dnsenginerecovery"
)

// A newly activated BIND target must be inactive, with dead SubState and zero
// systemd MainPID/ControlPID and an empty/absent native cgroup, before its
// configuration preimage is restored. This cannot exclude a later owner restart.
func verifyBINDTargetStoppedBeforeConfigRestoreWithOps(
	ctx context.Context,
	inspectUnit func(context.Context) (bindInstallUnitState, error),
	inspectProcesses func(context.Context) (dnsUnitProcesses, error),
	inspectCgroup func(context.Context) error,
) error {
	return verifyBINDTargetStoppedBeforeConfigRestoreForSourceWithOps(
		ctx, false, inspectUnit, inspectProcesses, inspectCgroup, nil,
	)
}

// A first-install journal (no source engine) also accepts the never-started
// target states: not-found, the package guard's persistent mask, or loaded,
// each without a public port-53 listener. A journal with a source keeps the
// loaded-unit proof above unchanged.
func verifyBINDTargetStoppedBeforeConfigRestoreForSourceWithOps(
	ctx context.Context,
	freshSource bool,
	inspectUnit func(context.Context) (bindInstallUnitState, error),
	inspectProcesses func(context.Context) (dnsUnitProcesses, error),
	inspectCgroup func(context.Context) error,
	inspectPublicDNSListeners func(context.Context) error,
) error {
	if inspectUnit == nil || inspectProcesses == nil || inspectCgroup == nil ||
		(freshSource && inspectPublicDNSListeners == nil) {
		return errors.New("BIND target stop proof requires native unit, process and listener observers")
	}
	verify := func(observe func(context.Context) (dnsenginerecovery.StoppedUnitObservation, error)) error {
		if freshSource {
			return dnsenginerecovery.VerifyStoppedFreshSourceTarget(
				ctx, "named.service", observe, inspectPublicDNSListeners,
			)
		}
		return dnsenginerecovery.VerifyStoppedUnit(ctx, "named.service", observe)
	}
	return verify(
		func(proofCtx context.Context) (dnsenginerecovery.StoppedUnitObservation, error) {
			unit, err := inspectUnit(proofCtx)
			if err != nil {
				return dnsenginerecovery.StoppedUnitObservation{}, err
			}
			processes, err := inspectProcesses(proofCtx)
			if err != nil {
				return dnsenginerecovery.StoppedUnitObservation{}, err
			}
			if err := inspectCgroup(proofCtx); err != nil {
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

func verifyBINDTargetStoppedBeforeConfigRestore(ctx context.Context, systemctl string, freshSource bool) error {
	guard := dnsSystemdStateGuard(systemctl)
	return verifyBINDTargetStoppedBeforeConfigRestoreForSourceWithOps(
		ctx, freshSource,
		func(proofCtx context.Context) (bindInstallUnitState, error) {
			return guard.inspect(proofCtx, "named.service")
		},
		func(proofCtx context.Context) (dnsUnitProcesses, error) {
			return inspectDNSUnitProcesses(proofCtx, systemctl, "named.service")
		},
		func(proofCtx context.Context) error {
			return dnsenginerecovery.ProbeEmptyUnitCgroup(proofCtx, "named.service", dnsenginerecovery.SystemdCgroupUnitRunner, dnsenginerecovery.NativeCgroupEvents)
		},
		proveNoPublicDNSPort53Listener,
	)
}
