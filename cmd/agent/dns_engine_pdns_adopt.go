package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/dnsenginerecovery"
	"github.com/alicelik/celikpanel/internal/hostplatform"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

// verifyPDNSAdoptionDatabase proves the existing runtime database implements
// every panel ledger snapshot without creating receipt tables or touching
// DNSSEC/cluster state. Paired nodes may additionally contain peer-owned
// SECONDARY/SLAVE zones; standalone nodes may not contain extra zones.
func verifyPDNSAdoptionDatabase(
	ctx context.Context,
	path string,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
) error {
	if manifest.Mode != transport.DNSEngineSwitchModeAdopt ||
		manifest.SourceEngine != "" ||
		manifest.TargetEngine != transport.DNSEnginePowerDNS {
		return errors.New("PowerDNS adoption database proof received a non-adoption manifest")
	}
	db, err := openPDNSEngineDB(path, true)
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := dnsenginerecovery.VerifyPDNSAdoptionDatabaseTx(ctx, tx, manifest); err != nil {
		return err
	}
	return tx.Commit()
}

type pdnsAdoptionConfigEvidence struct {
	policy     pdnsConfigOwnerPolicy
	snapshots  []dnsFileSnapshot
	identities map[string]pdnsConfigFileIdentity
}

func validatePDNSAdoptionConfigSnapshots(
	policy pdnsConfigOwnerPolicy,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
	snapshots []dnsFileSnapshot,
) error {
	if manifest.Mode != transport.DNSEngineSwitchModeAdopt ||
		manifest.SourceEngine != "" ||
		manifest.TargetEngine != transport.DNSEnginePowerDNS {
		return errors.New("PowerDNS adoption config proof received a non-adoption manifest")
	}
	if err := policy.validateSnapshots(snapshots); err != nil {
		return err
	}
	byPath := pdnsConfigSnapshotMap(snapshots)
	if !byPath[filepath.Clean(dnsMainConf)].Exists ||
		!byPath[filepath.Clean(dnsManagedConf)].Exists {
		return errors.New("PowerDNS adoption is missing managed config evidence")
	}
	peer, err := mutationpayload.CanonicalDNSClusterConfig(
		manifest.Topology, manifest.PeerIP, manifest.PeerNS,
	)
	if err != nil {
		return err
	}
	cluster := byPath[filepath.Clean(dnsClusterConf)]
	wantCluster := manifest.Topology == transport.DNSTopologyPaired
	if cluster.Exists != wantCluster {
		return errors.New("PowerDNS managed topology differs from the adoption receipt")
	}
	if wantCluster {
		expected := dnsClusterConfig(&DNSClusterRequest{
			Role: peer.Role, PeerIP: peer.PeerIP, PeerNS: peer.PeerNS,
		})
		if string(cluster.Data) != expected {
			return errors.New("PowerDNS managed peer differs from the adoption receipt")
		}
	}
	return nil
}

func validatePDNSAdoptionConfigOps(ops pdnsConfigAccessOps) error {
	if ops.resolve == nil || ops.capture == nil {
		return errors.New("PowerDNS adoption config proof operations are incomplete")
	}
	return nil
}

func capturePDNSAdoptionConfigsWithOps(
	ctx context.Context,
	profile hostplatform.Profile,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
	ops pdnsConfigAccessOps,
) (pdnsAdoptionConfigEvidence, error) {
	if ctx == nil {
		return pdnsAdoptionConfigEvidence{},
			errors.New("PowerDNS adoption config capture requires a context")
	}
	if err := certifyAPTPDNSCapabilities(profile); err != nil {
		return pdnsAdoptionConfigEvidence{}, err
	}
	if err := validatePDNSAdoptionConfigOps(ops); err != nil {
		return pdnsAdoptionConfigEvidence{}, err
	}
	policy, err := ops.resolve(ctx)
	if err != nil {
		return pdnsAdoptionConfigEvidence{}, err
	}
	observations, err := ops.capture(policy)
	if err != nil {
		return pdnsAdoptionConfigEvidence{}, err
	}
	if err := validatePDNSConfigObservations(policy, observations); err != nil {
		return pdnsAdoptionConfigEvidence{}, err
	}
	snapshots := make([]dnsFileSnapshot, len(observations))
	identities := make(map[string]pdnsConfigFileIdentity, len(observations))
	for index, observation := range observations {
		snapshots[index] = observation.Snapshot
		snapshots[index].Data = append([]byte(nil), observation.Snapshot.Data...)
		identities[observation.Snapshot.Path] = observation.Identity
	}
	if err := validatePDNSAdoptionConfigSnapshots(
		policy, manifest, snapshots,
	); err != nil {
		return pdnsAdoptionConfigEvidence{}, err
	}
	return pdnsAdoptionConfigEvidence{
		policy: policy, snapshots: snapshots, identities: identities,
	}, nil
}

func capturePDNSAdoptionConfigs(
	ctx context.Context,
	profile hostplatform.Profile,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
) (pdnsAdoptionConfigEvidence, error) {
	return capturePDNSAdoptionConfigsWithOps(
		ctx, profile, manifest, hostPDNSConfigAccessOps(),
	)
}

func pdnsAdoptionConfigEvidenceFromJournalWithOps(
	ctx context.Context,
	profile hostplatform.Profile,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
	journal dnsEngineSwitchJournal,
	ops pdnsConfigAccessOps,
) (pdnsAdoptionConfigEvidence, error) {
	if ctx == nil {
		return pdnsAdoptionConfigEvidence{},
			errors.New("PowerDNS adoption journal config proof requires a context")
	}
	if err := certifyAPTPDNSCapabilities(profile); err != nil {
		return pdnsAdoptionConfigEvidence{}, err
	}
	if err := validatePDNSAdoptionConfigOps(ops); err != nil {
		return pdnsAdoptionConfigEvidence{}, err
	}
	policy, err := ops.resolve(ctx)
	if err != nil {
		return pdnsAdoptionConfigEvidence{}, err
	}
	snapshots := clonePDNSConfigSnapshots(journal.ConfigBefore)
	if err := validatePDNSAdoptionConfigSnapshots(
		policy, manifest, snapshots,
	); err != nil {
		return pdnsAdoptionConfigEvidence{}, err
	}
	return pdnsAdoptionConfigEvidence{
		policy: policy, snapshots: snapshots,
	}, nil
}

func (evidence pdnsAdoptionConfigEvidence) verifyWithOps(
	ctx context.Context,
	profile hostplatform.Profile,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
	ops pdnsConfigAccessOps,
) error {
	if ctx == nil {
		return errors.New("PowerDNS adoption config verification requires a context")
	}
	if err := certifyAPTPDNSCapabilities(profile); err != nil {
		return err
	}
	if err := validatePDNSAdoptionConfigOps(ops); err != nil {
		return err
	}
	if err := validatePDNSAdoptionConfigSnapshots(
		evidence.policy, manifest, evidence.snapshots,
	); err != nil {
		return err
	}
	policy, err := ops.resolve(ctx)
	if err != nil {
		return err
	}
	if policy != evidence.policy {
		return errors.New("PowerDNS service group changed during adoption")
	}
	observations, err := ops.capture(policy)
	if err != nil {
		return err
	}
	if err := validatePDNSConfigObservations(policy, observations); err != nil {
		return err
	}
	actual := make([]dnsFileSnapshot, len(observations))
	for index, observation := range observations {
		actual[index] = observation.Snapshot
	}
	if err := validatePDNSAdoptionConfigSnapshots(
		policy, manifest, actual,
	); err != nil {
		return err
	}
	if !reflect.DeepEqual(actual, evidence.snapshots) {
		return errors.New("PowerDNS adoption config bytes or metadata changed")
	}
	if evidence.identities != nil {
		if len(evidence.identities) != len(observations) {
			return errors.New("PowerDNS adoption config identity set is incomplete")
		}
		for _, observation := range observations {
			expected, ok := evidence.identities[observation.Snapshot.Path]
			if !ok || expected != observation.Identity {
				return errors.New("PowerDNS adoption config inode identity changed")
			}
		}
	}
	return nil
}

func (evidence pdnsAdoptionConfigEvidence) verify(
	ctx context.Context,
	profile hostplatform.Profile,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
) error {
	return evidence.verifyWithOps(
		ctx, profile, manifest, hostPDNSConfigAccessOps(),
	)
}

func mutatePDNSAdoptionAfterConfigProofWithOps(
	ctx context.Context,
	profile hostplatform.Profile,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
	evidence pdnsAdoptionConfigEvidence,
	ops pdnsConfigAccessOps,
	mutation func() error,
) error {
	if mutation == nil {
		return errors.New("PowerDNS adoption mutation callback is required")
	}
	if err := evidence.verifyWithOps(ctx, profile, manifest, ops); err != nil {
		return err
	}
	return mutation()
}

func mutatePDNSAdoptionAfterConfigProof(
	ctx context.Context,
	profile hostplatform.Profile,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
	evidence pdnsAdoptionConfigEvidence,
	mutation func() error,
) error {
	return mutatePDNSAdoptionAfterConfigProofWithOps(
		ctx, profile, manifest, evidence, hostPDNSConfigAccessOps(), mutation,
	)
}

func validatePDNSAdoptionUnitEvidence(units []dnsUnitSnapshot) error {
	return dnsengineartifact.ValidatePDNSAdoptionUnits(units)
}

type pdnsAdoptionEvidenceStage uint8

const (
	pdnsAdoptionEvidencePreflight pdnsAdoptionEvidenceStage = iota + 1
	pdnsAdoptionEvidenceTarget
	pdnsAdoptionEvidenceRollback
	// pdnsAdoptionEvidenceRolledBack re-proves a V1 adoption whose inverse
	// effects are already complete (journal at rolled-back). It performs no
	// effect; only terminal publication and journal retirement remain.
	pdnsAdoptionEvidenceRolledBack
)

func validatePDNSAdoptionTransactionBinding(
	expectedJournal dnsEngineSwitchJournal,
	actualJournal dnsEngineSwitchJournal,
	journalExists bool,
	state dnsEngineStateReceipt,
	stateExists bool,
	stage pdnsAdoptionEvidenceStage,
) error {
	if expectedJournal.Mode != transport.DNSEngineSwitchModeAdopt ||
		expectedJournal.SourceEngine != "" ||
		expectedJournal.TargetEngine != transport.DNSEnginePowerDNS {
		return errors.New("PowerDNS adoption transaction identity is invalid")
	}
	switch stage {
	case pdnsAdoptionEvidencePreflight:
		if expectedJournal.Phase != dnsSwitchPhaseIntent {
			return errors.New("PowerDNS adoption preflight journal phase is invalid")
		}
		if journalExists {
			return errors.New("PowerDNS adoption preflight found an attached journal")
		}
		if stateExists {
			return errors.New("PowerDNS adoption preflight found an active engine receipt")
		}
	case pdnsAdoptionEvidenceTarget:
		if expectedJournal.Phase != dnsSwitchPhaseIntent &&
			expectedJournal.Phase != dnsSwitchPhaseTargetVerified &&
			expectedJournal.Phase != dnsSwitchPhaseCommitted {
			return errors.New("PowerDNS adoption target journal phase is invalid")
		}
		if !journalExists || !reflect.DeepEqual(actualJournal, expectedJournal) {
			return errors.New("PowerDNS adoption target journal identity changed")
		}
		if !stateExists || !exactDNSEngineStateForJournal(state, expectedJournal) {
			return errors.New("PowerDNS adoption target receipt is absent or different")
		}
	case pdnsAdoptionEvidenceRollback:
		if expectedJournal.Phase != dnsSwitchPhaseRollingBack {
			return errors.New("PowerDNS adoption rollback journal phase is invalid")
		}
		if !journalExists || !reflect.DeepEqual(actualJournal, expectedJournal) {
			return errors.New("PowerDNS adoption rollback journal identity changed")
		}
		if stateExists {
			return errors.New("PowerDNS adoption rollback did not restore the empty source receipt")
		}
	case pdnsAdoptionEvidenceRolledBack:
		// V2 journals never reach the Agent's adoption inverse; this stage is
		// V1-only so a rolled-back V1 journal is re-proved with exactly the
		// checks the rolling-back stage applies, and nothing is restored.
		if expectedJournal.Schema != dnsengineartifact.SwitchJournalSchemaV1 ||
			expectedJournal.Phase != dnsSwitchPhaseRolledBack {
			return errors.New("PowerDNS adoption rolled-back journal is not an exact V1 checkpoint")
		}
		if !journalExists || !reflect.DeepEqual(actualJournal, expectedJournal) {
			return errors.New("PowerDNS adoption rollback journal identity changed")
		}
		if stateExists {
			return errors.New("PowerDNS adoption rolled-back journal has a DNS engine receipt; the empty source receipt was not kept")
		}
	default:
		return errors.New("PowerDNS adoption evidence stage is unsupported")
	}
	return nil
}

func verifyPDNSAdoptionTransactionBinding(
	expectedJournal dnsEngineSwitchJournal,
	stage pdnsAdoptionEvidenceStage,
) error {
	actualJournal, journalExists, err := readDNSEngineSwitchJournal()
	if err != nil {
		return err
	}
	state, stateExists, err := readDNSEngineState()
	if err != nil {
		return err
	}
	return validatePDNSAdoptionTransactionBinding(
		expectedJournal, actualJournal, journalExists, state, stateExists, stage,
	)
}

// transitionPDNSAdoptionJournalToRollback records the adoption's rollback
// decision before any inverse effect. The journal on disk decides, not the
// phase last written: only this operation's journal at intent is moved to
// rolling-back, an already durable rolling-back or rolled-back journal resumes,
// and a durable target-verified or committed journal, an unreadable or foreign
// journal, or a rolling-back write that cannot be proved durable returns a
// *dnsSwitchInProcessHandoffError and admits no inverse.
func transitionPDNSAdoptionJournalToRollback(
	expected dnsEngineSwitchJournal,
	read func() (dnsEngineSwitchJournal, bool, error),
	write func(dnsEngineSwitchJournal) error,
	cause error,
) (dnsEngineSwitchJournal, error) {
	if read == nil || write == nil {
		return dnsEngineSwitchJournal{},
			errors.New("PowerDNS adoption rollback journal access is unavailable")
	}
	actual, exists, err := read()
	if err == nil && exists {
		switch actual.Phase {
		case dnsSwitchPhaseIntent, dnsSwitchPhaseRollingBack, dnsSwitchPhaseRolledBack,
			dnsSwitchPhaseTargetVerified, dnsSwitchPhaseCommitted:
		default:
			return dnsEngineSwitchJournal{}, &dnsSwitchInProcessHandoffError{cause: errors.Join(
				cause, fmt.Errorf("PowerDNS adoption journal is at phase %s, which has no rollback decision", actual.Phase),
			)}
		}
	}
	return decideDNSSwitchInProcessRollback(
		expected, dnsSwitchInProcessJournalOps{read: read, write: write}, cause,
	)
}

func handlePDNSAdoptionIntentJournalWriteError(
	cause error,
	rollback func(error) (transport.SwitchDNSEngineV1Response, error),
) (transport.SwitchDNSEngineV1Response, error) {
	if cause == nil {
		return transport.SwitchDNSEngineV1Response{},
			errors.New("PowerDNS adoption intent journal failure is nil")
	}
	if !errors.Is(cause, dnsEngineSwitchRollbackPrecursorError) {
		return transport.SwitchDNSEngineV1Response{}, cause
	}
	if rollback == nil {
		return transport.SwitchDNSEngineV1Response{}, errors.Join(
			cause, errors.New("PowerDNS adoption rollback callback is unavailable"),
		)
	}
	return rollback(cause)
}

func verifyPDNSAdoptionEvidence(
	ctx context.Context,
	systemctl string,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
	journal dnsEngineSwitchJournal,
	stage pdnsAdoptionEvidenceStage,
) error {
	profile, err := verifiedHostProfileForAnyFamily()
	if err != nil {
		return err
	}
	return verifyPDNSAdoptionEvidenceOnCertifiedProfile(
		ctx, profile, systemctl, manifest, journal, nil, stage,
	)
}

func verifyPDNSAdoptionEvidenceOnCertifiedProfile(
	ctx context.Context,
	profile hostplatform.Profile,
	systemctl string,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
	journal dnsEngineSwitchJournal,
	expectedConfigs *pdnsAdoptionConfigEvidence,
	stage pdnsAdoptionEvidenceStage,
) error {
	if manifest.Mode != transport.DNSEngineSwitchModeAdopt ||
		journal.Mode != transport.DNSEngineSwitchModeAdopt {
		return errors.New("PowerDNS adoption evidence received a switch transaction")
	}
	if err := certifyAPTPDNSCapabilities(profile); err != nil {
		return err
	}
	journalManifest, err := switchJournalManifest(journal)
	if err != nil || !reflect.DeepEqual(journalManifest, manifest) {
		return errors.New("PowerDNS adoption evidence differs from its journal manifest")
	}
	if err := verifyPDNSAdoptionTransactionBinding(journal, stage); err != nil {
		return err
	}
	if err := assertPDNSAdoptionArtifactsAbsent(journal); err != nil {
		return err
	}
	configs := pdnsAdoptionConfigEvidence{}
	if expectedConfigs == nil {
		configs, err = pdnsAdoptionConfigEvidenceFromJournalWithOps(
			ctx, profile, manifest, journal, hostPDNSConfigAccessOps(),
		)
		if err != nil {
			return err
		}
	} else {
		configs = *expectedConfigs
		if !reflect.DeepEqual(configs.snapshots, journal.ConfigBefore) {
			return errors.New("PowerDNS adoption config evidence differs from its journal")
		}
	}
	if err := configs.verify(ctx, profile, manifest); err != nil {
		return err
	}
	if err := requireManagedPowerDNSArtifacts(); err != nil {
		return err
	}
	if err := validatePDNSAdoptionUnitEvidence(journal.TargetUnitsBefore); err != nil {
		return err
	}
	if err := verifyDNSUnitSnapshotsExact(ctx, systemctl, journal.TargetUnitsBefore); err != nil {
		return err
	}
	exists, size, digest, err := inspectPDNSDatabaseFile(pdnsDBPath(), false)
	if err != nil || !exists || size != journal.PDNSLiveSize ||
		digest != journal.PDNSLiveSHA256 {
		if err == nil {
			err = errors.New("PowerDNS adoption database bytes changed during verification")
		}
		return err
	}
	if err := verifyPDNSAdoptionDatabase(ctx, pdnsDBPath(), manifest); err != nil {
		return err
	}
	if err := verifyOnlyPDNSActive(ctx, systemctl); err != nil {
		return err
	}
	return verifyDNSZoneManifestAuthority(ctx, manifest.Zones)
}

func rollbackPDNSAdoption(
	ctx context.Context,
	systemctl string,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
	journal dnsEngineSwitchJournal,
) error {
	if ctx == nil {
		return errors.New("rollback PowerDNS adoption requires a bounded context")
	}
	profile, err := verifiedHostProfileForAnyFamily()
	if err != nil {
		return err
	}
	configs, err := pdnsAdoptionConfigEvidenceFromJournalWithOps(
		ctx, profile, manifest, journal, hostPDNSConfigAccessOps(),
	)
	if err != nil {
		return err
	}
	return rollbackPDNSAdoptionOnCertifiedProfile(
		ctx, profile, systemctl, manifest, journal, configs,
	)
}

// pdnsAdoptionRollbackStage selects the Agent's adoption rollback proof. At
// rolling-back the Agent restores the empty source receipt and proves the
// owner's PowerDNS. At rolled-back the inverse effects of this exact operation
// are already durable: a restarted Agent re-proves the restored source with
// the same checks and changes nothing, then its caller publishes the terminal
// verdict (or keeps an already terminal one) and retires the journal. Only V1
// journals reach this inverse; V2 stays with the owner recovery command.
func pdnsAdoptionRollbackStage(
	journal dnsEngineSwitchJournal,
) (stage pdnsAdoptionEvidenceStage, restore bool, err error) {
	switch journal.Phase {
	case dnsSwitchPhaseRollingBack:
		return pdnsAdoptionEvidenceRollback, true, nil
	case dnsSwitchPhaseRolledBack:
		if journal.Schema != dnsengineartifact.SwitchJournalSchemaV1 {
			return 0, false, errors.New("PowerDNS adoption rolled-back journal is not V1; only the owner recovery command may finish it")
		}
		return pdnsAdoptionEvidenceRolledBack, false, nil
	default:
		// The rollback-stage binding refuses any other phase, as before.
		return pdnsAdoptionEvidenceRollback, true, nil
	}
}

func rollbackPDNSAdoptionOnCertifiedProfile(
	ctx context.Context,
	profile hostplatform.Profile,
	systemctl string,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
	journal dnsEngineSwitchJournal,
	configs pdnsAdoptionConfigEvidence,
) error {
	ops, err := pdnsAdoptionRollbackOps(
		journal,
		func(proofCtx context.Context) error {
			return configs.verify(proofCtx, profile, manifest)
		},
		func() error { return restoreDNSEngineStateSnapshot(journal.StateBefore) },
		func(verifyCtx context.Context, stage pdnsAdoptionEvidenceStage) error {
			return verifyPDNSAdoptionEvidenceOnCertifiedProfile(
				verifyCtx, profile, systemctl, manifest, journal,
				&configs, stage,
			)
		},
	)
	if err != nil {
		return err
	}
	return dnsenginerecovery.RollbackPDNSAdoption(ctx, ops)
}

// pdnsAdoptionRollbackOps binds the Agent's adoption rollback steps to the
// journal's stage: the state receipt is restored only at rolling-back, and the
// restored-source proof runs with the stage pdnsAdoptionRollbackStage selects.
func pdnsAdoptionRollbackOps(
	journal dnsEngineSwitchJournal,
	proveConfigs func(context.Context) error,
	restoreState func() error,
	verifyRestored func(context.Context, pdnsAdoptionEvidenceStage) error,
) (dnsenginerecovery.PDNSAdoptionRollbackOps, error) {
	if proveConfigs == nil || restoreState == nil || verifyRestored == nil {
		return dnsenginerecovery.PDNSAdoptionRollbackOps{},
			errors.New("PowerDNS adoption rollback operations are incomplete")
	}
	stage, restore, err := pdnsAdoptionRollbackStage(journal)
	if err != nil {
		return dnsenginerecovery.PDNSAdoptionRollbackOps{}, err
	}
	return dnsenginerecovery.PDNSAdoptionRollbackOps{
		ProveConfigs: proveConfigs,
		RestoreState: func(context.Context) error {
			if !restore {
				return nil
			}
			return restoreState()
		},
		VerifyRestored: func(verifyCtx context.Context) error {
			return verifyRestored(verifyCtx, stage)
		},
	}, nil
}

func adoptPDNS(
	ctx context.Context,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
	binding transport.ServiceMutationBinding,
) (transport.SwitchDNSEngineV1Response, error) {
	if manifest.Mode != transport.DNSEngineSwitchModeAdopt {
		return transport.SwitchDNSEngineV1Response{}, errors.New("PowerDNS adoption requires adopt mode")
	}
	profile, err := verifiedHostProfileForAnyFamily()
	if err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	return runCertifiedPDNSTargetMutation(profile, func() (transport.SwitchDNSEngineV1Response, error) {
		return adoptPDNSOnCertifiedProfile(ctx, manifest, binding, profile)
	})
}

func adoptPDNSOnCertifiedProfile(
	ctx context.Context,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
	binding transport.ServiceMutationBinding,
	profile hostplatform.Profile,
) (transport.SwitchDNSEngineV1Response, error) {
	systemctl, err := executableForProfile(profile, string(profile.PackageManager), "systemctl")
	if err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	state, stateExists, err := readDNSEngineState()
	if err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	exactState := dnsEngineStateReceipt{
		Schema: dnsEngineStateSchema, Mode: manifest.Mode,
		Engine: transport.DNSEnginePowerDNS, EngineEpoch: manifest.TargetEpoch,
		SourceRevision: manifest.SourceRevision, ManifestQualifier: manifest.Qualifier,
		MutationRequestID: binding.MutationRequestID, MutationOwnerID: binding.MutationOwnerID,
	}
	if stateExists {
		if state != exactState {
			return transport.SwitchDNSEngineV1Response{}, errors.New("PowerDNS adoption conflicts with an existing DNS engine receipt")
		}
		configs, err := capturePDNSAdoptionConfigs(ctx, profile, manifest)
		if err != nil {
			return transport.SwitchDNSEngineV1Response{}, err
		}
		if err := verifyPDNSAdoptionDatabase(ctx, pdnsDBPath(), manifest); err != nil {
			return transport.SwitchDNSEngineV1Response{}, err
		}
		if err := requireManagedDNSClusterReady(); err != nil {
			return transport.SwitchDNSEngineV1Response{}, err
		}
		if err := verifyOnlyPDNSActive(ctx, systemctl); err != nil {
			return transport.SwitchDNSEngineV1Response{}, err
		}
		if err := verifyDNSZoneManifestAuthority(ctx, manifest.Zones); err != nil {
			return transport.SwitchDNSEngineV1Response{}, err
		}
		if err := configs.verify(ctx, profile, manifest); err != nil {
			return transport.SwitchDNSEngineV1Response{}, err
		}
		return transport.SwitchDNSEngineV1Response{
			Applied: true, ActiveEngine: transport.DNSEnginePowerDNS,
			ActiveEpoch: manifest.TargetEpoch, AppliedZones: len(manifest.Zones),
			Detail: "the exact managed PowerDNS authority was already adopted and verified",
		}, nil
	}
	if _, exists, err := readDNSEngineSwitchJournal(); err != nil || exists {
		if err == nil {
			err = errors.New("a DNS engine adoption journal requires reconciliation")
		}
		return transport.SwitchDNSEngineV1Response{}, err
	}
	if err := verifyDNSEngineSwitchSource(ctx, profile, manifest, state, false); err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	configs, err := capturePDNSAdoptionConfigs(ctx, profile, manifest)
	if err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	units, err := captureDNSUnitSnapshots(
		ctx, systemctl, []string{"bind9.service", "named.service", "pdns.service"},
	)
	if err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	if err := validatePDNSAdoptionUnitEvidence(units); err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	stateBefore, err := captureDNSEngineStateSnapshot(true)
	if err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	exists, liveSize, liveDigest, err := inspectPDNSDatabaseFile(pdnsDBPath(), false)
	if err != nil || !exists {
		if err == nil {
			err = errors.New("managed PowerDNS database is absent")
		}
		return transport.SwitchDNSEngineV1Response{}, err
	}
	journal := dnsEngineSwitchJournal{
		Schema: dnsEngineSwitchJournalSchema, Phase: dnsSwitchPhaseIntent,
		Mode:              manifest.Mode,
		MutationRequestID: binding.MutationRequestID,
		MutationOwnerID:   binding.MutationOwnerID,
		ManifestQualifier: manifest.Qualifier,
		SourceEngine:      manifest.SourceEngine, TargetEngine: manifest.TargetEngine,
		SourceEpoch: manifest.SourceEpoch, TargetEpoch: manifest.TargetEpoch,
		SourceRevision: manifest.SourceRevision, Topology: manifest.Topology,
		PeerIP: manifest.PeerIP, PeerNS: manifest.PeerNS,
		SnapshotBytes: manifest.SnapshotBytes, Zones: manifest.Zones,
		StateBefore: stateBefore, ConfigBefore: configs.snapshots,
		TargetUnitsBefore: units, SourceUnitsBefore: []dnsUnitSnapshot{},
		PDNSLiveSHA256: liveDigest, PDNSLiveSize: liveSize,
	}
	if err := validatePDNSAdoptionJournal(journal); err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	if err := verifyPDNSAdoptionEvidenceOnCertifiedProfile(
		ctx, profile, systemctl, manifest, journal, &configs,
		pdnsAdoptionEvidencePreflight,
	); err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	writeJournal := func(journal dnsEngineSwitchJournal) error {
		return writeDNSEngineSwitchJournalForFaultDriver(
			dnsEngineSwitchFaultDriverPDNSAdopt, journal,
		)
	}
	if err := runDNSEngineSwitchPreIntentFaultHook(
		dnsEngineSwitchFaultDriverPDNSAdopt, manifest, binding,
	); err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	rollback := func(cause error) (transport.SwitchDNSEngineV1Response, error) {
		return transport.SwitchDNSEngineV1Response{}, runGatedDNSSwitchRollback(
			&journal, cause, gatedDNSSwitchRollbackOps{
				decide: func(current dnsEngineSwitchJournal, cause error) (dnsEngineSwitchJournal, error) {
					return transitionPDNSAdoptionJournalToRollback(
						current, readDNSEngineSwitchJournal, writeJournal, cause,
					)
				},
				inverse: func(decided dnsEngineSwitchJournal) error {
					recoveryCtx, cancel, contextErr := newDNSEngineRollbackContext(ctx)
					if contextErr != nil {
						return contextErr
					}
					defer cancel()
					return rollbackPDNSAdoptionOnCertifiedProfile(
						recoveryCtx, profile, systemctl, manifest, decided, configs,
					)
				},
				write: writeJournal,
			},
		)
	}
	if err := mutatePDNSAdoptionAfterConfigProof(
		ctx, profile, manifest, configs,
		func() error { return writeJournal(journal) },
	); err != nil {
		return handlePDNSAdoptionIntentJournalWriteError(err, rollback)
	}
	if err := mutatePDNSAdoptionAfterConfigProof(
		ctx, profile, manifest, configs,
		func() error { return writeDNSEngineState(exactState) },
	); err != nil {
		// Prove owner and mode, not just bytes. A write that reported an
		// error may still have landed, and this readback is what promotes
		// it to "durable" — so it must check the same contract
		// persistExactDNSEngineState enforces. The loose reader compares
		// content alone, which would accept a file whose ownership or mode
		// drifted and then continue into the committed phase.
		// Yalnız baytları değil sahipliği ve kipi de kanıtla. Hata bildiren
		// bir yazma yine de diske inmiş olabilir ve onu "kalıcı" ilan eden
		// şey bu geri okumadır; bu yüzden persistExactDNSEngineState'in
		// dayattığı sözleşmenin aynısını denetlemelidir. Gevşek okuyucu
		// yalnız içeriği karşılaştırır; sahipliği ya da kipi kaymış bir
		// dosyayı kabul edip commit aşamasına devam ederdi.
		actual, exists, readErr := readExactDNSEngineState()
		if readErr != nil || !exists || actual != exactState {
			return rollback(errors.Join(err, readErr))
		}
	}
	if err := verifyPDNSAdoptionEvidenceOnCertifiedProfile(
		ctx, profile, systemctl, manifest, journal, &configs,
		pdnsAdoptionEvidenceTarget,
	); err != nil {
		return rollback(err)
	}
	journal.Phase = dnsSwitchPhaseTargetVerified
	var verifiedWriteErr error
	if err := mutatePDNSAdoptionAfterConfigProof(
		ctx, profile, manifest, configs,
		func() error {
			verifiedWriteErr = writeJournal(journal)
			return verifiedWriteErr
		},
	); err != nil {
		if verifiedWriteErr != nil {
			// The write may be durable although it reported failure; the
			// rollback gate reads the journal back and goes forward when it
			// is, and takes the rollback decision only from intent.
			return rollback(err)
		}
		return transport.SwitchDNSEngineV1Response{}, err
	}
	journal.Phase = dnsSwitchPhaseCommitted
	if err := mutatePDNSAdoptionAfterConfigProof(
		ctx, profile, manifest, configs,
		func() error { return writeJournal(journal) },
	); err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	return transport.SwitchDNSEngineV1Response{
		Applied: true, ActiveEngine: transport.DNSEnginePowerDNS,
		ActiveEpoch: manifest.TargetEpoch, AppliedZones: len(manifest.Zones),
		Detail: "the existing managed PowerDNS authority was adopted without service or DNS-data changes",
	}, nil
}

func assertPDNSAdoptionArtifactsAbsent(journal dnsEngineSwitchJournal) error {
	for _, path := range []string{journal.PDNSCandidatePath, journal.PDNSBackupPath} {
		if strings.TrimSpace(path) != "" {
			return fmt.Errorf("PowerDNS adoption unexpectedly names a staging artifact")
		}
	}
	for _, path := range []string{
		pdnsSwitchCandidatePath(journal.MutationRequestID),
		pdnsSwitchBackupPath(journal.MutationRequestID),
	} {
		if _, err := os.Lstat(path); err == nil {
			return errors.New("PowerDNS adoption unexpectedly found a switch database artifact")
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}
