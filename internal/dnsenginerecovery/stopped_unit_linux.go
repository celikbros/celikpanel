//go:build linux

package dnsenginerecovery

import (
	"context"
	"errors"
	"fmt"
)

// ProbeStoppedUnit binds the shared stop predicate to the independent,
// fixed-name systemd readers. It makes no native change and is not recovery
// admission without exact operation, worker, owner and lock proofs.
func ProbeStoppedUnit(ctx context.Context, name string, unitRunner NativeUnitRunner, runtimeRunner BINDRuntimeRunner) error {
	return probeStoppedUnitWithCgroup(ctx, name, unitRunner, runtimeRunner,
		func(proofCtx context.Context, unit string) error {
			return ProbeEmptyUnitCgroup(proofCtx, unit, SystemdCgroupUnitRunner, NativeCgroupEvents)
		})
}

// ProbeStoppedPDNSPersistentMask includes the same native process and cgroup
// proof as ProbeStoppedUnit, but admits only the V3 sealed persistent mask.
func ProbeStoppedPDNSPersistentMask(ctx context.Context) error {
	return probeStoppedUnitWithCgroupClass(ctx, "pdns.service", SystemdUnitRunner, SystemdPDNSRuntimeRunner,
		func(proofCtx context.Context, unit string) error {
			return ProbeEmptyUnitCgroup(proofCtx, unit, SystemdCgroupUnitRunner, NativeCgroupEvents)
		}, true)
}

func probeStoppedUnitWithCgroup(
	ctx context.Context,
	name string,
	unitRunner NativeUnitRunner,
	runtimeRunner BINDRuntimeRunner,
	cgroup func(context.Context, string) error,
) error {
	return probeStoppedUnitWithCgroupClass(ctx, name, unitRunner, runtimeRunner, cgroup, false)
}

func probeStoppedUnitWithCgroupClass(
	ctx context.Context,
	name string,
	unitRunner NativeUnitRunner,
	runtimeRunner BINDRuntimeRunner,
	cgroup func(context.Context, string) error,
	persistentMask bool,
) error {
	if ctx == nil || unitRunner == nil || runtimeRunner == nil || cgroup == nil ||
		(name != "named.service" && name != "pdns.service") {
		return errors.New("invalid independent DNS stopped-unit probe")
	}
	verify := func(observe func(context.Context) (StoppedUnitObservation, error)) error {
		if persistentMask {
			return VerifyStoppedPDNSPersistentMask(ctx, observe)
		}
		return VerifyStoppedUnit(ctx, name, observe)
	}
	return verify(func(proofCtx context.Context) (StoppedUnitObservation, error) {
		units, err := ProbeNativeUnits(proofCtx, []string{name}, unitRunner)
		if err != nil {
			return StoppedUnitObservation{}, fmt.Errorf("read DNS target unit: %w", err)
		}
		raw, err := runtimeRunner(proofCtx, name)
		if err != nil {
			return StoppedUnitObservation{}, fmt.Errorf("read DNS target process state: %w", err)
		}
		processes, err := parseUnitRuntime(raw)
		if err != nil {
			return StoppedUnitObservation{}, fmt.Errorf("decode DNS target process state: %w", err)
		}
		if err := cgroup(proofCtx, name); err != nil {
			return StoppedUnitObservation{}, fmt.Errorf("prove DNS target cgroup empty: %w", err)
		}
		unit := units[0]
		return StoppedUnitObservation{
			Name: unit.Name, LoadState: unit.LoadState,
			ActiveState: unit.ActiveState, UnitFileState: unit.UnitFileState,
			MainPID: processes.MainPID, ControlPID: processes.ControlPID,
			SubState: processes.SubState,
		}, nil
	})
}
