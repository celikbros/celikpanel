//go:build linux

package main

import (
	"testing"

	"golang.org/x/sys/unix"
)

func TestValidatePinnedPreLedgerServiceOperationSnapshotStatBoundsSize(t *testing.T) {
	valid := unix.Stat_t{
		Mode:  unix.S_IFREG | 0o600,
		Uid:   0,
		Gid:   0,
		Nlink: 1,
		Size:  1,
	}
	if err := validatePinnedPreLedgerServiceOperationSnapshotStat(valid); err != nil {
		t.Fatalf("minimum non-empty snapshot rejected: %v", err)
	}
	valid.Size = maximumPreLedgerServiceOperationSnapshotBytes
	if err := validatePinnedPreLedgerServiceOperationSnapshotStat(valid); err != nil {
		t.Fatalf("maximum bounded snapshot rejected: %v", err)
	}
	for _, size := range []int64{0, -1, maximumPreLedgerServiceOperationSnapshotBytes + 1} {
		invalid := valid
		invalid.Size = size
		if err := validatePinnedPreLedgerServiceOperationSnapshotStat(invalid); err == nil {
			t.Fatalf("unsafe snapshot size %d accepted", size)
		}
	}
}
