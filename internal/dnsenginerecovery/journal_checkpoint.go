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

// JournalRemovalOps supplies secure host-locked reads and removal of the fixed
// journal path. The owner-aware filesystem adapter must prevent path traversal.
type JournalRemovalOps struct {
	Read   func() (dnsengineartifact.SwitchJournalV1, bool, error)
	Remove func() error
}

// RemoveJournalCheckpoint refuses to unlink a journal that no longer matches
// the exact accepted rollback checkpoint. The second read resolves an uncertain
// unlink result, but a later owner edit remains outside this point-in-time proof.
func RemoveJournalCheckpoint(policy dnsengineartifact.JournalPolicy, expected dnsengineartifact.SwitchJournalV1, ops JournalRemovalOps) error {
	if ops.Read == nil || ops.Remove == nil {
		return errors.New("DNS engine switch journal remover is incomplete")
	}
	if _, err := policy.EncodeSwitchJournal(expected); err != nil {
		return err
	}
	current, exists, err := ops.Read()
	if err != nil {
		return err
	}
	if !exists || !reflect.DeepEqual(current, expected) {
		return errors.New("DNS engine switch journal changed before removal; preserve it for owner review")
	}
	removeErr := ops.Remove()
	_, exists, readErr := ops.Read()
	if readErr == nil && !exists {
		return nil
	}
	if removeErr != nil {
		return errors.Join(removeErr, readErr)
	}
	if readErr != nil {
		return readErr
	}
	return errors.New("DNS engine switch journal still exists after removal")
}
