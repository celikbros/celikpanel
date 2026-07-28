//go:build !linux

package main

import "fmt"

func restoreServiceOperationSnapshotWithOwner(
	string,
	serviceOperationSnapshotSchema,
	serviceOperationRestoreOwner,
	serviceOperationRestoreHooks,
) error {
	return fmt.Errorf("secure service operation restore requires Linux")
}

func verifyServiceOperationReleaseTransaction(serviceOperationReleaseTransaction) error {
	return fmt.Errorf("secure service operation restore requires Linux")
}
