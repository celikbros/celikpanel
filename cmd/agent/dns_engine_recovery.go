package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/alicelik/celikpanel/internal/binddns"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

func switchJournalManifest(
	journal dnsEngineSwitchJournal,
) (mutationpayload.DNSEngineSwitchManifestCommitment, error) {
	manifest, err := mutationpayload.CanonicalDNSEngineSwitchManifest(
		journal.Mode,
		journal.SourceEngine, journal.TargetEngine,
		journal.SourceEpoch, journal.TargetEpoch, journal.SourceRevision,
		journal.Topology, journal.Zones,
	)
	if err != nil || manifest.Qualifier != journal.ManifestQualifier ||
		manifest.SnapshotBytes != journal.SnapshotBytes {
		return mutationpayload.DNSEngineSwitchManifestCommitment{},
			errors.New("DNS engine switch journal does not reconstruct its manifest")
	}
	return manifest, nil
}

func switchJournalBinding(journal dnsEngineSwitchJournal) transport.ServiceMutationBinding {
	return transport.ServiceMutationBinding{
		MutationRequestID: journal.MutationRequestID,
		MutationOwnerID:   journal.MutationOwnerID,
	}
}

func exactSwitchJournalIdentity(
	journal dnsEngineSwitchJournal,
	target transport.DNSEngine,
	qualifier string,
	binding transport.ServiceMutationBinding,
) bool {
	return journal.TargetEngine == target && journal.ManifestQualifier == qualifier &&
		journal.MutationRequestID == binding.MutationRequestID &&
		journal.MutationOwnerID == binding.MutationOwnerID
}

func exactDNSEngineStateForJournal(
	state dnsEngineStateReceipt,
	journal dnsEngineSwitchJournal,
) bool {
	if state.Schema != dnsEngineStateSchema || state.Engine != journal.TargetEngine ||
		state.Mode != journal.Mode ||
		state.EngineEpoch != journal.TargetEpoch || state.SourceRevision != journal.SourceRevision ||
		state.ManifestQualifier != journal.ManifestQualifier ||
		state.MutationRequestID != journal.MutationRequestID ||
		state.MutationOwnerID != journal.MutationOwnerID {
		return false
	}
	if journal.TargetEngine == transport.DNSEngineBIND {
		return state.Generation == journal.TargetGeneration
	}
	return state.Generation == ""
}

func verifyDNSSwitchJournalTarget(
	ctx context.Context,
	journal dnsEngineSwitchJournal,
) error {
	if err := verifyDNSSwitchSourceOwnership(journal); err != nil {
		return err
	}
	manifest, err := switchJournalManifest(journal)
	if err != nil {
		return err
	}
	binding := switchJournalBinding(journal)
	state, exists, err := readDNSEngineState()
	if err != nil || !exists || !exactDNSEngineStateForJournal(state, journal) {
		if err == nil {
			err = errors.New("DNS engine target state receipt is absent or different")
		}
		return err
	}
	profile, err := verifiedHostProfileForAnyFamily()
	if err != nil {
		return err
	}
	systemctl, err := executableForProfile(profile, string(profile.PackageManager), "systemctl")
	if err != nil {
		return err
	}
	switch journal.TargetEngine {
	case transport.DNSEngineBIND:
		layout, err := bindLayout(profile)
		if err != nil {
			return err
		}
		plan, err := bindSwitchTreePlan(manifest, binding)
		if err != nil {
			return err
		}
		expected, err := binddns.RenderTree(layout.GenerationRoot, plan)
		if err != nil || expected.ID != journal.TargetGeneration {
			return errors.New("BIND recovery generation differs from the journal")
		}
		_, err = verifyCompletedBINDEngineSwitch(ctx, profile, layout, expected, state, manifest.Zones)
		return err
	case transport.DNSEnginePowerDNS:
		if journal.Mode == transport.DNSEngineSwitchModeAdopt {
			return verifyPDNSAdoptionEvidence(ctx, systemctl, manifest, journal)
		}
		if err := verifyPDNSSwitchDatabase(ctx, pdnsDBPath(), manifest, binding); err != nil {
			return err
		}
		if err := verifyOnlyPDNSActive(ctx, systemctl); err != nil {
			return err
		}
		return verifyDNSZoneManifestAuthority(ctx, manifest.Zones)
	default:
		return errors.New("DNS engine switch journal target is unsupported")
	}
}

func dnsUnitSnapshotsMap(snapshots []dnsUnitSnapshot) map[string]dnsUnitState {
	states := make(map[string]dnsUnitState, len(snapshots))
	for _, snapshot := range snapshots {
		states[snapshot.Name] = dnsUnitState{
			Name: snapshot.Name, LoadState: snapshot.LoadState,
			ActiveState: snapshot.ActiveState, UnitFileState: snapshot.UnitFileState,
		}
	}
	return states
}

func bindConfigMutationFromJournal(journal dnsEngineSwitchJournal) bindConfigMutation {
	mutation := bindConfigMutation{
		paths:     make([]string, 0, len(journal.ConfigBefore)),
		original:  make(map[string][]byte, len(journal.ConfigBefore)),
		desired:   make(map[string][]byte),
		snapshots: make(map[string]dnsFileSnapshot, len(journal.ConfigBefore)),
	}
	for _, snapshot := range journal.ConfigBefore {
		mutation.paths = append(mutation.paths, snapshot.Path)
		mutation.original[snapshot.Path] = append([]byte(nil), snapshot.Data...)
		snapshot.Data = append([]byte(nil), snapshot.Data...)
		mutation.snapshots[snapshot.Path] = snapshot
	}
	return mutation
}

func rollbackDNSSwitchJournal(
	ctx context.Context,
	journal dnsEngineSwitchJournal,
) error {
	manifest, err := switchJournalManifest(journal)
	if err != nil {
		return err
	}
	profile, err := verifiedHostProfileForAnyFamily()
	if err != nil {
		return err
	}
	systemctl, err := executableForProfile(profile, string(profile.PackageManager), "systemctl")
	if err != nil {
		return err
	}
	switch journal.TargetEngine {
	case transport.DNSEngineBIND:
		layout, err := bindLayout(profile)
		if err != nil {
			return err
		}
		publisher, _, err := newHostBINDPublisher(layout)
		if err != nil {
			return err
		}
		if err := publisher.RestorePointer(
			journal.TargetGeneration, journal.PreviousGeneration, journal.HadPrevious,
		); err != nil {
			return err
		}
		if err := rollbackBINDActivation(
			ctx, systemctl, bindConfigMutationFromJournal(journal), journal.StateBefore,
			dnsUnitSnapshotsMap(journal.TargetUnitsBefore),
			dnsUnitSnapshotsMap(journal.SourceUnitsBefore),
		); err != nil {
			return err
		}
	case transport.DNSEnginePowerDNS:
		if journal.Mode == transport.DNSEngineSwitchModeAdopt {
			return rollbackPDNSAdoption(ctx, systemctl, manifest, journal)
		}
		if err := rollbackPDNSSwitch(ctx, systemctl, journal, pdnsConfigMutation{before: journal.ConfigBefore}); err != nil {
			return err
		}
	default:
		return errors.New("DNS engine rollback target is unsupported")
	}
	if journal.SourceEngine == transport.DNSEnginePowerDNS {
		return verifyOnlyPDNSActive(ctx, systemctl)
	}
	if journal.SourceEngine == transport.DNSEngineBIND {
		return verifyOnlyBINDActive(ctx, systemctl)
	}
	return verifyNoManagedDNSAuthority(ctx, systemctl, journal)
}

func verifyNoManagedDNSAuthority(
	ctx context.Context,
	systemctl string,
	journal dnsEngineSwitchJournal,
) error {
	for _, unit := range []string{"named.service", "bind9.service", "pdns.service"} {
		state, err := dnsSystemdStateGuard(systemctl).inspect(ctx, unit)
		if err != nil {
			return err
		}
		if state.active() {
			// Legacy PDNS adoption has an empty source identity but an active
			// target-before snapshot which rollback must restore exactly.
			if unit == "pdns.service" && targetSnapshotWasActive(journal, unit) {
				return verifyOnlyPDNSActive(ctx, systemctl)
			}
			return errors.New("DNS rollback left an unexpected managed authority active")
		}
	}
	ss, err := firstTrustedExecutable([]string{"/usr/sbin/ss", "/usr/bin/ss"}, "ss")
	if err != nil {
		return err
	}
	output, err := serviceMutationCommand(
		context.WithoutCancel(ctx), ss, "-H", "-lntup", "sport = :53",
	).CombinedOutputLimited(64 << 10)
	if err != nil {
		return err
	}
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		host, port, splitErr := net.SplitHostPort(fields[4])
		if splitErr != nil || port != "53" {
			continue
		}
		address := net.ParseIP(strings.Trim(host, "[]"))
		if address == nil || (!address.IsLoopback() && !address.IsLinkLocalUnicast()) {
			return errors.New("DNS rollback left a public port-53 authority active")
		}
	}
	return nil
}

func targetSnapshotWasActive(journal dnsEngineSwitchJournal, unit string) bool {
	for _, snapshot := range journal.TargetUnitsBefore {
		if snapshot.Name == unit {
			return snapshot.ActiveState == "active"
		}
	}
	return false
}

func (hostDNSEngineBackend) RecoverSwitch(
	ctx context.Context,
	target transport.DNSEngine,
	qualifier string,
	binding transport.ServiceMutationBinding,
) (dnsEngineSwitchRecoveryOutcome, error) {
	journal, exists, err := readDNSEngineSwitchJournal()
	if err != nil || !exists {
		return dnsEngineSwitchRecoveryAbsent, err
	}
	if !exactSwitchJournalIdentity(journal, target, qualifier, binding) {
		return dnsEngineSwitchRecoveryAbsent, errors.New("DNS engine switch journal belongs to another mutation")
	}
	if err := verifyDNSSwitchJournalTarget(ctx, journal); err == nil {
		journal.Phase = dnsSwitchPhaseCommitted
		if err := writeDNSEngineSwitchJournal(journal); err != nil {
			return dnsEngineSwitchRecoveryAbsent, err
		}
		return dnsEngineSwitchRecoveryCommitted, nil
	} else if journal.Phase == dnsSwitchPhaseTargetVerified || journal.Phase == dnsSwitchPhaseCommitted {
		return dnsEngineSwitchRecoveryAbsent,
			fmt.Errorf("verified DNS engine target no longer matches its journal: %w", err)
	}
	journal.Phase = dnsSwitchPhaseRollingBack
	if err := writeDNSEngineSwitchJournal(journal); err != nil {
		return dnsEngineSwitchRecoveryAbsent, err
	}
	if err := rollbackDNSSwitchJournal(ctx, journal); err != nil {
		return dnsEngineSwitchRecoveryAbsent, err
	}
	journal.Phase = dnsSwitchPhaseRolledBack
	if err := writeDNSEngineSwitchJournal(journal); err != nil {
		return dnsEngineSwitchRecoveryAbsent, err
	}
	if err := removeDNSEngineSwitchJournal(); err != nil {
		return dnsEngineSwitchRecoveryAbsent, err
	}
	return dnsEngineSwitchRecoveryRolledBack, nil
}

func reconcileExistingDNSEngineSwitchJournal(ctx context.Context) error {
	journal, exists, err := readDNSEngineSwitchJournal()
	if err != nil || !exists {
		return err
	}
	binding := switchJournalBinding(journal)
	outcome, err := (hostDNSEngineBackend{}).RecoverSwitch(
		ctx, journal.TargetEngine, journal.ManifestQualifier, binding,
	)
	if err != nil {
		return err
	}
	if outcome == dnsEngineSwitchRecoveryCommitted {
		return (hostDNSEngineBackend{}).FinalizeSwitch(
			ctx, journal.TargetEngine, journal.ManifestQualifier, binding,
		)
	}
	if outcome != dnsEngineSwitchRecoveryRolledBack {
		return errors.New("DNS engine switch journal recovery made no progress")
	}
	return nil
}

func (hostDNSEngineBackend) FinalizeSwitch(
	ctx context.Context,
	target transport.DNSEngine,
	qualifier string,
	binding transport.ServiceMutationBinding,
) error {
	journal, exists, err := readDNSEngineSwitchJournal()
	if err != nil || !exists {
		return err
	}
	if !exactSwitchJournalIdentity(journal, target, qualifier, binding) ||
		journal.Phase != dnsSwitchPhaseCommitted {
		return errors.New("DNS engine switch journal is not the exact committed transaction")
	}
	if err := verifyDNSSwitchJournalTarget(ctx, journal); err != nil {
		return err
	}
	if journal.TargetEngine == transport.DNSEnginePowerDNS &&
		journal.Mode == transport.DNSEngineSwitchModeSwitch {
		for _, path := range []string{journal.PDNSCandidatePath, journal.PDNSBackupPath} {
			if err := removePDNSSwitchArtifact(path); err != nil {
				return err
			}
		}
		if err := syncAtomicParentDirectory(filepath.Dir(pdnsDBPath())); err != nil {
			return err
		}
	}
	if err := retireDNSEngineInstallOwnership(journal); err != nil {
		return err
	}
	return removeDNSEngineSwitchJournal()
}

func removePDNSSwitchArtifact(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("PowerDNS switch artifact is not a safe regular file")
	}
	return os.Remove(path)
}
