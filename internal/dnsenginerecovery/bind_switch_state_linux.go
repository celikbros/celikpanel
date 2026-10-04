//go:build linux

package dnsenginerecovery

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
	"github.com/alicelik/celikpanel/internal/transport"
)

// RestoreExactBINDSwitchSourceReceipt performs only the state-file effect of
// an admitted inactive-target BIND inverse. The installed caller must hold both
// locks, exclude the accepted worker, and prove native config/unit ownership
// before calling. This function neither starts DNS nor establishes its health.
// Only the exact target receipt may be replaced; the frozen source is a no-op.
// Missing or owner-modified evidence is preserved, never reconstructed.
func RestoreExactBINDSwitchSourceReceipt(
	policy dnsengineartifact.JournalPolicy,
	owner servicemutationledger.FileOwner,
	journal dnsengineartifact.SwitchJournalV1,
) error {
	if !policy.RequireOwner || policy.StateUID != owner.UID || policy.StateGID != owner.GID ||
		filepath.Base(policy.StatePath) != "dns-engine-state.json" ||
		journal.Schema != dnsengineartifact.SwitchJournalSchemaV2 ||
		journal.Mode != transport.DNSEngineSwitchModeSwitch ||
		journal.SourceEngine != transport.DNSEnginePowerDNS || journal.TargetEngine != transport.DNSEngineBIND ||
		journal.Topology != transport.DNSTopologyStandalone || !journal.StateBefore.Exists ||
		(journal.Phase != dnsengineartifact.SwitchPhaseRollingBack && journal.Phase != dnsengineartifact.SwitchPhaseRolledBack) {
		return errors.New("BIND source receipt restoration lacks the exact rollback scope")
	}
	if err := policy.ValidateSwitchJournal(journal); err != nil {
		return fmt.Errorf("validate BIND rollback journal: %w", err)
	}
	raw, present, err := servicemutationledger.ReadFile(policy.StatePath, 64<<10, owner)
	if err != nil {
		return fmt.Errorf("read BIND rollback state: %w", err)
	}
	if !present {
		return errors.New("BIND rollback state is missing; preserve the retained evidence")
	}
	if bytes.Equal(raw, journal.StateBefore.Data) {
		return nil
	}
	if journal.Phase != dnsengineartifact.SwitchPhaseRollingBack {
		return errors.New("rolled-back BIND checkpoint no longer has the frozen source receipt")
	}
	state, _, err := dnsengineartifact.DecodeStateDocument(raw)
	if err != nil || !dnsengineartifact.ExactSwitchTargetStateV1(state, journal) {
		return errors.Join(errors.New("BIND rollback state differs from the exact target and frozen source"), err)
	}
	if err := servicemutationledger.ReplaceFileExact(policy.StatePath, raw, journal.StateBefore.Data, 64<<10, owner); err != nil {
		return fmt.Errorf("restore exact BIND source receipt: %w", err)
	}
	return nil
}
