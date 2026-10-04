//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/alicelik/celikpanel/internal/dnsenginerecovery"
)

// Every callback is an installed, guarded native operation. The assessor must
// reprove the same journal, worker exclusion, units, source and target before
// each effect; these state transitions alone grant no filesystem authority.
type pdnsTargetNativeInverseOpsV4 struct {
	Assess             func(context.Context) (dnsenginerecovery.PDNSTargetStageState, error)
	DisableTarget      func(context.Context) error
	RestoreConfigs     func(context.Context) error
	ReturnRenamed      func(context.Context) error
	RestoreSourceUnits func(context.Context) error
	RemoveCandidate    func(context.Context) error
}

func restorePDNSTargetNativeV4(ctx context.Context, ops pdnsTargetNativeInverseOpsV4) error {
	if ctx == nil || ops.Assess == nil || ops.DisableTarget == nil || ops.RestoreConfigs == nil || ops.ReturnRenamed == nil ||
		ops.RestoreSourceUnits == nil || ops.RemoveCandidate == nil {
		return errors.New("PowerDNS native inverse requires complete protected operations")
	}
	assess := func() (dnsenginerecovery.PDNSTargetStageState, error) {
		state, err := ops.Assess(ctx)
		if err != nil || state == dnsenginerecovery.PDNSTargetStageUnknown {
			return state, errors.Join(errors.New("PowerDNS target changed during native inverse"), err)
		}
		return state, nil
	}
	state, err := assess()
	if err != nil || (state != dnsenginerecovery.PDNSTargetStageNeedsRestore && state != dnsenginerecovery.PDNSTargetStageRenamed && state != dnsenginerecovery.PDNSTargetStageRenamedEnabled) {
		return errors.Join(errors.New("PowerDNS target is not a bounded pre-start inverse"), err)
	}
	if state == dnsenginerecovery.PDNSTargetStageRenamedEnabled {
		if err := ops.DisableTarget(ctx); err != nil {
			return fmt.Errorf("restore stopped PowerDNS unit to disabled: %w", err)
		}
		state, err = assess()
		if err != nil || state != dnsenginerecovery.PDNSTargetStageRenamed {
			return errors.Join(errors.New("PowerDNS enable intent was not compensated"), err)
		}
	}
	if err := ops.RestoreConfigs(ctx); err != nil {
		return fmt.Errorf("restore frozen PowerDNS config: %w", err)
	}
	state, err = assess()
	if err != nil {
		return err
	}
	if state == dnsenginerecovery.PDNSTargetStageRestored {
		return nil
	}
	if state == dnsenginerecovery.PDNSTargetStageRenamed {
		if err := ops.ReturnRenamed(ctx); err != nil {
			return fmt.Errorf("return exact PowerDNS candidate before BIND restart: %w", err)
		}
		state, err = assess()
		if err != nil || state != dnsenginerecovery.PDNSTargetStageNeedsRestore {
			return errors.Join(errors.New("returned PowerDNS candidate could not be proved"), err)
		}
	}
	if err := ops.RestoreSourceUnits(ctx); err != nil {
		return fmt.Errorf("restore frozen BIND source units: %w", err)
	}
	state, err = assess()
	if err != nil {
		return err
	}
	if state == dnsenginerecovery.PDNSTargetStageRestored {
		return nil
	}
	if state != dnsenginerecovery.PDNSTargetStageNeedsRestore {
		return errors.New("BIND restoration left an unexpected PowerDNS target shape")
	}
	if err := ops.RemoveCandidate(ctx); err != nil {
		return fmt.Errorf("remove exact staged PowerDNS candidate: %w", err)
	}
	state, err = assess()
	if err != nil || state != dnsenginerecovery.PDNSTargetStageRestored {
		return errors.Join(errors.New("restored BIND authority and target absence could not be proved"), err)
	}
	return nil
}
