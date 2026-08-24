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
	if err := sealSuccessfulBINDPackageInstall(ctx, guard); err != nil {
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

func sealSuccessfulBINDPackageInstall(ctx context.Context, guard *bindPackageInstallGuard) error {
	if guard == nil {
		return errors.New("missing BIND package install guard")
	}
	recoveryCtx, cancel, err := guard.ops.recoveryContext(ctx)
	if err != nil {
		return fmt.Errorf("create bounded recovery context: %w", err)
	}
	defer cancel()
	return guard.sealSuccessfulInstall(recoveryCtx)
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
	// Reconcile only unit-file states for which systemd exposes an exact inverse.
	for _, state := range g.before {
		if state.masked() {
			continue
		}
		if err := g.restoreUnitFileState(ctx, state); err != nil {
			restoreErrors = append(restoreErrors, err)
		}
	}
	// Preexisting masks are not owned by the guard. Reassert their exact
	// persistent-vs-runtime class in case a package hook replaced it.
	for _, state := range g.before {
		if !state.masked() {
			continue
		}
		if err := g.restoreExactMaskState(ctx, state); err != nil {
			restoreErrors = append(restoreErrors, err)
		}
	}
	for _, state := range g.before {
		if err := g.restoreActiveState(ctx, state); err != nil {
			restoreErrors = append(restoreErrors, err)
		}
	}
	for _, state := range g.before {
		if err := g.verifyRestoredState(ctx, state); err != nil {
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
		if err := g.ensurePersistentMasked(ctx, state.name); err != nil {
			sealErrors = append(sealErrors, fmt.Errorf("mask %s: %w", state.name, err))
		}
	}
	for _, state := range g.before {
		if err := g.ensureStopped(ctx, state.name); err != nil {
			sealErrors = append(sealErrors, fmt.Errorf("stop %s: %w", state.name, err))
		}
	}
	for _, state := range g.before {
		after, err := g.inspect(ctx, state.name)
		if err != nil {
			sealErrors = append(sealErrors, fmt.Errorf("prove %s sealed: %w", state.name, err))
			continue
		}
		if after.loadState != "masked" || after.unitFileState != "masked" || after.activeState != "inactive" {
			sealErrors = append(sealErrors, fmt.Errorf(
				"prove %s sealed: load=%s active=%s unit-file=%s, want masked/inactive/masked",
				state.name, after.loadState, after.activeState, after.unitFileState,
			))
		}
	}
	return errors.Join(sealErrors...)
}

func (g *bindPackageInstallGuard) restoreUnitFileState(ctx context.Context, before bindInstallUnitState) error {
	if before.loadState == "not-found" && before.unitFileState == "" {
		after, inspectErr := g.inspect(ctx, before.name)
		if inspectErr == nil && after.loadState == "not-found" && after.unitFileState == "" {
			return nil
		}
		// A failed package manager may have left a newly unpacked unit behind.
		// Package absence cannot be reconstructed here; the explicit safe
		// compensation is exact stopped+disabled+unmasked state.
		args := []string{"disable", before.name}
		output, commandErr := g.ops.runSystemd(ctx, g.systemctl, args...)
		after, inspectErr = g.inspect(ctx, before.name)
		// Disabling a distro alias may remove the alias entirely. That exact
		// absent readback restores the original state without compensation.
		if inspectErr == nil && after.loadState == "not-found" && after.unitFileState == "" {
			return nil
		}
		if inspectErr == nil && after.unitFileState == "disabled" && !after.masked() {
			return nil
		}
		return reconciledBINDSystemdError(args, output, commandErr, inspectErr, after)
	}
	var args []string
	switch before.unitFileState {
	case "enabled":
		args = []string{"enable", before.name}
	case "enabled-runtime":
		args = []string{"enable", "--runtime", before.name}
	case "disabled":
		args = []string{"disable", before.name}
	default:
		return fmt.Errorf("restore %s: unit-file state %q has no exact inverse", before.name, before.unitFileState)
	}
	output, commandErr := g.ops.runSystemd(ctx, g.systemctl, args...)
	after, inspectErr := g.inspect(ctx, before.name)
	if inspectErr == nil && after.unitFileState == before.unitFileState && !after.masked() {
		return nil
	}
	return reconciledBINDSystemdError(args, output, commandErr, inspectErr, after)
}

func (g *bindPackageInstallGuard) restoreExactMaskState(ctx context.Context, before bindInstallUnitState) error {
	switch before.unitFileState {
	case "masked":
		return g.ensurePersistentMasked(ctx, before.name)
	case "masked-runtime":
		// Establish the runtime mask before removing any persistent mask so the
		// unit is never exposed between commands.
		firstOutput, firstErr := g.ops.runSystemd(ctx, g.systemctl, "mask", "--runtime", before.name)
		secondOutput, secondErr := g.ops.runSystemd(ctx, g.systemctl, "unmask", before.name)
		after, inspectErr := g.inspect(ctx, before.name)
		if inspectErr == nil && after.loadState == "masked" && after.unitFileState == "masked-runtime" {
			return nil
		}
		return errors.Join(
			reconciledBINDSystemdError([]string{"mask", "--runtime", before.name}, firstOutput, firstErr, inspectErr, after),
			reconciledBINDSystemdError([]string{"unmask", before.name}, secondOutput, secondErr, inspectErr, after),
		)
	default:
		return fmt.Errorf("restore %s: invalid preexisting mask state %q", before.name, before.unitFileState)
	}
}

func (g *bindPackageInstallGuard) verifyRestoredState(ctx context.Context, before bindInstallUnitState) error {
	after, err := g.inspect(ctx, before.name)
	if err != nil {
		return fmt.Errorf("verify restored %s: %w", before.name, err)
	}
	wantActive := before.activeState
	if after.activeState != wantActive {
		return fmt.Errorf("verify restored %s: active=%s, want %s", before.name, after.activeState, wantActive)
	}
	if before.loadState == "not-found" && before.unitFileState == "" {
		if (after.loadState == "not-found" && after.unitFileState == "") ||
			(after.unitFileState == "disabled" && !after.masked()) {
			return nil
		}
		return fmt.Errorf("verify safe compensation for %s: load=%s unit-file=%s", before.name, after.loadState, after.unitFileState)
	}
	if after.unitFileState != before.unitFileState {
		return fmt.Errorf("verify restored %s: unit-file=%s, want %s", before.name, after.unitFileState, before.unitFileState)
	}
	return nil
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

func (g *bindPackageInstallGuard) ensurePersistentMasked(ctx context.Context, unit string) error {
	// Create the persistent mask first, then remove a possible runtime mask.
	// At least one mask therefore exists throughout the normalization.
	firstOutput, firstErr := g.ops.runSystemd(ctx, g.systemctl, "mask", unit)
	secondOutput, secondErr := g.ops.runSystemd(ctx, g.systemctl, "unmask", "--runtime", unit)
	after, inspectErr := g.inspect(ctx, unit)
	if inspectErr == nil && after.loadState == "masked" && after.unitFileState == "masked" {
		return nil
	}
	return errors.Join(
		reconciledBINDSystemdError([]string{"mask", unit}, firstOutput, firstErr, inspectErr, after),
		reconciledBINDSystemdError([]string{"unmask", "--runtime", unit}, secondOutput, secondErr, inspectErr, after),
	)
}

func (g *bindPackageInstallGuard) ensureUnmasked(ctx context.Context, unit string) error {
	output, commandErr := g.ops.runSystemd(ctx, g.systemctl, "unmask", unit)
	runtimeOutput, runtimeErr := g.ops.runSystemd(ctx, g.systemctl, "unmask", "--runtime", unit)
	after, inspectErr := g.inspect(ctx, unit)
	if inspectErr == nil && !after.masked() {
		return nil
	}
	return errors.Join(
		reconciledBINDSystemdError([]string{"unmask", unit}, output, commandErr, inspectErr, after),
		reconciledBINDSystemdError([]string{"unmask", "--runtime", unit}, runtimeOutput, runtimeErr, inspectErr, after),
	)
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
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return bindInstallUnitState{},
				errors.New("systemctl show returned a malformed unit state row")
		}
		if seen[key] {
			return bindInstallUnitState{},
				errors.New("systemctl show returned a duplicate unit state")
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
		default:
			return bindInstallUnitState{},
				errors.New("systemctl show returned an unexpected unit state property")
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
	case "loaded", "not-found", "masked":
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
	case "enabled", "enabled-runtime", "disabled":
		return loadState == "loaded"
	case "masked", "masked-runtime":
		return loadState == "masked"
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
