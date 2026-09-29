//go:build linux

package main

import (
	"context"
	"errors"
	"os"
	"reflect"
	"time"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/dnsenginerecovery"
	"github.com/alicelik/celikpanel/internal/dnsunitrestore"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
)

// recoverFreshPrimaryV3 routes the accepted V3 journal of a fresh paired
// PowerDNS primary. It is reached only from RecoverSwitch, whose callers hold
// the accepted operation's host lock and have excluded its worker (startup,
// idle-boot and in-process same-request recovery).
//
// A journal that records only pre-start work goes to the same-request
// pre-start inverse, except a target-enable-intent whose PowerDNS unit is
// active: that target has started and goes forward. Every other V3 journal
// goes forward (recoverFreshPrimaryForwardV3). A recovery that cannot finish
// returns an error that carries its guidance; the caller keeps the journal
// and holds only this DNS operation.
func recoverFreshPrimaryV3(ctx context.Context, id dnsengineartifact.SwitchIdentity, journal dnsEngineSwitchJournal) (dnsEngineSwitchRecoveryOutcome, error) {
	if ctx == nil || id.Validate() != nil || journal.Schema != dnsengineartifact.SwitchJournalSchemaV3 ||
		journal.MutationRequestID != id.RequestID || journal.MutationOwnerID != id.OwnerID ||
		journal.TargetEngine != id.Target || journal.ManifestQualifier != id.Qualifier {
		return dnsenginerecovery.OutcomeAbsent, errors.New("v3 recovery lacks the exact accepted journal")
	}
	profile, err := verifiedHostProfileForAnyFamily()
	if err != nil {
		return dnsenginerecovery.OutcomeAbsent, err
	}
	systemctl, err := executableForProfile(profile, string(profile.PackageManager), "systemctl")
	if err != nil {
		return dnsenginerecovery.OutcomeAbsent, err
	}
	running := func() string {
		observeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), dnsProbeTimeout)
		defer cancel()
		state, err := dnsSystemdStateGuard(systemctl).inspect(observeCtx, "pdns.service")
		switch {
		case err != nil:
			return ""
		case state.active():
			return "running"
		case state.activeState == "inactive":
			return "not running"
		case state.activeState == "failed":
			// Not running, but a start was attempted: routing is unchanged
			// (only "running" goes forward); the guidance says what it saw.
			return "failed"
		}
		return ""
	}
	if freshPrimaryPrestartJournalShapeV3(journal) &&
		!(journal.Phase == dnsengineartifact.SwitchPhaseTargetEnableIntent && running() == "running") {
		outcome, err := recoverFreshPrimaryPrestartV3(ctx, journal, systemctl)
		return outcome, classifyFreshPrimaryV3RecoveryError(journal.MutationRequestID, true, running(), err, journal.Phase)
	}
	outcome, err := recoverFreshPrimaryForwardV3(ctx, id, journal)
	return outcome, classifyFreshPrimaryV3RecoveryError(journal.MutationRequestID, false, running(), err, journal.Phase)
}

type freshPrimaryPrestartShapeV3 uint8

const (
	freshPrimaryPrestartUnknownV3 freshPrimaryPrestartShapeV3 = iota
	freshPrimaryPrestartIntentCleanV3
	freshPrimaryPrestartStagedV3
	freshPrimaryPrestartRenamedV3
	freshPrimaryPrestartRestoredV3
	// freshPrimaryPrestartIntentPartialV3: an unsealed intent whose only
	// native effect is this operation's own temporary candidate build file
	// (a crash while the database was being written). Not restored.
	freshPrimaryPrestartIntentPartialV3
)

// freshPrimaryPrestartObserversV3 are fixed, read-only native observers.
type freshPrimaryPrestartObserversV3 struct {
	policy dnsengineartifact.JournalPolicy
	// sourceUnitsInactive proves named.service and bind9.service inactive.
	sourceUnitsInactive func(context.Context) error
	targetUnit          func(context.Context) (dnsUnitSnapshot, error)
	// stoppedTarget is the first-install stopped-target proof: absent, the
	// guard's persistent mask or loaded; inactive/dead with zero systemd PIDs
	// and an empty cgroup; no public port-53 listener and only the resolver
	// stub on loopback/link-local, observed twice.
	stoppedTarget func(context.Context) error
	configs       func(context.Context, dnsEngineSwitchJournal) ([]dnsenginerecovery.PDNSTargetConfigStateV4, error)
	absent        func(...string) bool
	candidate     func(string) (dnsengineartifact.PDNSTargetCandidateProofV4, error)
	live          func(dnsengineartifact.PDNSTargetCandidateProofV4, string) error
	// partial lists the unsealed journal's own temporary build files present.
	partial func(dnsEngineSwitchJournal) ([]string, error)
}

func freshPrimaryPrestartRollbackPhaseV3(phase string) bool {
	return dnsenginerecovery.FreshPrimaryPrestartRollbackPhaseV3(phase)
}

// freshPrimaryPrestartTargetUnitV3 admits the unit states a stopped,
// never-started target can have. Before the enable-intent the unit must be
// exactly the frozen preimage; from the enable-intent it may also be loaded
// (unmasked, disabled or enabled). A mask is admitted only when the frozen
// preimage is the guard's persistent mask.
func freshPrimaryPrestartTargetUnitV3(unit, frozen dnsUnitSnapshot, phase string) bool {
	return dnsenginerecovery.FreshPrimaryPrestartTargetUnitV3(unit, frozen, phase)
}

// assessFreshPrimaryPrestartV3 classifies the native state of a pre-start
// journal without any effect. Unknown is returned with an error; a change
// the install did not make is marked with freshPrimaryV3ChangedError.
func assessFreshPrimaryPrestartV3(ctx context.Context, j dnsEngineSwitchJournal, obs freshPrimaryPrestartObserversV3) (freshPrimaryPrestartShapeV3, error) {
	unknown := freshPrimaryPrestartUnknownV3
	if ctx == nil || obs.sourceUnitsInactive == nil || obs.targetUnit == nil || obs.stoppedTarget == nil ||
		obs.configs == nil || obs.absent == nil || obs.candidate == nil || obs.live == nil || obs.partial == nil {
		return unknown, errors.New("v3 pre-start assessment observers are incomplete")
	}
	if err := ctx.Err(); err != nil {
		return unknown, err
	}
	if err := obs.policy.ValidateSwitchJournal(j); err != nil {
		return unknown, err
	}
	if !freshPrimaryPrestartJournalShapeV3(j) {
		return unknown, errors.New("v3 journal records no pre-start shape")
	}
	frozen := j.TargetUnitsBefore[0]
	if err := obs.sourceUnitsInactive(ctx); err != nil {
		return unknown, err
	}
	unit, err := obs.targetUnit(ctx)
	if err != nil {
		return unknown, err
	}
	if !freshPrimaryPrestartTargetUnitV3(unit, frozen, j.Phase) {
		return unknown, errors.New("v3 PowerDNS unit is not a stopped, never-started target of this install")
	}
	if err := obs.stoppedTarget(ctx); err != nil {
		return unknown, err
	}
	configs, err := obs.configs(ctx, j)
	if err != nil {
		return unknown, freshPrimaryV3Changed(freshPrimaryV3ChangedConfig, err)
	}
	allBefore := len(configs) > 0
	for _, state := range configs {
		allBefore = allBefore && state == dnsenginerecovery.PDNSTargetConfigBeforeV4
	}
	if !obs.absent(obs.policy.StatePath) {
		return unknown, freshPrimaryV3Changed(freshPrimaryV3ChangedState, errors.New("a DNS engine state record exists before PowerDNS started"))
	}
	db := obs.policy.PDNSDatabasePath
	sidecars := []string{db + "-wal", db + "-shm", db + "-journal"}
	shape := unknown
	plan := j.PDNSFreshPlan
	if plan.Candidate == nil {
		if !allBefore {
			return unknown, freshPrimaryV3Changed(freshPrimaryV3ChangedConfig, errors.New("configuration changed before a candidate was staged"))
		}
		if !obs.absent(append([]string{j.PDNSCandidatePath, db}, sidecars...)...) {
			return unknown, errors.New("v3 unsealed intent has unknown native effects")
		}
		// The candidate is built under a temporary name and renamed only when
		// complete, so a crash during the build leaves only that file.
		partial, err := obs.partial(j)
		if err != nil {
			return unknown, err
		}
		shape = freshPrimaryPrestartIntentCleanV3
		if len(partial) != 0 {
			shape = freshPrimaryPrestartIntentPartialV3
		}
	} else {
		proof := *plan.Candidate
		switch {
		case freshPrimaryPrestartRollbackPhaseV3(j.Phase) && allBefore && unit == frozen &&
			obs.absent(append([]string{j.PDNSCandidatePath, db}, sidecars...)...):
			shape = freshPrimaryPrestartRestoredV3
		case obs.absent(append([]string{db}, sidecars...)...):
			actual, err := obs.candidate(proof.Path)
			if err != nil || actual != proof {
				return unknown, freshPrimaryV3Changed(freshPrimaryV3ChangedDatabase, errors.Join(errors.New("staged candidate differs from its sealed identity"), err))
			}
			shape = freshPrimaryPrestartStagedV3
		case obs.absent(sidecars...):
			if err := obs.live(proof, db); err != nil {
				return unknown, freshPrimaryV3Changed(freshPrimaryV3ChangedDatabase, err)
			}
			shape = freshPrimaryPrestartRenamedV3
		default:
			return unknown, freshPrimaryV3Changed(freshPrimaryV3ChangedDatabase, errors.New("SQLite sidecars exist beside the never-started target"))
		}
	}
	againUnit, err := obs.targetUnit(ctx)
	if err != nil || againUnit != unit {
		return unknown, errors.Join(errors.New("v3 PowerDNS unit changed during assessment"), err)
	}
	againConfigs, err := obs.configs(ctx, j)
	if err != nil || !reflect.DeepEqual(configs, againConfigs) {
		return unknown, errors.Join(errors.New("v3 PowerDNS configuration changed during assessment"), err)
	}
	if err := ctx.Err(); err != nil {
		return unknown, err
	}
	return shape, nil
}

// freshPrimaryPrestartEffectsV3 are the exact durable effects of the Agent's
// pre-start inverse. Each receives a guard that rereads the unchanged journal
// and repeats the full assessment immediately before the effect.
type freshPrimaryPrestartEffectsV3 struct {
	read           func() (dnsEngineSwitchJournal, bool, error)
	assess         func(context.Context, dnsEngineSwitchJournal) (freshPrimaryPrestartShapeV3, error)
	checkpoint     func(dnsEngineSwitchJournal, dnsEngineSwitchJournal) error
	restoreUnit    func(context.Context, dnsengineartifact.UnitSnapshot, func(context.Context) error) error
	restoreRenamed func(dnsEngineSwitchJournal, func() error) error
	restoreConfigs func(context.Context, dnsEngineSwitchJournal, func(context.Context) error) error
	removeStaged   func(dnsEngineSwitchJournal, func() error) error
	removePartial  func(dnsEngineSwitchJournal, func() error) error
}

// runFreshPrimaryPrestartInverseV3 is the Agent's same-request inverse of a
// fresh paired PowerDNS primary that never started. It mirrors the owner
// command recover-dns-pdns-fresh-prestart: durable rollback decision first,
// then unit, renamed candidate, configuration and staged candidate, each
// behind the guard, then the rolled-back checkpoint after the restored state
// is proved. It never touches a started or changed target and never writes
// the ledger; the caller publishes the terminal verdict and retires the
// journal.
func runFreshPrimaryPrestartInverseV3(ctx context.Context, journal dnsEngineSwitchJournal, ops freshPrimaryPrestartEffectsV3) (dnsEngineSwitchRecoveryOutcome, error) {
	absent := dnsenginerecovery.OutcomeAbsent
	if ctx == nil || ops.read == nil || ops.assess == nil || ops.checkpoint == nil || ops.restoreUnit == nil ||
		ops.restoreRenamed == nil || ops.restoreConfigs == nil || ops.removeStaged == nil || ops.removePartial == nil {
		return absent, errors.New("v3 pre-start inverse operations are incomplete")
	}
	if !freshPrimaryPrestartJournalShapeV3(journal) {
		return absent, errors.New("v3 journal records no pre-start shape")
	}
	exact := func(want dnsEngineSwitchJournal) error {
		current, exists, err := ops.read()
		if err != nil || !exists || !reflect.DeepEqual(current, want) {
			return errors.Join(errors.New("v3 journal changed during pre-start recovery"), err)
		}
		return nil
	}
	restored := func(shape freshPrimaryPrestartShapeV3) bool {
		return shape == freshPrimaryPrestartRestoredV3 || shape == freshPrimaryPrestartIntentCleanV3
	}
	if err := exact(journal); err != nil {
		return absent, err
	}
	shape, err := ops.assess(ctx, journal)
	if err != nil {
		return absent, err
	}
	j := journal
	if !freshPrimaryPrestartRollbackPhaseV3(j.Phase) {
		if shape == freshPrimaryPrestartRestoredV3 {
			return absent, errors.New("v3 restored shape precedes the rollback decision")
		}
		next := j
		next.Phase = dnsengineartifact.SwitchPhaseRollingBack
		if j.Phase == dnsengineartifact.SwitchPhaseTargetEnableIntent {
			next.Phase = dnsengineartifact.SwitchPhaseRollingBackTargetEnable
		}
		if err := ops.checkpoint(j, next); err != nil {
			return absent, err
		}
		j = next
	}
	guard := func(guardCtx context.Context) error {
		if err := exact(j); err != nil {
			return err
		}
		_, err := ops.assess(guardCtx, j)
		return err
	}
	if j.Phase != dnsengineartifact.SwitchPhaseRolledBack {
		if j.PDNSFreshPlan.Candidate != nil {
			if j.Phase == dnsengineartifact.SwitchPhaseRollingBackTargetEnable {
				if err := ops.restoreUnit(ctx, j.TargetUnitsBefore[0], guard); err != nil {
					return absent, err
				}
			}
			current, err := ops.assess(ctx, j)
			if err != nil {
				return absent, err
			}
			if current == freshPrimaryPrestartRenamedV3 {
				if err := ops.restoreRenamed(j, func() error { return guard(ctx) }); err != nil {
					return absent, err
				}
			}
			if err := ops.restoreConfigs(ctx, j, guard); err != nil {
				return absent, err
			}
			if current, err = ops.assess(ctx, j); err != nil {
				return absent, err
			}
			if current == freshPrimaryPrestartStagedV3 {
				if err := ops.removeStaged(j, func() error { return guard(ctx) }); err != nil {
					return absent, err
				}
			}
		} else {
			current, err := ops.assess(ctx, j)
			if err != nil {
				return absent, err
			}
			if current == freshPrimaryPrestartIntentPartialV3 {
				if err := ops.removePartial(j, func() error { return guard(ctx) }); err != nil {
					return absent, err
				}
			}
		}
		if shape, err = ops.assess(ctx, j); err != nil || !restored(shape) {
			return absent, errors.Join(errors.New("v3 pre-start inverse did not reach the exact pre-install state"), err)
		}
		if err := exact(j); err != nil {
			return absent, err
		}
		next := j
		next.Phase = dnsengineartifact.SwitchPhaseRolledBack
		if err := ops.checkpoint(j, next); err != nil {
			return absent, err
		}
		j = next
	}
	if shape, err = ops.assess(ctx, j); err != nil || !restored(shape) {
		return absent, errors.Join(errors.New("v3 restored pre-install state changed before the terminal verdict"), err)
	}
	if err := exact(j); err != nil {
		return absent, err
	}
	return dnsenginerecovery.OutcomeRolledBack, nil
}

// recoverFreshPrimaryPrestartV3 binds the inverse to the fixed native host.
func recoverFreshPrimaryPrestartV3(ctx context.Context, journal dnsEngineSwitchJournal, systemctl string) (dnsEngineSwitchRecoveryOutcome, error) {
	policy := dnsJournalPolicy()
	if !policy.RequireOwner {
		return dnsenginerecovery.OutcomeAbsent, errors.New("v3 pre-start inverse requires the owned private state policy")
	}
	gid, err := resolvePDNSGroupGID(ctx)
	if err != nil {
		return dnsenginerecovery.OutcomeAbsent, err
	}
	obs := hostFreshPrimaryPrestartObserversV3(policy, systemctl, gid)
	owner := servicemutationledger.FileOwner{UID: policy.StateUID, GID: policy.StateGID}
	return runFreshPrimaryPrestartInverseV3(ctx, journal, freshPrimaryPrestartEffectsV3{
		read: readDNSEngineSwitchJournal,
		assess: func(assessCtx context.Context, j dnsEngineSwitchJournal) (freshPrimaryPrestartShapeV3, error) {
			return assessFreshPrimaryPrestartV3(assessCtx, j, obs)
		},
		checkpoint: func(before, after dnsEngineSwitchJournal) error {
			return dnsenginerecovery.ReplaceFreshPrimaryJournalV3(policy, owner, before, after)
		},
		restoreUnit: func(unitCtx context.Context, snapshot dnsengineartifact.UnitSnapshot, guard func(context.Context) error) error {
			return restoreFreshPrimaryTargetUnitV3(unitCtx, systemctl, snapshot, guard)
		},
		restoreRenamed: func(j dnsEngineSwitchJournal, guard func() error) error {
			return runDNSMutationWithSystemdParentProof(verifyBINDMaskParentMetadata, func() error {
				return dnsenginerecovery.RestoreFreshPrimaryRenamedV3(policy, j, guard)
			})
		},
		restoreConfigs: func(configCtx context.Context, j dnsEngineSwitchJournal, guard func(context.Context) error) error {
			return dnsenginerecovery.RestoreInstalledPDNSFreshConfigsV3(configCtx, policy, j, gid, guard)
		},
		removeStaged: func(j dnsEngineSwitchJournal, guard func() error) error {
			return dnsenginerecovery.RemoveFreshPrimaryStagedV3(policy, j, guard)
		},
		removePartial: func(j dnsEngineSwitchJournal, guard func() error) error {
			return dnsenginerecovery.RemoveFreshPrimaryPartialV3(policy, j, guard)
		},
	})
}

func hostFreshPrimaryPrestartObserversV3(policy dnsengineartifact.JournalPolicy, systemctl string, gid uint32) freshPrimaryPrestartObserversV3 {
	units := dnsSystemdStateGuard(systemctl)
	stopped := hostPDNSRollbackStoppedProofOps(systemctl)
	stopped.freshSource = true
	return freshPrimaryPrestartObserversV3{
		policy: policy,
		sourceUnitsInactive: func(ctx context.Context) error {
			for _, name := range []string{"named.service", "bind9.service"} {
				state, err := units.inspect(ctx, name)
				if err != nil {
					return err
				}
				if state.activeState != "inactive" {
					return errors.New("a BIND unit is not inactive beside the never-started PowerDNS target")
				}
			}
			return nil
		},
		targetUnit: func(ctx context.Context) (dnsUnitSnapshot, error) {
			state, err := units.inspect(ctx, "pdns.service")
			if err != nil {
				return dnsUnitSnapshot{}, err
			}
			return dnsUnitSnapshot{
				Name: state.name, LoadState: state.loadState,
				ActiveState: state.activeState, UnitFileState: state.unitFileState,
			}, nil
		},
		stoppedTarget: func(ctx context.Context) error {
			return verifyPDNSStoppedBeforeDatabaseRestoreWithOps(ctx, stopped)
		},
		configs: func(ctx context.Context, j dnsEngineSwitchJournal) ([]dnsenginerecovery.PDNSTargetConfigStateV4, error) {
			return dnsenginerecovery.ProbeInstalledPDNSFreshConfigsV3(ctx, policy, j, gid)
		},
		absent: func(paths ...string) bool {
			for _, path := range paths {
				if path == "" {
					return false
				}
				if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
					return false
				}
			}
			return true
		},
		candidate: dnsenginerecovery.CapturePDNSTargetCandidateV4,
		live:      dnsenginerecovery.VerifyPDNSTargetLiveV4,
		partial: func(j dnsEngineSwitchJournal) ([]string, error) {
			return dnsenginerecovery.FreshPrimaryPartialBuildV3(policy, j)
		},
	}
}

// restoreFreshPrimaryTargetUnitV3 returns a stopped target to its frozen
// rollback standby: the package guard's persistent mask, or loaded and
// disabled when the packages were already present. Every systemctl effect is
// preceded by the guard; after a guard refusal no readback can turn into
// success.
func restoreFreshPrimaryTargetUnitV3(ctx context.Context, systemctl string, snapshot dnsengineartifact.UnitSnapshot, guard func(context.Context) error) error {
	if ctx == nil || guard == nil || systemctl == "" ||
		(snapshot != freshPDNSGuardMaskV3 &&
			snapshot != (dnsengineartifact.UnitSnapshot{Name: "pdns.service", LoadState: "loaded", ActiveState: "inactive", UnitFileState: "disabled"})) {
		return errors.New("v3 PowerDNS target unit restore requires its frozen standby preimage")
	}
	unitCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	return dnsunitrestore.Restore(unitCtx, []dnsengineartifact.UnitSnapshot{snapshot}, nil, dnsunitrestore.Ops{
		Systemctl: systemctl, VerifyMaskParent: verifyBINDMaskParentMetadata,
		RunSystemd: guardedFreshPrimaryUnitRunnerV3(guard, runDNSSystemctl),
	})
}

func guardedFreshPrimaryUnitRunnerV3(
	guard func(context.Context) error,
	run func(context.Context, string, ...string) ([]byte, error),
) func(context.Context, string, ...string) ([]byte, error) {
	var refused error
	return func(ctx context.Context, path string, args ...string) ([]byte, error) {
		if guard == nil || run == nil || len(args) == 0 {
			return nil, errors.New("v3 PowerDNS target unit restore lacks a protected systemctl runner")
		}
		if refused != nil {
			return nil, refused
		}
		if args[0] != "show" {
			if err := guard(ctx); err != nil {
				refused = err
				return nil, err
			}
		}
		return run(ctx, path, args...)
	}
}
