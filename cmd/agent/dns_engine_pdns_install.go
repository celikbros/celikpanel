package main

import (
	"context"
	"errors"
	"fmt"
)

var pdnsInstallUnitNames = []string{"pdns.service"}

// installPDNSPackagesWithGuard keeps package-maintainer hooks from starting
// PowerDNS before its exact standalone database and config are staged. A
// successful install deliberately leaves pdns.service stopped and persistently
// masked; only the engine activation transaction may unmask it.
func installPDNSPackagesWithGuard(
	ctx context.Context,
	systemctl string,
	install func() (string, error),
	exactBefore ...dnsUnitSnapshot,
) (string, error) {
	if len(exactBefore) > 1 {
		return "", errors.New("PowerDNS package guard accepts at most one exact preimage")
	}
	var expected *dnsUnitSnapshot
	if len(exactBefore) == 1 {
		expected = &exactBefore[0]
	}
	return installPDNSPackagesWithGuardOpsExact(ctx, systemctl, install, bindInstallGuardOps{
		verifyMaskParent: verifyBINDMaskParentMetadata,
		runSystemd:       runServiceMutationCombinedOutput,
		recoveryContext: func(parent context.Context) (context.Context, context.CancelFunc, error) {
			return serviceMutationCancellingRecoveryContext(parent, bindInstallRollbackTimeout)
		},
	}, expected)
}

func installPDNSPackagesWithGuardOps(
	ctx context.Context,
	systemctl string,
	install func() (string, error),
	ops bindInstallGuardOps,
) (string, error) {
	return installPDNSPackagesWithGuardOpsExact(ctx, systemctl, install, ops, nil)
}

func installPDNSPackagesWithGuardOpsExact(
	ctx context.Context,
	systemctl string,
	install func() (string, error),
	ops bindInstallGuardOps,
	exactBefore *dnsUnitSnapshot,
) (string, error) {
	guard, err := beginDNSPackageInstallGuard(ctx, systemctl, pdnsInstallUnitNames, ops, exactBefore)
	if err != nil {
		return "", err
	}
	output, installErr := install()
	if installErr != nil {
		return output, errors.Join(installErr, restoreBINDPackageInstallGuard(ctx, guard))
	}
	if err := sealSuccessfulBINDPackageInstall(ctx, guard); err != nil {
		return output, fmt.Errorf("PowerDNS packages were installed but the unit could not be sealed: %w", err)
	}
	return output, nil
}

func beginDNSPackageInstallGuard(
	ctx context.Context,
	systemctl string,
	units []string,
	ops bindInstallGuardOps,
	exactBefore *dnsUnitSnapshot,
) (*bindPackageInstallGuard, error) {
	if ctx == nil || systemctl == "" || len(units) == 0 ||
		ops.verifyMaskParent == nil || ops.runSystemd == nil ||
		ops.recoveryContext == nil {
		return nil, errors.New("invalid DNS package install guard")
	}
	guard := &bindPackageInstallGuard{
		systemctl: systemctl, ops: ops, ownedMask: make(map[string]bool, len(units)),
	}
	for _, unit := range units {
		state, err := guard.inspect(ctx, unit)
		if err != nil {
			return guard, fmt.Errorf("inspect %s before DNS package install: %w", unit, err)
		}
		if state.activeState == "failed" || (state.masked() && state.active()) {
			return guard, fmt.Errorf("%s has no deterministic pre-install state", unit)
		}
		guard.before = append(guard.before, state)
	}
	if exactBefore != nil {
		if len(guard.before) != 1 {
			return guard, errors.New("fresh PowerDNS package guard requires one target unit")
		}
		observed := guard.before[0]
		actual := dnsUnitSnapshot{
			Name: observed.name, LoadState: observed.loadState,
			ActiveState: observed.activeState, UnitFileState: observed.unitFileState,
		}
		if err := validateFreshPDNSTargetBeforePackagesV3(actual); err != nil {
			return guard, err
		}
		if actual != *exactBefore {
			return guard, errors.New("fresh PowerDNS target changed before package guard ownership")
		}
	}
	if err := guard.verifyMaskParentBeforeSystemdMutation(); err != nil {
		return guard, err
	}
	for _, state := range guard.before {
		if state.masked() {
			continue
		}
		guard.ownedMask[state.name] = true
		if err := guard.ensureMasked(ctx, state.name); err != nil {
			return guard, errors.Join(err, restoreBINDPackageInstallGuard(ctx, guard))
		}
	}
	for _, state := range guard.before {
		if err := guard.ensureStopped(ctx, state.name); err != nil {
			return guard, errors.Join(err, restoreBINDPackageInstallGuard(ctx, guard))
		}
	}
	return guard, nil
}
