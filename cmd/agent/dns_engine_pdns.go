package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

const (
	pdnsV3ReceiptSchema = "dns-zone-sync/v3"
	pdnsV3ReceiptTable  = "celikpanel_dns_zone_sync_v3_receipts"
	pdnsManifestSchema  = "dns-engine-switch/v1"
)

const pdnsEngineV3Schema = `
CREATE TABLE IF NOT EXISTS celikpanel_dns_zone_sync_v3_receipts (
  domain TEXT NOT NULL PRIMARY KEY,
  engine TEXT NOT NULL CHECK (engine = 'pdns'),
  engine_epoch INTEGER NOT NULL CHECK (engine_epoch > 0),
  request_id TEXT NOT NULL,
  owner_id TEXT NOT NULL,
  qualifier TEXT NOT NULL,
  desired_generation INTEGER NOT NULL CHECK (desired_generation >= 0),
  action TEXT NOT NULL CHECK (action IN ('sync', 'delete')),
  zone_type TEXT NOT NULL CHECK (zone_type IN ('NATIVE', 'MASTER')),
  schema TEXT NOT NULL CHECK (schema = 'dns-zone-sync/v3')
) STRICT, WITHOUT ROWID;

CREATE TABLE IF NOT EXISTS celikpanel_dns_engine_manifest_receipt (
  singleton INTEGER NOT NULL PRIMARY KEY CHECK (singleton = 1),
  engine TEXT NOT NULL CHECK (engine = 'pdns'),
  engine_epoch INTEGER NOT NULL CHECK (engine_epoch > 0),
  request_id TEXT NOT NULL,
  owner_id TEXT NOT NULL,
  qualifier TEXT NOT NULL,
  source_revision INTEGER NOT NULL CHECK (source_revision >= 0),
  zone_count INTEGER NOT NULL CHECK (zone_count >= 0),
  snapshot_bytes INTEGER NOT NULL CHECK (snapshot_bytes >= 0),
  schema TEXT NOT NULL CHECK (schema = 'dns-engine-switch/v1')
) STRICT, WITHOUT ROWID;
`

type pdnsV3ZoneReceipt struct {
	Domain            string
	Engine            string
	EngineEpoch       int64
	RequestID         string
	OwnerID           string
	Qualifier         string
	DesiredGeneration int64
	Action            string
	ZoneType          string
	Schema            string
}

func validatePDNSV3Receipt(receipt pdnsV3ZoneReceipt) error {
	if !serviceMutationCanonicalFQDN(receipt.Domain) ||
		receipt.Engine != string(transport.DNSEnginePowerDNS) || receipt.EngineEpoch < 1 ||
		!validMutationIdentity(receipt.RequestID) || !validMutationIdentity(receipt.OwnerID) ||
		!mutationpayload.ValidDNSZoneSyncV3Qualifier(receipt.Qualifier) ||
		receipt.DesiredGeneration < 0 ||
		(receipt.Action != dnsZoneSyncActionSync && receipt.Action != dnsZoneSyncActionDelete) ||
		(receipt.ZoneType != "NATIVE" && receipt.ZoneType != "MASTER") ||
		receipt.Schema != pdnsV3ReceiptSchema {
		return errors.New("PowerDNS V3 zone receipt identity is invalid")
	}
	return nil
}

func openPDNSEngineDB(path string, readOnly bool) (*sql.DB, error) {
	path = filepath.Clean(path)
	if !filepath.IsAbs(path) {
		return nil, errors.New("PowerDNS engine database path must be absolute")
	}
	query := "?_busy_timeout=5000&_foreign_keys=1"
	if readOnly {
		query = "?mode=ro&_busy_timeout=5000&_foreign_keys=1"
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+query)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

func initializePDNSEngineDB(ctx context.Context, path string) (*sql.DB, error) {
	path = filepath.Clean(path)
	if !filepath.IsAbs(path) {
		return nil, errors.New("PowerDNS engine database path must be absolute")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := file.Close(); err != nil {
		return nil, err
	}
	db, err := openPDNSEngineDB(path, false)
	if err != nil {
		return nil, err
	}
	if _, err := db.ExecContext(ctx, `PRAGMA journal_mode=DELETE; PRAGMA synchronous=FULL;`); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.ExecContext(ctx, pdnsSchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("initialize PowerDNS schema: %w", err)
	}
	if _, err := db.ExecContext(ctx, pdnsEngineV3Schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("initialize PowerDNS engine receipts: %w", err)
	}
	return db, nil
}

func buildPDNSSwitchCandidate(
	ctx context.Context,
	path string,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
	binding transport.ServiceMutationBinding,
) error {
	if requiresPrimaryCatalogSerial(manifest) {
		return errors.New("paired primary PowerDNS candidate requires an explicit catalog serial")
	}
	return buildPDNSSwitchCandidateWithPrimaryCatalogSerial(
		ctx, path, manifest, binding, 0,
	)
}

func buildPDNSSwitchCandidateWithPrimaryCatalogSerial(
	ctx context.Context,
	path string,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
	binding transport.ServiceMutationBinding,
	primaryCatalogSerial uint32,
) error {
	if manifest.Mode != transport.DNSEngineSwitchModeSwitch ||
		manifest.TargetEngine != transport.DNSEnginePowerDNS ||
		!validMutationIdentity(binding.MutationRequestID) ||
		!validMutationIdentity(binding.MutationOwnerID) {
		return errors.New("invalid PowerDNS switch candidate identity")
	}
	if err := validatePrimaryCatalogSerialContract(
		manifest, primaryCatalogSerial,
	); err != nil {
		return err
	}
	db, err := initializePDNSEngineDB(ctx, path)
	if err != nil {
		return err
	}
	closed := false
	defer func() {
		if !closed {
			_ = db.Close()
		}
	}()
	var peerCatalog dnsCatalogAXFRResult
	peerCatalogDomain := ""
	if manifest.Topology == transport.DNSTopologyPaired &&
		manifest.PairRole == transport.DNSPairRoleSecondary {
		peerCatalog, peerCatalogDomain, err = peerPDNSCatalog(ctx, manifest)
		if err != nil {
			return err
		}
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return err
	}
	rollback := true
	defer func() {
		if rollback {
			_ = tx.Rollback()
		}
	}()
	for _, zone := range manifest.Zones {
		commitment, err := mutationpayload.CanonicalDNSZoneSyncV3(
			transport.DNSEnginePowerDNS, manifest.TargetEpoch,
			zone.DesiredGeneration, zone.Domain, zone.Delete, zone.ZoneType, zone.Records,
		)
		if err != nil || commitment.Qualifier != zone.ZoneQualifier {
			return errors.New("PowerDNS switch zone is not the canonical target snapshot")
		}
		if err := applyPDNSV3ZoneTx(ctx, tx, commitment, binding, false); err != nil {
			return err
		}
	}
	if manifest.Topology == transport.DNSTopologyPaired &&
		manifest.PairRole == transport.DNSPairRolePrimary {
		if _, err := reconcilePDNSBINDCatalogWithInitialSerialTx(
			ctx, tx, manifest.LocalIP, primaryCatalogSerial,
		); err != nil {
			return fmt.Errorf("stage PowerDNS pair catalog: %w", err)
		}
	}
	if manifest.Topology == transport.DNSTopologyPaired &&
		manifest.PairRole == transport.DNSPairRoleSecondary {
		if err := stagePDNSPairSecondaryTx(
			ctx, tx, manifest, peerCatalog, peerCatalogDomain,
		); err != nil {
			return fmt.Errorf("stage PowerDNS secondary zones: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO celikpanel_dns_engine_manifest_receipt
		(singleton, engine, engine_epoch, request_id, owner_id, qualifier,
		 source_revision, zone_count, snapshot_bytes, schema)
		VALUES (1, 'pdns', ?, ?, ?, ?, ?, ?, ?, ?)
	`, manifest.TargetEpoch, binding.MutationRequestID, binding.MutationOwnerID,
		manifest.Qualifier, manifest.SourceRevision, len(manifest.Zones),
		manifest.SnapshotBytes, pdnsManifestSchema); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	rollback = false
	var integrity string
	if err := db.QueryRowContext(ctx, `PRAGMA quick_check`).Scan(&integrity); err != nil || integrity != "ok" {
		if err == nil {
			err = errors.New("PowerDNS candidate database failed quick_check")
		}
		return err
	}
	if err := db.Close(); err != nil {
		return err
	}
	closed = true
	if err := syncRegularFile(path); err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		if err := syncAtomicParentDirectory(filepath.Dir(path)); err != nil {
			return err
		}
	}
	return verifyPDNSSwitchDatabaseWithPrimaryCatalogSerial(
		ctx, path, manifest, binding, primaryCatalogSerial,
	)
}

func syncRegularFile(path string) error {
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	err = file.Sync()
	return errors.Join(err, file.Close())
}

func applyPDNSV3ZoneTx(
	ctx context.Context,
	tx *sql.Tx,
	commitment mutationpayload.DNSZoneSyncV3Commitment,
	binding transport.ServiceMutationBinding,
	allowExisting bool,
) error {
	if tx == nil || commitment.Engine != string(transport.DNSEnginePowerDNS) ||
		!validMutationIdentity(binding.MutationRequestID) ||
		!validMutationIdentity(binding.MutationOwnerID) {
		return errors.New("invalid PowerDNS V3 zone transaction identity")
	}
	existing, found, err := readPDNSV3ReceiptTx(ctx, tx, commitment.Domain)
	if err != nil {
		return err
	}
	if found {
		if !allowExisting {
			return errors.New("PowerDNS switch candidate unexpectedly contains a prior zone receipt")
		}
		if existing.DesiredGeneration > commitment.DesiredGeneration {
			return errors.New("PowerDNS V3 zone update is older than the current generation")
		}
		if existing.DesiredGeneration == commitment.DesiredGeneration {
			action := dnsZoneSyncActionSync
			if commitment.Delete {
				action = dnsZoneSyncActionDelete
			}
			if existing.EngineEpoch != commitment.EngineEpoch ||
				existing.RequestID != binding.MutationRequestID ||
				existing.OwnerID != binding.MutationOwnerID ||
				existing.Qualifier != commitment.Qualifier || existing.Action != action ||
				existing.ZoneType != commitment.ZoneType {
				return errors.New("PowerDNS V3 generation was reused with a different binding or snapshot")
			}
			return verifyPDNSV3ZoneTx(ctx, tx, commitment)
		}
	}

	var domainID int64
	var existingType string
	err = tx.QueryRowContext(ctx, `SELECT id, type FROM domains WHERE name = ? COLLATE NOCASE`, commitment.Domain).Scan(&domainID, &existingType)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		domainID = 0
	case err != nil:
		return err
	case strings.EqualFold(existingType, "SLAVE") || strings.EqualFold(existingType, "SECONDARY"):
		return errors.New("PowerDNS V3 cannot replace a peer-owned secondary zone")
	}
	if domainID != 0 {
		tables := []string{"records"}
		if commitment.Delete {
			tables = append(tables, "comments", "domainmetadata", "cryptokeys")
		}
		for _, table := range tables {
			if _, err := tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE domain_id = ?", domainID); err != nil {
				return err
			}
		}
	}
	if commitment.Delete {
		if domainID != 0 {
			if _, err := tx.ExecContext(ctx, `DELETE FROM domains WHERE id = ?`, domainID); err != nil {
				return err
			}
		}
	} else {
		if domainID == 0 {
			result, err := tx.ExecContext(ctx, `INSERT INTO domains (name, type) VALUES (?, ?)`, commitment.Domain, commitment.ZoneType)
			if err != nil {
				return err
			}
			domainID, err = result.LastInsertId()
			if err != nil {
				return err
			}
		} else if _, err := tx.ExecContext(ctx, `
			UPDATE domains SET name = ?, type = ?, master = NULL,
			 last_check = NULL, notified_serial = NULL, account = NULL,
			 options = NULL, catalog = NULL WHERE id = ?
		`, commitment.Domain, commitment.ZoneType, domainID); err != nil {
			return err
		}
		for _, record := range commitment.Records {
			disabled := 0
			if record.Disabled {
				disabled = 1
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO records
				(domain_id, name, type, content, ttl, prio, disabled, auth)
				VALUES (?, ?, ?, ?, ?, ?, ?, 1)
			`, domainID, record.Name, record.Type, record.Content,
				record.TTL, record.Prio, disabled); err != nil {
				return err
			}
		}
	}
	action := dnsZoneSyncActionSync
	if commitment.Delete {
		action = dnsZoneSyncActionDelete
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO celikpanel_dns_zone_sync_v3_receipts
		(domain, engine, engine_epoch, request_id, owner_id, qualifier,
		 desired_generation, action, zone_type, schema)
		VALUES (?, 'pdns', ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(domain) DO UPDATE SET
		 engine = excluded.engine, engine_epoch = excluded.engine_epoch,
		 request_id = excluded.request_id, owner_id = excluded.owner_id,
		 qualifier = excluded.qualifier, desired_generation = excluded.desired_generation,
		 action = excluded.action, zone_type = excluded.zone_type, schema = excluded.schema
	`, commitment.Domain, commitment.EngineEpoch,
		binding.MutationRequestID, binding.MutationOwnerID, commitment.Qualifier,
		commitment.DesiredGeneration, action, commitment.ZoneType, pdnsV3ReceiptSchema); err != nil {
		return err
	}
	return verifyPDNSV3ZoneTx(ctx, tx, commitment)
}

func initializePDNSEngineReceipts(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return errors.New("PowerDNS engine database is required")
	}
	if _, err := db.ExecContext(ctx, pdnsEngineV3Schema); err != nil {
		return fmt.Errorf("initialize PowerDNS engine receipts: %w", err)
	}
	return validatePDNSEngineReceiptSchema(ctx, db)
}

type pdnsReceiptColumn struct {
	name     string
	typeName string
	notNull  int
	pk       int
}

func validatePDNSEngineReceiptSchema(ctx context.Context, db *sql.DB) error {
	want := map[string][]pdnsReceiptColumn{
		pdnsV3ReceiptTable: {
			{name: "domain", typeName: "TEXT", notNull: 1, pk: 1},
			{name: "engine", typeName: "TEXT", notNull: 1},
			{name: "engine_epoch", typeName: "INTEGER", notNull: 1},
			{name: "request_id", typeName: "TEXT", notNull: 1},
			{name: "owner_id", typeName: "TEXT", notNull: 1},
			{name: "qualifier", typeName: "TEXT", notNull: 1},
			{name: "desired_generation", typeName: "INTEGER", notNull: 1},
			{name: "action", typeName: "TEXT", notNull: 1},
			{name: "zone_type", typeName: "TEXT", notNull: 1},
			{name: "schema", typeName: "TEXT", notNull: 1},
		},
		"celikpanel_dns_engine_manifest_receipt": {
			{name: "singleton", typeName: "INTEGER", notNull: 1, pk: 1},
			{name: "engine", typeName: "TEXT", notNull: 1},
			{name: "engine_epoch", typeName: "INTEGER", notNull: 1},
			{name: "request_id", typeName: "TEXT", notNull: 1},
			{name: "owner_id", typeName: "TEXT", notNull: 1},
			{name: "qualifier", typeName: "TEXT", notNull: 1},
			{name: "source_revision", typeName: "INTEGER", notNull: 1},
			{name: "zone_count", typeName: "INTEGER", notNull: 1},
			{name: "snapshot_bytes", typeName: "INTEGER", notNull: 1},
			{name: "schema", typeName: "TEXT", notNull: 1},
		},
	}
	for table, columns := range want {
		var tableType string
		var columnCount, withoutRowID, strict int
		if err := db.QueryRowContext(ctx, `
			SELECT type, ncol, wr, strict FROM pragma_table_list WHERE name = ?
		`, table).Scan(&tableType, &columnCount, &withoutRowID, &strict); err != nil {
			return fmt.Errorf("inspect PowerDNS receipt table %s: %w", table, err)
		}
		if tableType != "table" || columnCount != len(columns) || withoutRowID != 1 || strict != 1 {
			return errors.New("PowerDNS engine receipt table is not strict canonical authority")
		}
		rows, err := db.QueryContext(ctx, "PRAGMA table_xinfo("+table+")")
		if err != nil {
			return err
		}
		index := 0
		for rows.Next() {
			var cid, notNull, pk, hidden int
			var name, typeName string
			var defaultValue any
			if err := rows.Scan(&cid, &name, &typeName, &notNull, &defaultValue, &pk, &hidden); err != nil {
				rows.Close()
				return err
			}
			if index >= len(columns) || cid != index || hidden != 0 || defaultValue != nil ||
				name != columns[index].name || strings.ToUpper(typeName) != columns[index].typeName ||
				notNull != columns[index].notNull || pk != columns[index].pk {
				rows.Close()
				return errors.New("PowerDNS engine receipt table has a noncanonical column layout")
			}
			index++
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if index != len(columns) {
			return errors.New("PowerDNS engine receipt table is incomplete")
		}
		var triggerCount int
		if err := db.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM sqlite_schema WHERE type = 'trigger' AND tbl_name = ?
		`, table).Scan(&triggerCount); err != nil || triggerCount != 0 {
			if err == nil {
				err = errors.New("PowerDNS engine receipt table has unsafe triggers")
			}
			return err
		}
	}
	return nil
}

func applyPDNSV3ZoneDatabase(
	ctx context.Context,
	path string,
	commitment mutationpayload.DNSZoneSyncV3Commitment,
	binding transport.ServiceMutationBinding,
) error {
	return applyPDNSV3ZoneDatabaseForRole(
		ctx, path, commitment, binding, ``,
	)
}

func applyPDNSV3ZoneDatabaseForRole(
	ctx context.Context,
	path string,
	commitment mutationpayload.DNSZoneSyncV3Commitment,
	binding transport.ServiceMutationBinding,
	pairRole string,
) error {
	return applyPDNSV3ZoneDatabaseForState(
		ctx, path, commitment, binding,
		dnsEngineStateReceipt{PairRole: pairRole},
	)
}

func applyPDNSV3ZoneDatabaseForState(
	ctx context.Context,
	path string,
	commitment mutationpayload.DNSZoneSyncV3Commitment,
	binding transport.ServiceMutationBinding,
	state dnsEngineStateReceipt,
) error {
	identity, catalogEnabled, err := resolveManagedPDNSCatalogIdentityForState(
		ctx, state, path,
	)
	if err != nil {
		return err
	}
	db, err := openPDNSEngineDB(path, false)
	if err != nil {
		return err
	}
	defer db.Close()
	if state.Mode == transport.DNSEngineSwitchModeSwitch {
		if err := validatePDNSEngineReceiptSchema(ctx, db); err != nil {
			return err
		}
	} else if err := initializePDNSEngineReceipts(ctx, db); err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if state.Mode == transport.DNSEngineSwitchModeSwitch {
		if err := verifyPDNSStateManifestReceiptTx(ctx, tx, state); err != nil {
			return err
		}
		if state.PairRole == transport.DNSPairRolePrimary {
			serial, err := readExactPDNSProducerSerialTx(
				ctx, tx, state.PairLocalIP,
			)
			if err != nil || serial < state.PrimaryCatalogSerial {
				if err == nil {
					err = errors.New("PowerDNS producer catalog serial predates its active state")
				}
				return err
			}
		}
	}
	// Snapshot before apply: deleting a zone also deletes the row that carries
	// its catalog membership, so post-state alone cannot decide whether the
	// producer SOA serial must advance.
	previousCatalog, err := reconcilePDNSBINDCatalogFromSnapshotTx(
		ctx, tx, catalogEnabled, identity.LocalIP, nil,
	)
	if err != nil {
		return fmt.Errorf("snapshot managed PowerDNS catalog: %w", err)
	}
	if catalogEnabled {
		previousCatalog.PeerIP = identity.PeerIP
	}
	if err := applyPDNSV3ZoneTx(ctx, tx, commitment, binding, true); err != nil {
		return err
	}
	var previous *managedPDNSCatalog
	if catalogEnabled {
		previous = &previousCatalog
	}
	after, err := reconcilePDNSBINDCatalogFromSnapshotTx(
		ctx, tx, catalogEnabled, identity.LocalIP, previous,
	)
	if err != nil {
		return fmt.Errorf("reconcile managed PowerDNS catalog: %w", err)
	}
	if catalogEnabled {
		after.PeerIP = identity.PeerIP
	}
	currentIdentity, currentEnabled, err :=
		resolveManagedPDNSCatalogIdentityForState(ctx, state, path)
	if err != nil {
		return err
	}
	if currentEnabled != catalogEnabled ||
		(currentEnabled &&
			(currentIdentity.Domain != identity.Domain ||
				currentIdentity.LocalIP != identity.LocalIP ||
				currentIdentity.PeerIP != identity.PeerIP)) {
		return errors.New("managed PowerDNS pair identity changed during zone mutation")
	}
	commitErr := tx.Commit()
	committed = commitErr == nil
	verified, verifyErr := verifyPDNSV3ZoneDatabase(
		context.WithoutCancel(ctx), path, commitment, binding,
	)
	if verifyErr != nil {
		return errors.Join(commitErr, verifyErr)
	}
	if !verified {
		if commitErr != nil {
			return commitErr
		}
		return errors.New("PowerDNS V3 transaction committed without its exact receipt")
	}
	return nil
}

func verifyPDNSV3ZoneDatabase(
	ctx context.Context,
	path string,
	commitment mutationpayload.DNSZoneSyncV3Commitment,
	binding transport.ServiceMutationBinding,
) (bool, error) {
	db, err := openPDNSEngineDB(path, true)
	if err != nil {
		return false, err
	}
	defer db.Close()
	if err := validatePDNSEngineReceiptSchema(ctx, db); err != nil {
		return false, err
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	receipt, found, err := readPDNSV3ReceiptTx(ctx, tx, commitment.Domain)
	if err != nil || !found {
		return false, err
	}
	action := dnsZoneSyncActionSync
	if commitment.Delete {
		action = dnsZoneSyncActionDelete
	}
	if receipt.EngineEpoch != commitment.EngineEpoch ||
		receipt.RequestID != binding.MutationRequestID ||
		receipt.OwnerID != binding.MutationOwnerID ||
		receipt.Qualifier != commitment.Qualifier ||
		receipt.DesiredGeneration != commitment.DesiredGeneration ||
		receipt.Action != action || receipt.ZoneType != commitment.ZoneType {
		return false, nil
	}
	if err := verifyPDNSV3ZoneTx(ctx, tx, commitment); err != nil {
		return false, err
	}
	return true, nil
}

func readPDNSV3ReceiptTx(ctx context.Context, tx *sql.Tx, domain string) (pdnsV3ZoneReceipt, bool, error) {
	var receipt pdnsV3ZoneReceipt
	err := tx.QueryRowContext(ctx, `
		SELECT domain, engine, engine_epoch, request_id, owner_id, qualifier,
		 desired_generation, action, zone_type, schema
		FROM celikpanel_dns_zone_sync_v3_receipts WHERE domain = ? COLLATE BINARY
	`, domain).Scan(&receipt.Domain, &receipt.Engine, &receipt.EngineEpoch,
		&receipt.RequestID, &receipt.OwnerID, &receipt.Qualifier,
		&receipt.DesiredGeneration, &receipt.Action, &receipt.ZoneType, &receipt.Schema)
	if errors.Is(err, sql.ErrNoRows) {
		return pdnsV3ZoneReceipt{}, false, nil
	}
	if err != nil {
		return pdnsV3ZoneReceipt{}, false, err
	}
	if err := validatePDNSV3Receipt(receipt); err != nil {
		return pdnsV3ZoneReceipt{}, false, err
	}
	return receipt, true, nil
}

func readPDNSV3ZoneTx(
	ctx context.Context,
	tx *sql.Tx,
	domain string,
) (string, []transport.ZoneRecord, bool, error) {
	var domainID int64
	var name, zoneType string
	err := tx.QueryRowContext(ctx, `
		SELECT id, name, type FROM domains WHERE name = ? COLLATE NOCASE
	`, domain).Scan(&domainID, &name, &zoneType)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil, false, nil
	}
	if err != nil {
		return "", nil, false, err
	}
	if name != domain || (zoneType != "NATIVE" && zoneType != "MASTER") {
		return "", nil, false, errors.New("PowerDNS zone row is not canonical")
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT name, type, content, ttl, prio, disabled
		FROM records WHERE domain_id = ?
		ORDER BY name COLLATE BINARY, type COLLATE BINARY, content COLLATE BINARY,
		 ttl, prio, disabled, id
	`, domainID)
	if err != nil {
		return "", nil, false, err
	}
	defer rows.Close()
	records := make([]transport.ZoneRecord, 0)
	for rows.Next() {
		var record transport.ZoneRecord
		var disabled int
		if err := rows.Scan(&record.Name, &record.Type, &record.Content,
			&record.TTL, &record.Prio, &disabled); err != nil {
			return "", nil, false, err
		}
		if disabled != 0 && disabled != 1 {
			return "", nil, false, errors.New("PowerDNS record has a noncanonical disabled flag")
		}
		record.Disabled = disabled == 1
		records = append(records, record)
	}
	return zoneType, records, true, rows.Err()
}

func verifyPDNSV3ZoneTx(
	ctx context.Context,
	tx *sql.Tx,
	expected mutationpayload.DNSZoneSyncV3Commitment,
) error {
	zoneType, records, found, err := readPDNSV3ZoneTx(ctx, tx, expected.Domain)
	if err != nil {
		return err
	}
	if expected.Delete {
		if found {
			return errors.New("deleted PowerDNS V3 zone still has a domain row")
		}
		return nil
	}
	if !found {
		return errors.New("PowerDNS V3 zone is missing its domain row")
	}
	actual, err := mutationpayload.CanonicalDNSZoneSyncV3(
		transport.DNSEnginePowerDNS, expected.EngineEpoch,
		expected.DesiredGeneration, expected.Domain, false, zoneType, records,
	)
	if err != nil || actual.Qualifier != expected.Qualifier ||
		!reflect.DeepEqual(actual.Records, expected.Records) {
		return errors.New("PowerDNS V3 zone rows differ from the committed snapshot")
	}
	return nil
}

func verifyPDNSSwitchDatabase(
	ctx context.Context,
	path string,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
	binding transport.ServiceMutationBinding,
) error {
	if requiresPrimaryCatalogSerial(manifest) {
		return errors.New("paired primary PowerDNS verification requires an explicit catalog serial")
	}
	return verifyPDNSSwitchDatabaseWithPrimaryCatalogSerial(
		ctx, path, manifest, binding, 0,
	)
}

func verifyPDNSSwitchDatabaseWithPrimaryCatalogSerial(
	ctx context.Context,
	path string,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
	binding transport.ServiceMutationBinding,
	primaryCatalogSerial uint32,
) error {
	if err := validatePrimaryCatalogSerialContract(
		manifest, primaryCatalogSerial,
	); err != nil {
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
	var peerCatalog dnsCatalogAXFRResult
	peerCatalogDomain := ""
	if manifest.Topology == transport.DNSTopologyPaired &&
		manifest.PairRole == transport.DNSPairRoleSecondary {
		peerCatalog, peerCatalogDomain, err = peerPDNSCatalog(ctx, manifest)
		if err != nil {
			return err
		}
	}
	var engine string
	var epoch, sourceRevision, zoneCount, snapshotBytes int64
	var requestID, ownerID, qualifier, schema string
	if err := tx.QueryRowContext(ctx, `
		SELECT engine, engine_epoch, request_id, owner_id, qualifier,
		 source_revision, zone_count, snapshot_bytes, schema
		FROM celikpanel_dns_engine_manifest_receipt WHERE singleton = 1
	`).Scan(&engine, &epoch, &requestID, &ownerID, &qualifier,
		&sourceRevision, &zoneCount, &snapshotBytes, &schema); err != nil {
		return err
	}
	if manifest.Mode != transport.DNSEngineSwitchModeSwitch ||
		engine != string(transport.DNSEnginePowerDNS) || epoch != manifest.TargetEpoch ||
		requestID != binding.MutationRequestID || ownerID != binding.MutationOwnerID ||
		qualifier != manifest.Qualifier || sourceRevision != manifest.SourceRevision ||
		zoneCount != int64(len(manifest.Zones)) || snapshotBytes != manifest.SnapshotBytes ||
		schema != pdnsManifestSchema {
		return errors.New("PowerDNS switch manifest receipt mismatch")
	}
	for _, zone := range manifest.Zones {
		commitment, err := mutationpayload.CanonicalDNSZoneSyncV3(
			transport.DNSEnginePowerDNS, manifest.TargetEpoch,
			zone.DesiredGeneration, zone.Domain, zone.Delete, zone.ZoneType, zone.Records,
		)
		if err != nil {
			return err
		}
		receipt, found, err := readPDNSV3ReceiptTx(ctx, tx, zone.Domain)
		if err != nil || !found {
			if err == nil {
				err = errors.New("PowerDNS switch zone receipt is missing")
			}
			return err
		}
		if receipt.EngineEpoch != manifest.TargetEpoch || receipt.RequestID != binding.MutationRequestID ||
			receipt.OwnerID != binding.MutationOwnerID || receipt.Qualifier != commitment.Qualifier ||
			receipt.DesiredGeneration != commitment.DesiredGeneration {
			return errors.New("PowerDNS switch zone receipt mismatch")
		}
		if err := verifyPDNSV3ZoneTx(ctx, tx, commitment); err != nil {
			return err
		}
	}
	var receiptCount, domainCount int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM celikpanel_dns_zone_sync_v3_receipts`).Scan(&receiptCount); err != nil {
		return err
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM domains`).Scan(&domainCount); err != nil {
		return err
	}
	expectedDomains := 0
	for _, zone := range manifest.Zones {
		if !zone.Delete {
			expectedDomains++
		}
	}
	if manifest.Topology == transport.DNSTopologyPaired &&
		manifest.PairRole == transport.DNSPairRolePrimary {
		expectedMembers := make([]string, 0, len(manifest.Zones))
		for _, zone := range manifest.Zones {
			if !zone.Delete {
				expectedMembers = append(expectedMembers, zone.Domain)
			}
		}
		if err := verifyPDNSProducerMembershipTx(
			ctx, tx, manifest.LocalIP, expectedMembers,
		); err != nil {
			return fmt.Errorf("verify PowerDNS pair catalog: %w", err)
		}
		if err := verifyPDNSProducerSerialTx(
			ctx, tx, manifest.LocalIP, primaryCatalogSerial,
		); err != nil {
			return fmt.Errorf("verify PowerDNS pair catalog serial: %w", err)
		}
		expectedDomains++
	}
	if manifest.Topology == transport.DNSTopologyPaired &&
		manifest.PairRole == transport.DNSPairRoleSecondary {
		catalogMembers, err := verifyPDNSPairSecondaryTx(
			ctx, tx, manifest, peerCatalog, peerCatalogDomain,
		)
		if err != nil {
			return err
		}
		expectedDomains += 1 + catalogMembers
	}
	if receiptCount != len(manifest.Zones) || domainCount != expectedDomains {
		return errors.New("PowerDNS switch database contains unreceipted zones")
	}
	return tx.Commit()
}

func readPDNSV3ZoneSnapshot(
	ctx context.Context,
	path string,
	state dnsEngineStateReceipt,
	domain, qualifier string,
	binding transport.ServiceMutationBinding,
) (transport.DNSEngineSwitchZoneSnapshot, bool, error) {
	db, err := openPDNSEngineDB(path, true)
	if err != nil {
		return transport.DNSEngineSwitchZoneSnapshot{}, false, err
	}
	defer db.Close()
	if err := validatePDNSEngineReceiptSchema(ctx, db); err != nil {
		return transport.DNSEngineSwitchZoneSnapshot{}, false, err
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return transport.DNSEngineSwitchZoneSnapshot{}, false, err
	}
	defer tx.Rollback()
	if state.Mode == transport.DNSEngineSwitchModeSwitch {
		if err := verifyPDNSStateManifestReceiptTx(ctx, tx, state); err != nil {
			return transport.DNSEngineSwitchZoneSnapshot{}, false, err
		}
		if state.PairRole == transport.DNSPairRolePrimary {
			serial, err := readExactPDNSProducerSerialTx(
				ctx, tx, state.PairLocalIP,
			)
			if err != nil || serial < state.PrimaryCatalogSerial {
				if err == nil {
					err = errors.New("PowerDNS producer catalog serial predates its active state")
				}
				return transport.DNSEngineSwitchZoneSnapshot{}, false, err
			}
		}
	}
	receipt, found, err := readPDNSV3ReceiptTx(ctx, tx, domain)
	if err != nil || !found {
		return transport.DNSEngineSwitchZoneSnapshot{}, false, err
	}
	if receipt.EngineEpoch != state.EngineEpoch || receipt.Qualifier != qualifier ||
		receipt.RequestID != binding.MutationRequestID ||
		receipt.OwnerID != binding.MutationOwnerID {
		return transport.DNSEngineSwitchZoneSnapshot{}, false, nil
	}
	zoneType, records, domainFound, err := readPDNSV3ZoneTx(ctx, tx, domain)
	if err != nil {
		return transport.DNSEngineSwitchZoneSnapshot{}, false, err
	}
	deleteZone := receipt.Action == dnsZoneSyncActionDelete
	if deleteZone == domainFound {
		return transport.DNSEngineSwitchZoneSnapshot{}, false, errors.New("PowerDNS V3 receipt and zone rows disagree")
	}
	if deleteZone {
		zoneType = receipt.ZoneType
		records = nil
	}
	commitment, err := mutationpayload.CanonicalDNSZoneSyncV3(
		transport.DNSEnginePowerDNS, receipt.EngineEpoch,
		receipt.DesiredGeneration, domain, deleteZone, zoneType, records,
	)
	if err != nil || commitment.Qualifier != qualifier {
		return transport.DNSEngineSwitchZoneSnapshot{}, false, errors.New("PowerDNS V3 receipt does not reconstruct its commitment")
	}
	return transport.DNSEngineSwitchZoneSnapshot{
		Domain: domain, DesiredGeneration: receipt.DesiredGeneration,
		Delete: deleteZone, ZoneType: zoneType, Records: commitment.Records,
		ZoneQualifier: qualifier,
	}, true, nil
}

func sortedPDNSRecords(records []transport.ZoneRecord) []transport.ZoneRecord {
	cloned := append([]transport.ZoneRecord(nil), records...)
	sort.SliceStable(cloned, func(left, right int) bool {
		return fmt.Sprintf("%s\x00%s\x00%s\x00%010d\x00%05d\x00%t",
			cloned[left].Name, cloned[left].Type, cloned[left].Content,
			cloned[left].TTL, cloned[left].Prio, cloned[left].Disabled) <
			fmt.Sprintf("%s\x00%s\x00%s\x00%010d\x00%05d\x00%t",
				cloned[right].Name, cloned[right].Type, cloned[right].Content,
				cloned[right].TTL, cloned[right].Prio, cloned[right].Disabled)
	})
	return cloned
}
