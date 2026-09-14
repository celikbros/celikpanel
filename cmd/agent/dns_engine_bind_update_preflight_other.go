//go:build !linux

package main

import "context"

func verifyExistingManagedBINDGenerationForPreflight(ctx context.Context, state dnsEngineStateReceipt) error {
	return verifyExistingManagedBINDGenerationForSignedUpdate(ctx, state)
}
