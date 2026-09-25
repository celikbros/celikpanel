//go:build linux

package dnsenginerecovery

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
	"github.com/alicelik/celikpanel/internal/transport"
)

// RemoveExactPDNSAdoptionTargetReceipt is one host effect of an independently
// admitted adoption inverse, not an admission decision. Its caller must hold
// both release and host locks, prove the exact accepted job and absent worker,
// and reprove the frozen native PowerDNS source before and after this removal.
// An absent receipt is an idempotent no-op, not proof of restored DNS.
func RemoveExactPDNSAdoptionTargetReceipt(
	policy dnsengineartifact.JournalPolicy,
	owner servicemutationledger.FileOwner,
	journal dnsengineartifact.SwitchJournalV1,
) error {
	if !policy.RequireOwner || policy.StateUID != owner.UID || policy.StateGID != owner.GID ||
		filepath.Base(policy.StatePath) != "dns-engine-state.json" ||
		journal.Mode != transport.DNSEngineSwitchModeAdopt ||
		journal.SourceEngine != "" || journal.TargetEngine != transport.DNSEnginePowerDNS ||
		journal.StateBefore.Exists ||
		(journal.Phase != dnsengineartifact.SwitchPhaseRollingBack &&
			journal.Phase != dnsengineartifact.SwitchPhaseRolledBack) {
		return errors.New("PowerDNS adoption receipt removal lacks the exact rollback scope")
	}
	if err := policy.ValidateSwitchJournal(journal); err != nil {
		return fmt.Errorf("validate PowerDNS adoption rollback journal: %w", err)
	}
	raw, present, err := servicemutationledger.ReadFile(policy.StatePath, 64<<10, owner)
	if err != nil {
		return fmt.Errorf("read PowerDNS adoption target receipt: %w", err)
	}
	if !present {
		return nil
	}
	state, _, err := dnsengineartifact.DecodeStateDocument(raw)
	if err != nil || !dnsengineartifact.ExactSwitchTargetStateV1(state, journal) {
		return errors.Join(errors.New("PowerDNS adoption target receipt differs from the exact rollback operation"), err)
	}
	if err := servicemutationledger.RemoveFileExact(policy.StatePath, raw, 64<<10, owner); err != nil {
		return fmt.Errorf("remove exact PowerDNS adoption target receipt: %w", err)
	}
	return nil
}
