//go:build linux

package dnsenginerecovery

import (
	"context"
	"errors"
	"fmt"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
)

// PDNSTargetConfigRestoreOps keeps host effects in the fixed installed adapter.
// Read classifies every file against its frozen before/after image; Write and
// Remove must compare the exact observed after-image before changing a path.
type PDNSTargetConfigRestoreOps struct {
	Read   func(context.Context) ([]PDNSTargetConfigStateV4, error)
	Guard  func(context.Context) error
	Write  func(context.Context, dnsengineartifact.FileSnapshot, dnsengineartifact.FileSnapshot) error
	Remove func(context.Context, dnsengineartifact.FileSnapshot) error
}

// RestorePDNSTargetConfigCheckpointV4 is an idempotent three-file inverse.
// Each file may independently be at its complete before or after image after
// interruption. A third state is an owner edit or unknown result and stops the
// inverse while retaining the operation journal.
func RestorePDNSTargetConfigCheckpointV4(ctx context.Context, before, after []dnsengineartifact.FileSnapshot, ops PDNSTargetConfigRestoreOps) error {
	if ctx == nil || ops.Read == nil || ops.Guard == nil || ops.Write == nil || ops.Remove == nil ||
		len(before) != 3 || len(after) != 3 {
		return errors.New("PowerDNS target config inverse requires complete guarded three-file operations")
	}
	for i := range before {
		if before[i].Path == "" || before[i].Path != after[i].Path ||
			(before[i].Exists && dnsengineartifact.ValidateFileSnapshotIntegrity(before[i]) != nil) ||
			(after[i].Exists && dnsengineartifact.ValidateFileSnapshotIntegrity(after[i]) != nil) ||
			(before[i].Exists && after[i].Exists &&
				(before[i].Mode != after[i].Mode || before[i].UID != after[i].UID || before[i].GID != after[i].GID || !before[i].OwnerKnown || !after[i].OwnerKnown)) {
			return errors.New("PowerDNS target config inverse has invalid frozen file pairs")
		}
	}
	read := func() ([]PDNSTargetConfigStateV4, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := ops.Guard(ctx); err != nil {
			return nil, err
		}
		states, err := ops.Read(ctx)
		if err != nil {
			return nil, err
		}
		if len(states) != len(before) {
			return nil, errors.New("PowerDNS target config set is incomplete")
		}
		for _, state := range states {
			if state != PDNSTargetConfigBeforeV4 && state != PDNSTargetConfigAfterV4 {
				return nil, errors.New("PowerDNS target config contains an unknown file state")
			}
		}
		return states, ctx.Err()
	}
	states, err := read()
	if err != nil {
		return err
	}
	for i := len(before) - 1; i >= 0; i-- {
		if states[i] == PDNSTargetConfigBeforeV4 {
			continue
		}
		if !after[i].Exists && before[i].Exists {
			if err := ops.Write(ctx, after[i], before[i]); err != nil {
				return fmt.Errorf("restore absent PowerDNS config %s: %w", before[i].Path, err)
			}
		} else if before[i].Exists {
			if err := ops.Write(ctx, after[i], before[i]); err != nil {
				return fmt.Errorf("restore PowerDNS config %s: %w", before[i].Path, err)
			}
		} else if after[i].Exists {
			if err := ops.Remove(ctx, after[i]); err != nil {
				return fmt.Errorf("remove exact PowerDNS config %s: %w", after[i].Path, err)
			}
		} else {
			return errors.New("PowerDNS config has indistinguishable absent before/after images")
		}
		states, err = read()
		if err != nil {
			return err
		}
		if states[i] != PDNSTargetConfigBeforeV4 {
			return fmt.Errorf("PowerDNS config %s did not reach frozen before-image", before[i].Path)
		}
	}
	states, err = read()
	if err != nil {
		return err
	}
	for _, state := range states {
		if state != PDNSTargetConfigBeforeV4 {
			return errors.New("PowerDNS target config changed before final readback")
		}
	}
	return nil
}
