package systemsqlite

import (
	"math"
	"testing"
)

func TestMutableSnapshotCapacitySameFilesystemAccountsForBothCopies(t *testing.T) {
	limits := SnapshotLimits{MaxBytes: 100, FreeSpaceFloor: 20}
	for _, test := range []struct {
		name      string
		available int64
		wantError bool
	}{
		{name: "one byte below double peak", available: 219, wantError: true},
		{name: "exact double peak", available: 220},
		{name: "above double peak", available: 221},
	} {
		t.Run(test.name, func(t *testing.T) {
			capacity := snapshotFilesystemCapacity{ID: 7, AvailableBytes: test.available}
			err := ensureMutableSnapshotCapacity(capacity, capacity, limits)
			if (err != nil) != test.wantError {
				t.Fatalf("ensureMutableSnapshotCapacity() error = %v, wantError %v", err, test.wantError)
			}
		})
	}
}

func TestMutableSnapshotCapacitySeparateFilesystemsReservesEachFilesystem(t *testing.T) {
	limits := SnapshotLimits{MaxBytes: 100, FreeSpaceFloor: 20}
	for _, test := range []struct {
		name                 string
		temporaryAvailable   int64
		destinationAvailable int64
		wantError            bool
	}{
		{name: "temporary short", temporaryAvailable: 119, destinationAvailable: 120, wantError: true},
		{name: "destination short", temporaryAvailable: 120, destinationAvailable: 119, wantError: true},
		{name: "both exact", temporaryAvailable: 120, destinationAvailable: 120},
	} {
		t.Run(test.name, func(t *testing.T) {
			temporary := snapshotFilesystemCapacity{ID: 1, AvailableBytes: test.temporaryAvailable}
			destination := snapshotFilesystemCapacity{ID: 2, AvailableBytes: test.destinationAvailable}
			err := ensureMutableSnapshotCapacity(temporary, destination, limits)
			if (err != nil) != test.wantError {
				t.Fatalf("ensureMutableSnapshotCapacity() error = %v, wantError %v", err, test.wantError)
			}
		})
	}
}

func TestMutableSnapshotCapacityFailsClosedWhenItCannotProveThePeak(t *testing.T) {
	valid := SnapshotLimits{MaxBytes: 100, FreeSpaceFloor: 20}
	if err := ensureMutableSnapshotCapacity(
		snapshotFilesystemCapacity{AvailableBytes: 1_000},
		snapshotFilesystemCapacity{ID: 2, AvailableBytes: 1_000},
		valid,
	); err == nil {
		t.Fatal("missing filesystem identity was accepted")
	}
	if err := ensureMutableSnapshotCapacity(
		snapshotFilesystemCapacity{ID: 1, AvailableBytes: -1},
		snapshotFilesystemCapacity{ID: 2, AvailableBytes: 1_000},
		valid,
	); err == nil {
		t.Fatal("negative available capacity was accepted")
	}
	overflowing := SnapshotLimits{
		MaxBytes:       maxOwnerWorkerSnapshotBytes,
		FreeSpaceFloor: math.MaxInt64 - maxOwnerWorkerSnapshotBytes - 1,
	}
	if err := ensureMutableSnapshotCapacity(
		snapshotFilesystemCapacity{ID: 1, AvailableBytes: math.MaxInt64},
		snapshotFilesystemCapacity{ID: 1, AvailableBytes: math.MaxInt64},
		overflowing,
	); err == nil {
		t.Fatal("same-filesystem peak overflow was accepted")
	}
}
