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

	expected := make(map[string]transport.DNSEngineSwitchZoneSnapshot, len(manifest.Zones))
	for _, zone := range manifest.Zones {
		expected[zone.Domain] = zone
		zoneType, records, found, err := readPDNSV3ZoneTx(ctx, tx, zone.Domain)
		if err != nil {
			return err
		}
		if zone.Delete {
			if found {
				return errors.New("PowerDNS adoption found a ledger-deleted zone")
			}
			continue
		}
		if !found {
			return errors.New("PowerDNS adoption is missing a ledger zone")
		}
		actual, err := mutationpayload.CanonicalDNSZoneSyncV3(
			transport.DNSEnginePowerDNS, manifest.TargetEpoch,
			zone.DesiredGeneration, zone.Domain, false, zoneType, records,
		)
		if err != nil || actual.Qualifier != zone.ZoneQualifier ||
			actual.ZoneType != zone.ZoneType ||
			!reflect.DeepEqual(actual.Records, zone.Records) {
			return errors.New("PowerDNS adoption zone differs from the panel ledger")
		}
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT name, type, COALESCE(master, ''), COALESCE(account, '') FROM domains
		ORDER BY name COLLATE BINARY, type COLLATE BINARY, id
	`)
	if err != nil {
		return err
	}
	defer rows.Close()
	seen := make(map[string]struct{})
	for rows.Next() {
		var name, zoneType, master, account string
		if err := rows.Scan(&name, &zoneType, &master, &account); err != nil {
			return err
		}
		if !serviceMutationCanonicalFQDN(name) {
			return errors.New("PowerDNS adoption found a noncanonical zone name")
		}
		if _, duplicate := seen[name]; duplicate {
			return errors.New("PowerDNS adoption found duplicate zone authority")
		}
		seen[name] = struct{}{}
		if zone, listed := expected[name]; listed {
			if zone.Delete || zoneType != zone.ZoneType {
				return errors.New("PowerDNS adoption zone type differs from the panel ledger")
			}
			continue
		}
		if manifest.Topology != transport.DNSTopologyPaired ||
			(strings.ToUpper(zoneType) != "SLAVE" &&
				strings.ToUpper(zoneType) != "SECONDARY") ||
			master != manifest.PeerIP || account != "celikpanel" {
			return errors.New("PowerDNS adoption found an unowned extra zone")
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	var supermasters, exactSupermasters int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM supermasters`).Scan(
		&supermasters,
	); err != nil {
		return err
	}
	if manifest.Topology == transport.DNSTopologyPaired {
		if err := tx.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM supermasters
			WHERE ip = ? AND nameserver = ? AND account = 'celikpanel'
		`, manifest.PeerIP, manifest.PeerNS).Scan(&exactSupermasters); err != nil {
			return err
		}
		if supermasters != 1 || exactSupermasters != 1 {
			return errors.New("PowerDNS adoption autoprimary peer differs from the manifest")
		}
	} else if supermasters != 0 {
		return errors.New("PowerDNS standalone adoption found an autoprimary peer")
	}
	var integrity string
	if err := tx.QueryRowContext(ctx, `PRAGMA quick_check`).Scan(&integrity); err != nil ||
		integrity != "ok" {
		if err == nil {
			err = errors.New("PowerDNS adoption database failed quick_check")
		}
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
	if !dnsUnitSnapshotNamesEqual(
		units, []string{"bind9.service", "named.service", "pdns.service"},
	) {
		return errors.New("PowerDNS adoption unit evidence is incomplete")
	}
	for _, unit := range units {
		active := unit.ActiveState == "active"
		if unit.Name == "pdns.service" {
			if !active {
				return errors.New("PowerDNS adoption target is not running")
			}
			continue
		}
		if active {
			return errors.New("PowerDNS adoption found another DNS engine running")
		}
	}
	return nil
}

type pdnsAdoptionEvidenceStage uint8

const (
	pdnsAdoptionEvidencePreflight pdnsAdoptionEvidenceStage = iota + 1
	pdnsAdoptionEvidenceTarget
	pdnsAdoptionEvidenceRollback
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

func transitionPDNSAdoptionJournalToRollback(
	expected dnsEngineSwitchJournal,
	read func() (dnsEngineSwitchJournal, bool, error),
	write func(dnsEngineSwitchJournal) error,
) (dnsEngineSwitchJournal, error) {
	if read == nil || write == nil {
		return dnsEngineSwitchJournal{},
			errors.New("PowerDNS adoption rollback journal access is unavailable")
	}
	if expected.Phase != dnsSwitchPhaseIntent {
		return dnsEngineSwitchJournal{},
			errors.New("PowerDNS adoption rollback can start only from intent")
	}
	actual, exists, err := read()
	if err != nil {
		return dnsEngineSwitchJournal{}, err
	}
	if !exists || !reflect.DeepEqual(actual, expected) {
		return dnsEngineSwitchJournal{},
			errors.New("PowerDNS adoption rollback journal identity changed")
	}
	next := expected
	next.Phase = dnsSwitchPhaseRollingBack
	if err := write(next); err != nil {
		return dnsEngineSwitchJournal{}, err
	}
	return next, nil
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

func rollbackPDNSAdoptionOnCertifiedProfile(
	ctx context.Context,
	profile hostplatform.Profile,
	systemctl string,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
	journal dnsEngineSwitchJournal,
	configs pdnsAdoptionConfigEvidence,
) error {
	return rollbackPDNSAdoptionAfterConfigProof(
		func() error {
			return configs.verify(ctx, profile, manifest)
		},
		func() error {
			return rollbackPDNSAdoptionWithOps(
				ctx,
				func() error {
					return restoreDNSFileSnapshot(journal.StateBefore)
				},
				func(verifyCtx context.Context) error {
					return verifyPDNSAdoptionEvidenceOnCertifiedProfile(
						verifyCtx, profile, systemctl, manifest, journal,
						&configs, pdnsAdoptionEvidenceRollback,
					)
				},
			)
		},
	)
}

func rollbackPDNSAdoptionAfterConfigProof(
	proveConfigs func() error,
	rollback func() error,
) error {
	if proveConfigs == nil || rollback == nil {
		return errors.New("PowerDNS adoption rollback requires config proof")
	}
	if err := proveConfigs(); err != nil {
		return err
	}
	return rollback()
}

func rollbackPDNSAdoptionWithOps(
	ctx context.Context,
	restoreState func() error,
	verifyRestored func(context.Context) error,
) error {
	if ctx == nil || restoreState == nil || verifyRestored == nil {
		return errors.New("invalid PowerDNS adoption rollback operations")
	}
	restoreErr := restoreState()
	if restoreErr != nil {
		return restoreErr
	}
	return verifyRestored(ctx)
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
	stateBefore, err := captureDNSFileSnapshot(dnsEngineStatePath(), 0o600, true)
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
		rollingBack, transitionErr := transitionPDNSAdoptionJournalToRollback(
			journal, readDNSEngineSwitchJournal, writeJournal,
		)
		if transitionErr != nil {
			return transport.SwitchDNSEngineV1Response{}, errors.Join(cause, transitionErr)
		}
		journal = rollingBack
		var journalErr error
		recoveryCtx, cancel, contextErr := newDNSEngineRollbackContext(ctx)
		if contextErr != nil {
			return transport.SwitchDNSEngineV1Response{},
				errors.Join(cause, journalErr, contextErr)
		}
		defer cancel()
		rollbackErr := rollbackPDNSAdoptionOnCertifiedProfile(
			recoveryCtx, profile, systemctl, manifest, journal, configs,
		)
		if rollbackErr == nil {
			journal.Phase = dnsSwitchPhaseRolledBack
			journalErr = writeJournal(journal)
			if journalErr == nil {
				journalErr = removeDNSEngineSwitchJournal()
			}
		}
		return transport.SwitchDNSEngineV1Response{}, errors.Join(cause, journalErr, rollbackErr)
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
		actual, exists, readErr := readDNSEngineState()
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
	if err := mutatePDNSAdoptionAfterConfigProof(
		ctx, profile, manifest, configs,
		func() error { return writeJournal(journal) },
	); err != nil {
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
