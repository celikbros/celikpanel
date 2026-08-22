package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/alicelik/celikpanel/internal/binddns"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

func switchJournalManifest(
	journal dnsEngineSwitchJournal,
) (mutationpayload.DNSEngineSwitchManifestCommitment, error) {
	manifest, err := mutationpayload.CanonicalDNSEngineSwitchManifestWithPairIdentity(
		journal.Mode,
		journal.SourceEngine, journal.TargetEngine,
		journal.SourceEpoch, journal.TargetEpoch, journal.SourceRevision,
		journal.Topology, journal.PairRole, journal.LocalIP, journal.LocalNS,
		journal.PeerIP, journal.PeerNS, journal.Zones,
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
	legacyTarget := isLegacyDNSEngineState(state) &&
		(journal.Phase == dnsSwitchPhaseTargetVerified ||
			journal.Phase == dnsSwitchPhaseCommitted) &&
		(journal.PairRole == transport.DNSPairRolePrimary ||
			journal.PairRole == transport.DNSPairRoleSecondary)
	pairRoleMatches := state.PairRole == journal.PairRole ||
		legacyTarget
	pairAddressesMatch := state.PairLocalIP == journal.LocalIP &&
		state.PairPeerIP == journal.PeerIP
	if legacyTarget || (state.PairRole == "" && state.PrimaryCatalogSerial == 0) {
		pairAddressesMatch = state.PairLocalIP == "" && state.PairPeerIP == ""
	}
	catalogSerialMatches := state.PrimaryCatalogSerial == journal.PrimaryCatalogSerial ||
		(legacyTarget && state.PrimaryCatalogSerial == 0)
	if state.Schema != dnsEngineStateSchema || state.Engine != journal.TargetEngine ||
		state.Mode != journal.Mode ||
		state.EngineEpoch != journal.TargetEpoch || state.SourceRevision != journal.SourceRevision ||
		state.ManifestQualifier != journal.ManifestQualifier ||
		!pairRoleMatches || !pairAddressesMatch ||
		!catalogSerialMatches ||
		state.MutationRequestID != journal.MutationRequestID ||
		state.MutationOwnerID != journal.MutationOwnerID {
		return false
	}
	if journal.TargetEngine == transport.DNSEngineBIND {
		return state.Generation == journal.TargetGeneration
	}
	return state.Generation == ""
}

func exactBINDPairingForSwitchJournal(
	receipt binddns.Receipt,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
	journal dnsEngineSwitchJournal,
) bool {
	if manifest.Topology != transport.DNSTopologyPaired {
		return receipt.Pairing == nil && journal.PairRole == "" &&
			journal.LocalIP == "" && journal.LocalNS == "" &&
			journal.PeerIP == "" && journal.PeerNS == "" &&
			journal.PrimaryCatalogSerial == 0
	}
	pairing := receipt.Pairing
	if pairing == nil || pairing.Role != manifest.PairRole ||
		pairing.LocalIP != manifest.LocalIP || pairing.LocalNS != manifest.LocalNS ||
		pairing.PeerIP != manifest.PeerIP || pairing.PeerNS != manifest.PeerNS ||
		journal.PairRole != manifest.PairRole ||
		journal.LocalIP != manifest.LocalIP || journal.LocalNS != manifest.LocalNS ||
		journal.PeerIP != manifest.PeerIP || journal.PeerNS != manifest.PeerNS {
		return false
	}
	if pairing.Role == binddns.PairRolePrimary {
		return journal.PrimaryCatalogSerial > 0 &&
			pairing.CatalogSerial == journal.PrimaryCatalogSerial
	}
	return pairing.Role == binddns.PairRoleSecondary &&
		journal.PrimaryCatalogSerial == 0 && pairing.CatalogSerial == 1
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
		publisher, _, err := newHostBINDPublisher(ctx, layout)
		if err != nil {
			return err
		}
		tree, err := publisher.LoadCurrent()
		if err != nil {
			return err
		}
		receipt := tree.CurrentReceipt()
		if receipt.Generation != journal.TargetGeneration ||
			receipt.EngineEpoch != journal.TargetEpoch ||
			!exactBINDPairingForSwitchJournal(receipt, manifest, journal) {
			return errors.New("BIND recovery target pairing differs from the journal")
		}
		legacyPairedTarget := isLegacyDNSEngineState(state) &&
			manifest.Topology == transport.DNSTopologyPaired
		transferPeer := ""
		if manifest.Topology == transport.DNSTopologyPaired &&
			manifest.PairRole == transport.DNSPairRoleSecondary {
			transferPeer = manifest.PeerIP
		}
		if err := verifyManagedBINDConfigExact(
			layout, transferPeer, legacyPairedTarget,
		); err != nil {
			return err
		}
		expected := binddns.Generation{ID: journal.TargetGeneration}
		if !legacyPairedTarget {
			plan, planErr := bindSwitchTreePlanWithPrimaryCatalogSerial(
				manifest, binding, journal.PrimaryCatalogSerial,
			)
			if planErr != nil {
				return planErr
			}
			expected, err = binddns.RenderTree(layout.GenerationRoot, plan)
			if err != nil || expected.ID != journal.TargetGeneration {
				return errors.New("BIND recovery generation differs from the journal")
			}
		}
		if legacyPairedTarget && receipt.Pairing != nil &&
			receipt.Pairing.Role == binddns.PairRoleSecondary {
			plan, planErr := bindSwitchTreePlanWithPrimaryCatalogSerial(
				manifest, binding, journal.PrimaryCatalogSerial,
			)
			if planErr != nil {
				return planErr
			}
			expected, err = binddns.RenderTree(layout.GenerationRoot, plan)
			if err != nil || expected.ID != receipt.Generation ||
				expected.ReceiptValue.ConfigSHA256 != receipt.ConfigSHA256 {
				return errors.New("legacy BIND secondary config differs from the journal")
			}
		}
		if legacyPairedTarget && receipt.Pairing != nil &&
			receipt.Pairing.Role == binddns.PairRolePrimary {
			style, styleErr := binddns.ClassifyPrimaryTransferACL(
				layout.GenerationRoot, tree,
			)
			if styleErr != nil {
				return styleErr
			}
			switch style {
			case binddns.PrimaryTransferACLLegacyPeerOnly:
				// Exact released target: preserve the all-empty state receipt.
			case binddns.PrimaryTransferACLDirectionalSelfPeer:
				return errors.New("directional BIND target has an all-empty legacy state receipt")
			default:
				return errors.New("BIND recovery target has an unknown transfer policy")
			}
		}
		if _, err = verifyCompletedBINDEngineSwitch(
			ctx, profile, layout, expected, state, manifest.Zones,
		); err != nil {
			return err
		}
		if legacyPairedTarget {
			return verifyLegacyCompletedPrimaryCatalogTarget(
				ctx, profile, manifest, state, journal.PrimaryCatalogSerial,
			)
		}
		return verifyCompletedPrimaryCatalogTarget(ctx, profile, manifest, state)
	case transport.DNSEnginePowerDNS:
		if journal.Mode == transport.DNSEngineSwitchModeAdopt {
			return verifyPDNSAdoptionEvidence(
				ctx, systemctl, manifest, journal, pdnsAdoptionEvidenceTarget,
			)
		}
		if err := verifyPDNSSwitchDatabaseWithPrimaryCatalogSerial(
			ctx, pdnsDBPath(), manifest, binding, journal.PrimaryCatalogSerial,
		); err != nil {
			return err
		}
		if err := verifyOnlyPDNSActive(ctx, systemctl); err != nil {
			return err
		}
		if err := verifyDNSZoneManifestAuthority(ctx, manifest.Zones); err != nil {
			return err
		}
		if manifest.Topology == transport.DNSTopologyPaired {
			if err := verifyManagedPDNSPairIdentity(manifest, state); err != nil {
				return err
			}
		}
		legacyPairedTarget := isLegacyDNSEngineState(state) &&
			manifest.Topology == transport.DNSTopologyPaired
		if legacyPairedTarget &&
			manifest.PairRole == transport.DNSPairRolePrimary {
			return verifyLegacyCompletedPrimaryCatalogTarget(
				ctx, profile, manifest, state, journal.PrimaryCatalogSerial,
			)
		}
		if err := verifyPDNSPairingAuthority(ctx, manifest); err != nil {
			return err
		}
		if legacyPairedTarget {
			return nil
		}
		return verifyCompletedPrimaryCatalogTarget(ctx, profile, manifest, state)
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
		publisher, _, err := newHostBINDPublisher(ctx, layout)
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
	return verifyRestoredDNSSwitchSource(
		ctx, profile, systemctl, manifest, journal,
	)
}

func verifyNoManagedDNSAuthority(
	ctx context.Context,
	systemctl string,
	journal dnsEngineSwitchJournal,
) error {
	if ctx == nil {
		return errors.New("verify restored DNS authority requires a context")
	}
	proofCtx, cancel := context.WithTimeout(ctx, dnsRuntimeInspectionTimeout)
	defer cancel()
	// Legacy PDNS adoption has an empty source identity but an active
	// target-before snapshot which rollback must restore exactly. Prove that
	// complete active topology instead of applying the source-none proof.
	if journal.Mode == transport.DNSEngineSwitchModeAdopt &&
		targetSnapshotWasActive(journal, "pdns.service") {
		return verifyOnlyPDNSActive(proofCtx, systemctl)
	}
	ss, err := firstTrustedExecutable([]string{"/usr/sbin/ss", "/usr/bin/ss"}, "ss")
	if err != nil {
		return err
	}
	guard := dnsSystemdStateGuard(systemctl)
	return verifyNoManagedDNSAuthorityWithOps(noManagedDNSAuthorityProofOps{
		inspectUnit: func(unit string) (bindInstallUnitState, error) {
			return guard.inspect(proofCtx, unit)
		},
		inspectListeners: func() (string, error) {
			output, commandErr := serviceMutationCommand(
				proofCtx, ss, "-H", "-lntup", "sport = :53",
			).CombinedOutputLimited(64 << 10)
			if commandErr != nil {
				return "", commandErr
			}
			return string(output), nil
		},
	})
}

type noManagedDNSAuthoritySnapshot struct {
	named     bindInstallUnitState
	bindAlias bindInstallUnitState
	pdns      bindInstallUnitState
	listeners []string
}

type noManagedDNSAuthorityProofOps struct {
	inspectUnit      func(string) (bindInstallUnitState, error)
	inspectListeners func() (string, error)
}

func captureNoManagedDNSAuthority(
	ops noManagedDNSAuthorityProofOps,
) (noManagedDNSAuthoritySnapshot, error) {
	var snapshot noManagedDNSAuthoritySnapshot
	if ops.inspectUnit == nil || ops.inspectListeners == nil {
		return snapshot, errors.New("invalid no-authority proof operations")
	}
	var err error
	snapshot.named, err = ops.inspectUnit("named.service")
	if err != nil {
		return snapshot, err
	}
	snapshot.bindAlias, err = ops.inspectUnit("bind9.service")
	if err != nil {
		return snapshot, err
	}
	snapshot.pdns, err = ops.inspectUnit("pdns.service")
	if err != nil {
		return snapshot, err
	}
	for _, state := range []bindInstallUnitState{
		snapshot.named, snapshot.bindAlias, snapshot.pdns,
	} {
		if state.activeState != "inactive" {
			return snapshot, errors.New(
				"DNS rollback did not prove every managed authority inactive",
			)
		}
	}
	output, err := ops.inspectListeners()
	if err != nil {
		return snapshot, err
	}
	snapshot.listeners, err = canonicalNoPublicDNSAuthorityListeners(output)
	if err != nil {
		return snapshot, err
	}
	return snapshot, nil
}

func verifyNoManagedDNSAuthorityWithOps(
	ops noManagedDNSAuthorityProofOps,
) error {
	first, err := captureNoManagedDNSAuthority(ops)
	if err != nil {
		return err
	}
	second, err := captureNoManagedDNSAuthority(ops)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(second, first) {
		return errors.New(
			"DNS rollback authority snapshot changed during no-authority proof",
		)
	}
	return nil
}

func canonicalNoPublicDNSAuthorityListeners(
	output string,
) ([]string, error) {
	identities := map[string]struct{}{}
	for _, line := range strings.Split(output, "\n") {
		if line == "" {
			continue
		}
		row, err := parseCanonicalDNSPort53ListenerRow(line)
		if err != nil {
			return nil, err
		}
		if !row.address.IsLoopback() && !row.address.IsLinkLocalUnicast() {
			return nil, errors.New(
				"DNS rollback left a public port-53 authority active",
			)
		}
		identity := fmt.Sprintf(
			"%s|%s|%s|%d",
			row.protocol, row.address.String(), row.process, row.pid,
		)
		if _, duplicate := identities[identity]; duplicate {
			return nil, errors.New(
				"DNS rollback listener snapshot contains a duplicate",
			)
		}
		identities[identity] = struct{}{}
	}
	result := make([]string, 0, len(identities))
	for identity := range identities {
		result = append(result, identity)
	}
	sort.Strings(result)
	return result, nil
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
