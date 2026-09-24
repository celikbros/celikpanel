package dnsenginerecovery

import (
	"errors"
	"fmt"
)

// RunningBINDAdoptionOps supplies the native proofs and effects for recovery of
// an already-answering owner BIND. The caller must hold accepted-operation and
// host authority; this sequence does not acquire locks or inspect live services.
type RunningBINDAdoptionOps[E any] struct {
	CaptureEvidence func() (E, error)
	ProveCurrent    func() error
	Rollback        func(E) error
	RestorePointer  func() error
	VerifyRestored  func(E) error
}

// RecoverRunningBINDAdoption first pins fresh running-service evidence, then
// proves owner-aware configuration, restores and reloads without stopping BIND,
// drops the generation pointer only afterward, and verifies the restored server.
// Any failed step retains the caller's operation journal for exact replay.
func RecoverRunningBINDAdoption[E any](ops RunningBINDAdoptionOps[E]) error {
	if ops.CaptureEvidence == nil || ops.ProveCurrent == nil ||
		ops.Rollback == nil || ops.RestorePointer == nil ||
		ops.VerifyRestored == nil {
		return errors.New("invalid BIND adoption recovery operations")
	}
	evidence, err := ops.CaptureEvidence()
	if err != nil {
		return fmt.Errorf("prove the interrupted takeover's BIND is still answering: %w", err)
	}
	if err := ops.ProveCurrent(); err != nil {
		return err
	}
	if err := ops.Rollback(evidence); err != nil {
		return err
	}
	if err := ops.RestorePointer(); err != nil {
		return err
	}
	return ops.VerifyRestored(evidence)
}
