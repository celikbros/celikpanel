package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/alicelik/celikpanel/internal/binddns"
	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/hostplatform"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

func isPDNSPairSecondaryReconfigureManifest(
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
) bool {
	return manifest.Mode == transport.DNSEngineSwitchModeSwitch &&
		manifest.SourceEngine == "" && manifest.SourceEpoch == 0 &&
		manifest.TargetEngine == transport.DNSEnginePowerDNS &&
		manifest.TargetEpoch == 1 &&
		manifest.Topology == transport.DNSTopologyPaired &&
		manifest.PairRole == transport.DNSPairRoleSecondary &&
		len(manifest.Zones) == 0 && manifest.SnapshotBytes == 0
}

type pdnsPairSecondarySourceClass uint8

const (
	pdnsPairSecondarySourceNotApplicable pdnsPairSecondarySourceClass = iota
	pdnsPairSecondarySourceFresh
	pdnsPairSecondarySourceReconfigure
)

// dnsEngineSwitchSourceProof records facts established from the live source,
// not merely inferred from the target manifest. The paired-secondary manifest
// is shared by a fresh PowerDNS install and the narrow legacy reconfiguration.
type dnsEngineSwitchSourceProof struct {
	PDNSPairSecondaryReconfigure bool
}

// classifyPDNSPairSecondarySource selects which exact source proof is needed.
// A running PDNS unit is only a reconfiguration candidate; the caller must
// still prove the managed config, sole authority, unsigned topology and empty
// live database before publishing a reconfiguration source proof.
func classifyPDNSPairSecondarySource(
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
	stateExists bool,
	bindActive bool,
	bindAliasActive bool,
	pdnsActive bool,
) (pdnsPairSecondarySourceClass, error) {
	if !isPDNSPairSecondaryReconfigureManifest(manifest) {
		return pdnsPairSecondarySourceNotApplicable, nil
	}
	if stateExists {
		return pdnsPairSecondarySourceNotApplicable, errors.New(
			"initial paired-secondary PowerDNS transition requires no durable source state",
		)
	}
	if bindActive || bindAliasActive {
		return pdnsPairSecondarySourceNotApplicable, errors.New(
			"initial paired-secondary PowerDNS transition found a running BIND authority",
		)
	}
	if pdnsActive {
		return pdnsPairSecondarySourceReconfigure, nil
	}
	return pdnsPairSecondarySourceFresh, nil
}

func validatePDNSSwitchPackagePolicy(
	proof dnsEngineSwitchSourceProof,
	missingPackages int,
) error {
	if missingPackages < 0 {
		return errors.New("PowerDNS missing package count is invalid")
	}
	if proof.PDNSPairSecondaryReconfigure && missingPackages != 0 {
		return errors.New(
			"PowerDNS secondary reconfiguration cannot install missing packages",
		)
	}
	return nil
}

func validatePDNSSwitchSourceProofCAS(
	initial dnsEngineSwitchSourceProof,
	current dnsEngineSwitchSourceProof,
) error {
	if initial.PDNSPairSecondaryReconfigure !=
		current.PDNSPairSecondaryReconfigure {
		return errors.New(
			"DNS source classification changed before the switch journal",
		)
	}
	return nil
}

func finishDNSSwitchRollbackJournal(
	journal *dnsEngineSwitchJournal,
	write func(dnsEngineSwitchJournal) error,
	remove func() error,
) error {
	if journal == nil || write == nil || remove == nil {
		return errors.New("invalid DNS switch rollback journal operations")
	}
	journal.Phase = dnsSwitchPhaseRolledBack
	if err := write(*journal); err != nil {
		return err
	}
	return remove()
}

var pdnsReconfigureDataTables = []string{
	"domains",
	"records",
	"supermasters",
	"comments",
	"domainmetadata",
	"cryptokeys",
	"tsigkeys",
	"celikpanel_dns_zone_sync_receipts",
	"celikpanel_dns_zone_sync_v3_receipts",
	"celikpanel_dns_engine_manifest_receipt",
}

var pdnsReplacementIndexes = map[string]bool{
	"name_index": true, "catalog_idx": true,
	"records_lookup_idx": true, "records_lookup_id_idx": true,
	"records_order_idx": true, "ip_nameserver_pk": true,
	"comments_idx": true, "comments_order_idx": true,
	"domainmetadata_idx": true, "domainidindex": true,
	"namealgoindex": true,
}

func requireNoPDNSDatabaseSidecars(path string) error {
	for _, suffix := range []string{"-journal", "-wal", "-shm"} {
		if _, err := os.Lstat(path + suffix); !errors.Is(err, os.ErrNotExist) {
			if err == nil {
				err = errors.New(
					"PowerDNS database has an unresolved SQLite sidecar",
				)
			}
			return err
		}
	}
	return nil
}

func verifyPDNSReplacementDatabaseEnvelope(ctx context.Context, path string) error {
	if _, _, _, err := inspectPDNSDatabaseFile(path, false); err != nil {
		return err
	}
	if err := requireNoPDNSDatabaseSidecars(path); err != nil {
		return err
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
	var journalMode, integrity string
	if err := tx.QueryRowContext(ctx, `PRAGMA journal_mode`).Scan(&journalMode); err != nil ||
		!strings.EqualFold(journalMode, "delete") {
		if err == nil {
			err = errors.New("PowerDNS source database must use DELETE journaling")
		}
		return err
	}
	if err := tx.QueryRowContext(ctx, `PRAGMA quick_check`).Scan(&integrity); err != nil ||
		integrity != "ok" {
		if err == nil {
			err = errors.New("PowerDNS source database failed quick_check")
		}
		return err
	}
	allowedTables := make(map[string]bool, len(pdnsReconfigureDataTables))
	for _, table := range pdnsReconfigureDataTables {
		allowedTables[table] = true
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT type, name FROM sqlite_master
		WHERE type IN ('table','view','trigger','index')
		  AND name NOT LIKE 'sqlite_autoindex_%'
		  AND name NOT LIKE 'sqlite_%'
		ORDER BY type, name COLLATE BINARY
	`)
	if err != nil {
		return err
	}
	defer rows.Close()
	foundTables := make(map[string]bool, len(allowedTables))
	foundIndexes := make(map[string]bool, len(pdnsReplacementIndexes))
	for rows.Next() {
		var objectType, name string
		if err := rows.Scan(&objectType, &name); err != nil {
			return err
		}
		switch objectType {
		case "table":
			if !allowedTables[name] {
				return fmt.Errorf("PowerDNS source database contains unrecognized table %q", name)
			}
			foundTables[name] = true
		case "index":
			if !pdnsReplacementIndexes[name] {
				return fmt.Errorf("PowerDNS source database contains unrecognized index %q", name)
			}
			foundIndexes[name] = true
		default:
			return fmt.Errorf("PowerDNS source database contains unsafe %s %q", objectType, name)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, table := range pdnsReconfigureDataTables {
		if !foundTables[table] {
			return fmt.Errorf("PowerDNS source database is missing table %q", table)
		}
	}
	for index := range pdnsReplacementIndexes {
		if !foundIndexes[index] {
			return fmt.Errorf("PowerDNS source database is missing index %q", index)
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return requireNoPDNSDatabaseSidecars(path)
}

// verifyEmptyStandalonePDNSDatabase proves that an unreceipted, already
// running PowerDNS contains no authority that could be destroyed by the
// secondary candidate replacement. It is read-only and rejects unfamiliar
// application tables, incomplete schemas, sidecars and non-DELETE journaling.
func verifyEmptyStandalonePDNSDatabase(
	ctx context.Context,
	path string,
) error {
	if _, _, _, err := inspectPDNSDatabaseFile(path, false); err != nil {
		return err
	}
	if err := requireNoPDNSDatabaseSidecars(path); err != nil {
		return err
	}
	db, err := openPDNSEngineDB(path, true)
	if err != nil {
		return err
	}
	closed := false
	defer func() {
		if !closed {
			_ = db.Close()
		}
	}()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var journalMode, integrity string
	if err := tx.QueryRowContext(ctx, `PRAGMA journal_mode`).Scan(
		&journalMode,
	); err != nil || !strings.EqualFold(journalMode, "delete") {
		if err == nil {
			err = errors.New(
				"PowerDNS database must use DELETE journaling before replacement",
			)
		}
		return err
	}
	if err := tx.QueryRowContext(ctx, `PRAGMA quick_check`).Scan(
		&integrity,
	); err != nil || integrity != "ok" {
		if err == nil {
			err = errors.New("PowerDNS database failed quick_check")
		}
		return err
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT name FROM sqlite_master
		WHERE type = 'table' AND name NOT LIKE 'sqlite_%'
		ORDER BY name COLLATE BINARY
	`)
	if err != nil {
		return err
	}
	found := make(map[string]bool, len(pdnsReconfigureDataTables))
	allowed := make(map[string]bool, len(pdnsReconfigureDataTables))
	for _, table := range pdnsReconfigureDataTables {
		allowed[table] = true
	}
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			rows.Close()
			return err
		}
		if !allowed[table] {
			rows.Close()
			return fmt.Errorf(
				"PowerDNS database contains an unrecognized table %q", table,
			)
		}
		found[table] = true
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, table := range pdnsReconfigureDataTables {
		// The two V3 tables are absent on a legitimate legacy database.
		optional := table == pdnsV3ReceiptTable ||
			table == "celikpanel_dns_engine_manifest_receipt"
		if !found[table] {
			if optional {
				continue
			}
			return fmt.Errorf(
				"PowerDNS database is missing required table %q", table,
			)
		}
		var count int
		if err := tx.QueryRowContext(
			ctx, "SELECT count(*) FROM "+table,
		).Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			return fmt.Errorf(
				"PowerDNS database table %q is not empty", table,
			)
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if err := db.Close(); err != nil {
		return err
	}
	closed = true
	return requireNoPDNSDatabaseSidecars(path)
}

func managedPowerDNSStandaloneConfig(ctx context.Context) ([]byte, error) {
	addresses, err := publicListenAddresses(ctx)
	if err != nil {
		return nil, err
	}
	return managedPowerDNSStandaloneConfigForAddresses(addresses)
}

func managedPowerDNSStandaloneConfigForAddresses(addresses []string) ([]byte, error) {
	if len(addresses) == 0 {
		return nil, errors.New("managed PowerDNS requires at least one listen address")
	}
	seen := make(map[string]struct{}, len(addresses))
	for _, address := range addresses {
		parsed := net.ParseIP(address)
		if parsed == nil || parsed.String() != address || !parsed.IsGlobalUnicast() ||
			parsed.IsUnspecified() || parsed.IsLoopback() || parsed.IsLinkLocalUnicast() {
			return nil, errors.New("managed PowerDNS listen address is not canonical global unicast")
		}
		if _, duplicate := seen[address]; duplicate {
			return nil, errors.New("managed PowerDNS listen addresses contain a duplicate")
		}
		seen[address] = struct{}{}
	}
	return []byte(fmt.Sprintf(`# Managed by CelikPanel; do not edit by hand.
launch=gsqlite3
gsqlite3-dnssec=yes
gsqlite3-database=%s
local-address=%s
zone-cache-refresh-interval=0
webserver=no
api=no
`, pdnsDBPath(), strings.Join(addresses, ","))), nil
}

func preparePDNSConfigMutation(
	ctx context.Context,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
	managedConfig []byte,
) (pdnsConfigMutation, error) {
	return prepareOwnerAwarePDNSConfigMutation(ctx, manifest, managedConfig)
}

func verifyStandaloneUnsignedPowerDNS(ctx context.Context) error {
	if _, err := os.Lstat(dnsClusterConf); !errors.Is(err, os.ErrNotExist) {
		if err == nil {
			return errors.New("paired PowerDNS topology must be disabled before switching DNS engines")
		}
		return err
	}
	if err := requireManagedDNSClusterReady(); err != nil {
		return err
	}
	return verifyUnsignedPowerDNSData(ctx)
}

func verifyUnsignedPowerDNSForManifest(
	ctx context.Context,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
	state dnsEngineStateReceipt,
) error {
	if state.Mode == transport.DNSEngineSwitchModeSwitch {
		if err := verifyPDNSReplacementDatabaseEnvelope(ctx, pdnsDBPath()); err != nil {
			return err
		}
		if err := verifyPDNSStateManifestReceipt(ctx, state); err != nil {
			return err
		}
	}
	if manifest.Topology == transport.DNSTopologyStandalone {
		db, err := openPDNSEngineDB(pdnsDBPath(), true)
		if err != nil {
			return err
		}
		defer db.Close()
		tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if err := verifyPDNSSourceProjectionTx(ctx, tx, manifest); err != nil {
			return err
		}
		var supermasters int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM supermasters`).Scan(
			&supermasters,
		); err != nil {
			return err
		}
		if supermasters != 0 {
			return errors.New("standalone PowerDNS source retains autoprimary authority")
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		return verifyStandaloneUnsignedPowerDNS(ctx)
	}
	if manifest.Topology != transport.DNSTopologyPaired ||
		manifest.TargetEngine != transport.DNSEngineBIND {
		return errors.New("PowerDNS source topology is not supported for this switch")
	}
	if isLegacyDNSEngineState(state) {
		expectedConfig, err := dnsClusterConfigForEngineState(manifest, state)
		if err != nil {
			return err
		}
		if err := verifyExactManagedPDNSClusterConfig(expectedConfig); err != nil {
			return fmt.Errorf("verify paired PowerDNS source: %w", err)
		}
		if err := verifyLegacyPDNSSourceRole(ctx, manifest); err != nil {
			return fmt.Errorf("verify paired PowerDNS source role: %w", err)
		}
		return verifyUnsignedPowerDNSData(ctx)
	}
	if state.Mode != transport.DNSEngineSwitchModeSwitch ||
		state.PairRole != manifest.PairRole ||
		state.PairLocalIP != manifest.LocalIP || state.PairPeerIP != manifest.PeerIP {
		return errors.New("directional PowerDNS source state differs from the switch manifest")
	}
	_, primary, err := readManagedPDNSPrimaryCatalogForState(ctx, state)
	if err != nil {
		return err
	}
	if (state.PairRole == transport.DNSPairRolePrimary) != primary {
		return errors.New("directional PowerDNS catalog role differs from its active state")
	}
	db, err := openPDNSEngineDB(pdnsDBPath(), true)
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if state.PairRole == transport.DNSPairRolePrimary {
		if err := verifyPDNSSourceProjectionTx(ctx, tx, manifest); err != nil {
			return err
		}
	}
	var legacyRows int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM supermasters
	`).Scan(&legacyRows); err != nil {
		return err
	}
	if legacyRows != 0 {
		return errors.New("directional PowerDNS source retains legacy supermaster authority")
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return verifyUnsignedPowerDNSData(ctx)
}

func verifyExactManagedPDNSClusterConfig(expected string) error {
	if expected == "" {
		return errors.New("paired PowerDNS source configuration is empty")
	}
	if err := validateDNSClusterConfigTarget(); err != nil {
		return err
	}
	actual, err := dnsClusterConfigReadFile(dnsClusterConf)
	if err != nil || string(actual) != expected {
		if err == nil {
			err = errors.New("PowerDNS pair configuration bytes changed")
		}
		return err
	}
	return requireManagedDNSClusterReady()
}

func verifyPDNSSourceProjectionTx(
	ctx context.Context,
	tx *sql.Tx,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
) error {
	pairedPrimary := manifest.Topology == transport.DNSTopologyPaired &&
		manifest.PairRole == transport.DNSPairRolePrimary
	standalone := manifest.Topology == transport.DNSTopologyStandalone &&
		manifest.PairRole == ""
	if tx == nil || manifest.SourceEngine != transport.DNSEnginePowerDNS ||
		manifest.SourceEpoch < 1 || (!pairedPrimary && !standalone) {
		return errors.New("PowerDNS source projection is invalid")
	}
	catalogDomain := ""
	expectedDomains := 0
	if pairedPrimary {
		var err error
		catalogDomain, err = binddns.CatalogDomain(manifest.LocalIP)
		if err != nil {
			return err
		}
		expectedDomains = 1
	}
	for _, zone := range manifest.Zones {
		commitment, err := mutationpayload.CanonicalDNSZoneSyncV3(
			transport.DNSEnginePowerDNS, manifest.SourceEpoch,
			zone.DesiredGeneration, zone.Domain, zone.Delete, zone.ZoneType, zone.Records,
		)
		if err != nil {
			return err
		}
		receipt, found, err := readPDNSV3ReceiptTx(ctx, tx, zone.Domain)
		if err != nil || !found {
			if err == nil {
				err = errors.New("PowerDNS primary source zone receipt is missing")
			}
			return err
		}
		action := dnsZoneSyncActionSync
		if zone.Delete {
			action = dnsZoneSyncActionDelete
		} else {
			expectedDomains++
		}
		if receipt.EngineEpoch != manifest.SourceEpoch ||
			receipt.Qualifier != commitment.Qualifier ||
			receipt.DesiredGeneration != commitment.DesiredGeneration ||
			receipt.Action != action || receipt.ZoneType != commitment.ZoneType {
			return errors.New("PowerDNS primary source zone receipt differs from the switch manifest")
		}
		if err := verifyPDNSSourceZoneTx(
			ctx, tx, commitment, catalogDomain, pairedPrimary,
		); err != nil {
			return err
		}
	}
	var domainCount, receiptCount, auxiliaryAuthority int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM domains`).Scan(&domainCount); err != nil {
		return err
	}
	if err := tx.QueryRowContext(
		ctx, `SELECT COUNT(*) FROM celikpanel_dns_zone_sync_v3_receipts`,
	).Scan(&receiptCount); err != nil {
		return err
	}
	if err := tx.QueryRowContext(ctx, `
		SELECT
		 (SELECT COUNT(*) FROM comments) +
		 (SELECT COUNT(*) FROM domainmetadata) +
		 (SELECT COUNT(*) FROM cryptokeys) +
		 (SELECT COUNT(*) FROM tsigkeys) +
		 (SELECT COUNT(*) FROM celikpanel_dns_zone_sync_receipts) +
		 (SELECT COUNT(*) FROM records
		   WHERE domain_id IS NULL OR domain_id NOT IN (SELECT id FROM domains))
	`).Scan(&auxiliaryAuthority); err != nil {
		return err
	}
	if domainCount != expectedDomains || receiptCount != len(manifest.Zones) ||
		auxiliaryAuthority != 0 {
		return errors.New("PowerDNS primary source contains authority outside the switch manifest")
	}
	return nil
}

func verifyPDNSSourceZoneTx(
	ctx context.Context,
	tx *sql.Tx,
	commitment mutationpayload.DNSZoneSyncV3Commitment,
	catalogDomain string,
	requireCatalog bool,
) error {
	if commitment.Delete {
		return verifyPDNSV3ZoneTx(ctx, tx, commitment)
	}
	var domainID int64
	var name, zoneType string
	var master, account, options, catalog sql.NullString
	var lastCheck sql.NullInt64
	if err := tx.QueryRowContext(ctx, `
		SELECT id, name, UPPER(type), master, last_check, account, options, catalog
		FROM domains WHERE name = ? COLLATE NOCASE
	`, commitment.Domain).Scan(
		&domainID, &name, &zoneType, &master, &lastCheck, &account, &options, &catalog,
	); err != nil {
		return err
	}
	catalogExact := !catalog.Valid
	if requireCatalog {
		catalogExact = catalog.Valid && catalog.String == catalogDomain
	}
	if name != commitment.Domain || zoneType != commitment.ZoneType ||
		master.Valid || lastCheck.Valid || account.Valid || options.Valid || !catalogExact {
		return errors.New("PowerDNS primary source zone identity is noncanonical")
	}
	var noncanonicalRecords int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM records
		WHERE domain_id = ? AND
		 (ordername IS NOT NULL OR auth IS NULL OR auth != 1)
	`, domainID).Scan(&noncanonicalRecords); err != nil {
		return err
	}
	if noncanonicalRecords != 0 {
		return errors.New("PowerDNS primary source zone record authority is noncanonical")
	}
	return verifyPDNSV3ZoneTx(ctx, tx, commitment)
}

func verifyLegacyPDNSSourceRole(
	ctx context.Context,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
) error {
	if manifest.Topology != transport.DNSTopologyPaired ||
		(manifest.PairRole != transport.DNSPairRolePrimary &&
			manifest.PairRole != transport.DNSPairRoleSecondary) {
		return errors.New("legacy PowerDNS source manifest has no exact pair role")
	}
	if err := requireHostOwnedDNSPairAddress(manifest.LocalIP); err != nil {
		return err
	}
	db, err := openPDNSEngineDB(pdnsDBPath(), true)
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var ownedCatalogs, peerCatalogs int
	if err := tx.QueryRowContext(
		ctx, `SELECT COUNT(*) FROM domains WHERE account = ? AND UPPER(type) = 'PRODUCER'`,
		pdnsBINDCatalogAccount,
	).Scan(&ownedCatalogs); err != nil {
		return err
	}
	if err := tx.QueryRowContext(
		ctx, `SELECT COUNT(*) FROM domains WHERE account = ? AND UPPER(type) = 'CONSUMER'`,
		pdnsPeerCatalogAccount,
	).Scan(&peerCatalogs); err != nil {
		return err
	}
	if ownedCatalogs == 0 && peerCatalogs == 1 {
		if manifest.PairRole != transport.DNSPairRoleSecondary {
			return errors.New("legacy PowerDNS consumer cannot become a primary source")
		}
		peerCatalog, err := readLegacyPDNSPeerCatalogAuthority(ctx, manifest)
		if err != nil {
			return err
		}
		if err := verifyLegacyPDNSConsumerSourceTx(
			ctx, tx, manifest, peerCatalog,
		); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		return verifyLegacyPDNSConsumerMemberAuthority(
			ctx, manifest, peerCatalog.Members,
		)
	}
	if ownedCatalogs != 1 || peerCatalogs != 0 {
		return errors.New("legacy PowerDNS catalog ownership is ambiguous")
	}
	identity, enabled, err := managedPDNSLegacyCatalogIdentity(ctx, pdnsDBPath())
	if err != nil || !enabled {
		if err == nil {
			err = errors.New("legacy PowerDNS producer identity is unavailable")
		}
		return err
	}
	if identity.LocalIP != manifest.LocalIP || identity.PeerIP != manifest.PeerIP {
		return errors.New("legacy PowerDNS producer identity differs from the switch manifest")
	}
	if manifest.PairRole == transport.DNSPairRolePrimary {
		catalog, primary, err := readManagedPDNSPrimaryCatalogWithIdentity(ctx, identity)
		if err != nil || !primary {
			if err == nil {
				err = errors.New("legacy PowerDNS producer evidence is unavailable")
			}
			return err
		}
		if !slices.Equal(catalog.Members, primaryCatalogManifestMembers(manifest)) {
			return errors.New("legacy PowerDNS producer members differ from the switch manifest")
		}
		if err := verifyPDNSSourceProjectionTx(ctx, tx, manifest); err != nil {
			return err
		}
		var totalSupermasters, exactSupermasters int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM supermasters`).Scan(
			&totalSupermasters,
		); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM supermasters
			WHERE account = 'celikpanel' AND ip = ? AND nameserver = ?
		`, manifest.PeerIP, strings.TrimSuffix(manifest.PeerNS, ".")).Scan(
			&exactSupermasters,
		); err != nil {
			return err
		}
		validSupermasters := totalSupermasters == 0 ||
			(totalSupermasters == 1 && exactSupermasters == 1)
		if !validSupermasters {
			return errors.New("legacy PowerDNS producer retains uncommitted authority")
		}
		return tx.Commit()
	}
	// Released paired PowerDNS was symmetric: both nodes owned a producer.
	// Reclassifying a producer as secondary is safe only for the exact empty
	// source used by the reviewed Boston bootstrap. Any local member or other
	// authority would be discarded by the directional consumer target.
	if len(manifest.Zones) != 0 || manifest.SnapshotBytes != 0 {
		return errors.New("legacy PowerDNS secondary transition requires an empty source snapshot")
	}
	var domains, totalSupermasters, exactSupermasters, auxiliaryAuthority int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM domains`).Scan(&domains); err != nil {
		return err
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM supermasters`).Scan(
		&totalSupermasters,
	); err != nil {
		return err
	}
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM supermasters
		WHERE account = 'celikpanel' AND ip = ? AND nameserver = ?
	`, manifest.PeerIP, strings.TrimSuffix(manifest.PeerNS, ".")).Scan(
		&exactSupermasters,
	); err != nil {
		return err
	}
	if err := tx.QueryRowContext(ctx, `
		SELECT
		 (SELECT COUNT(*) FROM comments) +
		 (SELECT COUNT(*) FROM domainmetadata) +
		 (SELECT COUNT(*) FROM cryptokeys) +
		 (SELECT COUNT(*) FROM tsigkeys) +
		 (SELECT COUNT(*) FROM celikpanel_dns_zone_sync_receipts) +
		 (SELECT COUNT(*) FROM celikpanel_dns_zone_sync_v3_receipts) +
		 (SELECT COUNT(*) FROM records
		   WHERE domain_id IS NULL OR domain_id NOT IN
		     (SELECT id FROM domains WHERE account = ?))
	`, pdnsBINDCatalogAccount).Scan(&auxiliaryAuthority); err != nil {
		return err
	}
	if domains != 1 || totalSupermasters != 1 || exactSupermasters != 1 ||
		auxiliaryAuthority != 0 {
		return errors.New("legacy PowerDNS secondary source retains local or ambiguous authority")
	}
	catalog, primary, err := readManagedPDNSPrimaryCatalogWithIdentity(ctx, identity)
	if err != nil || !primary {
		if err == nil {
			err = errors.New("legacy PowerDNS empty producer evidence is unavailable")
		}
		return err
	}
	if len(catalog.Members) != 0 || len(catalog.MemberSerials) != 0 {
		return errors.New("legacy PowerDNS secondary source producer is not empty")
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	peerCatalog, err := readLegacyPDNSPeerCatalogAuthority(ctx, manifest)
	if err != nil {
		return err
	}
	if len(peerCatalog.Members) != 0 {
		return errors.New("legacy PowerDNS peer producer catalog is not empty")
	}
	return nil
}

func verifyLegacyPDNSConsumerSourceTx(
	ctx context.Context,
	tx *sql.Tx,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
	peerCatalog dnsCatalogAXFRResult,
) error {
	if tx == nil || peerCatalog.Serial == 0 {
		return errors.New("legacy PowerDNS consumer source proof is incomplete")
	}
	peerDomain, err := binddns.CatalogDomain(manifest.PeerIP)
	if err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT name, UPPER(type), COALESCE(master,''), COALESCE(account,''),
		       COALESCE(catalog,''), COALESCE(options,'')
		FROM domains ORDER BY name COLLATE BINARY
	`)
	if err != nil {
		return err
	}
	defer rows.Close()
	members := make([]string, 0, len(peerCatalog.Members))
	consumerSeen := 0
	for rows.Next() {
		var name, zoneType, master, account, catalog, options string
		if err := rows.Scan(
			&name, &zoneType, &master, &account, &catalog, &options,
		); err != nil {
			return err
		}
		if name == peerDomain {
			if zoneType != "CONSUMER" || master != manifest.PeerIP ||
				account != pdnsPeerCatalogAccount || catalog != "" || options != "" {
				return errors.New("legacy PowerDNS consumer identity differs from the switch manifest")
			}
			consumerSeen++
			continue
		}
		if (zoneType != "SLAVE" && zoneType != "SECONDARY") ||
			master != manifest.PeerIP || catalog != peerDomain || options != "" ||
			(account != "" && account != pdnsPeerCatalogAccount) {
			return errors.New("legacy PowerDNS consumer contains foreign member authority")
		}
		members = append(members, name)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if consumerSeen != 1 || !slices.Equal(members, peerCatalog.Members) {
		return errors.New("legacy PowerDNS consumer members differ from the peer catalog")
	}
	var supermasters, auxiliaryAuthority int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM supermasters`).Scan(
		&supermasters,
	); err != nil {
		return err
	}
	if err := tx.QueryRowContext(ctx, `
		SELECT
		 (SELECT COUNT(*) FROM comments) +
		 (SELECT COUNT(*) FROM domainmetadata) +
		 (SELECT COUNT(*) FROM cryptokeys) +
		 (SELECT COUNT(*) FROM tsigkeys) +
		 (SELECT COUNT(*) FROM celikpanel_dns_zone_sync_receipts) +
		 (SELECT COUNT(*) FROM celikpanel_dns_zone_sync_v3_receipts) +
		 (SELECT COUNT(*) FROM records
		   WHERE domain_id IS NULL OR domain_id NOT IN (SELECT id FROM domains))
	`).Scan(&auxiliaryAuthority); err != nil {
		return err
	}
	if supermasters != 0 || auxiliaryAuthority != 0 {
		return errors.New("legacy PowerDNS consumer retains extra authority")
	}
	return nil
}

func readLegacyPDNSPeerCatalogAuthority(
	ctx context.Context,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
) (dnsCatalogAXFRResult, error) {
	peerDomain, err := binddns.CatalogDomain(manifest.PeerIP)
	if err != nil {
		return dnsCatalogAXFRResult{}, err
	}
	proofCtx, cancel := context.WithTimeout(ctx, dnsPairProofLimit)
	defer cancel()
	peerCatalog, err := probeDNSBoundCatalogAXFR(
		proofCtx, manifest.LocalIP, manifest.PeerIP, peerDomain,
	)
	if err != nil || peerCatalog.Serial == 0 ||
		!sort.StringsAreSorted(peerCatalog.Members) {
		return dnsCatalogAXFRResult{}, errors.New("legacy PowerDNS peer producer catalog is not exact")
	}
	for index, member := range peerCatalog.Members {
		if !serviceMutationCanonicalFQDN(member) || member == peerDomain ||
			(index > 0 && member == peerCatalog.Members[index-1]) {
			return dnsCatalogAXFRResult{}, errors.New("legacy PowerDNS peer producer members are invalid")
		}
	}
	peerSerial, err := exactDNSZoneSerialAtWithProbe(
		proofCtx, manifest.PeerIP, peerDomain, probeDNSZoneSOA,
	)
	if err != nil || peerSerial != peerCatalog.Serial {
		return dnsCatalogAXFRResult{}, errors.New("legacy PowerDNS peer producer SOA differs from its catalog")
	}
	return peerCatalog, nil
}

func verifyLegacyPDNSConsumerMemberAuthority(
	ctx context.Context,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
	members []string,
) error {
	proofCtx, cancel := context.WithTimeout(ctx, dnsPairProofLimit)
	defer cancel()
	for _, member := range members {
		localSerial, localErr := exactDNSZoneSerialAtWithProbe(
			proofCtx, manifest.LocalIP, member, probeDNSZoneSOA,
		)
		peerSerial, peerErr := exactDNSZoneSerialAtWithProbe(
			proofCtx, manifest.PeerIP, member, probeDNSZoneSOA,
		)
		if localErr != nil || peerErr != nil || localSerial == 0 ||
			localSerial != peerSerial {
			return errors.New("legacy PowerDNS consumer member differs from the peer")
		}
	}
	return nil
}

func verifyUnsignedPowerDNSData(ctx context.Context) error {
	db, err := openPDNSEngineDB(pdnsDBPath(), true)
	if err != nil {
		return err
	}
	defer db.Close()
	var keys, signingMetadata int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM cryptokeys`).Scan(&keys); err != nil {
		return err
	}
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM domainmetadata
		WHERE UPPER(kind) IN ('PRESIGNED', 'NSEC3PARAM', 'NSEC3NARROW')
	`).Scan(&signingMetadata); err != nil {
		return err
	}
	if keys != 0 || signingMetadata != 0 {
		return errors.New("DNSSEC must be disabled for every zone before switching DNS engines")
	}
	return nil
}

func pdnsSwitchCandidatePath(requestID string) string {
	return filepath.Join(filepath.Dir(pdnsDBPath()), ".celikpanel-switch-"+requestID+".sqlite3")
}

func pdnsSwitchBackupPath(requestID string) string {
	return filepath.Join(filepath.Dir(pdnsDBPath()), ".celikpanel-before-switch-"+requestID+".sqlite3")
}

func inspectPDNSDatabaseFile(path string, allowAbsent bool) (bool, int64, string, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) && allowAbsent {
		return false, 0, "", nil
	}
	if err != nil {
		return false, 0, "", err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() <= 0 {
		return false, 0, "", errors.New("PowerDNS database path is not a safe regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return false, 0, "", err
	}
	defer file.Close()
	digest := sha256.New()
	written, err := io.Copy(digest, file)
	if err != nil || written != info.Size() {
		if err == nil {
			err = errors.New("PowerDNS database changed while it was hashed")
		}
		return false, 0, "", err
	}
	return true, written, hex.EncodeToString(digest.Sum(nil)), nil
}

func setPDNSDatabaseOwnership(path string) error {
	account, err := user.Lookup(pdnsUser())
	if err != nil {
		return err
	}
	uid, err := strconv.Atoi(account.Uid)
	if err != nil {
		return err
	}
	gid, err := strconv.Atoi(account.Gid)
	if err != nil {
		return err
	}
	if err := os.Chown(path, uid, gid); err != nil {
		return err
	}
	return os.Chmod(path, 0o640)
}

func activatePDNSCandidate(journal dnsEngineSwitchJournal) error {
	live := filepath.Clean(pdnsDBPath())
	if journal.PDNSCandidatePath != pdnsSwitchCandidatePath(journal.MutationRequestID) ||
		journal.PDNSBackupPath != pdnsSwitchBackupPath(journal.MutationRequestID) {
		return errors.New("PowerDNS switch database paths changed")
	}
	backupExists, _, _, err := inspectPDNSDatabaseFile(journal.PDNSBackupPath, true)
	if err != nil || backupExists {
		if err == nil {
			err = errors.New("PowerDNS switch backup path already exists")
		}
		return err
	}
	liveExists, liveSize, liveHash, err := inspectPDNSDatabaseFile(live, true)
	if err != nil {
		return err
	}
	if liveExists {
		if liveSize != journal.PDNSBackupSize || liveHash != journal.PDNSBackupSHA256 {
			return errors.New("PowerDNS live database changed after switch staging")
		}
		if err := os.Rename(live, journal.PDNSBackupPath); err != nil {
			return err
		}
		if err := syncAtomicParentDirectory(filepath.Dir(live)); err != nil {
			return err
		}
	}
	if err := os.Rename(journal.PDNSCandidatePath, live); err != nil {
		return err
	}
	if err := syncAtomicParentDirectory(filepath.Dir(live)); err != nil {
		return err
	}
	return setPDNSDatabaseOwnership(live)
}

func restorePDNSDatabase(journal dnsEngineSwitchJournal) error {
	live := filepath.Clean(pdnsDBPath())
	liveExists, liveSize, liveHash, err := inspectPDNSDatabaseFile(live, true)
	if err != nil {
		return err
	}
	backupExists, backupSize, backupHash, err := inspectPDNSDatabaseFile(journal.PDNSBackupPath, true)
	if err != nil {
		return err
	}
	if journal.PDNSBackupSHA256 != "" {
		if !backupExists {
			if !liveExists || liveSize != journal.PDNSBackupSize || liveHash != journal.PDNSBackupSHA256 {
				return errors.New("PowerDNS rollback database backup is missing or changed")
			}
			if err := os.Remove(journal.PDNSCandidatePath); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			return syncAtomicParentDirectory(filepath.Dir(live))
		}
		if backupSize != journal.PDNSBackupSize || backupHash != journal.PDNSBackupSHA256 {
			return errors.New("PowerDNS rollback database backup changed")
		}
	} else if backupExists {
		return errors.New("PowerDNS rollback found an unexpected database backup")
	}
	if liveExists {
		manifest, canonicalErr := mutationpayload.CanonicalDNSEngineSwitchManifestWithPairIdentity(
			journal.Mode,
			journal.SourceEngine, journal.TargetEngine, journal.SourceEpoch,
			journal.TargetEpoch, journal.SourceRevision, journal.Topology,
			journal.PairRole, journal.LocalIP, journal.LocalNS,
			journal.PeerIP, journal.PeerNS, journal.Zones,
		)
		binding := transport.ServiceMutationBinding{
			MutationRequestID: journal.MutationRequestID, MutationOwnerID: journal.MutationOwnerID,
		}
		if canonicalErr != nil || verifyPDNSSwitchDatabaseWithPrimaryCatalogSerial(
			context.Background(), live, manifest, binding,
			journal.PrimaryCatalogSerial,
		) != nil {
			return errors.New("PowerDNS rollback live database is not the staged target")
		}
	}
	if err := os.Remove(live); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if journal.PDNSBackupSHA256 != "" {
		if err := os.Rename(journal.PDNSBackupPath, live); err != nil {
			return err
		}
		if err := setPDNSDatabaseOwnership(live); err != nil {
			return err
		}
	}
	if err := os.Remove(journal.PDNSCandidatePath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return syncAtomicParentDirectory(filepath.Dir(live))
}

func stopDNSSourceForPDNSTarget(
	ctx context.Context,
	systemctl string,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
) error {
	switch manifest.SourceEngine {
	case transport.DNSEngineBIND:
		output, err := runDNSSystemctl(ctx, systemctl, "disable", "--now", "named.service")
		if err != nil {
			return fmt.Errorf("stop BIND source: %w: %s", err, firstLine(string(output)))
		}
	case "":
		output, err := runDNSSystemctl(ctx, systemctl, "stop", "pdns.service")
		if err != nil {
			return fmt.Errorf("stop adopted PowerDNS source: %w: %s", err, firstLine(string(output)))
		}
	default:
		return errors.New("PowerDNS target received an unsupported source engine")
	}
	return nil
}

type pdnsTargetActivationOps struct {
	verifySealed   func(context.Context) error
	unmask         func(context.Context) error
	daemonReload   func(context.Context) error
	inspectStopped func(context.Context, ...string) (pdnsInactiveTargetSnapshot, error)
	enable         func(context.Context) error
	start          func(context.Context) error
}

func startPDNSTarget(
	ctx context.Context,
	profile hostplatform.Profile,
	systemctl string,
) error {
	guard := dnsSystemdStateGuard(systemctl)
	return startPDNSTargetWithOps(ctx, pdnsTargetActivationOps{
		verifySealed: func(verifyCtx context.Context) error {
			return verifyPDNSTargetSealedBeforeUnmask(
				verifyCtx, profile, systemctl,
			)
		},
		unmask: func(commandCtx context.Context) error {
			return guard.ensureUnmasked(commandCtx, "pdns.service")
		},
		daemonReload: func(commandCtx context.Context) error {
			reloadCtx, cancel := context.WithTimeout(
				commandCtx, bindDaemonReloadTimeout,
			)
			defer cancel()
			output, err := runServiceMutationCombinedOutput(
				reloadCtx, systemctl, "daemon-reload",
			)
			if err != nil {
				return fmt.Errorf(
					"systemctl daemon-reload: %w: %s",
					err, firstLine(string(output)),
				)
			}
			return nil
		},
		inspectStopped: func(
			verifyCtx context.Context,
			allowed ...string,
		) (pdnsInactiveTargetSnapshot, error) {
			return inspectVerifiedPDNSInactiveTarget(
				verifyCtx, profile, systemctl, allowed...,
			)
		},
		enable: func(commandCtx context.Context) error {
			return enableServiceForMutationWithExecutable(
				commandCtx, systemctl, "pdns.service", false,
			)
		},
		start: func(commandCtx context.Context) error {
			output, commandErr := runServiceMutationCombinedOutput(
				commandCtx, systemctl, "start", "pdns.service",
			)
			verifyErr := verifyServiceMutationUnitWithExecutable(
				commandCtx, systemctl, "pdns.service", true,
			)
			if verifyErr == nil {
				return nil
			}
			if commandErr != nil {
				return fmt.Errorf(
					"systemctl-start-failed:%v:%s; reconciliation: %v",
					commandErr, strings.TrimSpace(string(output)), verifyErr,
				)
			}
			return verifyErr
		},
	})
}

func startPDNSTargetWithOps(
	ctx context.Context,
	ops pdnsTargetActivationOps,
) error {
	if ctx == nil || ops.verifySealed == nil ||
		ops.unmask == nil || ops.daemonReload == nil ||
		ops.inspectStopped == nil || ops.enable == nil || ops.start == nil {
		return errors.New("invalid PowerDNS target activation operation")
	}
	if err := ops.verifySealed(ctx); err != nil {
		return fmt.Errorf(
			"verify sealed PowerDNS vendor unit before unmask: %w", err,
		)
	}
	if err := ops.unmask(ctx); err != nil {
		return fmt.Errorf("unmask PowerDNS target without starting it: %w", err)
	}
	if err := ops.daemonReload(ctx); err != nil {
		return err
	}
	beforeEnable, err := ops.inspectStopped(ctx, "disabled", "enabled")
	if err != nil {
		return fmt.Errorf(
			"verify stopped PowerDNS vendor identity before enabling it: %w", err,
		)
	}
	if beforeEnable.state.unitFileState == "disabled" {
		if err := ops.enable(ctx); err != nil {
			return fmt.Errorf("enable PowerDNS without starting it: %w", err)
		}
		if err := ops.daemonReload(ctx); err != nil {
			return err
		}
	}
	if _, err := ops.inspectStopped(ctx, "enabled"); err != nil {
		return fmt.Errorf(
			"verify stopped PowerDNS vendor identity immediately before start: %w",
			err,
		)
	}
	return ops.start(ctx)
}

func rollbackPDNSSwitch(
	ctx context.Context,
	systemctl string,
	journal dnsEngineSwitchJournal,
	configs pdnsConfigMutation,
) error {
	if ctx == nil {
		return errors.New("rollback PowerDNS switch requires a bounded context")
	}
	return rollbackPDNSSwitchAfterConfigProof(
		func() error {
			_, err := configs.captureOwnerAwareCurrentWithOps(
				ctx, false, hostPDNSConfigAccessOps(),
			)
			return err
		},
		func() error {
			return rollbackPDNSSwitchWithOps(ctx, pdnsSwitchRollbackOps{
				stopTarget: func(commandCtx context.Context) error {
					_, err := runDNSSystemctl(
						commandCtx, systemctl, "stop", "pdns.service",
					)
					return err
				},
				restorePDNSDatabaseSnapshot: func() error {
					return restorePDNSDatabase(journal)
				},
				restoreConfigs: func() error {
					return configs.restoreOwnerAware(ctx)
				},
				restoreState: func() error {
					return restoreDNSFileSnapshot(journal.StateBefore)
				},
				restoreTarget: func(commandCtx context.Context) error {
					return restoreDNSUnitSnapshots(
						commandCtx, systemctl, journal.TargetUnitsBefore,
					)
				},
				restoreSource: func(commandCtx context.Context) error {
					return restoreDNSUnitSnapshots(
						commandCtx, systemctl, journal.SourceUnitsBefore,
					)
				},
			})
		},
	)
}

func rollbackPDNSSwitchAfterConfigProof(
	proveConfigs func() error,
	rollback func() error,
) error {
	if proveConfigs == nil || rollback == nil {
		return errors.New("PowerDNS rollback requires config proof and rollback operations")
	}
	if err := proveConfigs(); err != nil {
		return err
	}
	return rollback()
}

type pdnsSwitchRollbackOps struct {
	stopTarget                  func(context.Context) error
	restorePDNSDatabaseSnapshot func() error
	restoreConfigs              func() error
	restoreState                func() error
	restoreTarget               func(context.Context) error
	restoreSource               func(context.Context) error
}

func rollbackPDNSSwitchWithOps(
	ctx context.Context,
	ops pdnsSwitchRollbackOps,
) error {
	if ctx == nil || ops.stopTarget == nil ||
		ops.restorePDNSDatabaseSnapshot == nil || ops.restoreConfigs == nil ||
		ops.restoreState == nil || ops.restoreTarget == nil ||
		ops.restoreSource == nil {
		return errors.New("invalid PowerDNS switch rollback operations")
	}
	return errors.Join(
		ops.stopTarget(ctx),
		ops.restorePDNSDatabaseSnapshot(),
		ops.restoreConfigs(),
		ops.restoreState(),
		ops.restoreTarget(ctx),
		ops.restoreSource(ctx),
	)
}

func switchToPDNS(
	ctx context.Context,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
	binding transport.ServiceMutationBinding,
) (transport.SwitchDNSEngineV1Response, error) {
	if manifest.Mode == transport.DNSEngineSwitchModeAdopt {
		return adoptPDNS(ctx, manifest, binding)
	}
	if manifest.Mode != transport.DNSEngineSwitchModeSwitch {
		return transport.SwitchDNSEngineV1Response{}, errors.New("PowerDNS engine operation mode is unsupported")
	}
	profile, err := verifiedHostProfileForAnyFamily()
	if err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	return runCertifiedPDNSTargetMutation(profile, func() (transport.SwitchDNSEngineV1Response, error) {
		return switchToPDNSOnCertifiedProfile(ctx, manifest, binding, profile)
	})
}

func switchToPDNSOnCertifiedProfile(
	ctx context.Context,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
	binding transport.ServiceMutationBinding,
	profile hostplatform.Profile,
) (transport.SwitchDNSEngineV1Response, error) {
	systemctl, err := executableForProfile(profile, string(profile.PackageManager), "systemctl")
	if err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	managedConfig, err := managedPowerDNSStandaloneConfig(ctx)
	if err != nil {
		return transport.SwitchDNSEngineV1Response{},
			fmt.Errorf("discover managed PowerDNS listen addresses: %w", err)
	}
	state, stateExists, err := readDNSEngineState()
	if err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	if stateExists && state.Engine == transport.DNSEnginePowerDNS &&
		state.EngineEpoch == manifest.TargetEpoch && state.ManifestQualifier == manifest.Qualifier &&
		state.MutationRequestID == binding.MutationRequestID && state.MutationOwnerID == binding.MutationOwnerID {
		if err := validateEngineStateCatalogContract(manifest, state); err != nil {
			return transport.SwitchDNSEngineV1Response{}, err
		}
		if err := verifyPDNSSwitchDatabaseWithPrimaryCatalogSerial(
			ctx, pdnsDBPath(), manifest, binding, state.PrimaryCatalogSerial,
		); err != nil {
			return transport.SwitchDNSEngineV1Response{}, err
		}
		if err := verifyOnlyPDNSActive(ctx, systemctl); err != nil {
			return transport.SwitchDNSEngineV1Response{}, err
		}
		if err := verifyDNSZoneManifestAuthority(ctx, manifest.Zones); err != nil {
			return transport.SwitchDNSEngineV1Response{}, err
		}
		if err := verifyPDNSPairingAuthority(ctx, manifest); err != nil {
			return transport.SwitchDNSEngineV1Response{}, err
		}
		if err := verifyCompletedPrimaryCatalogTarget(
			ctx, profile, manifest, state,
		); err != nil {
			return transport.SwitchDNSEngineV1Response{}, err
		}
		return transport.SwitchDNSEngineV1Response{Applied: true, ActiveEngine: transport.DNSEnginePowerDNS, ActiveEpoch: manifest.TargetEpoch, AppliedZones: len(manifest.Zones), Detail: "the exact PowerDNS engine switch was already completed and verified"}, nil
	}
	if _, exists, err := readDNSEngineSwitchJournal(); err != nil || exists {
		if err == nil {
			err = errors.New("a DNS engine switch recovery journal requires reconciliation")
		}
		return transport.SwitchDNSEngineV1Response{}, err
	}
	sourceProof, err := proveDNSEngineSwitchSource(
		ctx, profile, manifest, state, stateExists,
	)
	if err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	reconfigureSecondary := sourceProof.PDNSPairSecondaryReconfigure
	primaryCatalogSerial, err := primaryCatalogSerialFromSource(
		ctx, profile, manifest, state, stateExists,
	)
	if err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	if manifest.Topology == transport.DNSTopologyPaired &&
		manifest.PairRole == transport.DNSPairRoleSecondary {
		if _, _, err := peerPDNSCatalog(ctx, manifest); err != nil {
			return transport.SwitchDNSEngineV1Response{}, err
		}
	}
	if manifest.SourceEngine == "" && capturePDNSActive(ctx, systemctl) {
		if err := verifyStandaloneUnsignedPowerDNS(ctx); err != nil {
			return transport.SwitchDNSEngineV1Response{}, err
		}
	}
	service := core.GetManagedServiceByID("pdns")
	if service == nil {
		return transport.SwitchDNSEngineV1Response{}, errors.New("PowerDNS service definition is unavailable")
	}
	packages := service.Packages[string(profile.PackageManager)]
	if len(packages) == 0 {
		return transport.SwitchDNSEngineV1Response{}, errors.New("PowerDNS packages are unavailable for this host")
	}
	missing := make([]string, 0, len(packages))
	for _, packageName := range packages {
		installed, packageErr := exactDNSEnginePackageInstalled(
			ctx, profile, packageName,
		)
		if packageErr != nil {
			return transport.SwitchDNSEngineV1Response{}, packageErr
		}
		if !installed {
			missing = append(missing, packageName)
		}
	}
	if err := publishDNSEngineSourceOwnership(
		manifest, state, stateExists,
	); err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	if err := validatePDNSSwitchPackagePolicy(sourceProof, len(missing)); err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	if len(missing) != 0 {
		installReceipt, receiptErr := newDNSEngineInstallOwnership(
			transport.DNSEnginePowerDNS, profile.PackageManager,
			packages, missing, manifest, binding,
		)
		if receiptErr != nil {
			return transport.SwitchDNSEngineV1Response{}, receiptErr
		}
		if err := runDNSPort53PreMutationGuard(
			ctx, !stateExists && manifest.SourceEngine == "",
			func() error {
				return installOwnedDNSEnginePackages(installReceipt, func() error {
					_, installErr := installPDNSPackagesWithGuard(ctx, systemctl, func() (string, error) {
						return installPackagesWithCandidateContext(
							ctx, string(profile.PackageManager), missing, "",
						)
					})
					return installErr
				})
			},
		); err != nil {
			return transport.SwitchDNSEngineV1Response{}, err
		}
	}
	configs, err := preparePDNSConfigMutation(ctx, manifest, managedConfig)
	if err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	stateBefore, err := captureDNSFileSnapshot(dnsEngineStatePath(), 0o600, true)
	if err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	targetBefore, err := captureDNSUnitSnapshots(ctx, systemctl, []string{"pdns.service"})
	if err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	sourceUnits := []string{}
	if manifest.SourceEngine == transport.DNSEngineBIND {
		sourceUnits = []string{"bind9.service", "named.service"}
	}
	sourceBefore, err := captureDNSUnitSnapshots(ctx, systemctl, sourceUnits)
	if err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	candidate := pdnsSwitchCandidatePath(binding.MutationRequestID)
	backup := pdnsSwitchBackupPath(binding.MutationRequestID)
	for _, path := range []string{candidate, backup} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			if err == nil {
				err = errors.New("PowerDNS switch staging path already exists")
			}
			return transport.SwitchDNSEngineV1Response{}, err
		}
	}
	liveExists, liveSize, liveHash, err := inspectPDNSDatabaseFile(pdnsDBPath(), true)
	if err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	if reconfigureSecondary && !liveExists {
		return transport.SwitchDNSEngineV1Response{}, errors.New(
			"PowerDNS secondary reconfiguration requires the existing live database",
		)
	}
	journal := dnsEngineSwitchJournal{
		Schema: dnsEngineSwitchJournalSchema, Phase: dnsSwitchPhaseIntent,
		Mode:              manifest.Mode,
		MutationRequestID: binding.MutationRequestID, MutationOwnerID: binding.MutationOwnerID,
		ManifestQualifier: manifest.Qualifier, SourceEngine: manifest.SourceEngine,
		TargetEngine: manifest.TargetEngine, SourceEpoch: manifest.SourceEpoch,
		TargetEpoch: manifest.TargetEpoch, SourceRevision: manifest.SourceRevision,
		Topology: manifest.Topology,
		PairRole: manifest.PairRole, LocalIP: manifest.LocalIP, LocalNS: manifest.LocalNS,
		PeerIP: manifest.PeerIP, PeerNS: manifest.PeerNS,
		PrimaryCatalogSerial: primaryCatalogSerial,
		SnapshotBytes:        manifest.SnapshotBytes, Zones: manifest.Zones,
		StateBefore: stateBefore, ConfigBefore: configs.before,
		TargetUnitsBefore: targetBefore, SourceUnitsBefore: sourceBefore,
		PDNSCandidatePath: candidate, PDNSBackupPath: backup,
	}
	if liveExists {
		journal.PDNSBackupSHA256, journal.PDNSBackupSize = liveHash, liveSize
	}
	writeIntent := func() error {
		actualState, actualExists, err := readDNSEngineState()
		if err != nil {
			return err
		}
		if actualExists != stateExists || (actualExists && actualState != state) {
			return errors.New("DNS source state changed before the switch journal")
		}
		actualSourceProof, err := proveDNSEngineSwitchSource(
			ctx, profile, manifest, actualState, actualExists,
		)
		if err != nil {
			return err
		}
		if err := validatePDNSSwitchSourceProofCAS(
			sourceProof, actualSourceProof,
		); err != nil {
			return err
		}
		actualCatalogSerial, err := primaryCatalogSerialFromSource(
			ctx, profile, manifest, actualState, actualExists,
		)
		if err != nil {
			return err
		}
		if actualCatalogSerial != primaryCatalogSerial {
			return errors.New("primary catalog source changed before the switch journal")
		}
		if reconfigureSecondary {
			exists, size, digest, err := inspectPDNSDatabaseFile(
				pdnsDBPath(), false,
			)
			if err != nil {
				return err
			}
			if !exists || size != liveSize || digest != liveHash {
				return errors.New(
					"PowerDNS database changed before the reconfiguration journal",
				)
			}
		}
		if err := configs.verifyOwnerAwarePreimage(ctx); err != nil {
			return err
		}
		return writeDNSEngineSwitchJournal(journal)
	}
	if len(missing) == 0 {
		if err := runDNSPort53PreMutationGuard(
			ctx,
			!stateExists && manifest.SourceEngine == "" &&
				!reconfigureSecondary,
			writeIntent,
		); err != nil {
			return transport.SwitchDNSEngineV1Response{}, err
		}
	} else if err := writeIntent(); err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	rollback := func(cause error) (transport.SwitchDNSEngineV1Response, error) {
		journal.Phase = dnsSwitchPhaseRollingBack
		journalErr := writeDNSEngineSwitchJournal(journal)
		recoveryCtx, cancel, contextErr := newDNSEngineRollbackContext(ctx)
		rollbackErr := contextErr
		if contextErr == nil {
			defer cancel()
			rollbackErr = rollbackPDNSSwitch(
				recoveryCtx, systemctl, journal, configs,
			)
			if rollbackErr == nil {
				rollbackErr = verifyRestoredDNSSwitchSource(
					recoveryCtx, profile, systemctl, manifest, journal,
				)
			}
		}
		if rollbackErr == nil {
			journalErr = errors.Join(
				journalErr,
				finishDNSSwitchRollbackJournal(
					&journal,
					writeDNSEngineSwitchJournal,
					removeDNSEngineSwitchJournal,
				),
			)
		}
		return transport.SwitchDNSEngineV1Response{}, errors.Join(cause, journalErr, rollbackErr)
	}
	if err := configs.verifyOwnerAwarePreimage(ctx); err != nil {
		return rollback(err)
	}
	if err := buildPDNSSwitchCandidateWithPrimaryCatalogSerial(
		ctx, candidate, manifest, binding, primaryCatalogSerial,
	); err != nil {
		return rollback(err)
	}
	if err := verifyPDNSSwitchDatabaseWithPrimaryCatalogSerial(
		ctx, candidate, manifest, binding, primaryCatalogSerial,
	); err != nil {
		return rollback(err)
	}
	if err := configs.applyOwnerAware(ctx); err != nil {
		return rollback(err)
	}
	effective, detail, err := effectiveManagedPowerDNSConfig()
	if err != nil {
		return rollback(err)
	}
	if !effective {
		return rollback(fmt.Errorf(
			"PowerDNS managed configuration is not effective: %s", detail,
		))
	}
	journal.Phase = dnsSwitchPhaseTargetStaged
	if err := writeDNSEngineSwitchJournal(journal); err != nil {
		return rollback(err)
	}
	if err := stopDNSSourceForPDNSTarget(ctx, systemctl, manifest); err != nil {
		return rollback(err)
	}
	journal.Phase = dnsSwitchPhaseSourceStopped
	if err := writeDNSEngineSwitchJournal(journal); err != nil {
		return rollback(err)
	}
	if err := activatePDNSCandidate(journal); err != nil {
		return rollback(err)
	}
	if err := startPDNSTarget(ctx, profile, systemctl); err != nil {
		return rollback(err)
	}
	if manifest.Topology == transport.DNSTopologyPaired &&
		manifest.PairRole == transport.DNSPairRoleSecondary {
		if err := retrievePDNSPairSecondaryZones(ctx, manifest); err != nil {
			return rollback(err)
		}
	}
	journal.Phase = dnsSwitchPhaseTargetStarted
	if err := writeDNSEngineSwitchJournal(journal); err != nil {
		return rollback(err)
	}
	if err := verifyPDNSSwitchDatabaseWithPrimaryCatalogSerial(
		ctx, pdnsDBPath(), manifest, binding, primaryCatalogSerial,
	); err != nil {
		return rollback(err)
	}
	if err := verifyOnlyPDNSActive(ctx, systemctl); err != nil {
		return rollback(err)
	}
	if err := verifyDNSZoneManifestAuthority(ctx, manifest.Zones); err != nil {
		return rollback(err)
	}
	if err := verifyPDNSPairingAuthority(ctx, manifest); err != nil {
		return rollback(err)
	}
	nextState := dnsEngineStateReceipt{
		Schema: dnsEngineStateSchema, Mode: manifest.Mode,
		Engine:               transport.DNSEnginePowerDNS,
		EngineEpoch:          manifest.TargetEpoch,
		PairRole:             pairRoleForEngineState(manifest),
		PairLocalIP:          manifest.LocalIP,
		PairPeerIP:           manifest.PeerIP,
		PrimaryCatalogSerial: primaryCatalogSerial,
		SourceRevision:       manifest.SourceRevision,
		ManifestQualifier:    manifest.Qualifier, MutationRequestID: binding.MutationRequestID,
		MutationOwnerID: binding.MutationOwnerID,
	}
	if err := verifyCompletedPrimaryCatalogTarget(
		ctx, profile, manifest, nextState,
	); err != nil {
		return rollback(err)
	}
	if err := writeDNSEngineState(nextState); err != nil {
		if actual, exists, readErr := readDNSEngineState(); readErr != nil || !exists || actual != nextState {
			return rollback(errors.Join(err, readErr))
		}
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
		Detail: "PowerDNS is the verified active authoritative DNS engine",
	}, nil
}

func capturePDNSActive(ctx context.Context, systemctl string) bool {
	state, err := dnsSystemdStateGuard(systemctl).inspect(ctx, "pdns.service")
	return err == nil && state.active()
}
