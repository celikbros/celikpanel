package main

import (
	"bytes"
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
			ctx, layout, transferPeer, legacyPairedTarget,
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

func bindConfigMutationFromJournal(
	layout bindHostLayout,
	transferPeer string,
	journal dnsEngineSwitchJournal,
) (bindConfigMutation, error) {
	snapshots := make(map[string]dnsFileSnapshot, len(journal.ConfigBefore))
	for _, snapshot := range journal.ConfigBefore {
		snapshot.Data = append([]byte(nil), snapshot.Data...)
		snapshots[filepath.Clean(snapshot.Path)] = snapshot
	}
	mutation, err := prepareBINDConfigMutationWithSnapshotReader(
		layout, transferPeer,
		func(path string, mode os.FileMode, allowAbsent bool) (dnsFileSnapshot, error) {
			snapshot, exists := snapshots[filepath.Clean(path)]
			if allowAbsent || !exists || !snapshot.Exists || snapshot.Mode != uint32(mode.Perm()) {
				return dnsFileSnapshot{}, errors.New("BIND recovery journal config set is incomplete")
			}
			return snapshot, nil
		},
	)
	if err != nil {
		return bindConfigMutation{}, err
	}
	mutation.ownerAware = true
	return mutation, nil
}

func restoreBINDPointerAfterConfigProof(
	proveCurrent func() error,
	restorePointer func() error,
) error {
	if proveCurrent == nil || restorePointer == nil {
		return errors.New("BIND pointer recovery requires config proof and restore operations")
	}
	if err := proveCurrent(); err != nil {
		return err
	}
	return restorePointer()
}

func runBINDMutationWithMaskParentProof(
	verifyMaskParent func() error,
	mutate func() error,
) error {
	if verifyMaskParent == nil || mutate == nil {
		return errors.New("invalid BIND mutation proof operations")
	}
	if err := verifyMaskParent(); err != nil {
		return fmt.Errorf(
			"verify BIND mask parent before mutation: %w", err,
		)
	}
	return mutate()
}

func runDNSMutationWithSystemdParentProof(
	verifySystemdParent func() error,
	mutate func() error,
) error {
	if verifySystemdParent == nil || mutate == nil {
		return errors.New("invalid DNS systemd parent proof operations")
	}
	if err := verifySystemdParent(); err != nil {
		return fmt.Errorf(
			"verify DNS systemd parent before mutation: %w", err,
		)
	}
	return mutate()
}

func runDNSSwitchRollbackWithMaskParentProof(
	journal dnsEngineSwitchJournal,
	verifyMaskParent func() error,
	rollback func() error,
) error {
	if rollback == nil {
		return errors.New("invalid DNS switch rollback operation")
	}
	if journal.TargetEngine == transport.DNSEngineBIND ||
		journal.SourceEngine == transport.DNSEngineBIND {
		return runBINDMutationWithMaskParentProof(
			verifyMaskParent, rollback,
		)
	}
	if journal.Mode == transport.DNSEngineSwitchModeSwitch {
		return runDNSMutationWithSystemdParentProof(
			verifyMaskParent, rollback,
		)
	}
	return rollback()
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
	rollback := func() error {
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
			transferPeer := ""
			if manifest.Topology == transport.DNSTopologyPaired &&
				manifest.PairRole == transport.DNSPairRoleSecondary {
				transferPeer = manifest.PeerIP
			}
			configs, err := bindConfigMutationFromJournal(
				layout, transferPeer, journal,
			)
			if err != nil {
				return err
			}
			if err := restoreBINDPointerAfterConfigProof(
				func() error {
					_, _, proofErr := configs.captureOwnerAwareCurrent(ctx, false)
					return proofErr
				},
				func() error {
					return runBINDMutationWithMaskParentProof(
						verifyBINDMaskParentMetadata,
						func() error {
							return publisher.RestorePointer(
								journal.TargetGeneration, journal.PreviousGeneration,
								journal.HadPrevious,
							)
						},
					)
				},
			); err != nil {
				return err
			}
			return rollbackBINDActivation(
				ctx, systemctl, configs, journal.StateBefore,
				dnsUnitSnapshotsMap(journal.TargetUnitsBefore),
				dnsUnitSnapshotsMap(journal.SourceUnitsBefore),
			)
		case transport.DNSEnginePowerDNS:
			if journal.Mode == transport.DNSEngineSwitchModeAdopt {
				return rollbackPDNSAdoption(ctx, systemctl, manifest, journal)
			}
			configs, err := pdnsConfigMutationFromJournal(ctx, manifest, journal)
			if err != nil {
				return err
			}
			return rollbackPDNSSwitch(ctx, systemctl, journal, configs)
		default:
			return errors.New("DNS engine rollback target is unsupported")
		}
	}
	if err := runDNSSwitchRollbackWithMaskParentProof(
		journal, verifyBINDMaskParentMetadata, rollback,
	); err != nil {
		return err
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
	return verifyRestoredEmptySourceAuthorityWithOps(
		proofCtx, journal,
		restoredEmptySourceAuthorityProofOps{
			pdnsConfig: hostPDNSConfigAccessOps(),
			verifyOnlyPDNS: func() error {
				return verifyOnlyPDNSActive(proofCtx, systemctl)
			},
			verifyNoAuthority: func() error {
				ss, err := firstTrustedExecutable(
					[]string{"/usr/sbin/ss", "/usr/bin/ss"}, "ss",
				)
				if err != nil {
					return err
				}
				guard := dnsSystemdStateGuard(systemctl)
				return verifyNoManagedDNSAuthorityWithOps(
					noManagedDNSAuthorityProofOps{
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
					},
				)
			},
		},
	)
}

type restoredEmptySourceAuthorityProofOps struct {
	pdnsConfig        pdnsConfigAccessOps
	verifyOnlyPDNS    func() error
	verifyNoAuthority func() error
}

func verifyRestoredEmptySourceAuthorityWithOps(
	ctx context.Context,
	journal dnsEngineSwitchJournal,
	ops restoredEmptySourceAuthorityProofOps,
) error {
	if ctx == nil || ops.verifyOnlyPDNS == nil || ops.verifyNoAuthority == nil {
		return errors.New("restored empty-source authority proof is incomplete")
	}
	// Legacy adoption already binds an active pdns.service preimage in its
	// separately validated adoption journal. Preserve that recovery contract.
	if journal.Mode == transport.DNSEngineSwitchModeAdopt &&
		targetSnapshotWasActive(journal, "pdns.service") {
		return ops.verifyOnlyPDNS()
	}
	reconfigure, err := provePDNSPairSecondaryReconfigureRollbackWithOps(
		ctx, journal, ops.pdnsConfig,
	)
	if err != nil {
		return err
	}
	if reconfigure {
		return ops.verifyOnlyPDNS()
	}
	return ops.verifyNoAuthority()
}

func provePDNSPairSecondaryReconfigureRollbackWithOps(
	ctx context.Context,
	journal dnsEngineSwitchJournal,
	ops pdnsConfigAccessOps,
) (bool, error) {
	if ctx == nil {
		return false, errors.New("PowerDNS reconfiguration rollback proof requires a context")
	}
	manifest, err := switchJournalManifest(journal)
	if err != nil {
		return false, err
	}
	if !isPDNSPairSecondaryReconfigureManifest(manifest) ||
		!targetSnapshotWasActive(journal, "pdns.service") {
		return false, nil
	}
	// An active target preimage distinguishes the narrow legacy
	// reconfiguration from a fresh initial install sharing this manifest.
	// From here onward incomplete evidence is a hard failure, not a fallback.
	if err := validateDNSEngineSwitchJournal(journal); err != nil {
		return false, err
	}
	if journal.Phase != dnsSwitchPhaseRollingBack {
		return false, errors.New("PowerDNS reconfiguration rollback journal is not durably rolling back")
	}
	if !reflect.DeepEqual(
		journal.StateBefore,
		dnsFileSnapshot{Path: filepath.Clean(dnsEngineStatePath())},
	) {
		return false, errors.New("PowerDNS reconfiguration rollback has unexpected prior engine state")
	}
	wantTarget := []dnsUnitSnapshot{{
		Name: "pdns.service", LoadState: "loaded",
		ActiveState: "active", UnitFileState: "enabled",
	}}
	if !reflect.DeepEqual(journal.TargetUnitsBefore, wantTarget) ||
		len(journal.SourceUnitsBefore) != 0 {
		return false, errors.New("PowerDNS reconfiguration rollback lacks the exact active unit preimage")
	}
	if !validDNSGeneration(journal.PDNSBackupSHA256) ||
		journal.PDNSBackupSize <= 0 ||
		journal.PDNSCandidatePath != filepath.Clean(
			pdnsSwitchCandidatePath(journal.MutationRequestID),
		) || journal.PDNSBackupPath != filepath.Clean(
		pdnsSwitchBackupPath(journal.MutationRequestID),
	) {
		return false, errors.New("PowerDNS reconfiguration rollback lacks the exact database preimage")
	}
	if ops.resolve == nil || ops.capture == nil {
		return false, errors.New("PowerDNS reconfiguration rollback config proof is incomplete")
	}
	expectedManagedConfig, err := managedPowerDNSStandaloneConfig(ctx)
	if err != nil {
		return false, fmt.Errorf(
			"discover managed PowerDNS rollback listen addresses: %w", err,
		)
	}
	policy, err := ops.resolve(ctx)
	if err != nil {
		return false, err
	}
	if err := policy.validateSnapshots(journal.ConfigBefore); err != nil {
		return false, err
	}
	configs := pdnsConfigSnapshotMap(journal.ConfigBefore)
	main := configs[filepath.Clean(dnsMainConf)]
	managed := configs[filepath.Clean(dnsManagedConf)]
	cluster := configs[filepath.Clean(dnsClusterConf)]
	hasInclude, err := validateManagedPowerDNSMainConfig(
		string(main.Data), filepath.Clean(filepath.Dir(dnsManagedConf)),
	)
	if err != nil || !hasInclude {
		if err == nil {
			err = errors.New("PowerDNS main config did not load the managed directory")
		}
		return false, err
	}
	if !managed.Exists ||
		!bytes.Equal(managed.Data, expectedManagedConfig) ||
		cluster.Exists {
		return false, errors.New("PowerDNS reconfiguration rollback config preimage is not exact standalone state")
	}
	if _, err := capturePDNSConfigSnapshotsExactWithOps(
		policy, journal.ConfigBefore, ops,
	); err != nil {
		return false, err
	}
	return true, nil
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

type dnsSwitchRecoveryRollbackOps struct {
	write    func(dnsEngineSwitchJournal) error
	rollback func(dnsEngineSwitchJournal) error
	remove   func() error
}

func runDNSSwitchRecoveryRollbackWithJournal(
	journal *dnsEngineSwitchJournal,
	ops dnsSwitchRecoveryRollbackOps,
) error {
	if journal == nil || ops.write == nil || ops.rollback == nil ||
		ops.remove == nil {
		return errors.New("invalid DNS switch recovery rollback operations")
	}
	journal.Phase = dnsSwitchPhaseRollingBack
	if err := ops.write(*journal); err != nil {
		return err
	}
	if err := ops.rollback(*journal); err != nil {
		return err
	}
	journal.Phase = dnsSwitchPhaseRolledBack
	if err := ops.write(*journal); err != nil {
		return err
	}
	return ops.remove()
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
	if err := runDNSSwitchRecoveryRollbackWithJournal(
		&journal,
		dnsSwitchRecoveryRollbackOps{
			write: writeDNSEngineSwitchJournal,
			rollback: func(current dnsEngineSwitchJournal) error {
				return rollbackDNSSwitchJournal(ctx, current)
			},
			remove: removeDNSEngineSwitchJournal,
		},
	); err != nil {
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

var (
	finalizeDNSEngineVerifiedHostProfile = verifiedHostProfileForAnyFamily
	finalizeDNSEngineVerifyTarget        = verifyDNSSwitchJournalTarget
)

func finalizeCommittedDNSEngineSwitchArtifacts(journal dnsEngineSwitchJournal) error {
	if journal.TargetEngine != transport.DNSEnginePowerDNS ||
		journal.Mode != transport.DNSEngineSwitchModeSwitch {
		return nil
	}
	for _, path := range []string{journal.PDNSCandidatePath, journal.PDNSBackupPath} {
		if err := removePDNSSwitchArtifact(path); err != nil {
			return err
		}
	}
	return syncAtomicParentDirectory(filepath.Dir(pdnsDBPath()))
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
	profile, err := finalizeDNSEngineVerifiedHostProfile()
	if err != nil {
		return err
	}
	if _, _, _, err := exactCommittedDNSEngineProvenanceOnHost(
		journal, profile,
	); err != nil {
		return err
	}
	if err := finalizeDNSEngineVerifyTarget(ctx, journal); err != nil {
		return err
	}
	if err := publishCommittedDNSEngineTargetOwnership(journal); err != nil {
		return err
	}
	if _, _, ownershipExists, err := exactCommittedDNSEngineProvenanceOnHost(
		journal, profile,
	); err != nil {
		return err
	} else if !ownershipExists {
		return errors.New("committed DNS engine ownership is absent after publication")
	}
	if err := finalizeCommittedDNSEngineSwitchArtifacts(journal); err != nil {
		return err
	}
	if err := retireDNSEngineInstallOwnership(journal); err != nil {
		return err
	}
	if _, installExists, ownershipExists, err := exactCommittedDNSEngineProvenanceOnHost(
		journal, profile,
	); err != nil {
		return err
	} else if installExists || !ownershipExists {
		return errors.New("committed DNS engine ownership handoff is incomplete")
	}
	return removeDNSEngineSwitchJournal()
}
