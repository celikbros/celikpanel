package main

import (
	"fmt"
	"os"
	"path/filepath"
)

const agentReleaseTransactionRoot = "/var/lib/celikpanel-release-transaction"

var agentReleaseTransactionMarkers = [...]string{
	"quiesce.pending",
	"active",
	"completion.pending",
	"scheduler-restore.pending",
}

// persistentReleaseTransactionPresent treats every durable transaction marker,
// including a dangling symlink, as an active release boundary. Unexpected path
// topology or inspection failures are returned to callers so they can fail
// closed instead of admitting a privileged host mutation.
func persistentReleaseTransactionPresent(root string) (bool, error) {
	info, err := os.Lstat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("inspect release transaction root: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return false, fmt.Errorf("release transaction root is not a canonical directory")
	}
	for _, marker := range agentReleaseTransactionMarkers {
		path := filepath.Join(root, marker)
		if _, err := os.Lstat(path); err == nil {
			return true, nil
		} else if !os.IsNotExist(err) {
			return false, fmt.Errorf("inspect release transaction marker %s: %w", marker, err)
		}
	}
	return false, nil
}

func productionReleaseTransactionPresent() (bool, error) {
	return persistentReleaseTransactionPresent(agentReleaseTransactionRoot)
}
