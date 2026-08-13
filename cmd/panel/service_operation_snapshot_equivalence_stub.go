//go:build !linux

package main

import "fmt"

func provePreLedgerSnapshotEquivalence(
	string,
	string,
	serviceOperationReleaseTransaction,
) error {
	return fmt.Errorf("secure pre-ledger snapshot equivalence proof requires Linux")
}
