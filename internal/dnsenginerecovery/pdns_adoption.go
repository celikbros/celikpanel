package dnsenginerecovery

import (
	"context"
	"errors"
	"fmt"
)

// PDNSAdoptionRollbackOps are owner-aware host effects supplied under the
// accepted operation's lock. In particular, ProveConfigs must reject owner
// edits before RestoreState removes or replaces the engine state receipt.
type PDNSAdoptionRollbackOps struct {
	ProveConfigs   func(context.Context) error
	RestoreState   func(context.Context) error
	VerifyRestored func(context.Context) error
}

// RollbackPDNSAdoption preserves the existing authoritative PowerDNS service.
// A failed or cancelled predecessor withholds later effects; the caller keeps
// the original rolling-back journal for an exact retry.
func RollbackPDNSAdoption(ctx context.Context, ops PDNSAdoptionRollbackOps) error {
	if ctx == nil || ops.ProveConfigs == nil || ops.RestoreState == nil || ops.VerifyRestored == nil {
		return errors.New("invalid PowerDNS adoption rollback operations")
	}
	for _, step := range []struct {
		name string
		run  func(context.Context) error
	}{
		{"prove configs", ops.ProveConfigs},
		{"restore state", ops.RestoreState},
		{"verify restored source", ops.VerifyRestored},
	} {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("PowerDNS adoption rollback interrupted before %s: %w", step.name, err)
		}
		if err := step.run(ctx); err != nil {
			return fmt.Errorf("PowerDNS adoption rollback %s: %w", step.name, err)
		}
	}
	return nil
}
