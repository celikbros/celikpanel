//go:build linux

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

const (
	initialLedgerPublishBeforeRename = "before-rename"
	initialLedgerPublishAfterRename  = "after-rename"
)

var initialLedgerPublishFault func(point string) error

func injectInitialLedgerPublishFault(point string) error {
	if initialLedgerPublishFault == nil {
		return nil
	}
	return initialLedgerPublishFault(point)
}

// publishInitialServiceMutationLedger atomically renames a closed, fsynced staged
// inode to a previously absent final name, then fsyncs their shared directory.
// publishInitialServiceMutationLedger kapatılmış ve fsync edilmiş staged inode'u
// önceden bulunmayan nihai ada atomik taşır, sonra ortak dizinlerini fsync eder.
func publishInitialServiceMutationLedger(stagePath, finalPath string) (returnErr error) {
	stagePath = filepath.Clean(stagePath)
	finalPath = filepath.Clean(finalPath)
	if !filepath.IsAbs(stagePath) || !filepath.IsAbs(finalPath) {
		return errors.New("initial service mutation publish paths must be absolute")
	}
	stageDir := filepath.Dir(stagePath)
	if stageDir != filepath.Dir(finalPath) {
		return errors.New("initial service mutation ledger must be staged in its final directory")
	}
	stageName := filepath.Base(stagePath)
	finalName := filepath.Base(finalPath)
	if stageName == "." || finalName == "." || stageName == finalName {
		return errors.New("invalid initial service mutation ledger publish names")
	}

	dirFD, err := unix.Open(stageDir, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_DIRECTORY, 0)
	if err != nil {
		return &os.PathError{Op: "open initial ledger directory", Path: stageDir, Err: err}
	}
	defer func() {
		if err := unix.Close(dirFD); err != nil && returnErr == nil {
			returnErr = &os.PathError{Op: "close initial ledger directory", Path: stageDir, Err: err}
		}
	}()

	if err := injectInitialLedgerPublishFault(initialLedgerPublishBeforeRename); err != nil {
		return fmt.Errorf("before initial ledger rename: %w", err)
	}
	if err := unix.Renameat2(dirFD, stageName, dirFD, finalName, unix.RENAME_NOREPLACE); err != nil {
		return &os.LinkError{Op: "rename initial ledger no-replace", Old: stagePath, New: finalPath, Err: err}
	}
	if err := injectInitialLedgerPublishFault(initialLedgerPublishAfterRename); err != nil {
		return fmt.Errorf("after initial ledger rename: %w", err)
	}
	if err := unix.Fsync(dirFD); err != nil {
		return &os.PathError{Op: "sync initial ledger directory", Path: stageDir, Err: err}
	}
	return nil
}
