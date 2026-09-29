package main

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/alicelik/celikpanel/internal/bindroot"
	"github.com/alicelik/celikpanel/internal/dnsenginerecovery"
)

// The Agent's in-process rollback of a V2 PowerDNS-to-BIND switch whose
// journal froze both BIND units absent or under the package guard's
// persistent mask (dnsenginerecovery.BINDSwitchNeverStartedTargetJournal)
// ends in the same state as recover-dns-bind-switch for that journal: BIND
// stopped, disabled and then re-masked with the guard's own mask on both
// names (or left absent when no unit file exists), and the exact staged
// generation removed. A journal that froze another BIND preimage keeps the
// unchanged rollbackBINDActivation.

func rollbackBINDSwitchToStandby(
	ctx context.Context,
	systemctl string,
	configs bindConfigMutation,
	stateBefore dnsFileSnapshot,
	sourceBefore map[string]dnsUnitState,
	proveSource func(context.Context) error,
) error {
	guard := dnsSystemdStateGuard(systemctl)
	return rollbackBINDActivationWithTarget(ctx, systemctl, configs, stateBefore, sourceBefore,
		func(commandCtx context.Context) error {
			return restoreBINDTargetToStandbyWithOps(commandCtx, hostBINDStandbyTargetOps(guard))
		},
		func(proofCtx context.Context) error {
			_, err := dnsenginerecovery.VerifyStoppedNeverStartedTarget(proofCtx, "named.service",
				func(observeCtx context.Context) (dnsenginerecovery.StoppedUnitObservation, error) {
					unit, err := guard.inspect(observeCtx, "named.service")
					if err != nil {
						return dnsenginerecovery.StoppedUnitObservation{}, err
					}
					processes, err := inspectDNSUnitProcesses(observeCtx, systemctl, "named.service")
					if err != nil {
						return dnsenginerecovery.StoppedUnitObservation{}, err
					}
					if err := dnsenginerecovery.ProbeEmptyUnitCgroup(observeCtx, "named.service", dnsenginerecovery.SystemdCgroupUnitRunner, dnsenginerecovery.NativeCgroupEvents); err != nil {
						return dnsenginerecovery.StoppedUnitObservation{}, err
					}
					return dnsenginerecovery.StoppedUnitObservation{
						Name: unit.name, LoadState: unit.loadState,
						ActiveState: unit.activeState, UnitFileState: unit.unitFileState,
						MainPID: processes.MainPID, ControlPID: processes.ControlPID,
						SubState: processes.SubState,
					}, nil
				},
				func(sourceCtx context.Context) error {
					return proveBINDStandbySourceOnlyDNSWithOps(sourceCtx, hostBINDStandbySourceOnlyOps(systemctl, guard))
				})
			return err
		},
		proveSource,
	)
}

type bindStandbyTargetOps struct {
	inspect func(context.Context) (bindInstallUnitState, bindInstallUnitState, error)
	// restoreAbsent stops, unmasks and disables both names from their
	// absent preimage (dnsunitrestore's safe compensation).
	restoreAbsent func(context.Context) error
	// seal applies the guard's persistent mask to both names and stops them.
	seal func(context.Context) error
	// verifyMasks proves each mask a root-owned single-link /dev/null link.
	verifyMasks func() error
}

func hostBINDStandbyTargetOps(guard *bindPackageInstallGuard) bindStandbyTargetOps {
	return bindStandbyTargetOps{
		inspect: func(ctx context.Context) (bindInstallUnitState, bindInstallUnitState, error) {
			named, err := guard.inspect(ctx, "named.service")
			if err != nil {
				return bindInstallUnitState{}, bindInstallUnitState{}, err
			}
			alias, err := guard.inspect(ctx, "bind9.service")
			return named, alias, err
		},
		restoreAbsent: func(ctx context.Context) error {
			return restoreDNSUnitSnapshotsWithGuard(ctx, guard, []dnsUnitSnapshot{
				{Name: "bind9.service", LoadState: "not-found", ActiveState: "inactive"},
				{Name: "named.service", LoadState: "not-found", ActiveState: "inactive"},
			})
		},
		seal: func(ctx context.Context) error {
			var errs []error
			for _, unit := range bindInstallUnitNames {
				if err := guard.ensurePersistentMasked(ctx, unit); err != nil {
					errs = append(errs, fmt.Errorf("mask %s: %w", unit, err))
				}
			}
			for _, unit := range bindInstallUnitNames {
				if err := guard.ensureStopped(ctx, unit); err != nil {
					errs = append(errs, fmt.Errorf("stop %s: %w", unit, err))
				}
			}
			return errors.Join(errs...)
		},
		verifyMasks: verifyBINDPersistentMaskFiles,
	}
}

// restoreBINDTargetToStandbyWithOps leaves a still-sealed or absent target
// untouched; otherwise it brings both names to stopped, unmasked and disabled
// (the absent preimage's compensation, which removes an enabled alias that
// would block masking) and seals them again with the guard's own mask.
func restoreBINDTargetToStandbyWithOps(ctx context.Context, ops bindStandbyTargetOps) error {
	if ctx == nil || ops.inspect == nil || ops.restoreAbsent == nil || ops.seal == nil || ops.verifyMasks == nil {
		return errors.New("BIND standby rollback requires unit, restore, seal and mask proofs")
	}
	named, alias, err := ops.inspect(ctx)
	if err != nil {
		return err
	}
	if exactPersistentMaskedInactiveBINDUnit(named) && exactPersistentMaskedInactiveBINDUnit(alias) {
		return ops.verifyMasks()
	}
	if exactAbsentBINDTarget(named, alias) {
		return nil
	}
	if err := ops.restoreAbsent(ctx); err != nil {
		return err
	}
	named, alias, err = ops.inspect(ctx)
	if err != nil {
		return err
	}
	if exactAbsentBINDTarget(named, alias) {
		return nil
	}
	if err := ops.seal(ctx); err != nil {
		return err
	}
	named, alias, err = ops.inspect(ctx)
	if err != nil {
		return err
	}
	if !exactPersistentMaskedInactiveBINDUnit(named) || !exactPersistentMaskedInactiveBINDUnit(alias) {
		return fmt.Errorf("BIND target did not reach the guard's persistent mask: named=%s/%s/%s bind9=%s/%s/%s",
			named.loadState, named.activeState, named.unitFileState, alias.loadState, alias.activeState, alias.unitFileState)
	}
	return ops.verifyMasks()
}

type bindStandbySourceOnlyOps struct {
	noNamedProcess func(context.Context) error
	sourceUnit     func(context.Context) (bindInstallUnitState, error)
	sourcePID      func(context.Context) (uint64, error)
	// authority proves every public port-53 listener belongs to pid, and
	// every local one to pid or the resolver stub.
	authority func(context.Context, uint64) error
	// noListener proves no public listener and only the resolver stub
	// locally.
	noListener func(context.Context) error
}

func hostBINDStandbySourceOnlyOps(systemctl string, guard *bindPackageInstallGuard) bindStandbySourceOnlyOps {
	return bindStandbySourceOnlyOps{
		noNamedProcess: dnsenginerecovery.ProbeNoNamedProcess,
		sourceUnit: func(ctx context.Context) (bindInstallUnitState, error) {
			return guard.inspect(ctx, "pdns.service")
		},
		sourcePID: func(ctx context.Context) (uint64, error) {
			processes, err := inspectDNSUnitProcesses(ctx, systemctl, "pdns.service")
			if err != nil {
				return 0, err
			}
			return processes.MainPID, nil
		},
		authority: func(ctx context.Context, pid uint64) error {
			if err := dnsenginerecovery.ProbeAuthorityListeners(ctx, "pdns_server", pid, "", readDNSPort53ListenerInventory); err != nil {
				return err
			}
			stubs, err := dnsenginerecovery.ProbeLocalDNSListeners(ctx, "pdns_server", pid, readDNSPort53ListenerInventory, dnsenginerecovery.NativeProcessUnifiedCgroup)
			if note := dnsenginerecovery.LocalDNSListenerRecordText(stubs); err == nil && note != "" {
				log.Print(note)
			}
			return err
		},
		noListener: proveNoPublicDNSPort53Listener,
	}
}

// proveBINDStandbySourceOnlyDNSWithOps is the Agent's source-only proof
// beside a stopped standby BIND target, the same rule as the owner command's:
// no named process, and either the source PowerDNS is active and owns every
// public (and every non-stub local) port-53 listener, or it is inactive and no
// DNS daemon holds one.
func proveBINDStandbySourceOnlyDNSWithOps(ctx context.Context, ops bindStandbySourceOnlyOps) error {
	if ctx == nil || ops.noNamedProcess == nil || ops.sourceUnit == nil || ops.sourcePID == nil ||
		ops.authority == nil || ops.noListener == nil {
		return errors.New("BIND standby source-only proof is incomplete")
	}
	if err := ops.noNamedProcess(ctx); err != nil {
		return err
	}
	source, err := ops.sourceUnit(ctx)
	if err != nil {
		return err
	}
	switch source.activeState {
	case "active":
		pid, err := ops.sourcePID(ctx)
		if err != nil {
			return err
		}
		if pid == 0 {
			return errors.New("active source PowerDNS has no systemd MainPID")
		}
		if err := ops.authority(ctx, pid); err != nil {
			return fmt.Errorf("port-53 listeners are not only the source PowerDNS: %w", err)
		}
	case "inactive":
		if err := ops.noListener(ctx); err != nil {
			return err
		}
	default:
		return errors.New("source PowerDNS unit is neither active nor inactive beside the standby BIND target")
	}
	again, err := ops.sourceUnit(ctx)
	if err != nil || again != source {
		return errors.Join(errors.New("source PowerDNS unit changed during the source-only DNS proof"), err)
	}
	return ops.noNamedProcess(ctx)
}

// removeStagedBINDGenerationAfterAgentRollback removes the exact staged
// generation after a completed standby rollback, as the owner command does.
// A tree that differs is left; every outcome is logged. A failure here never
// fails the rollback: the tree is unreferenced residue.
func removeStagedBINDGenerationAfterAgentRollback(ctx context.Context, journal dnsEngineSwitchJournal) {
	gid, err := resolveBINDGroupGID(ctx)
	if err != nil {
		log.Printf("DNS switch rollback (request %s): staged BIND generation %s was not removed; BIND group unknown: %v", journal.MutationRequestID, journal.TargetGeneration, err)
		return
	}
	logBINDStandbyResidue(journal, func() (dnsenginerecovery.BINDGenerationResidue, string, error) {
		return dnsenginerecovery.ClassifyStagedBINDGeneration(ctx, journal, bindroot.APT, gid)
	}, func() (bool, error) {
		return dnsenginerecovery.RemoveStagedBINDGeneration(ctx, journal, bindroot.APT, gid)
	}, func() ([]string, error) {
		return dnsenginerecovery.ListBINDRuntimeFiles(dnsenginerecovery.BINDWorkingDirectory(bindroot.APT))
	})
}

func logBINDStandbyResidue(
	journal dnsEngineSwitchJournal,
	classify func() (dnsenginerecovery.BINDGenerationResidue, string, error),
	remove func() (bool, error),
	runtimeFiles func() ([]string, error),
) {
	request := journal.MutationRequestID
	state, detail, err := classify()
	switch {
	case err != nil:
		log.Printf("DNS switch rollback (request %s): staged BIND generation %s was not removed; its state is unknown: %v", request, journal.TargetGeneration, err)
	case state == dnsenginerecovery.BINDGenerationRetained:
		log.Printf("DNS switch rollback (request %s): staged BIND generation %s was left because it is not exactly what this switch staged: %s", request, journal.TargetGeneration, detail)
	case state == dnsenginerecovery.BINDGenerationStaged:
		if removed, removeErr := remove(); removeErr != nil {
			log.Printf("DNS switch rollback (request %s): staged BIND generation %s was not removed: %v", request, journal.TargetGeneration, removeErr)
		} else if removed {
			log.Printf("DNS switch rollback (request %s): removed staged BIND generation %s", request, journal.TargetGeneration)
		}
	}
	if files, err := runtimeFiles(); err == nil && len(files) > 0 {
		log.Printf("DNS switch rollback (request %s): left BIND runtime files outside the managed root: %v; the bind9 packages, rndc key and install-ownership record stay as rollback standby", request, files)
	}
}
