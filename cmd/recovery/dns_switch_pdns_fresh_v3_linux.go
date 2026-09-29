//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"

	"github.com/alicelik/celikpanel/internal/binddns"
	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/dnsenginerecovery"
	"github.com/alicelik/celikpanel/internal/pdnsnative"
)

type freshPDNSRecoveryShapeV3 uint8

const (
	freshPDNSRecoveryUnknownV3 freshPDNSRecoveryShapeV3 = iota
	freshPDNSRecoveryIntentCleanV3
	freshPDNSRecoveryStagedV3
	freshPDNSRecoveryRenamedV3
	freshPDNSRecoveryRestoredV3
	freshPDNSRecoveryServingUnrecordedV3
	freshPDNSRecoveryServingRecordedV3
)

// assessInstalledFreshPrimaryV3 is a read-only native classification. The
// caller holds release/host locks, reads exact journal+ledger and excludes the
// accepted worker. In particular target-enable-intent may already be serving;
// phase alone cannot authorize a prestart inverse.
func assessInstalledFreshPrimaryV3(ctx context.Context, policy dnsengineartifact.JournalPolicy, j dnsengineartifact.SwitchJournalV1) (freshPDNSRecoveryShapeV3, error) {
	unknown := freshPDNSRecoveryUnknownV3
	if ctx == nil || ctx.Err() != nil || !policy.RequireOwner || policy.StateUID != 0 ||
		policy.StateGID == 0 || policy != installedDNSJournalPolicy(policy.StateGID) {
		return unknown, errors.New("v3 PowerDNS assessment requires fixed installed owner policy")
	}
	if err := policy.ValidateSwitchJournal(j); err != nil {
		return unknown, err
	}
	if j.Schema != dnsengineartifact.SwitchJournalSchemaV3 || j.PDNSFreshPlan == nil {
		return unknown, errors.New("v3 fresh PowerDNS journal is absent")
	}
	const target = "pdns.service"
	names := []string{"named.service", "bind9.service", target}
	units, err := dnsenginerecovery.ProbeNativeUnits(ctx, names, dnsenginerecovery.SystemdUnitRunner)
	if err != nil {
		return unknown, err
	}
	if len(units) != 3 || units[0].ActiveState != "inactive" || units[1].ActiveState != "inactive" ||
		!freshPDNSRecoveryTargetLoadV3(units[2], j.TargetUnitsBefore[0]) {
		return unknown, errors.New("v3 native DNS unit topology differs from the frozen fresh target")
	}
	pdnsGID, err := localServiceGroupID("/etc/group", "pdns")
	if err != nil {
		return unknown, err
	}
	configs, err := dnsenginerecovery.ProbeInstalledPDNSFreshConfigsV3(ctx, policy, j, pdnsGID)
	if err != nil {
		return unknown, err
	}
	allBefore, allAfter := true, true
	for _, state := range configs {
		allBefore = allBefore && state == dnsenginerecovery.PDNSTargetConfigBeforeV4
		allAfter = allAfter && state == dnsenginerecovery.PDNSTargetConfigAfterV4
	}
	var shape freshPDNSRecoveryShapeV3
	switch units[2].ActiveState {
	case "active":
		if err := verifyInstalledFreshPDNSNativeVersionV3(ctx); err != nil {
			return unknown, err
		}
		if j.Phase != dnsengineartifact.SwitchPhaseTargetEnableIntent &&
			j.Phase != dnsengineartifact.SwitchPhaseTargetStarted &&
			j.Phase != dnsengineartifact.SwitchPhaseTargetVerified &&
			j.Phase != dnsengineartifact.SwitchPhaseCommitted || !allAfter ||
			units[2].UnitFileState != "enabled" || j.PDNSFreshPlan.Candidate == nil {
			return unknown, errors.New("v3 active PowerDNS differs from the committed enable intent")
		}
		pid, err := verifyInstalledPDNSRuntime(ctx)
		if err != nil || pid == 0 {
			return unknown, errors.Join(errors.New("v3 native PowerDNS process is unknown"), err)
		}
		if j.PDNSFreshPlan.Native == nil {
			live, err := dnsenginerecovery.CaptureFreshPrimaryNativeV3(ctx, policy, j)
			if err != nil {
				return unknown, err
			}
			catalog, err := binddns.CatalogDomain(j.LocalIP)
			if err != nil {
				return unknown, err
			}
			if _, err := pdnsnative.ObserveFreshPrimaryCatalogTransition(*j.PDNSFreshPlan.Staged, live, catalog, j.PrimaryCatalogSerial); err != nil {
				return unknown, err
			}
			shape = freshPDNSRecoveryServingUnrecordedV3
		} else {
			if err := dnsenginerecovery.VerifyRecordedFreshPrimaryNativeV3(ctx, policy, j); err != nil {
				return unknown, err
			}
			shape = freshPDNSRecoveryServingRecordedV3
		}
		if again, err := verifyInstalledPDNSRuntime(ctx); err != nil || again != pid {
			return unknown, errors.Join(errors.New("v3 PowerDNS process changed during assessment"), err)
		}
	case "inactive":
		var stoppedErr error
		if units[2].LoadState == "masked" {
			stoppedErr = dnsenginerecovery.ProbeStoppedPDNSPersistentMask(ctx)
		} else {
			stoppedErr = dnsenginerecovery.ProbeStoppedUnit(ctx, target, dnsenginerecovery.SystemdUnitRunner, dnsenginerecovery.SystemdPDNSRuntimeRunner)
		}
		if stoppedErr != nil {
			return unknown, stoppedErr
		}
		if err := probeNoDNSPort53ListenersV4(ctx); err != nil {
			return unknown, err
		}
		if j.PDNSFreshPlan.Candidate == nil {
			if !freshPDNSAllAbsentV3(policy.StatePath) {
				return unknown, errors.New("v3 unstarted target unexpectedly has a state receipt")
			}
			if !dnsenginerecovery.FreshPrimaryPrestartJournalV3(j) || !allBefore ||
				units[2].UnitFileState != j.TargetUnitsBefore[0].UnitFileState ||
				!freshPDNSAllAbsentV3(j.PDNSCandidatePath, policy.PDNSDatabasePath, policy.PDNSDatabasePath+"-wal", policy.PDNSDatabasePath+"-shm", policy.PDNSDatabasePath+"-journal") {
				return unknown, errors.New("v3 unsealed intent has unknown native effects")
			}
			shape = freshPDNSRecoveryIntentCleanV3
		} else {
			// The same pre-start shape names this owner command in dns-switch-status.
			if !dnsenginerecovery.FreshPrimaryPrestartJournalV3(j) {
				return unknown, errors.New("v3 stopped target lacks a bounded prestart decision")
			}
			if (j.Phase == dnsengineartifact.SwitchPhaseRollingBack ||
				j.Phase == dnsengineartifact.SwitchPhaseRollingBackTargetEnable ||
				j.Phase == dnsengineartifact.SwitchPhaseRolledBack) && allBefore &&
				units[2].UnitFileState == j.TargetUnitsBefore[0].UnitFileState &&
				freshPDNSAllAbsentV3(j.PDNSCandidatePath, policy.PDNSDatabasePath,
					policy.PDNSDatabasePath+"-wal", policy.PDNSDatabasePath+"-shm", policy.PDNSDatabasePath+"-journal") {
				shape = freshPDNSRecoveryRestoredV3
				break
			}
			proof := *j.PDNSFreshPlan.Candidate
			if freshPDNSAllAbsentV3(policy.PDNSDatabasePath, policy.PDNSDatabasePath+"-wal", policy.PDNSDatabasePath+"-shm", policy.PDNSDatabasePath+"-journal") {
				actual, err := dnsenginerecovery.CapturePDNSTargetCandidateV4(proof.Path)
				if err != nil || actual != proof {
					return unknown, errors.Join(errors.New("v3 staged candidate changed"), err)
				}
				shape = freshPDNSRecoveryStagedV3
			} else if err := dnsenginerecovery.VerifyPDNSTargetLiveV4(proof, policy.PDNSDatabasePath); err == nil &&
				freshPDNSAllAbsentV3(policy.PDNSDatabasePath+"-wal", policy.PDNSDatabasePath+"-shm", policy.PDNSDatabasePath+"-journal") {
				shape = freshPDNSRecoveryRenamedV3
			} else {
				return unknown, errors.New("v3 stopped target is neither exact staged nor exact unstarted rename")
			}
			if units[2].UnitFileState != j.TargetUnitsBefore[0].UnitFileState &&
				!(j.Phase == dnsengineartifact.SwitchPhaseTargetEnableIntent || j.Phase == dnsengineartifact.SwitchPhaseRollingBackTargetEnable) {
				return unknown, errors.New("v3 target enablement changed before enable-intent")
			}
		}
	default:
		return unknown, errors.New("v3 PowerDNS unit is neither active nor proven stopped")
	}
	againUnits, err := dnsenginerecovery.ProbeNativeUnits(ctx, names, dnsenginerecovery.SystemdUnitRunner)
	if err != nil || !reflect.DeepEqual(units, againUnits) {
		return unknown, errors.Join(errors.New("v3 native units changed during assessment"), err)
	}
	againConfigs, err := dnsenginerecovery.ProbeInstalledPDNSFreshConfigsV3(ctx, policy, j, pdnsGID)
	if err != nil || !reflect.DeepEqual(configs, againConfigs) {
		return unknown, errors.Join(errors.New("v3 PowerDNS configs changed during assessment"), err)
	}
	if ctx.Err() != nil {
		return unknown, ctx.Err()
	}
	return shape, nil
}

func freshPDNSAllAbsentV3(paths ...string) bool {
	for _, path := range paths {
		if path == "" {
			return false
		}
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			return false
		}
	}
	return true
}

func freshPDNSRecoveryDescriptionV3(shape freshPDNSRecoveryShapeV3) string {
	switch shape {
	case freshPDNSRecoveryIntentCleanV3:
		return "empty intent with no native effect"
	case freshPDNSRecoveryStagedV3:
		return "exact unstarted staged candidate"
	case freshPDNSRecoveryRestoredV3:
		return "exact prestart inverse restored original native state"
	case freshPDNSRecoveryRenamedV3:
		return "exact unstarted live rename"
	case freshPDNSRecoveryServingUnrecordedV3:
		return "PowerDNS serving; native observation is not durable"
	case freshPDNSRecoveryServingRecordedV3:
		return "PowerDNS serving with exact native receipt"
	default:
		return fmt.Sprint("unknown PowerDNS native state")
	}
}

func freshPDNSRecoveryTargetLoadV3(seen dnsenginerecovery.NativeUnitObservation, before dnsengineartifact.UnitSnapshot) bool {
	if seen.Name != "pdns.service" {
		return false
	}
	if seen.LoadState == "loaded" {
		return true
	}
	return seen.LoadState == "masked" && seen.ActiveState == "inactive" &&
		seen.UnitFileState == "masked" &&
		before == (dnsengineartifact.UnitSnapshot{Name: "pdns.service", LoadState: "masked", ActiveState: "inactive", UnitFileState: "masked"})
}
