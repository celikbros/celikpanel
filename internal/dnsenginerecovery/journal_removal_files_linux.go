//go:build linux

package dnsenginerecovery

import (
	"errors"
	"path/filepath"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
)

// RemoveExactRollbackJournal is the independent executor's narrow terminal
// cleanup adapter. The caller must hold the release and host locks, prove the
// same operation's durable failed ledger verdict and reprove its native inverse.
// This helper alone does not establish any of those authorities.
func RemoveExactRollbackJournal(
	policy dnsengineartifact.JournalPolicy,
	owner servicemutationledger.FileOwner,
	expected dnsengineartifact.SwitchJournalV1,
) error {
	if !policy.RequireOwner || policy.StateUID != owner.UID || policy.StateGID != owner.GID ||
		filepath.Base(policy.StatePath) != "dns-engine-state.json" ||
		expected.Phase != dnsengineartifact.SwitchPhaseRolledBack {
		return errors.New("DNS rollback journal cleanup requires the trusted owner and terminal phase")
	}
	raw, err := policy.EncodeSwitchJournal(expected)
	if err != nil {
		return err
	}
	path := filepath.Join(filepath.Dir(policy.StatePath), "dns-engine-switch-journal.json")
	read := func() (dnsengineartifact.SwitchJournalV1, bool, error) {
		content, exists, err := servicemutationledger.ReadFile(path, dnsengineartifact.SwitchJournalLimit, owner)
		if err != nil || !exists {
			return dnsengineartifact.SwitchJournalV1{}, exists, err
		}
		journal, err := policy.DecodeSwitchJournal(content)
		return journal, err == nil, err
	}
	return RemoveJournalCheckpoint(policy, expected, JournalRemovalOps{
		Read: read,
		Remove: func() error {
			return servicemutationledger.RemoveFileExact(path, raw, dnsengineartifact.SwitchJournalLimit, owner)
		},
	})
}
