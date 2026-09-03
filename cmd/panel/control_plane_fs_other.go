//go:build !linux

package main

import (
	"fmt"
	"os"
	"os/user"
)

// Windows is the development host. It has no POSIX owner, no fchown and no
// directory fsync, so the archive round trip stays exercisable there while the
// ownership half of the contract is proved on Linux. Production restore is a
// root-only Linux operation and never reaches this file.
//
// Windows geliştirme makinesidir. POSIX sahipliği, fchown ve dizin fsync yoktur;
// bu yüzden arşiv gidiş-dönüşü orada da çalıştırılabilir kalır, sahiplik yarısı
// ise Linux'ta kanıtlanır.

const controlPlaneOpenNoFollow = 0

// controlPlaneOwnershipUnavailable is the id pair that means "this host cannot
// express POSIX ownership"; applying it is a deliberate no-op.
const controlPlaneOwnershipUnavailable = -1

func controlPlaneOwnership(path string, _ os.FileInfo) (string, string, error) {
	current, err := user.Current()
	if err != nil {
		return "", "", fmt.Errorf("read the owner of %s: %w", path, err)
	}
	return current.Username, current.Username, nil
}

func controlPlaneResolveOwnership(_ string, _ string) (int, int, error) {
	return controlPlaneOwnershipUnavailable, controlPlaneOwnershipUnavailable, nil
}

func controlPlaneApplyFileMetadata(
	file *os.File,
	mode os.FileMode,
	uid int,
	_ int,
) error {
	if uid != controlPlaneOwnershipUnavailable {
		return fmt.Errorf("setting a POSIX owner requires Linux")
	}
	// The mode is applied after the content is written, not before: a Windows
	// read-only attribute set on the staging file would block the rename that
	// publishes it. controlPlaneFinalizeFileMode does it.
	_ = mode
	return nil
}

func controlPlaneFinalizeFileMode(path string, mode os.FileMode) error {
	if err := os.Chmod(path, mode.Perm()); err != nil {
		return fmt.Errorf("set the mode of %s: %w", path, err)
	}
	return nil
}

func controlPlaneApplyDirectoryMetadata(
	path string,
	mode os.FileMode,
	uid int,
	_ int,
) error {
	if uid != controlPlaneOwnershipUnavailable {
		return fmt.Errorf("setting a POSIX owner requires Linux")
	}
	if err := os.Chmod(path, mode.Perm()); err != nil {
		return fmt.Errorf("set the mode of %s: %w", path, err)
	}
	return nil
}

func controlPlaneSyncDirectory(_ string) error {
	return nil
}
