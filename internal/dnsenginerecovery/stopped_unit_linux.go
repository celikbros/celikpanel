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
func ProbeStoppedUnit(
	ctx context.Context,
	name string,
	unitRunner NativeUnitRunner,
	runtimeRunner BINDRuntimeRunner,
) error {
	if ctx == nil || unitRunner == nil || runtimeRunner == nil ||
		(name != "named.service" && name != "pdns.service") {
		return errors.New("invalid independent DNS stopped-unit probe")
	}
	return VerifyStoppedUnit(ctx, name,
		func(proofCtx context.Context) (StoppedUnitObservation, error) {
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
			unit := units[0]
			return StoppedUnitObservation{
				Name: unit.Name, LoadState: unit.LoadState,
				ActiveState: unit.ActiveState, UnitFileState: unit.UnitFileState,
				MainPID: processes.MainPID, ControlPID: processes.ControlPID,
				SubState: processes.SubState,
			}, nil
		})
}
