package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const bindInstallRollbackTimeout = 30 * time.Second

var bindInstallUnitNames = []string{"bind9.service", "named.service"}

type bindInstallSystemdRunner func(context.Context, string, ...string) ([]byte, error)

type bindInstallRecoveryContextFactory func(context.Context) (context.Context, context.CancelFunc, error)

type bindInstallGuardOps struct {
	runSystemd      bindInstallSystemdRunner
	recoveryContext bindInstallRecoveryContextFactory
}

type bindInstallUnitState struct {
	name          string
	loadState     string
	activeState   string
	unitFileState string
}

func (s bindInstallUnitState) masked() bool {
	return s.loadState == "masked" || s.unitFileState == "masked" || s.unitFileState == "masked-runtime"
}

func (s bindInstallUnitState) active() bool {
	return s.activeState == "active"
}

func (s bindInstallUnitState) enabled() bool {
	return s.unitFileState == "enabled" || s.unitFileState == "enabled-runtime"
}

type bindPackageInstallGuard struct {
	systemctl string
	ops       bindInstallGuardOps
	before    []bindInstallUnitState
	ownedMask map[string]bool
}

// installBINDPackagesWithGuard prevents distro package-maintainer hooks from
// starting an unconfigured authoritative DNS daemon. The persistent masks are
// deliberately retained after a successful package transaction: the later,
// explicit CelikPanel BIND activation step owns unmask + enable + start.
//
// A failed package transaction instead restores only masks created here and
// reconciles the pre-install enabled/running state through the same durable
// service-mutation execution tracker as the package manager.
func installBINDPackagesWithGuard(
	ctx context.Context,
	systemctl string,
	install func() (string, error),
) (string, error) {
	return installBINDPackagesWithGuardOps(ctx, systemctl, install, bindInstallGuardOps{
		runSystemd: runServiceMutationCombinedOutput,
		recoveryContext: func(parent context.Context) (context.Context, context.CancelFunc, error) {
			return serviceMutationCancellingRecoveryContext(parent, bindInstallRollbackTimeout)
		},
	})
}

func installBINDPackagesWithGuardOps(
	ctx context.Context,
	systemctl string,
	install func() (string, error),
	ops bindInstallGuardOps,
) (string, error) {
	if ctx == nil || strings.TrimSpace(systemctl) == "" || install == nil ||
		ops.runSystemd == nil || ops.recoveryContext == nil {
		return "", errors.New("invalid BIND package install guard")
	}
	guard, beginErr := beginBINDPackageInstallGuard(ctx, systemctl, ops)
	if beginErr != nil {
		rollbackErr := restoreBINDPackageInstallGuard(ctx, guard)
		if rollbackErr != nil {
			return "", errors.Join(beginErr, fmt.Errorf("restore BIND unit state: %w", rollbackErr))
		}
		return "", beginErr
	}

	output, installErr := install()
	if installErr != nil {
		rollbackErr := restoreBINDPackageInstallGuard(ctx, guard)
		if rollbackErr != nil {
			return output, errors.Join(installErr, fmt.Errorf("restore BIND unit state: %w", rollbackErr))
		}
		return output, installErr
	}
	if err := guard.sealSuccessfulInstall(ctx); err != nil {
		return output, fmt.Errorf("BIND packages were installed but their units could not be left safely stopped: %w", err)
	}
	return output, nil
}

func beginBINDPackageInstallGuard(
	ctx context.Context,
	systemctl string,
	ops bindInstallGuardOps,
) (*bindPackageInstallGuard, error) {
	guard := &bindPackageInstallGuard{
		systemctl: systemctl,
		ops:       ops,
		ownedMask: make(map[string]bool, len(bindInstallUnitNames)),
	}
	for _, unit := range bindInstallUnitNames {
		state, err := guard.inspect(ctx, unit)
		if err != nil {
			return guard, fmt.Errorf("inspect %s before BIND install: %w", unit, err)
		}
		if state.activeState == "failed" {
			return guard, fmt.Errorf("inspect %s before BIND install: unit is failed and must be reconciled first", unit)
		}
		if state.masked() && state.active() {
			return guard, fmt.Errorf("inspect %s before BIND install: a masked active unit cannot be restored safely", unit)
		}
		guard.before = append(guard.before, state)
	}
	for _, state := range guard.before {
		if state.masked() {
			continue
		}
		guard.ownedMask[state.name] = true
		if err := guard.ensureMasked(ctx, state.name); err != nil {
			return guard, fmt.Errorf("guard %s before BIND install: %w", state.name, err)
		}
	}
	for _, state := range guard.before {
		if err := guard.ensureStopped(ctx, state.name); err != nil {
			return guard, fmt.Errorf("stop %s before BIND install: %w", state.name, err)
		}
	}
	return guard, nil
}

func restoreBINDPackageInstallGuard(ctx context.Context, guard *bindPackageInstallGuard) error {
	if guard == nil {
		return nil
	}
	recoveryCtx, cancel, err := guard.ops.recoveryContext(ctx)
	if err != nil {
		return fmt.Errorf("create bounded recovery context: %w", err)
	}
	defer cancel()
	return guard.restore(recoveryCtx)
}

func (g *bindPackageInstallGuard) restore(ctx context.Context) error {
	var restoreErrors []error
	// A failed package transaction may still have unpacked and started a unit.
	// Quiesce units which were not running before removing our masks.
	for _, state := range g.before {
		if !state.active() {
			if err := g.ensureStopped(ctx, state.name); err != nil {
				restoreErrors = append(restoreErrors, fmt.Errorf("stop %s: %w", state.name, err))
			}
		}
	}
	for i := len(g.before) - 1; i >= 0; i-- {
		state := g.before[i]
		if !g.ownedMask[state.name] {
			continue
		}
		if err := g.ensureUnmasked(ctx, state.name); err != nil {
			restoreErrors = append(restoreErrors, fmt.Errorf("unmask %s: %w", state.name, err))
		}
	}
	// Package hooks may have changed enablement underneath the temporary mask.
	// Reconcile the exact enabled-vs-disabled class captured before installation.
	for _, state := range g.before {
		if state.masked() {
			continue
		}
		if err := g.restoreEnabledState(ctx, state); err != nil {
			restoreErrors = append(restoreErrors, err)
		}
	}
	for _, state := range g.before {
		if err := g.restoreActiveState(ctx, state); err != nil {
			restoreErrors = append(restoreErrors, err)
		}
	}
	return errors.Join(restoreErrors...)
}

func (g *bindPackageInstallGuard) sealSuccessfulInstall(ctx context.Context) error {
	var sealErrors []error
	// Package hooks are untrusted to preserve the pre-install mask. Re-assert it
	// before stopping, so no dependency or preset can race a restart afterward.
	for _, state := range g.before {
		if err := g.ensureMasked(ctx, state.name); err != nil {
			sealErrors = append(sealErrors, fmt.Errorf("mask %s: %w", state.name, err))
		}
	}
	for _, state := range g.before {
		if err := g.ensureStopped(ctx, state.name); err != nil {
			sealErrors = append(sealErrors, fmt.Errorf("stop %s: %w", state.name, err))
		}
	}
	return errors.Join(sealErrors...)
}

func (g *bindPackageInstallGuard) restoreEnabledState(ctx context.Context, before bindInstallUnitState) error {
	args := []string{"disable", before.name}
	if before.enabled() {
		args = []string{"enable", before.name}
		if before.unitFileState == "enabled-runtime" {
			args = []string{"enable", "--runtime", before.name}
		}
	}
	output, commandErr := g.ops.runSystemd(ctx, g.systemctl, args...)
	after, inspectErr := g.inspect(ctx, before.name)
	if inspectErr == nil {
		if before.enabled() && after.unitFileState == before.unitFileState {
			return nil
		}
		if !before.enabled() && !after.enabled() {
			return nil
		}
	}
	return reconciledBINDSystemdError(args, output, commandErr, inspectErr, after)
}

func (g *bindPackageInstallGuard) restoreActiveState(ctx context.Context, before bindInstallUnitState) error {
	if before.active() {
		output, commandErr := g.ops.runSystemd(ctx, g.systemctl, "start", before.name)
		after, inspectErr := g.inspect(ctx, before.name)
		if inspectErr == nil && after.active() {
			return nil
		}
		return reconciledBINDSystemdError([]string{"start", before.name}, output, commandErr, inspectErr, after)
	}
	if err := g.ensureStopped(ctx, before.name); err != nil {
		return fmt.Errorf("restore stopped %s: %w", before.name, err)
	}
	return nil
}

func (g *bindPackageInstallGuard) ensureMasked(ctx context.Context, unit string) error {
	output, commandErr := g.ops.runSystemd(ctx, g.systemctl, "mask", unit)
	after, inspectErr := g.inspect(ctx, unit)
	if inspectErr == nil && after.masked() {
		return nil
	}
	return reconciledBINDSystemdError([]string{"mask", unit}, output, commandErr, inspectErr, after)
}

func (g *bindPackageInstallGuard) ensureUnmasked(ctx context.Context, unit string) error {
	output, commandErr := g.ops.runSystemd(ctx, g.systemctl, "unmask", unit)
	after, inspectErr := g.inspect(ctx, unit)
	if inspectErr == nil && !after.masked() {
		return nil
	}
	return reconciledBINDSystemdError([]string{"unmask", unit}, output, commandErr, inspectErr, after)
}

func (g *bindPackageInstallGuard) ensureStopped(ctx context.Context, unit string) error {
	before, err := g.inspect(ctx, unit)
	if err != nil {
		return err
	}
	if before.activeState == "inactive" {
		return nil
	}
	action := "stop"
	if before.activeState == "failed" {
		action = "reset-failed"
	}
	output, commandErr := g.ops.runSystemd(ctx, g.systemctl, action, unit)
	after, inspectErr := g.inspect(ctx, unit)
	if inspectErr == nil && after.activeState == "inactive" {
		return nil
	}
	return reconciledBINDSystemdError([]string{action, unit}, output, commandErr, inspectErr, after)
}

func (g *bindPackageInstallGuard) inspect(ctx context.Context, unit string) (bindInstallUnitState, error) {
	output, err := g.ops.runSystemd(
		ctx,
		g.systemctl,
		"show", unit,
		"--property=LoadState,ActiveState,UnitFileState",
		"--no-pager",
	)
	if err != nil {
		return bindInstallUnitState{}, fmt.Errorf("systemctl show failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	state := bindInstallUnitState{name: unit}
	seen := map[string]bool{}
	for _, line := range strings.Split(string(output), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch key {
		case "LoadState":
			state.loadState = value
			seen[key] = true
		case "ActiveState":
			state.activeState = value
			seen[key] = true
		case "UnitFileState":
			state.unitFileState = value
			seen[key] = true
		}
	}
	if !seen["LoadState"] || !seen["ActiveState"] || !seen["UnitFileState"] {
		return bindInstallUnitState{}, errors.New("systemctl show returned incomplete unit state")
	}
	if !validBINDInstallLoadState(state.loadState) || !validBINDInstallActiveState(state.activeState) ||
		!validBINDInstallUnitFileState(state.loadState, state.unitFileState) {
		return bindInstallUnitState{}, fmt.Errorf(
			"unsupported unit state load=%q active=%q unit-file=%q",
			state.loadState, state.activeState, state.unitFileState,
		)
	}
	return state, nil
}

func validBINDInstallLoadState(state string) bool {
	switch state {
	case "loaded", "not-found", "masked", "merged", "stub":
		return true
	default:
		return false
	}
}

func validBINDInstallActiveState(state string) bool {
	// Transitional or unknown states cannot be restored deterministically.
	switch state {
	case "active", "inactive", "failed":
		return true
	default:
		return false
	}
}

func validBINDInstallUnitFileState(loadState, state string) bool {
	if state == "" {
		return loadState == "not-found"
	}
	switch state {
	case "enabled", "enabled-runtime", "linked", "linked-runtime", "alias",
		"masked", "masked-runtime", "static", "disabled", "indirect", "generated", "transient":
		return true
	default:
		return false
	}
}

func reconciledBINDSystemdError(
	args []string,
	output []byte,
	commandErr error,
	inspectErr error,
	after bindInstallUnitState,
) error {
	parts := []string{fmt.Sprintf("systemctl %s did not reach the required state", strings.Join(args, " "))}
	if commandErr != nil {
		parts = append(parts, fmt.Sprintf("command: %v", commandErr))
	}
	if detail := strings.TrimSpace(string(output)); detail != "" {
		parts = append(parts, "output: "+detail)
	}
	if inspectErr != nil {
		parts = append(parts, fmt.Sprintf("readback: %v", inspectErr))
	} else {
		parts = append(parts, fmt.Sprintf(
			"readback: load=%s active=%s unit-file=%s",
			after.loadState, after.activeState, after.unitFileState,
		))
	}
	return errors.New(strings.Join(parts, "; "))
}
