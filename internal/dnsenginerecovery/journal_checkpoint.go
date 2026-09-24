package dnsenginerecovery

import (
	"errors"
	"fmt"
	"reflect"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
)

// JournalCheckpointOps supplies the host-locked, owner-aware durable file
// operations. This protocol validates and verifies one checkpoint but neither
// acquires authority nor makes a filesystem adapter safe by itself.
type JournalCheckpointOps struct {
	Persist     func([]byte) error
	Read        func() (dnsengineartifact.SwitchJournalV1, bool, error)
	BeforeWrite func(dnsengineartifact.SwitchJournalV1) error
	AfterWrite  func(dnsengineartifact.SwitchJournalV1) error
}

// WriteJournalCheckpoint preserves one accepted journal phase across uncertain
// write results. A write error is successful only when a secure readback proves
// that the exact requested journal was published. Unknown leaves the caller's
// operation unresolved; it never authorizes another mutation.
func WriteJournalCheckpoint(policy dnsengineartifact.JournalPolicy, journal dnsengineartifact.SwitchJournalV1, ops JournalCheckpointOps) error {
	if ops.Persist == nil || ops.Read == nil {
		return errors.New("DNS engine switch journal writer is incomplete")
	}
	encoded, err := policy.EncodeSwitchJournal(journal)
	if err != nil {
		return err
	}
	if ops.BeforeWrite != nil {
		if err := ops.BeforeWrite(journal); err != nil {
			return err
		}
	}
	persistErr := ops.Persist(encoded)
	if persistErr != nil {
		verified, exists, readErr := ops.Read()
		if readErr != nil || !exists || !reflect.DeepEqual(verified, journal) {
			return errors.Join(persistErr, readErr)
		}
		if ops.AfterWrite != nil {
			if err := ops.AfterWrite(journal); err != nil {
				return err
			}
		}
		return nil
	}
	if ops.AfterWrite != nil {
		if err := ops.AfterWrite(journal); err != nil {
			return err
		}
	}
	verified, exists, err := ops.Read()
	if err != nil || !exists || !reflect.DeepEqual(verified, journal) {
		if err == nil {
			err = fmt.Errorf("DNS engine switch journal readback mismatch")
		}
		return err
	}
	return nil
}
