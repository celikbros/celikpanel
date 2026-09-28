//go:build linux

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/alicelik/celikpanel/internal/bindroot"
	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/dnsenginerecovery"
	"github.com/alicelik/celikpanel/internal/dnslistener"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
)

var pdnsTargetStageUnitNamesV4 = []string{"named.service", "bind9.service", "pdns.service"}

// assessInstalledPDNSTargetStageNativeV4 is read-only. Its caller must hold the
// release and host locks, validate the accepted request/ledger and exclude the
// worker. This probe admits the exact pre-start candidate or rename cut, including
// enabled/inactive only with a durable V4 enable-intent rollback decision.
func assessInstalledPDNSTargetStageNativeV4(ctx context.Context, policy dnsengineartifact.JournalPolicy, j dnsengineartifact.SwitchJournalV1) (dnsenginerecovery.PDNSTargetStageState, error) {
	unknown := dnsenginerecovery.PDNSTargetStageUnknown
	if ctx == nil || ctx.Err() != nil || !policy.RequireOwner || policy.StateUID != 0 || policy.StateGID == 0 ||
		policy != installedDNSJournalPolicy(policy.StateGID) {
		return unknown, errors.New("PowerDNS target assessment requires the fixed installed owner policy and live context")
	}
	if err := policy.ValidateSwitchJournal(j); err != nil {
		return unknown, err
	}
	if j.Schema != dnsengineartifact.SwitchJournalSchemaV4 || j.PDNSTargetPlan == nil || j.PDNSTargetPlan.Candidate == nil ||
		(j.Phase != dnsengineartifact.SwitchPhaseRollingBack && j.Phase != dnsengineartifact.SwitchPhaseRollingBackTargetEnable && j.Phase != dnsengineartifact.SwitchPhaseRolledBack) {
		return unknown, errors.New("PowerDNS target assessment requires staged V4 rollback evidence")
	}
	owner := servicemutationledger.FileOwner{UID: policy.StateUID, GID: policy.StateGID}
	state, present, err := servicemutationledger.ReadFile(policy.StatePath, 64<<10, owner)
	if err != nil || !present || !bytes.Equal(state, j.StateBefore.Data) {
		return unknown, errors.Join(errors.New("managed BIND source state differs from frozen preimage"), err)
	}
	layout, bindGID, err := installedBINDLayout()
	if err != nil || layout != bindroot.APT {
		return unknown, errors.Join(errors.New("V4 source requires installed Debian BIND"), err)
	}
	if err := dnsenginerecovery.VerifyManagedBINDSourceProofV4(ctx, *j.PDNSTargetPlan.SourceBIND, bindGID); err != nil {
		return unknown, fmt.Errorf("managed BIND source: %w", err)
	}
	if err := verifyInstalledBINDVendorAndUnit(ctx); err != nil {
		return unknown, err
	}
	if err := bindInverseSourceUnitIdentity(ctx); err != nil {
		return unknown, err
	}
	pdnsGID, err := localServiceGroupID("/etc/group", "pdns")
	if err != nil {
		return unknown, err
	}
	configs, err := dnsenginerecovery.ProbeInstalledPDNSTargetConfigsV4(ctx, policy, j, pdnsGID)
	if err != nil {
		return unknown, fmt.Errorf("PowerDNS config checkpoint: %w", err)
	}
	units, err := dnsenginerecovery.ProbeNativeUnits(ctx, pdnsTargetStageUnitNamesV4, dnsenginerecovery.SystemdUnitRunner)
	if err != nil {
		return unknown, err
	}
	activeSource, err := classifyPDNSTargetStageUnitsV4(j, units)
	if err != nil {
		return unknown, err
	}
	if err := dnsenginerecovery.ProbeStoppedUnit(ctx, "pdns.service", dnsenginerecovery.SystemdUnitRunner, dnsenginerecovery.SystemdPDNSRuntimeRunner); err != nil {
		return unknown, err
	}
	var sourcePID uint64
	if activeSource {
		sourcePID, err = verifyInstalledBINDRuntime(ctx)
		if err != nil {
			return unknown, err
		}
		if err := bindInverseAuthorityAnswers(ctx, j, "named", sourcePID); err != nil {
			return unknown, err
		}
	} else {
		if err := dnsenginerecovery.ProbeStoppedUnit(ctx, "named.service", dnsenginerecovery.SystemdUnitRunner, dnsenginerecovery.SystemdBINDRuntimeRunner); err != nil {
			return unknown, err
		}
		if err := probeNoDNSPort53ListenersV4(ctx); err != nil {
			return unknown, err
		}
	}
	stageErr := dnsenginerecovery.VerifyStagedPDNSTargetV4(policy, j)
	renamedErr := dnsenginerecovery.VerifyRenamedPDNSTargetV4(policy, j)
	restoredErr := dnsenginerecovery.VerifyRestoredPDNSTargetV4(policy, j)
	result, err := classifyPDNSTargetStageCheckpointV4(j.Phase, stageErr == nil, renamedErr == nil, restoredErr == nil,
		units[2].UnitFileState == "enabled", activeSource, pdnsTargetAllBeforeConfigsV4(configs), pdnsTargetSourceUnitsExactV4(j, units))
	if err != nil {
		return unknown, errors.Join(err, stageErr, renamedErr, restoredErr)
	}
	if err := dnsenginerecovery.VerifyManagedBINDSourceProofV4(ctx, *j.PDNSTargetPlan.SourceBIND, bindGID); err != nil {
		return unknown, err
	}
	again, err := dnsenginerecovery.ProbeNativeUnits(ctx, pdnsTargetStageUnitNamesV4, dnsenginerecovery.SystemdUnitRunner)
	if err != nil || !reflect.DeepEqual(units, again) {
		return unknown, errors.Join(errors.New("DNS native units changed during target assessment"), err)
	}
	if activeSource {
		pid, err := verifyInstalledBINDRuntime(ctx)
		if err != nil || pid != sourcePID {
			return unknown, errors.Join(errors.New("BIND process changed during target assessment"), err)
		}
		if err := bindInverseAuthorityAnswers(ctx, j, "named", pid); err != nil {
			return unknown, err
		}
	} else if err := probeNoDNSPort53ListenersV4(ctx); err != nil {
		return unknown, err
	}
	againState, present, err := servicemutationledger.ReadFile(policy.StatePath, 64<<10, owner)
	if err != nil || !present || !bytes.Equal(state, againState) {
		return unknown, errors.Join(errors.New("managed BIND source state changed during target assessment"), err)
	}
	againConfigs, err := dnsenginerecovery.ProbeInstalledPDNSTargetConfigsV4(ctx, policy, j, pdnsGID)
	if err != nil || !reflect.DeepEqual(configs, againConfigs) {
		return unknown, errors.Join(errors.New("PowerDNS config checkpoint changed during target assessment"), err)
	}
	switch result {
	case dnsenginerecovery.PDNSTargetStageRestored:
		err = dnsenginerecovery.VerifyRestoredPDNSTargetV4(policy, j)
	case dnsenginerecovery.PDNSTargetStageRenamed, dnsenginerecovery.PDNSTargetStageRenamedEnabled:
		err = dnsenginerecovery.VerifyRenamedPDNSTargetV4(policy, j)
	default:
		err = dnsenginerecovery.VerifyStagedPDNSTargetV4(policy, j)
	}
	if err != nil {
		return unknown, fmt.Errorf("PowerDNS target changed during assessment: %w", err)
	}
	return result, ctx.Err()
}

// A completed candidate unlink is only an intermediate inverse effect. The
// exact absence proof plus frozen config and unit states permits replaying the
// same rollback; terminal success still requires all original state restored.
func classifyPDNSTargetStageCheckpointV4(phase string, staged, renamed, absent, targetEnabled, sourceActive, allBefore, sourceUnitsExact bool) (dnsenginerecovery.PDNSTargetStageState, error) {
	unknown := dnsenginerecovery.PDNSTargetStageUnknown
	shapes := 0
	for _, exact := range []bool{staged, renamed, absent} {
		if exact {
			shapes++
		}
	}
	if shapes != 1 || (phase != dnsengineartifact.SwitchPhaseRollingBack && phase != dnsengineartifact.SwitchPhaseRollingBackTargetEnable && phase != dnsengineartifact.SwitchPhaseRolledBack) {
		return unknown, errors.New("PowerDNS target is not exactly one frozen pre-start candidate, live rename or target absence")
	}
	if targetEnabled {
		if phase != dnsengineartifact.SwitchPhaseRollingBackTargetEnable || !renamed || sourceActive {
			return unknown, errors.New("enabled PowerDNS target lacks its exact pre-start enable-intent rollback")
		}
		return dnsenginerecovery.PDNSTargetStageRenamedEnabled, nil
	}
	if absent && sourceActive && allBefore && sourceUnitsExact {
		return dnsenginerecovery.PDNSTargetStageRestored, nil
	}
	if phase != dnsengineartifact.SwitchPhaseRolledBack {
		if renamed {
			if sourceActive {
				return unknown, errors.New("renamed PowerDNS candidate cannot coexist with active BIND source")
			}
			return dnsenginerecovery.PDNSTargetStageRenamed, nil
		}
		return dnsenginerecovery.PDNSTargetStageNeedsRestore, nil
	}
	return unknown, errors.New("rolled-back checkpoint lacks complete BIND restoration")
}
func classifyPDNSTargetStageUnitsV4(j dnsengineartifact.SwitchJournalV1, units []dnsenginerecovery.NativeUnitObservation) (bool, error) {
	if len(units) != 3 || len(j.SourceUnitsBefore) != 2 || len(j.TargetUnitsBefore) != 1 ||
		units[0].Name != "named.service" || units[1].Name != "bind9.service" || units[2].Name != "pdns.service" {
		return false, errors.New("PowerDNS target native unit set is incomplete")
	}
	for i, saved := range []dnsengineartifact.UnitSnapshot{j.SourceUnitsBefore[1], j.SourceUnitsBefore[0], j.TargetUnitsBefore[0]} {
		if saved.Name != units[i].Name {
			return false, errors.New("PowerDNS target unit order differs from frozen journal")
		}
	}
	targetExact := pdnsTargetUnitExactV4(units[2], j.TargetUnitsBefore[0])
	targetEnabledByIntent := j.Phase == dnsengineartifact.SwitchPhaseRollingBackTargetEnable &&
		units[2].LoadState == j.TargetUnitsBefore[0].LoadState &&
		units[2].ActiveState == "inactive" && units[2].UnitFileState == "enabled"
	if (!targetExact && !targetEnabledByIntent) || units[2].ActiveState != "inactive" {
		return false, errors.New("PowerDNS target unit differs from frozen inactive preimage")
	}
	active := pdnsTargetSourceUnitsExactV4(j, units)
	stoppedOrRestoring := true
	for i, unit := range units[:2] {
		saved := j.SourceUnitsBefore[1-i]
		// disable --now yields disabled/inactive. dnsunitrestore.Restore
		// may enable each alias before starting named, so a reboot or cut can
		// leave enabled/inactive. Only the frozen enablement class is accepted.
		if unit.LoadState != "loaded" || unit.ActiveState != "inactive" ||
			(unit.UnitFileState != "disabled" && unit.UnitFileState != saved.UnitFileState) {
			stoppedOrRestoring = false
		}
	}
	if !active && !stoppedOrRestoring {
		return false, errors.New("BIND source units differ from frozen, operation-stopped or inverse-enabling state")
	}
	if active && units[0].ActiveState != "active" {
		return false, errors.New("frozen BIND source is not active")
	}
	return active, nil
}

func pdnsTargetUnitExactV4(current dnsenginerecovery.NativeUnitObservation, saved dnsengineartifact.UnitSnapshot) bool {
	return current.Name == saved.Name && current.LoadState == saved.LoadState && current.ActiveState == saved.ActiveState && current.UnitFileState == saved.UnitFileState
}

func pdnsTargetSourceUnitsExactV4(j dnsengineartifact.SwitchJournalV1, units []dnsenginerecovery.NativeUnitObservation) bool {
	return len(units) == 3 && len(j.SourceUnitsBefore) == 2 && pdnsTargetUnitExactV4(units[0], j.SourceUnitsBefore[1]) && pdnsTargetUnitExactV4(units[1], j.SourceUnitsBefore[0])
}

func pdnsTargetAllBeforeConfigsV4(states []dnsenginerecovery.PDNSTargetConfigStateV4) bool {
	if len(states) != 3 {
		return false
	}
	for _, state := range states {
		if state != dnsenginerecovery.PDNSTargetConfigBeforeV4 {
			return false
		}
	}
	return true
}

func probeNoDNSPort53ListenersV4(ctx context.Context) error {
	for i := 0; i < 2; i++ {
		rows, err := dnsenginerecovery.SSListenerRunner(ctx)
		if err != nil {
			return err
		}
		if err := requireNoPublicDNSPort53ListenersV4(rows); err != nil {
			return err
		}
	}
	return ctx.Err()
}

// requireNoPublicDNSPort53ListenersV4 uses the same canonical row parser as
// the producer. Debian's local resolver stub is not public authority; malformed
// rows and every non-loopback/non-link-local listener still fail closed.
func requireNoPublicDNSPort53ListenersV4(rows []byte) error {
	for _, line := range strings.Split(strings.TrimSpace(string(rows)), "\n") {
		if line == "" {
			continue
		}
		row, err := dnslistener.ParseRow(line)
		if err != nil {
			return err
		}
		if !row.Address.IsLoopback() && !row.Address.IsLinkLocalUnicast() {
			return errors.New("another public DNS listener occupies port 53 while BIND is stopped")
		}
	}
	return nil
}
