package dnsenginerecovery

import (
	"context"
	"errors"
	"fmt"
)

// BINDActivationRollbackOps are the native effects for a managed BIND switch
// or reconfiguration. A running unmanaged owner BIND adoption has a
// different, non-stopping inverse and must never enter this sequence.
type BINDActivationRollbackOps struct {
	RestoreTarget            func(context.Context) error
	VerifyTargetBeforeConfig func(context.Context) error
	RestoreConfigs           func() error
	RestoreState             func() error
	RestoreSource            func(context.Context) error
}

// RollbackBINDActivation restores the target unit preimage before changing
// configuration. A failed predecessor withholds all later
// effects; the accepted rolling-back journal is retained by the caller.
func RollbackBINDActivation(ctx context.Context, ops BINDActivationRollbackOps) error {
	if ctx == nil || ops.RestoreTarget == nil || ops.VerifyTargetBeforeConfig == nil || ops.RestoreConfigs == nil ||
		ops.RestoreState == nil || ops.RestoreSource == nil {
		return errors.New("invalid BIND activation rollback operations")
	}
	steps := []struct {
		name string
		run  func() error
	}{
		{"restore target unit", func() error { return ops.RestoreTarget(ctx) }},
		{"verify target before config restore", func() error { return ops.VerifyTargetBeforeConfig(ctx) }},
		{"restore configs", ops.RestoreConfigs},
		{"restore state", ops.RestoreState},
		{"restore source unit", func() error { return ops.RestoreSource(ctx) }},
	}
	for _, step := range steps {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("BIND activation rollback interrupted before %s: %w", step.name, err)
		}
		if err := step.run(); err != nil {
			return fmt.Errorf("BIND activation rollback %s: %w", step.name, err)
		}
	}
	return nil
}
