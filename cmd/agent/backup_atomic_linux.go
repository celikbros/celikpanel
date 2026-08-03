//go:build linux

package main

import (
	"fmt"
	"log"
	"os"
	"path"
	"syscall"

	"github.com/alicelik/celikpanel/internal/hostingpath"
	"golang.org/x/sys/unix"
)

// preparedTreeRestore owns a fully populated replacement tree and the two
// parent directory handles needed to publish it with RENAME_EXCHANGE. Keeping
// those handles open closes the path-swap window between preparation and
// commit.
type preparedTreeRestore struct {
	targetRoot     string
	targetDev      uint64
	targetIno      uint64
	stagingFD      int
	stageName      string
	targetParentFD int
	targetLeaf     string
	stageExists    bool
	closed         bool
}

func prepareAtomicTreeRestore(
	targetRoot string,
	populate func(stagePath string) error,
) (*preparedTreeRestore, error) {
	targetFD, err := openFileManagerRoot(targetRoot)
	if err != nil {
		return nil, err
	}
	var targetStat unix.Stat_t
	if err := unix.Fstat(targetFD, &targetStat); err != nil {
		unix.Close(targetFD)
		return nil, err
	}
	if targetStat.Mode&unix.S_IFMT != unix.S_IFDIR {
		unix.Close(targetFD)
		return nil, os.ErrPermission
	}
	unix.Close(targetFD)

	stagingFD, stageName, stagePath, err := createRestoreStage(
		restoreStagingBaseDir, &targetStat,
	)
	if err != nil {
		return nil, err
	}
	cleanupStage := func() {
		_ = secureDeleteAt(stagingFD, stageName, false)
		unix.Close(stagingFD)
	}
	if err := populate(stagePath); err != nil {
		cleanupStage()
		return nil, err
	}
	stageFD, err := openFileManagerAt(
		stagingFD, stageName,
		unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0,
	)
	if err != nil {
		cleanupStage()
		return nil, err
	}
	if err := unix.Fsync(stageFD); err != nil {
		unix.Close(stageFD)
		cleanupStage()
		return nil, err
	}
	unix.Close(stageFD)

	targetParentPath, targetLeaf := path.Split(targetRoot)
	targetParentPath = path.Clean(targetParentPath)
	if err := hostingpath.ValidateFileName(targetLeaf); err != nil {
		cleanupStage()
		return nil, err
	}
	targetParentFD, err := openFileManagerRoot(targetParentPath)
	if err != nil {
		cleanupStage()
		return nil, err
	}
	return &preparedTreeRestore{
		targetRoot:     targetRoot,
		targetDev:      uint64(targetStat.Dev),
		targetIno:      targetStat.Ino,
		stagingFD:      stagingFD,
		stageName:      stageName,
		targetParentFD: targetParentFD,
		targetLeaf:     targetLeaf,
		stageExists:    true,
	}, nil
}

func (restore *preparedTreeRestore) Commit() error {
	if restore == nil || restore.closed || !restore.stageExists {
		return os.ErrInvalid
	}
	var current unix.Stat_t
	if err := unix.Fstatat(
		restore.targetParentFD,
		restore.targetLeaf,
		&current,
		unix.AT_SYMLINK_NOFOLLOW,
	); err != nil {
		return err
	}
	if current.Mode&unix.S_IFMT != unix.S_IFDIR ||
		uint64(current.Dev) != restore.targetDev ||
		current.Ino != restore.targetIno {
		return syscall.EBUSY
	}

	if err := unix.Renameat2(
		restore.stagingFD,
		restore.stageName,
		restore.targetParentFD,
		restore.targetLeaf,
		unix.RENAME_EXCHANGE,
	); err != nil {
		return fmt.Errorf("atomic document-root exchange failed: %w", err)
	}
	rollback := func(cause error) error {
		if rollbackErr := unix.Renameat2(
			restore.stagingFD,
			restore.stageName,
			restore.targetParentFD,
			restore.targetLeaf,
			unix.RENAME_EXCHANGE,
		); rollbackErr != nil {
			return fmt.Errorf("%v; restore rollback failed: %w", cause, rollbackErr)
		}
		_ = unix.Fsync(restore.targetParentFD)
		_ = unix.Fsync(restore.stagingFD)
		return cause
	}
	if err := unix.Fsync(restore.targetParentFD); err != nil {
		return rollback(fmt.Errorf("fsync live document-root parent: %w", err))
	}
	if err := unix.Fsync(restore.stagingFD); err != nil {
		return rollback(fmt.Errorf("fsync restore staging parent: %w", err))
	}

	if err := secureDeleteAt(
		restore.stagingFD, restore.stageName, false,
	); err != nil {
		log.Printf(
			"restore committed for %s but old tree cleanup failed: %v",
			restore.targetRoot, err,
		)
		return nil
	}
	restore.stageExists = false
	return nil
}

func (restore *preparedTreeRestore) Close() {
	if restore == nil || restore.closed {
		return
	}
	restore.closed = true
	if restore.stageExists {
		_ = secureDeleteAt(restore.stagingFD, restore.stageName, false)
	}
	unix.Close(restore.targetParentFD)
	unix.Close(restore.stagingFD)
}
