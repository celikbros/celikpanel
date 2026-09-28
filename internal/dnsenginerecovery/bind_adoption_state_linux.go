//go:build linux

package dnsenginerecovery

import (
	"errors"
	"fmt"
	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
	"path/filepath"
)

// RemoveExactBINDAdoptionTargetReceipt removes only this accepted operation's
// target receipt. Caller holds both locks, excludes its worker and proves the
// native original owner BIND. Already absent is not evidence of DNS health.
func RemoveExactBINDAdoptionTargetReceipt(policy dnsengineartifact.JournalPolicy, owner servicemutationledger.FileOwner, j dnsengineartifact.SwitchJournalV1) error {
	kind, err := PlanNativeInverse(j)
	if err != nil || kind != NativeInverseBINDRunningAdoption ||
		!policy.RequireOwner || policy.StateUID != owner.UID || policy.StateGID != owner.GID ||
		filepath.Base(policy.StatePath) != "dns-engine-state.json" ||
		j.Schema != dnsengineartifact.SwitchJournalSchemaV2 || j.InversePlan == nil || j.InversePlan.SourceBIND == nil ||
		j.StateBefore.Exists ||
		(j.Phase != dnsengineartifact.SwitchPhaseRollingBack && j.Phase != dnsengineartifact.SwitchPhaseRolledBack) {
		return errors.New("BIND adoption state removal lacks the exact rollback scope")
	}
	if err := policy.ValidateSwitchJournal(j); err != nil {
		return err
	}
	raw, present, err := servicemutationledger.ReadFile(policy.StatePath, 64<<10, owner)
	if err != nil {
		return err
	}
	if !present {
		return nil
	}
	if j.Phase != dnsengineartifact.SwitchPhaseRollingBack {
		return errors.New("rolled-back BIND adoption receipt reappeared")
	}
	state, _, err := dnsengineartifact.DecodeStateDocument(raw)
	if err != nil || !dnsengineartifact.ExactSwitchTargetStateV1(state, j) {
		return errors.Join(errors.New("BIND adoption receipt differs from exact accepted target"), err)
	}
	if err := servicemutationledger.RemoveFileExact(policy.StatePath, raw, 64<<10, owner); err != nil {
		return fmt.Errorf("remove exact BIND adoption target: %w", err)
	}
	return nil
}
