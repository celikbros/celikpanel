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

// ReplaceFreshPrimaryJournalV3 is the independent exact-CAS checkpoint for
// the V3 owner decision. The caller holds release and host locks, excludes the
// accepted worker and re-proves native state immediately before every effect.
// This adapter cannot turn an unrecorded poststart target into a rollback.
func ReplaceFreshPrimaryJournalV3(policy dnsengineartifact.JournalPolicy, owner servicemutationledger.FileOwner,
	before, after dnsengineartifact.SwitchJournalV1) error {
	if !policy.RequireOwner || policy.StateUID != owner.UID || policy.StateGID != owner.GID ||
		filepath.Base(policy.StatePath) != "dns-engine-state.json" ||
		before.Schema != dnsengineartifact.SwitchJournalSchemaV3 || after.Schema != dnsengineartifact.SwitchJournalSchemaV3 {
		return errors.New("v3 fresh PowerDNS checkpoint requires fixed owner and schema")
	}
	prior, err := policy.EncodeSwitchJournal(before)
	if err != nil {
		return err
	}
	next, err := policy.EncodeSwitchJournal(after)
	if err != nil {
		return err
	}
	if !dnsengineartifact.SameImmutablePDNSFreshPrimaryPlanV3(before, after) {
		return errors.New("v3 fresh PowerDNS checkpoint changes frozen evidence")
	}
	if !dnsengineartifact.ValidPDNSFreshPrimaryForwardPhaseTransitionV3(before, after) &&
		!freshPrimaryRollbackPhaseV3(before, after) {
		return errors.New("v3 fresh PowerDNS checkpoint skips a protected phase")
	}
	path := filepath.Join(filepath.Dir(policy.StatePath), "dns-engine-switch-journal.json")
	return WriteJournalCheckpoint(policy, after, JournalCheckpointOps{
		Persist: func(encoded []byte) error {
			if !bytes.Equal(encoded, next) {
				return errors.New("v3 journal encoding changed")
			}
			return servicemutationledger.ReplaceFileExact(path, prior, next, dnsengineartifact.SwitchJournalLimit, owner)
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

func freshPrimaryRollbackPhaseV3(before, after dnsengineartifact.SwitchJournalV1) bool {
	if before.PDNSFreshPlan == nil || before.PDNSFreshPlan.Native != nil ||
		after.PDNSFreshPlan == nil || after.PDNSFreshPlan.Native != nil {
		return false
	}
	copy := before
	copy.Phase = after.Phase
	if !reflect.DeepEqual(copy, after) {
		return false
	}
	switch before.Phase {
	case dnsengineartifact.SwitchPhaseIntent, dnsengineartifact.SwitchPhaseTargetStaged:
		return after.Phase == dnsengineartifact.SwitchPhaseRollingBack
	case dnsengineartifact.SwitchPhaseTargetEnableIntent:
		return after.Phase == dnsengineartifact.SwitchPhaseRollingBackTargetEnable
	case dnsengineartifact.SwitchPhaseRollingBack, dnsengineartifact.SwitchPhaseRollingBackTargetEnable:
		return after.Phase == dnsengineartifact.SwitchPhaseRolledBack
	default:
		return false
	}
}
