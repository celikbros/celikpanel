//go:build !linux

package main

import "fmt"

func ensureServiceOperationRescueSnapshotWithOwner(
	string,
	string,
	serviceOperationSnapshotSchema,
	serviceOperationRestoreOwner,
) error {
	return fmt.Errorf("secure service operation rescue snapshots require Linux")
}
