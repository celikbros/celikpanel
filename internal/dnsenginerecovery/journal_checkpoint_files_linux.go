//go:build linux

package dnsenginerecovery

import (
	"bytes"
	"errors"
	"path/filepath"
	"reflect"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
)

// ReplaceRollbackJournalPhase is the independent executor's narrow durable
// checkpoint adapter. Its caller must already hold the release and DNS host
// locks, prove the accepted operation and native inverse admission, and supply
// the exact journal it observed. It cannot create a missing journal or advance
// any phase except the bounded rollback checkpoints, including the V4-only
// target-enable decision. It does not run the inverse.
func ReplaceRollbackJournalPhase(
	policy dnsengineartifact.JournalPolicy,
	owner servicemutationledger.FileOwner,
	before, after dnsengineartifact.SwitchJournalV1,
) error {
	if before.Schema == dnsengineartifact.SwitchJournalSchemaV3 || after.Schema == dnsengineartifact.SwitchJournalSchemaV3 {
		return errors.New("v3 fresh PowerDNS primary requires its independent rollback checkpoint adapter")
	}
	if !policy.RequireOwner || policy.StateUID != owner.UID || policy.StateGID != owner.GID ||
		filepath.Base(policy.StatePath) != "dns-engine-state.json" {
		return errors.New("DNS rollback checkpoint host owner or state path is untrusted")
	}
	prior, err := policy.EncodeSwitchJournal(before)
	if err != nil {
		return err
	}
	next, err := policy.EncodeSwitchJournal(after)
	if err != nil {
		return err
	}
	allowed := allowedRollbackJournalPhase(before, after)
	unchanged := before
	unchanged.Phase = after.Phase
	if !allowed || !reflect.DeepEqual(unchanged, after) {
		return errors.New("DNS rollback checkpoint changes frozen evidence or has an invalid phase transition")
	}
	path := filepath.Join(filepath.Dir(policy.StatePath), "dns-engine-switch-journal.json")
	return WriteJournalCheckpoint(policy, after, JournalCheckpointOps{
		Persist: func(encoded []byte) error {
			if !bytes.Equal(encoded, next) {
				return errors.New("DNS rollback checkpoint encoding changed")
			}
			return servicemutationledger.ReplaceFileExact(path, prior, encoded, dnsengineartifact.SwitchJournalLimit, owner)
		},
		Read: func() (dnsengineartifact.SwitchJournalV1, bool, error) {
			raw, exists, err := servicemutationledger.ReadFile(path, dnsengineartifact.SwitchJournalLimit, owner)
			if err != nil || !exists {
				return dnsengineartifact.SwitchJournalV1{}, exists, err
			}
			journal, err := policy.DecodeSwitchJournal(raw)
			return journal, err == nil, err
		},
	})
}

func allowedRollbackJournalPhase(before, after dnsengineartifact.SwitchJournalV1) bool {
	if before.Schema == dnsengineartifact.SwitchJournalSchemaV4 {
		switch {
		case before.Phase == dnsengineartifact.SwitchPhaseTargetEnableIntent &&
			after.Phase == dnsengineartifact.SwitchPhaseRollingBackTargetEnable:
			return true
		case before.Phase == dnsengineartifact.SwitchPhaseRollingBackTargetEnable &&
			after.Phase == dnsengineartifact.SwitchPhaseRolledBack:
			return true
		}
	}
	if before.Phase == dnsengineartifact.SwitchPhaseRollingBack {
		return after.Phase == dnsengineartifact.SwitchPhaseRolledBack
	}
	return before.Phase != dnsengineartifact.SwitchPhaseTargetVerified &&
		before.Phase != dnsengineartifact.SwitchPhaseCommitted &&
		before.Phase != dnsengineartifact.SwitchPhaseRolledBack &&
		before.Phase != dnsengineartifact.SwitchPhaseTargetEnableIntent &&
		before.Phase != dnsengineartifact.SwitchPhaseRollingBackTargetEnable &&
		after.Phase == dnsengineartifact.SwitchPhaseRollingBack
}
