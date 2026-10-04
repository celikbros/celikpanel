package main

import (
	"errors"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
)

// publishBINDIntentAfterIndependentSourceProof binds the frozen source to the
// accepted manifest while ordinary PowerDNS readiness still permits it. Once
// the journal exists, only the exact frozen source proof may be rechecked.
func publishBINDIntentAfterIndependentSourceProof(journal dnsEngineSwitchJournal, verifyAccepted, verifyFrozen, publish func() error) error {
	if publish == nil {
		return errors.New("BIND switch intent publisher is missing")
	}
	if journal.Schema == dnsengineartifact.SwitchJournalSchemaV2 {
		if verifyAccepted == nil || verifyFrozen == nil {
			return errors.New("BIND switch intent lacks accepted and frozen source proofs")
		}
		if err := verifyAccepted(); err != nil {
			return err
		}
		if err := verifyFrozen(); err != nil {
			return err
		}
	}
	return publish()
}
