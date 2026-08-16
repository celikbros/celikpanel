package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

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

func capturePDNSAdoptionConfigs() ([]dnsFileSnapshot, error) {
	paths := []string{
		filepath.Clean(dnsMainConf),
		filepath.Clean(dnsManagedConf),
		filepath.Clean(dnsClusterConf),
	}
	sort.Strings(paths)
	snapshots := make([]dnsFileSnapshot, len(paths))
	for index, path := range paths {
		allowAbsent := path == filepath.Clean(dnsClusterConf)
		snapshot, err := captureDNSFileSnapshotPreserve(path, allowAbsent)
		if err != nil {
			return nil, err
		}
		snapshots[index] = snapshot
	}
	return snapshots, nil
}

func verifyPDNSAdoptionTopology(
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
) error {
	peer, err := mutationpayload.CanonicalDNSClusterConfig(
		manifest.Topology, manifest.PeerIP, manifest.PeerNS,
	)
	if err != nil {
		return err
	}
	cluster, err := captureDNSFileSnapshotPreserve(dnsClusterConf, true)
	if err != nil {
		return err
	}
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

func verifyPDNSAdoptionEvidence(
	ctx context.Context,
	systemctl string,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
	journal dnsEngineSwitchJournal,
	stage pdnsAdoptionEvidenceStage,
) error {
	if manifest.Mode != transport.DNSEngineSwitchModeAdopt ||
		journal.Mode != transport.DNSEngineSwitchModeAdopt {
		return errors.New("PowerDNS adoption evidence received a switch transaction")
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
	if err := requireManagedPowerDNSArtifacts(); err != nil {
		return err
	}
	if err := verifyDNSFileSnapshotsExact(journal.ConfigBefore); err != nil {
		return err
	}
	if err := verifyPDNSAdoptionTopology(manifest); err != nil {
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
	restoreErr := restoreDNSFileSnapshot(journal.StateBefore)
	if restoreErr != nil {
		return restoreErr
	}
	return verifyPDNSAdoptionEvidence(
		context.WithoutCancel(ctx), systemctl, manifest, journal,
		pdnsAdoptionEvidenceRollback,
	)
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
		if err := verifyPDNSAdoptionDatabase(ctx, pdnsDBPath(), manifest); err != nil {
			return transport.SwitchDNSEngineV1Response{}, err
		}
		if err := verifyPDNSAdoptionTopology(manifest); err != nil {
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
	configs, err := capturePDNSAdoptionConfigs()
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
		StateBefore: stateBefore, ConfigBefore: configs,
		TargetUnitsBefore: units, SourceUnitsBefore: []dnsUnitSnapshot{},
		PDNSLiveSHA256: liveDigest, PDNSLiveSize: liveSize,
	}
	if err := validatePDNSAdoptionJournal(journal); err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	if err := verifyPDNSAdoptionEvidence(
		ctx, systemctl, manifest, journal, pdnsAdoptionEvidencePreflight,
	); err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	if err := writeDNSEngineSwitchJournal(journal); err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	rollback := func(cause error) (transport.SwitchDNSEngineV1Response, error) {
		rollingBack, transitionErr := transitionPDNSAdoptionJournalToRollback(
			journal, readDNSEngineSwitchJournal, writeDNSEngineSwitchJournal,
		)
		if transitionErr != nil {
			return transport.SwitchDNSEngineV1Response{}, errors.Join(cause, transitionErr)
		}
		journal = rollingBack
		var journalErr error
		recoveryCtx, cancel := context.WithTimeout(
			context.WithoutCancel(ctx), dnsEngineSwitchRecoveryLimit,
		)
		defer cancel()
		rollbackErr := rollbackPDNSAdoption(recoveryCtx, systemctl, manifest, journal)
		if rollbackErr == nil {
			journal.Phase = dnsSwitchPhaseRolledBack
			journalErr = writeDNSEngineSwitchJournal(journal)
			if journalErr == nil {
				journalErr = removeDNSEngineSwitchJournal()
			}
		}
		return transport.SwitchDNSEngineV1Response{}, errors.Join(cause, journalErr, rollbackErr)
	}
	if err := writeDNSEngineState(exactState); err != nil {
		actual, exists, readErr := readDNSEngineState()
		if readErr != nil || !exists || actual != exactState {
			return rollback(errors.Join(err, readErr))
		}
	}
	if err := verifyPDNSAdoptionEvidence(
		ctx, systemctl, manifest, journal, pdnsAdoptionEvidenceTarget,
	); err != nil {
		return rollback(err)
	}
	journal.Phase = dnsSwitchPhaseTargetVerified
	if err := writeDNSEngineSwitchJournal(journal); err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	journal.Phase = dnsSwitchPhaseCommitted
	if err := writeDNSEngineSwitchJournal(journal); err != nil {
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
