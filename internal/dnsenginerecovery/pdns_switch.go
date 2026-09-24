package dnsenginerecovery

import (
	"context"
	"errors"
	"fmt"
)

// PDNSSwitchRollbackOps are effects supplied by the selected native executor.
// It must verify owner authority and configuration preimages before calling
// RollbackPDNSSwitch, and retain the accepted journal until terminal proof.
type PDNSSwitchRollbackOps struct {
	StopTarget      func(context.Context) error
	RestoreDatabase func() error
	RestoreConfigs  func() error
	RestoreState    func() error
	RestoreTarget   func(context.Context) error
	RestoreSource   func(context.Context) error
}

// RollbackPDNSSwitch serializes the native inverse. A failed stop must never
// be followed by writing SQLite while PowerDNS may still be serving it. Later
// effects are also withheld after any failed or interrupted predecessor;
// recovery resumes the same journal after fresh owner/native proofs.
func RollbackPDNSSwitch(ctx context.Context, ops PDNSSwitchRollbackOps) error {
	if ctx == nil || ops.StopTarget == nil || ops.RestoreDatabase == nil ||
		ops.RestoreConfigs == nil || ops.RestoreState == nil ||
		ops.RestoreTarget == nil || ops.RestoreSource == nil {
		return errors.New("invalid PowerDNS switch rollback operations")
	}
	steps := []struct {
		name string
		run  func() error
	}{
		{"stop target", func() error { return ops.StopTarget(ctx) }},
		{"restore database", ops.RestoreDatabase},
		{"restore configs", ops.RestoreConfigs},
		{"restore state", ops.RestoreState},
		{"restore target unit", func() error { return ops.RestoreTarget(ctx) }},
		{"restore source unit", func() error { return ops.RestoreSource(ctx) }},
	}
	for _, step := range steps {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("PowerDNS switch rollback interrupted before %s: %w", step.name, err)
		}
		if err := step.run(); err != nil {
			return fmt.Errorf("PowerDNS switch rollback %s: %w", step.name, err)
		}
	}
	return nil
}
