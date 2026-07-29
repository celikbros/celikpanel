package systemsqlite

import (
	"errors"
	"math"
	"os"
)

// SnapshotLimits carries the hard output bound and free-space reserve enforced by the isolated worker.
// SnapshotLimits, yalıtılmış çalışanın uyguladığı katı çıktı sınırını ve boş alan rezervini taşır.
type SnapshotLimits struct {
	MaxBytes       int64
	FreeSpaceFloor int64
}

type snapshotFilesystemID uint64

type snapshotFilesystemCapacity struct {
	ID             snapshotFilesystemID
	AvailableBytes int64
}

type snapshotCapacityProbe func(*os.File) (snapshotFilesystemCapacity, error)

func (limits SnapshotLimits) validate() error {
	if limits.MaxBytes <= 0 || limits.MaxBytes > maxOwnerWorkerSnapshotBytes ||
		limits.FreeSpaceFloor <= 0 {
		return errors.New("isolated snapshot capacity limits are invalid")
	}
	if _, ok := snapshotRequiredBytes(1, limits); !ok {
		return errors.New("isolated snapshot capacity limits overflow")
	}
	return nil
}

func ensureMutableSnapshotCapacity(
	temporary snapshotFilesystemCapacity,
	destination snapshotFilesystemCapacity,
	limits SnapshotLimits,
) error {
	if err := limits.validate(); err != nil {
		return err
	}
	if temporary.ID == 0 || destination.ID == 0 ||
		temporary.AvailableBytes < 0 || destination.AvailableBytes < 0 {
		return errors.New("isolated snapshot capacity could not be verified")
	}

	if temporary.ID == destination.ID {
		required, ok := snapshotRequiredBytes(2, limits)
		if !ok {
			return errors.New("isolated snapshot capacity limits overflow")
		}
		available := min(temporary.AvailableBytes, destination.AvailableBytes)
		if available < required {
			return errors.New("isolated snapshot filesystem does not have enough free space")
		}
		return nil
	}

	required, _ := snapshotRequiredBytes(1, limits)
	if temporary.AvailableBytes < required {
		return errors.New("isolated snapshot workspace does not have enough free space")
	}
	if destination.AvailableBytes < required {
		return errors.New("isolated snapshot destination does not have enough free space")
	}
	return nil
}

func snapshotRequiredBytes(copies int64, limits SnapshotLimits) (int64, bool) {
	if copies <= 0 || limits.MaxBytes <= 0 || limits.FreeSpaceFloor <= 0 ||
		limits.MaxBytes > (math.MaxInt64-limits.FreeSpaceFloor)/copies {
		return 0, false
	}
	return copies*limits.MaxBytes + limits.FreeSpaceFloor, true
}
