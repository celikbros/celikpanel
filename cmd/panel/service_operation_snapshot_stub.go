//go:build !linux

package main

import "fmt"

type serviceOperationSnapshotDestination struct {
	stagePath    string
	beforeRename func() error
	afterRename  func() error
}

func prepareServiceOperationSnapshotDestination(
	string,
) (*serviceOperationSnapshotDestination, error) {
	return nil, fmt.Errorf("secure service operation snapshots require Linux")
}

func createReleaseServiceOperationSnapshotWithOwner(
	string,
	string,
	serviceOperationSnapshotSchema,
	serviceOperationRestoreOwner,
) error {
	return fmt.Errorf("secure service operation snapshots require Linux")
}

func (*serviceOperationSnapshotDestination) createStage() (string, error) {
	return "", fmt.Errorf("secure service operation snapshots require Linux")
}

func (*serviceOperationSnapshotDestination) syncAndVerifyStage() error {
	return fmt.Errorf("secure service operation snapshots require Linux")
}

func (*serviceOperationSnapshotDestination) validateStage(serviceOperationSnapshotSchema) error {
	return fmt.Errorf("secure service operation snapshots require Linux")
}

func (*serviceOperationSnapshotDestination) publish() error {
	return fmt.Errorf("secure service operation snapshots require Linux")
}

func (*serviceOperationSnapshotDestination) cleanup() error {
	return nil
}

func (*serviceOperationSnapshotDestination) close() error {
	return nil
}
