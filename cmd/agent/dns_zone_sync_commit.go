package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

const (
	dnsZoneSyncCommitPhasePrefix = "commit/dns-zone-sync/v1/"
	dnsZoneSyncReceiptTable      = "celikpanel_dns_zone_sync_receipts"
	dnsZoneSyncReceiptSchema     = "dns-zone-sync/v1"

	dnsZoneSyncCommitIntent    = "intent"
	dnsZoneSyncCommitApplied   = "applied"
	dnsZoneSyncCommitPublished = "published"

	dnsZoneSyncActionSync   = "sync"
	dnsZoneSyncActionDelete = "delete"

	dnsZoneSyncPreparationTimeout = 30 * time.Second
	dnsZoneSyncFinalizeTimeout    = 2 * time.Minute
)

type dnsZoneSyncReceipt struct {
	Domain            string
	RequestID         string
	Qualifier         string
	DesiredGeneration int64
	Action            string
	ZoneType          string
	Schema            string
}

type verifiedDNSZoneSyncReceipt struct {
	Receipt    dnsZoneSyncReceipt
	Commitment mutationpayload.DNSZoneSyncCommitment
}

type dnsZoneSyncReceiptResult uint8

const (
	dnsZoneSyncReceiptAbsent dnsZoneSyncReceiptResult = iota
	dnsZoneSyncReceiptAuthorityAbsent
	dnsZoneSyncReceiptPreviousExact
	dnsZoneSyncReceiptExact
)

// dnsZoneSyncAuthorityAmbiguityError means the exact committed SQLite state
// can no longer be proven to be the backend served by running PowerDNS. It is
// not a known finalize failure: unlocking would admit a second mutation while
// publication authority is split, so callers must poison and retain the flock.
type dnsZoneSyncAuthorityAmbiguityError struct{ err error }

func (e *dnsZoneSyncAuthorityAmbiguityError) Error() string { return e.err.Error() }
func (e *dnsZoneSyncAuthorityAmbiguityError) Unwrap() error { return e.err }

func formatDNSZoneSyncCommitPhase(
	state, requestID, domain, qualifier string,
) (string, error) {
	if (state != dnsZoneSyncCommitIntent &&
		state != dnsZoneSyncCommitApplied &&
		state != dnsZoneSyncCommitPublished) ||
		!validMutationIdentity(requestID) ||
		!serviceMutationCanonicalFQDN(domain) ||
		!mutationpayload.ValidDNSZoneSyncQualifier(qualifier) {
		return "", errors.New("invalid DNS zone sync commit phase identity")
	}
	return dnsZoneSyncCommitPhasePrefix + state + "/" + requestID + "/" +
		domain + "/" + qualifier, nil
}

func parseDNSZoneSyncCommitPhase(value string) (
	state, requestID, domain, qualifier string,
	err error,
) {
	if !strings.HasPrefix(value, dnsZoneSyncCommitPhasePrefix) {
		return "", "", "", "", errors.New("not a DNS zone sync commit phase")
	}
	remainder := strings.TrimPrefix(value, dnsZoneSyncCommitPhasePrefix)
	state, remainder, found := strings.Cut(remainder, "/")
	if !found {
		return "", "", "", "", errors.New("invalid DNS zone sync commit phase")
	}
	requestID, remainder, found = strings.Cut(remainder, "/")
	if !found {
		return "", "", "", "", errors.New("invalid DNS zone sync commit phase")
	}
	domain, qualifier, found = strings.Cut(remainder, "/")
	if !found {
		return "", "", "", "", errors.New("invalid DNS zone sync commit phase")
	}
	canonical, formatErr := formatDNSZoneSyncCommitPhase(
		state, requestID, domain, qualifier,
	)
	if formatErr != nil || canonical != value {
		return "", "", "", "", errors.New("invalid DNS zone sync commit phase")
	}
	return state, requestID, domain, qualifier, nil
}

func activeDirectDNSZoneSyncJob(job *ServiceMutationJob) bool {
	return job != nil && serviceMutationStatusActive(job.Status) &&
		job.Kind == "dns_zone_sync" &&
		serviceMutationCanonicalFQDN(job.Target)
}

func validateDNSZoneSyncReceipt(receipt dnsZoneSyncReceipt) error {
	if !serviceMutationCanonicalFQDN(receipt.Domain) ||
		!validMutationIdentity(receipt.RequestID) ||
		!mutationpayload.ValidDNSZoneSyncQualifier(receipt.Qualifier) ||
		receipt.DesiredGeneration < 0 ||
		(receipt.Action != dnsZoneSyncActionSync &&
			receipt.Action != dnsZoneSyncActionDelete) ||
		(receipt.ZoneType != "NATIVE" && receipt.ZoneType != "MASTER") ||
		receipt.Schema != dnsZoneSyncReceiptSchema {
		return errors.New("PowerDNS zone receipt identity is invalid")
	}
	return nil
}

func openRawDNSZoneReceiptDB() (*sql.DB, error) {
	path := filepath.Clean(pdnsDBPath())
	if !filepath.IsAbs(path) {
		return nil, errors.New("PowerDNS database path must be absolute")
	}
	db, err := sql.Open(
		"sqlite",
		"file:"+filepath.ToSlash(path)+"?mode=ro&_busy_timeout=5000",
	)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

// prevalidateExistingDNSZoneSyncReceiptSchema inspects an already-present
// receipt authority before openPdnsDB is allowed to execute any CREATE IF NOT
// EXISTS statements. A genuinely absent database or absent receipt table is a
// bootstrap case; an existing object with this authority name must already be
// the exact strict table or the mutation fails with zero database changes.
func prevalidateExistingDNSZoneSyncReceiptSchema(ctx context.Context) error {
	path := filepath.Clean(pdnsDBPath())
	if !filepath.IsAbs(path) {
		return errors.New("PowerDNS database path must be absolute")
	}
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect existing PowerDNS database: %w", err)
	}
	if !info.Mode().IsRegular() {
		return errors.New("PowerDNS database path is not a regular file")
	}
	db, err := openRawDNSZoneReceiptDB()
	if err != nil {
		return err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `
		SELECT name, type FROM sqlite_master
		WHERE name = ? COLLATE NOCASE
	`, dnsZoneSyncReceiptTable)
	if err != nil {
		return fmt.Errorf("inspect existing PowerDNS receipt authority: %w", err)
	}
	defer rows.Close()
	var objectName, objectType string
	count := 0
	for rows.Next() {
		count++
		if err := rows.Scan(&objectName, &objectType); err != nil {
			return fmt.Errorf("read existing PowerDNS receipt authority: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("inspect existing PowerDNS receipt authority rows: %w", err)
	}
	if count == 0 {
		return nil
	}
	if count != 1 || objectName != dnsZoneSyncReceiptTable || objectType != "table" {
		return errors.New("PowerDNS zone receipt authority is not a canonical table")
	}
	return validateDNSZoneSyncReceiptSchema(ctx, db)
}

type dnsZoneReceiptColumn struct {
	name     string
	typeName string
	notNull  int
	primary  int
}

func validateDNSZoneSyncReceiptSchema(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return errors.New("PowerDNS receipt database is required")
	}
	var storedDDL string
	if err := db.QueryRowContext(ctx, `
		SELECT sql FROM sqlite_master
		WHERE type = 'table' AND name = ? COLLATE BINARY
	`, dnsZoneSyncReceiptTable).Scan(&storedDDL); err != nil {
		return fmt.Errorf("read PowerDNS receipt table definition: %w", err)
	}
	normalizedDDL := strings.Join(strings.Fields(storedDDL), " ")
	normalizedDDL = strings.Replace(
		normalizedDDL,
		"CREATE TABLE IF NOT EXISTS ",
		"CREATE TABLE ",
		1,
	)
	const canonicalDDL = "CREATE TABLE celikpanel_dns_zone_sync_receipts ( domain TEXT NOT NULL PRIMARY KEY, request_id TEXT NOT NULL, qualifier TEXT NOT NULL, desired_generation INTEGER NOT NULL, action TEXT NOT NULL, zone_type TEXT NOT NULL, schema TEXT NOT NULL ) STRICT, WITHOUT ROWID"
	if normalizedDDL != canonicalDDL {
		return errors.New("PowerDNS zone receipt table definition is noncanonical")
	}
	rows, err := db.QueryContext(
		ctx,
		"PRAGMA table_xinfo('"+dnsZoneSyncReceiptTable+"')",
	)
	if err != nil {
		return fmt.Errorf("inspect PowerDNS receipt columns: %w", err)
	}
	defer rows.Close()
	want := []dnsZoneReceiptColumn{
		{name: "domain", typeName: "TEXT", notNull: 1, primary: 1},
		{name: "request_id", typeName: "TEXT", notNull: 1},
		{name: "qualifier", typeName: "TEXT", notNull: 1},
		{name: "desired_generation", typeName: "INTEGER", notNull: 1},
		{name: "action", typeName: "TEXT", notNull: 1},
		{name: "zone_type", typeName: "TEXT", notNull: 1},
		{name: "schema", typeName: "TEXT", notNull: 1},
	}
	index := 0
	for rows.Next() {
		var (
			cid, notNull, primary, hidden int
			name, typeName                string
			defaultValue                  sql.NullString
		)
		if err := rows.Scan(
			&cid, &name, &typeName, &notNull, &defaultValue, &primary, &hidden,
		); err != nil {
			return fmt.Errorf("read PowerDNS receipt columns: %w", err)
		}
		if index >= len(want) || cid != index || hidden != 0 ||
			defaultValue.Valid || name != want[index].name ||
			typeName != want[index].typeName ||
			notNull != want[index].notNull ||
			primary != want[index].primary {
			return errors.New("PowerDNS zone receipt table has an unsafe schema")
		}
		index++
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read PowerDNS receipt columns: %w", err)
	}
	if index != len(want) {
		return errors.New("PowerDNS zone receipt table is missing or incomplete")
	}

	tableRows, err := db.QueryContext(ctx, "PRAGMA table_list")
	if err != nil {
		return fmt.Errorf("inspect PowerDNS receipt table flags: %w", err)
	}
	defer tableRows.Close()
	found := false
	for tableRows.Next() {
		var schema, name, tableType string
		var columns, withoutRowID, strict int
		if err := tableRows.Scan(
			&schema, &name, &tableType, &columns, &withoutRowID, &strict,
		); err != nil {
			return fmt.Errorf("read PowerDNS receipt table flags: %w", err)
		}
		if schema != "main" || name != dnsZoneSyncReceiptTable {
			continue
		}
		if found || tableType != "table" || columns != len(want) ||
			withoutRowID != 1 || strict != 1 {
			return errors.New("PowerDNS zone receipt table flags are unsafe")
		}
		found = true
	}
	if err := tableRows.Err(); err != nil {
		return fmt.Errorf("read PowerDNS receipt table flags: %w", err)
	}
	if !found {
		return errors.New("PowerDNS zone receipt table is missing")
	}
	// WITHOUT ROWID primary keys still inherit declared column collation. The
	// receipt identity is byte-canonical, so the PK must use SQLite's BINARY
	// collation and no alternate index/constraint may redefine conflict rules.
	indexRows, err := db.QueryContext(ctx, "PRAGMA index_list('"+dnsZoneSyncReceiptTable+"')")
	if err != nil {
		return fmt.Errorf("inspect PowerDNS receipt indexes: %w", err)
	}
	indexCount := 0
	primaryIndex := ""
	for indexRows.Next() {
		var seq, unique, partial int
		var name, origin string
		if err := indexRows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
			indexRows.Close()
			return fmt.Errorf("read PowerDNS receipt index: %w", err)
		}
		indexCount++
		if seq != 0 || unique != 1 || origin != "pk" || partial != 0 {
			indexRows.Close()
			return errors.New("PowerDNS zone receipt table has a noncanonical index")
		}
		primaryIndex = name
	}
	if err := indexRows.Err(); err != nil {
		indexRows.Close()
		return err
	}
	if err := indexRows.Close(); err != nil {
		return err
	}
	if indexCount != 1 || primaryIndex == "" {
		return errors.New("PowerDNS zone receipt table has an invalid primary index")
	}
	xinfoRows, err := db.QueryContext(ctx, "PRAGMA index_xinfo('"+primaryIndex+"')")
	if err != nil {
		return err
	}
	indexedColumns := 0
	for xinfoRows.Next() {
		var seqno, cid, desc, key int
		var name, coll sql.NullString
		if err := xinfoRows.Scan(&seqno, &cid, &name, &desc, &coll, &key); err != nil {
			xinfoRows.Close()
			return err
		}
		if key == 0 {
			continue
		}
		indexedColumns++
		if seqno != 0 || cid != 0 || !name.Valid || name.String != "domain" ||
			desc != 0 || !coll.Valid || coll.String != "BINARY" {
			xinfoRows.Close()
			return errors.New("PowerDNS zone receipt primary key is noncanonical")
		}
	}
	if err := xinfoRows.Err(); err != nil {
		xinfoRows.Close()
		return err
	}
	if err := xinfoRows.Close(); err != nil {
		return err
	}
	if indexedColumns != 1 {
		return errors.New("PowerDNS zone receipt primary key has an invalid shape")
	}
	var foreignKeys, triggers int
	if err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM pragma_foreign_key_list('"+dnsZoneSyncReceiptTable+"')",
	).Scan(&foreignKeys); err != nil {
		return err
	}
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM sqlite_master
		WHERE type = 'trigger' AND tbl_name = ? COLLATE NOCASE
	`, dnsZoneSyncReceiptTable).Scan(&triggers); err != nil {
		return err
	}
	if foreignKeys != 0 || triggers != 0 {
		return errors.New("PowerDNS zone receipt table has unsafe side effects")
	}
	return nil
}

func readDNSZoneSyncReceipt(
	ctx context.Context,
	db *sql.DB,
	domain string,
) (*dnsZoneSyncReceipt, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT domain, request_id, qualifier, desired_generation,
		       action, zone_type, schema
		FROM celikpanel_dns_zone_sync_receipts
		WHERE domain = ? COLLATE NOCASE
	`, domain)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var receipt *dnsZoneSyncReceipt
	for rows.Next() {
		if receipt != nil {
			return nil, errors.New("PowerDNS has ambiguous zone receipt identities")
		}
		current := dnsZoneSyncReceipt{}
		if err := rows.Scan(
			&current.Domain,
			&current.RequestID,
			&current.Qualifier,
			&current.DesiredGeneration,
			&current.Action,
			&current.ZoneType,
			&current.Schema,
		); err != nil {
			return nil, err
		}
		receipt = &current
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if receipt != nil && receipt.Domain != domain {
		return nil, errors.New("PowerDNS zone receipt domain is noncanonical")
	}
	return receipt, nil
}

func equalDNSZoneRecords(left, right []transport.ZoneRecord) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func verifiedDNSZoneCommitment(
	ctx context.Context,
	db *sql.DB,
	receipt dnsZoneSyncReceipt,
) (mutationpayload.DNSZoneSyncCommitment, error) {
	if err := validateDNSZoneSyncReceipt(receipt); err != nil {
		return mutationpayload.DNSZoneSyncCommitment{}, err
	}
	rows, err := db.QueryContext(ctx, `
		SELECT id, name, type, master
		FROM domains
		WHERE name = ? COLLATE NOCASE
	`, receipt.Domain)
	if err != nil {
		return mutationpayload.DNSZoneSyncCommitment{}, err
	}
	defer rows.Close()
	var (
		zoneFound  bool
		zoneID     int64
		zoneName   string
		zoneType   string
		zoneMaster sql.NullString
	)
	for rows.Next() {
		if zoneFound {
			return mutationpayload.DNSZoneSyncCommitment{},
				errors.New("PowerDNS contains ambiguous zone identities")
		}
		if err := rows.Scan(
			&zoneID, &zoneName, &zoneType, &zoneMaster,
		); err != nil {
			return mutationpayload.DNSZoneSyncCommitment{}, err
		}
		zoneFound = true
	}
	if err := rows.Err(); err != nil {
		return mutationpayload.DNSZoneSyncCommitment{}, err
	}

	deleteZone := receipt.Action == dnsZoneSyncActionDelete
	if deleteZone {
		if zoneFound {
			return mutationpayload.DNSZoneSyncCommitment{},
				errors.New("PowerDNS deletion receipt conflicts with a live zone")
		}
		commitment, err := mutationpayload.CanonicalDNSZoneSync(
			receipt.DesiredGeneration,
			receipt.Domain,
			true,
			receipt.ZoneType,
			nil,
		)
		if err != nil || commitment.Qualifier != receipt.Qualifier {
			return mutationpayload.DNSZoneSyncCommitment{},
				errors.New("PowerDNS deletion receipt does not bind its generation")
		}
		return commitment, nil
	}
	if !zoneFound || zoneName != receipt.Domain ||
		zoneType != receipt.ZoneType || zoneMaster.Valid {
		return mutationpayload.DNSZoneSyncCommitment{},
			errors.New("PowerDNS zone receipt conflicts with zone identity")
	}

	recordRows, err := db.QueryContext(ctx, `
		SELECT name, type, content, ttl, prio, disabled
		FROM records
		WHERE domain_id = ?
	`, zoneID)
	if err != nil {
		return mutationpayload.DNSZoneSyncCommitment{}, err
	}
	defer recordRows.Close()
	records := make([]transport.ZoneRecord, 0)
	for recordRows.Next() {
		var (
			name, recordType, content sql.NullString
			ttl, priority, disabled   sql.NullInt64
		)
		if err := recordRows.Scan(
			&name, &recordType, &content, &ttl, &priority, &disabled,
		); err != nil {
			return mutationpayload.DNSZoneSyncCommitment{}, err
		}
		if !name.Valid || !recordType.Valid || !content.Valid ||
			!ttl.Valid || !priority.Valid || !disabled.Valid ||
			ttl.Int64 < 0 || ttl.Int64 > 1<<31-1 ||
			priority.Int64 < 0 || priority.Int64 > 65535 ||
			(disabled.Int64 != 0 && disabled.Int64 != 1) {
			return mutationpayload.DNSZoneSyncCommitment{},
				errors.New("PowerDNS zone contains a noncanonical record")
		}
		records = append(records, transport.ZoneRecord{
			Name: name.String, Type: recordType.String, Content: content.String,
			TTL: int(ttl.Int64), Prio: int(priority.Int64),
			Disabled: disabled.Int64 == 1,
		})
	}
	if err := recordRows.Err(); err != nil {
		return mutationpayload.DNSZoneSyncCommitment{}, err
	}
	commitment, err := mutationpayload.CanonicalDNSZoneSync(
		receipt.DesiredGeneration,
		receipt.Domain,
		false,
		receipt.ZoneType,
		records,
	)
	if err != nil || commitment.Qualifier != receipt.Qualifier {
		return mutationpayload.DNSZoneSyncCommitment{},
			errors.New("PowerDNS zone contents do not match the durable receipt")
	}
	return commitment, nil
}

func inspectDNSZoneSyncReceipt(
	ctx context.Context,
	requestID, domain, qualifier string,
) (
	dnsZoneSyncReceiptResult,
	*verifiedDNSZoneSyncReceipt,
	error,
) {
	if !validMutationIdentity(requestID) ||
		!serviceMutationCanonicalFQDN(domain) ||
		!mutationpayload.ValidDNSZoneSyncQualifier(qualifier) {
		return dnsZoneSyncReceiptAbsent, nil,
			errors.New("invalid DNS zone receipt lookup identity")
	}
	path := filepath.Clean(pdnsDBPath())
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return dnsZoneSyncReceiptAuthorityAbsent, nil, nil
	} else if err != nil {
		return dnsZoneSyncReceiptAbsent, nil, err
	}
	db, err := openRawDNSZoneReceiptDB()
	if err != nil {
		return dnsZoneSyncReceiptAbsent, nil, err
	}
	defer db.Close()
	var authorityCount int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM sqlite_master
		WHERE name = ? COLLATE NOCASE
	`, dnsZoneSyncReceiptTable).Scan(&authorityCount); err != nil {
		return dnsZoneSyncReceiptAbsent, nil, err
	}
	if authorityCount == 0 {
		return dnsZoneSyncReceiptAuthorityAbsent, nil, nil
	}
	if authorityCount != 1 {
		return dnsZoneSyncReceiptAbsent, nil, errors.New("PowerDNS receipt authority is ambiguous")
	}
	if err := validateDNSZoneSyncReceiptSchema(ctx, db); err != nil {
		return dnsZoneSyncReceiptAbsent, nil, err
	}
	receipt, err := readDNSZoneSyncReceipt(ctx, db, domain)
	if err != nil {
		return dnsZoneSyncReceiptAbsent, nil, err
	}
	if receipt == nil {
		return dnsZoneSyncReceiptAbsent, nil, nil
	}
	commitment, err := verifiedDNSZoneCommitment(ctx, db, *receipt)
	if err != nil {
		return dnsZoneSyncReceiptAbsent, nil, err
	}
	verified := &verifiedDNSZoneSyncReceipt{
		Receipt: *receipt, Commitment: commitment,
	}
	if receipt.RequestID == requestID && receipt.Qualifier == qualifier {
		return dnsZoneSyncReceiptExact, verified, nil
	}
	// The receipt and its complete host snapshot are exact, but belong to a
	// previous request for this domain. Atomic replacement means this is
	// positive proof that the requested transaction did not land; callers may
	// classify it as precommit only while their phase is empty/intent.
	return dnsZoneSyncReceiptPreviousExact, verified, nil
}

type preparedDNSZoneSync struct {
	db         *sql.DB
	tx         *sql.Tx
	commitment mutationpayload.DNSZoneSyncCommitment
}

func dnsZoneSyncCommitIdentity(
	ctx context.Context,
	commitment mutationpayload.DNSZoneSyncCommitment,
) (string, error) {
	tracker, _ := ctx.Value(
		serviceMutationExecutionTrackerKey{},
	).(*serviceMutationExecutionTracker)
	if tracker == nil || tracker.manager == nil || tracker.runtime == nil {
		return "", errors.New("DNS zone sync requires a durable execution tracker")
	}
	m, runtime := tracker.manager, tracker.runtime
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.healthErrorLocked(); err != nil {
		return "", err
	}
	job := runtime.job
	if m.active != runtime || runtime.steps != 1 || job == nil ||
		job.Status != serviceMutationStatusRunning ||
		job.WorkerPID != 0 ||
		job.Kind != "dns_zone_sync" ||
		job.Target != commitment.Domain ||
		job.PackageName != commitment.Qualifier {
		return "", errors.New("DNS zone sync lost its exact direct mutation identity")
	}
	return job.RequestID, nil
}

func (prepared *preparedDNSZoneSync) close() error {
	if prepared == nil {
		return nil
	}
	var rollbackErr, closeErr error
	if prepared.tx != nil {
		rollbackErr = prepared.tx.Rollback()
		if errors.Is(rollbackErr, sql.ErrTxDone) {
			rollbackErr = nil
		}
	}
	if prepared.db != nil {
		closeErr = prepared.db.Close()
	}
	return errors.Join(rollbackErr, closeErr)
}

func prepareDNSZoneSync(
	ctx context.Context,
	requestID string,
	commitment mutationpayload.DNSZoneSyncCommitment,
) (*preparedDNSZoneSync, error) {
	if !validMutationIdentity(requestID) {
		return nil, errors.New("invalid DNS zone sync request identity")
	}
	canonical, err := mutationpayload.CanonicalDNSZoneSync(
		commitment.DesiredGeneration,
		commitment.Domain,
		commitment.Delete,
		commitment.ZoneType,
		commitment.Records,
	)
	if err != nil || canonical.Qualifier != commitment.Qualifier ||
		!equalDNSZoneRecords(canonical.Records, commitment.Records) {
		return nil, errors.New("DNS zone sync commitment is not canonical")
	}
	if err := prevalidateExistingDNSZoneSyncReceiptSchema(ctx); err != nil {
		return nil, fmt.Errorf("validate existing PowerDNS receipt authority: %w", err)
	}
	db, err := openPdnsDB()
	if err != nil {
		return nil, err
	}
	prepared := &preparedDNSZoneSync{db: db, commitment: canonical}
	fail := func(cause error) (*preparedDNSZoneSync, error) {
		return nil, errors.Join(cause, prepared.close())
	}
	// openPdnsDB uses CREATE IF NOT EXISTS, which intentionally leaves an
	// existing table untouched. Reject a malformed/loose receipt table before
	// beginning any zone write so every possible commit has strict recovery
	// authority at its linearization point.
	if err := validateDNSZoneSyncReceiptSchema(ctx, db); err != nil {
		return fail(fmt.Errorf("validate PowerDNS receipt authority: %w", err))
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fail(err)
	}
	prepared.tx = tx

	var zoneID int64
	var existingName, existingType string
	err = tx.QueryRowContext(
		ctx,
		`SELECT id, name, type FROM domains WHERE name = ? COLLATE NOCASE`,
		canonical.Domain,
	).Scan(&zoneID, &existingName, &existingType)
	if err == nil &&
		(strings.EqualFold(existingType, "SLAVE") ||
			strings.EqualFold(existingType, "SECONDARY")) {
		return fail(errors.New("this zone is owned by the peer and is read-only on this server"))
	}
	switch {
	case errors.Is(err, sql.ErrNoRows) && canonical.Delete:
		// An absent domain and its deletion receipt are committed atomically.
	case errors.Is(err, sql.ErrNoRows):
		result, insertErr := tx.ExecContext(
			ctx,
			`INSERT INTO domains (name, type) VALUES (?, ?)`,
			canonical.Domain,
			canonical.ZoneType,
		)
		if insertErr != nil {
			return fail(insertErr)
		}
		zoneID, err = result.LastInsertId()
		if err != nil {
			return fail(fmt.Errorf("read inserted zone identity: %w", err))
		}
	case err != nil:
		return fail(err)
	case canonical.Delete:
		for _, table := range []string{
			"records", "comments", "domainmetadata", "cryptokeys",
		} {
			if _, err := tx.ExecContext(
				ctx, "DELETE FROM "+table+" WHERE domain_id = ?", zoneID,
			); err != nil {
				return fail(err)
			}
		}
		if _, err := tx.ExecContext(
			ctx, `DELETE FROM domains WHERE id = ?`, zoneID,
		); err != nil {
			return fail(err)
		}
	default:
		if existingName == "" {
			return fail(errors.New("PowerDNS zone identity is incomplete"))
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE domains
			SET name = ?, type = ?, master = NULL
			WHERE id = ?
		`, canonical.Domain, canonical.ZoneType, zoneID); err != nil {
			return fail(err)
		}
	}

	if !canonical.Delete {
		if _, err := tx.ExecContext(
			ctx, `DELETE FROM records WHERE domain_id = ?`, zoneID,
		); err != nil {
			return fail(err)
		}
		for _, record := range canonical.Records {
			disabled := 0
			if record.Disabled {
				disabled = 1
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO records
				(domain_id, name, type, content, ttl, prio, disabled, auth)
				VALUES (?, ?, ?, ?, ?, ?, ?, 1)
			`, zoneID, record.Name, record.Type, record.Content,
				record.TTL, record.Prio, disabled); err != nil {
				return fail(fmt.Errorf(
					"insert DNS record %s %s: %w",
					record.Type, record.Name, err,
				))
			}
		}
	}
	action := dnsZoneSyncActionSync
	if canonical.Delete {
		action = dnsZoneSyncActionDelete
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO celikpanel_dns_zone_sync_receipts
		(domain, request_id, qualifier, desired_generation,
		 action, zone_type, schema)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(domain) DO UPDATE SET
		 request_id = excluded.request_id,
		 qualifier = excluded.qualifier,
		 desired_generation = excluded.desired_generation,
		 action = excluded.action,
		 zone_type = excluded.zone_type,
		 schema = excluded.schema
	`, canonical.Domain, requestID, canonical.Qualifier,
		canonical.DesiredGeneration, action, canonical.ZoneType,
		dnsZoneSyncReceiptSchema); err != nil {
		return fail(err)
	}
	return prepared, nil
}

var (
	dnsZoneSyncCommitTransaction = func(tx *sql.Tx) error {
		return tx.Commit()
	}
	dnsZoneSyncReceiptInspector = inspectDNSZoneSyncReceipt
)

func sameDNSZoneSyncCommitment(
	left, right mutationpayload.DNSZoneSyncCommitment,
) bool {
	return left.DesiredGeneration == right.DesiredGeneration &&
		left.Domain == right.Domain &&
		left.Delete == right.Delete &&
		left.ZoneType == right.ZoneType &&
		left.Qualifier == right.Qualifier &&
		equalDNSZoneRecords(left.Records, right.Records)
}

// commitPreparedDNSZoneSync is the cancellation/expiry linearization gate.
// The privileged step already owns runtime.stepMu. The manager mutex orders
// intent publication, the already-prepared SQLite commit and its applied
// receipt against every lifecycle action; the commit callback runs no
// subprocess.
func commitPreparedDNSZoneSync(
	ctx context.Context,
	requestID string,
	prepared *preparedDNSZoneSync,
) (bool, error) {
	tracker, _ := ctx.Value(
		serviceMutationExecutionTrackerKey{},
	).(*serviceMutationExecutionTracker)
	if tracker == nil || tracker.manager == nil || tracker.runtime == nil ||
		prepared == nil || prepared.tx == nil {
		return false, errors.New("DNS zone sync commit requires a prepared durable step")
	}
	m, runtime := tracker.manager, tracker.runtime
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.healthErrorLocked(); err != nil {
		return false, err
	}
	job := runtime.job
	if m.active != runtime || runtime.steps != 1 || job == nil ||
		job.Status != serviceMutationStatusRunning || job.WorkerPID != 0 ||
		job.RequestID != requestID ||
		job.Kind != "dns_zone_sync" ||
		job.Target != prepared.commitment.Domain ||
		job.PackageName != prepared.commitment.Qualifier {
		return false, errors.New("DNS zone sync commit rejected the active identity")
	}
	now := m.now()
	if ctx.Err() != nil || !now.Before(job.LeaseExpiresAt) ||
		!now.Before(job.DeadlineAt) ||
		strings.HasPrefix(job.Phase, dnsZoneSyncCommitPhasePrefix) {
		return false, errors.New("service mutation lease ended before the DNS zone commit")
	}
	intentPhase, err := formatDNSZoneSyncCommitPhase(
		dnsZoneSyncCommitIntent,
		job.RequestID,
		job.Target,
		job.PackageName,
	)
	if err != nil {
		return false, err
	}
	appliedPhase, err := formatDNSZoneSyncCommitPhase(
		dnsZoneSyncCommitApplied,
		job.RequestID,
		job.Target,
		job.PackageName,
	)
	if err != nil {
		return false, err
	}
	before := cloneServiceMutationLedger(m.ledger)
	job.Phase = intentPhase
	job.UpdatedAt = now
	if err := m.persistLedgerMutationLocked(before); err != nil {
		return false, err
	}

	commitErr := dnsZoneSyncCommitTransaction(prepared.tx)
	verifyCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	result, verified, verifyErr := dnsZoneSyncReceiptInspector(
		verifyCtx,
		job.RequestID,
		job.Target,
		job.PackageName,
	)
	cancel()
	if verifyErr != nil {
		return false, m.poisonLocked(fmt.Errorf(
			"verify DNS zone transaction receipt: %w", verifyErr,
		))
	}
	if result != dnsZoneSyncReceiptExact {
		if commitErr != nil {
			if result == dnsZoneSyncReceiptPreviousExact ||
				result == dnsZoneSyncReceiptAbsent {
				return false, commitErr
			}
			return false, m.poisonLocked(fmt.Errorf(
				"DNS zone commit failed with ambiguous receipt authority: %w", commitErr,
			))
		}
		if result == dnsZoneSyncReceiptPreviousExact {
			return false, m.poisonLocked(
				errors.New("PowerDNS transaction returned success but retained a previous receipt"),
			)
		}
		return false, m.poisonLocked(
			errors.New("PowerDNS transaction committed without its exact receipt"),
		)
	}
	if verified == nil ||
		!sameDNSZoneSyncCommitment(verified.Commitment, prepared.commitment) {
		return false, m.poisonLocked(
			errors.New("PowerDNS receipt verified a different DNS snapshot"),
		)
	}

	// From here cancellation may not report failure: the exact zone and
	// receipt are already atomic host authority. Applied is an active-only
	// guard; it is never a success signal.
	runtime.dnsZoneSyncAppliedPhase = appliedPhase
	before = cloneServiceMutationLedger(m.ledger)
	job.Phase = appliedPhase
	job.UpdatedAt = m.now()
	if err := m.persistLedgerMutationLocked(before); err != nil {
		return true, m.poisonLocked(fmt.Errorf(
			"persist applied DNS zone receipt: %w", err,
		))
	}
	return true, nil
}

func runDNSZonePDNSUtil(
	ctx context.Context,
	current, legacy []string,
) ([]byte, error) {
	out, err := dnsSyncCommand(ctx, "pdnsutil", current...)
	if err == nil || len(legacy) == 0 || !pdnsutilSyntaxMismatch(out) {
		return out, err
	}
	return dnsSyncCommand(ctx, "pdnsutil", legacy...)
}

func finalizeDNSZoneSync(
	ctx context.Context,
	commitment mutationpayload.DNSZoneSyncCommitment,
) error {
	if err := requireManagedDNSClusterReady(); err != nil {
		return &dnsZoneSyncAuthorityAmbiguityError{err: fmt.Errorf(
			"PowerDNS effective publication authority changed after the zone commit: %w",
			err,
		)}
	}
	if !commitment.Delete {
		show, err := runDNSZonePDNSUtil(
			ctx,
			[]string{"zone", "show", commitment.Domain},
			[]string{"show-zone", commitment.Domain},
		)
		if err != nil {
			return errors.New(dnssecCommandError(
				"inspect synced zone DNSSEC state", show, err,
			))
		}
		secured := strings.Contains(string(show), "DS = ") ||
			strings.Contains(string(show), "tag = ")
		if secured {
			out, err := runDNSZonePDNSUtil(
				ctx,
				[]string{"zone", "rectify", commitment.Domain},
				[]string{"rectify-zone", commitment.Domain},
			)
			if err != nil {
				return errors.New(dnssecCommandError(
					"rectify synced zone", out, err,
				))
			}
		}
	}

	// Cache invalidation is advisory. MASTER notification is required and is
	// intentionally ordered after signed-zone rectification.
	_, _ = dnsSyncCommand(ctx, "pdns_control", "purge", commitment.Domain+"$")
	if !commitment.Delete && commitment.ZoneType == "MASTER" {
		out, err := dnsSyncCommand(
			ctx, "pdns_control", "notify", commitment.Domain,
		)
		if err != nil {
			return errors.New(dnssecCommandError("notify peer", out, err))
		}
	}
	return nil
}

func failAppliedDNSZoneSync(
	ctx context.Context,
	requestID string,
	commitment mutationpayload.DNSZoneSyncCommitment,
	cause error,
) error {
	tracker, _ := ctx.Value(
		serviceMutationExecutionTrackerKey{},
	).(*serviceMutationExecutionTracker)
	if tracker == nil || tracker.manager == nil || tracker.runtime == nil {
		return cause
	}
	m, runtime := tracker.manager, tracker.runtime
	verifyCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	result, verified, verifyErr := dnsZoneSyncReceiptInspector(
		verifyCtx, requestID, commitment.Domain, commitment.Qualifier,
	)
	cancel()
	if verifyErr != nil || result != dnsZoneSyncReceiptExact ||
		verified == nil ||
		!sameDNSZoneSyncCommitment(verified.Commitment, commitment) {
		if verifyErr == nil {
			verifyErr = errors.New("PowerDNS host state changed after finalization began")
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.active != runtime {
			return errors.New("DNS zone finalize failure lost its applied identity")
		}
		return m.poisonLocked(fmt.Errorf(
			"verify applied DNS zone after finalization failure: %w", verifyErr,
		))
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active != runtime || runtime.dnsZoneSyncAppliedPhase == "" {
		return errors.New("DNS zone finalize failure lost its applied identity")
	}
	var authorityErr *dnsZoneSyncAuthorityAmbiguityError
	if errors.As(cause, &authorityErr) {
		return m.poisonLocked(fmt.Errorf(
			"DNS zone committed while served authority became ambiguous: %w", cause,
		))
	}
	message := "The DNS zone database commit is exact, but PowerDNS finalization failed: " +
		cause.Error()
	if err := m.finishRuntimeTerminalLocked(
		runtime,
		false,
		"dns_zone_finalize_failed",
		"dns_zone_finalize_failed",
		message,
	); err != nil {
		if m.active == runtime {
			return m.poisonLocked(fmt.Errorf(
				"persist DNS zone finalize failure: %w", err,
			))
		}
		return err
	}
	return cause
}

func publishDNSZoneSync(
	ctx context.Context,
	requestID string,
	commitment mutationpayload.DNSZoneSyncCommitment,
) error {
	tracker, _ := ctx.Value(
		serviceMutationExecutionTrackerKey{},
	).(*serviceMutationExecutionTracker)
	if tracker == nil || tracker.manager == nil || tracker.runtime == nil {
		return errors.New("DNS zone publication requires a durable tracker")
	}
	m, runtime := tracker.manager, tracker.runtime
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active != runtime || runtime.job == nil ||
		runtime.dnsZoneSyncAppliedPhase == "" ||
		runtime.job.RequestID != requestID {
		return errors.New("DNS zone publication lost its applied identity")
	}
	verifyCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	result, verified, err := dnsZoneSyncReceiptInspector(
		verifyCtx, requestID, commitment.Domain, commitment.Qualifier,
	)
	cancel()
	if err != nil || result != dnsZoneSyncReceiptExact ||
		verified == nil ||
		!sameDNSZoneSyncCommitment(verified.Commitment, commitment) {
		if err == nil {
			err = errors.New("PowerDNS host state changed after the applied receipt")
		}
		return m.poisonLocked(fmt.Errorf(
			"verify DNS zone before terminal publication: %w", err,
		))
	}
	publishedPhase, err := formatDNSZoneSyncCommitPhase(
		dnsZoneSyncCommitPublished,
		requestID,
		commitment.Domain,
		commitment.Qualifier,
	)
	if err != nil {
		return m.poisonLocked(err)
	}
	runtime.dnsZoneSyncPublishedPhase = publishedPhase
	if err := m.finishRuntimeTerminalLocked(
		runtime, true, publishedPhase, "", "",
	); err != nil {
		if m.active == runtime {
			return m.poisonLocked(fmt.Errorf(
				"persist terminal DNS zone publication: %w", err,
			))
		}
		return err
	}
	return nil
}

func syncDNSZoneV2(
	ctx context.Context,
	commitment mutationpayload.DNSZoneSyncCommitment,
	response *SyncDNSZoneV2Response,
) error {
	requestID, err := dnsZoneSyncCommitIdentity(ctx, commitment)
	if err != nil {
		response.Error = err.Error()
		return nil
	}
	prepared, err := prepareDNSZoneSync(ctx, requestID, commitment)
	if err != nil {
		response.Error = err.Error()
		return nil
	}
	defer prepared.close()
	applied, err := commitPreparedDNSZoneSync(
		ctx, requestID, prepared,
	)
	if err != nil {
		response.Error = err.Error()
		return nil
	}
	if !applied {
		response.Error = "DNS zone transaction was not applied"
		return nil
	}
	finalizeCtx, cancel := context.WithTimeout(
		context.WithoutCancel(ctx),
		dnsZoneSyncFinalizeTimeout,
	)
	defer cancel()
	if err := finalizeDNSZoneSync(finalizeCtx, commitment); err != nil {
		response.Error = failAppliedDNSZoneSync(
			ctx, requestID, commitment, err,
		).Error()
		return nil
	}
	if err := publishDNSZoneSync(
		ctx, requestID, commitment,
	); err != nil {
		response.Error = err.Error()
		return nil
	}
	*response = SyncDNSZoneV2Response{
		Synced: true, AppliedGeneration: commitment.DesiredGeneration,
	}
	return nil
}

func (m *serviceMutationManager) recoverPersistedDNSZoneSyncLocked(
	job *ServiceMutationJob,
	lock *serviceMutationFileLock,
) (bool, error) {
	if job == nil || job.Kind != "dns_zone_sync" {
		return false, nil
	}
	if !serviceMutationCanonicalFQDN(job.Target) {
		m.poisonLock = lock
		return true, m.poisonLocked(
			errors.New("active DNS zone mutation has an invalid target"),
		)
	}
	if job.PackageName == "" {
		writeErr := m.finishPersistedOrphanLocked(
			job,
			"legacy_dns_zone_sync_requires_retry",
			"The legacy DNS zone mutation cannot be recovered safely; retry it with the V2 agent.",
		)
		if m.poisoned != nil {
			m.poisonLock = lock
			return true, writeErr
		}
		return true, errors.Join(writeErr, lock.Close())
	}
	if !mutationpayload.ValidDNSZoneSyncQualifier(job.PackageName) {
		m.poisonLock = lock
		return true, m.poisonLocked(
			errors.New("active DNS zone mutation has an invalid payload qualifier"),
		)
	}

	phaseState := ""
	if strings.HasPrefix(job.Phase, dnsZoneSyncCommitPhasePrefix) {
		state, requestID, domain, qualifier, err :=
			parseDNSZoneSyncCommitPhase(job.Phase)
		if err != nil || requestID != job.RequestID ||
			domain != job.Target || qualifier != job.PackageName {
			m.poisonLock = lock
			return true, m.poisonLocked(
				errors.New("active DNS zone mutation has an invalid commit phase"),
			)
		}
		phaseState = state
	}
	if serviceMutationWorkerMatches(job.WorkerPID, job.WorkerStarted) {
		before := cloneServiceMutationLedger(m.ledger)
		job.Status = serviceMutationStatusOrphaned
		if phaseState == "" {
			job.Phase = "waiting_for_orphaned_process"
		}
		job.ErrorCode = "agent_restart_worker_alive"
		job.ErrorMessage = "The previous DNS zone worker is still alive."
		job.UpdatedAt = m.now()
		writeErr := m.persistLedgerMutationLocked(before)
		if m.poisoned != nil {
			m.poisonLock = lock
			return true, writeErr
		}
		return true, errors.Join(writeErr, lock.Close())
	}
	verifyCtx, verifyCancel := context.WithTimeout(
		context.Background(), 10*time.Second,
	)
	result, verified, verifyErr := dnsZoneSyncReceiptInspector(
		verifyCtx, job.RequestID, job.Target, job.PackageName,
	)
	verifyCancel()
	if verifyErr != nil {
		m.poisonLock = lock
		return true, m.poisonLocked(fmt.Errorf(
			"inspect persisted DNS zone receipt: %w", verifyErr,
		))
	}
	if result == dnsZoneSyncReceiptAuthorityAbsent {
		if phaseState != "" {
			m.poisonLock = lock
			return true, m.poisonLocked(
				errors.New("DNS zone commit phase lost its receipt authority"),
			)
		}
		writeErr := m.finishPersistedOrphanLocked(
			job,
			"agent_restarted_before_dns_zone_prepare",
			"The agent restarted before the DNS zone receipt authority was prepared.",
		)
		if m.poisoned != nil {
			m.poisonLock = lock
			return true, writeErr
		}
		return true, errors.Join(writeErr, lock.Close())
	}
	if result == dnsZoneSyncReceiptPreviousExact {
		if phaseState == dnsZoneSyncCommitApplied ||
			phaseState == dnsZoneSyncCommitPublished {
			m.poisonLock = lock
			return true, m.poisonLocked(
				errors.New("committed DNS zone ledger phase conflicts with the verified previous host receipt"),
			)
		}
		writeErr := m.finishPersistedOrphanLocked(
			job,
			"agent_restarted_before_dns_zone_commit",
			"The agent restarted before the replacement DNS zone transaction committed.",
		)
		if m.poisoned != nil {
			m.poisonLock = lock
			return true, writeErr
		}
		return true, errors.Join(writeErr, lock.Close())
	}
	if result == dnsZoneSyncReceiptAbsent {
		if phaseState == dnsZoneSyncCommitApplied ||
			phaseState == dnsZoneSyncCommitPublished {
			m.poisonLock = lock
			return true, m.poisonLocked(
				errors.New("committed DNS zone ledger phase lost its host receipt"),
			)
		}
		writeErr := m.finishPersistedOrphanLocked(
			job,
			"agent_restarted_before_dns_zone_commit",
			"The agent restarted before the DNS zone transaction committed.",
		)
		if m.poisoned != nil {
			m.poisonLock = lock
			return true, writeErr
		}
		return true, errors.Join(writeErr, lock.Close())
	}
	if verified == nil ||
		verified.Receipt.RequestID != job.RequestID ||
		verified.Receipt.Domain != job.Target ||
		verified.Receipt.Qualifier != job.PackageName {
		m.poisonLock = lock
		return true, m.poisonLocked(
			errors.New("persisted DNS zone receipt identity is ambiguous"),
		)
	}
	appliedPhase, err := formatDNSZoneSyncCommitPhase(
		dnsZoneSyncCommitApplied,
		job.RequestID,
		job.Target,
		job.PackageName,
	)
	if err != nil {
		m.poisonLock = lock
		return true, m.poisonLocked(err)
	}

	recoveryBase, cancel := context.WithTimeout(
		context.Background(), dnsZoneSyncFinalizeTimeout,
	)
	runtime := &serviceMutationRuntime{
		job: job, lock: lock, ctx: recoveryBase, cancel: cancel,
		dnsZoneSyncAppliedPhase: appliedPhase,
	}
	// Normal mutation steps own stepMu before entering manager state.
	m.mu.Unlock()
	runtime.stepMu.Lock()
	m.mu.Lock()
	if m.active != nil || m.ledger.ActiveRequestID != job.RequestID {
		cancel()
		m.poisonLock = lock
		identityErr := m.poisonLocked(
			errors.New("DNS zone recovery identity changed"),
		)
		m.mu.Unlock()
		runtime.stepMu.Unlock()
		m.mu.Lock()
		return true, identityErr
	}
	m.active = runtime
	runtime.steps = 1
	before := cloneServiceMutationLedger(m.ledger)
	runtime.job.Status = serviceMutationStatusCancelling
	runtime.job.Phase = appliedPhase
	runtime.job.ErrorCode = "agent_restart_during_dns_zone_sync"
	runtime.job.ErrorMessage = "The agent is finalizing an exact PowerDNS database commit after restart."
	runtime.job.WorkerPID = 0
	runtime.job.WorkerStarted = ""
	runtime.job.WorkerCommand = ""
	runtime.job.UpdatedAt = m.now()
	if persistErr := m.persistLedgerMutationLocked(before); persistErr != nil {
		poisonErr := m.poisonLocked(fmt.Errorf(
			"persist DNS zone recovery state: %w", persistErr,
		))
		runtime.steps = 0
		m.mu.Unlock()
		runtime.stepMu.Unlock()
		m.mu.Lock()
		return true, poisonErr
	}
	tracker := &serviceMutationExecutionTracker{
		manager: m, runtime: runtime, allowCancellingRecovery: true,
	}
	recoveryCtx := context.WithValue(
		recoveryBase,
		serviceMutationExecutionTrackerKey{},
		tracker,
	)
	commitment := verified.Commitment
	m.mu.Unlock()
	commandErr := finalizeDNSZoneSync(recoveryCtx, commitment)
	finalVerifyCtx, finalVerifyCancel := context.WithTimeout(
		context.Background(), 10*time.Second,
	)
	finalResult, finalVerified, hostVerifyErr :=
		dnsZoneSyncReceiptInspector(
			finalVerifyCtx, job.RequestID, job.Target, job.PackageName,
		)
	finalVerifyCancel()
	if hostVerifyErr == nil &&
		(finalResult != dnsZoneSyncReceiptExact ||
			finalVerified == nil ||
			!sameDNSZoneSyncCommitment(
				finalVerified.Commitment, commitment,
			)) {
		hostVerifyErr = errors.New(
			"PowerDNS host state changed during DNS zone recovery",
		)
	}
	cancel()
	m.mu.Lock()
	runtime.steps = 0
	m.mu.Unlock()
	runtime.stepMu.Unlock()
	m.mu.Lock()
	if hostVerifyErr != nil {
		m.poisonLock = lock
		return true, m.poisonLocked(fmt.Errorf(
			"verify committed DNS zone recovery state: %w", hostVerifyErr,
		))
	}
	if commandErr != nil {
		var authorityErr *dnsZoneSyncAuthorityAmbiguityError
		if errors.As(commandErr, &authorityErr) {
			m.poisonLock = lock
			return true, m.poisonLocked(fmt.Errorf(
				"DNS zone recovery found ambiguous served authority: %w", commandErr,
			))
		}
		if err := m.finishRuntimeTerminalLocked(
			runtime,
			false,
			"dns_zone_finalize_failed",
			"dns_zone_finalize_failed",
			"The exact DNS database commit could not be finalized after agent restart: "+
				commandErr.Error(),
		); err != nil {
			return true, m.poisonLocked(fmt.Errorf(
				"persist recovered DNS finalize failure: %w", err,
			))
		}
		return true, nil
	}
	publishedPhase, err := formatDNSZoneSyncCommitPhase(
		dnsZoneSyncCommitPublished,
		job.RequestID,
		job.Target,
		job.PackageName,
	)
	if err != nil {
		return true, m.poisonLocked(err)
	}
	runtime.dnsZoneSyncPublishedPhase = publishedPhase
	if err := m.finishRuntimeTerminalLocked(
		runtime, true, publishedPhase, "", "",
	); err != nil {
		return true, m.poisonLocked(fmt.Errorf(
			"persist recovered DNS zone publication: %w", err,
		))
	}
	return true, nil
}
