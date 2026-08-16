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
		SELECT name, type FROM domains
		ORDER BY name COLLATE BINARY, type COLLATE BINARY, id
	`)
	if err != nil {
		return err
	}
	defer rows.Close()
	seen := make(map[string]struct{})
	for rows.Next() {
		var name, zoneType string
		if err := rows.Scan(&name, &zoneType); err != nil {
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
				strings.ToUpper(zoneType) != "SECONDARY") {
			return errors.New("PowerDNS adoption found an unowned extra zone")
		}
	}
	if err := rows.Err(); err != nil {
		return err
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

func verifyPDNSAdoptionTopology(topology string) error {
	cluster, err := captureDNSFileSnapshotPreserve(dnsClusterConf, true)
	if err != nil {
		return err
	}
	wantCluster := topology == transport.DNSTopologyPaired
	if topology != transport.DNSTopologyStandalone && !wantCluster {
		return errors.New("PowerDNS adoption topology is unsupported")
	}
	if cluster.Exists != wantCluster {
		return errors.New("PowerDNS managed topology differs from the adoption receipt")
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

func verifyPDNSAdoptionEvidence(
	ctx context.Context,
	systemctl string,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
	journal dnsEngineSwitchJournal,
) error {
	if manifest.Mode != transport.DNSEngineSwitchModeAdopt ||
		journal.Mode != transport.DNSEngineSwitchModeAdopt {
		return errors.New("PowerDNS adoption evidence received a switch transaction")
	}
	if err := assertPDNSAdoptionArtifactsAbsent(journal); err != nil {
		return err
	}
	if err := requireManagedDNSClusterReady(); err != nil {
		return err
	}
	if err := verifyDNSFileSnapshotsExact(journal.ConfigBefore); err != nil {
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
	return verifyPDNSAdoptionEvidence(context.WithoutCancel(ctx), systemctl, manifest, journal)
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
		if err := verifyPDNSAdoptionTopology(manifest.Topology); err != nil {
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
		SnapshotBytes: manifest.SnapshotBytes, Zones: manifest.Zones,
		StateBefore: stateBefore, ConfigBefore: configs,
		TargetUnitsBefore: units, SourceUnitsBefore: []dnsUnitSnapshot{},
		PDNSLiveSHA256: liveDigest, PDNSLiveSize: liveSize,
	}
	if err := validatePDNSAdoptionJournal(journal); err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	if err := verifyPDNSAdoptionEvidence(ctx, systemctl, manifest, journal); err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	if err := writeDNSEngineSwitchJournal(journal); err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	rollback := func(cause error) (transport.SwitchDNSEngineV1Response, error) {
		journal.Phase = dnsSwitchPhaseRollingBack
		journalErr := writeDNSEngineSwitchJournal(journal)
		recoveryCtx, cancel := context.WithTimeout(
			context.WithoutCancel(ctx), dnsEngineSwitchRecoveryLimit,
		)
		defer cancel()
		rollbackErr := rollbackPDNSAdoption(recoveryCtx, systemctl, manifest, journal)
		if rollbackErr == nil {
			journal.Phase = dnsSwitchPhaseRolledBack
			journalErr = errors.Join(
				journalErr, writeDNSEngineSwitchJournal(journal),
				removeDNSEngineSwitchJournal(),
			)
		}
		return transport.SwitchDNSEngineV1Response{}, errors.Join(cause, journalErr, rollbackErr)
	}
	if err := writeDNSEngineState(exactState); err != nil {
		actual, exists, readErr := readDNSEngineState()
		if readErr != nil || !exists || actual != exactState {
			return rollback(errors.Join(err, readErr))
		}
	}
	if err := verifyPDNSAdoptionEvidence(ctx, systemctl, manifest, journal); err != nil {
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
