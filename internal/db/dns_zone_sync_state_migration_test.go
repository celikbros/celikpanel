package db

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestDNSZoneSyncMigrationSeedsOneDeterministicGenerationPerZone(t *testing.T) {
	database := newPreDNSZoneSyncMigrationDB(t)
	for _, zone := range []struct {
		name     string
		zoneType string
		records  int
	}{
		{name: "empty.example", zoneType: "native", records: 0},
		{name: "large.example", zoneType: " MASTER ", records: 3},
	} {
		result, err := database.Exec(
			`INSERT INTO pdns_domains(name, type) VALUES (?, ?)`, zone.name, zone.zoneType,
		)
		if err != nil {
			t.Fatalf("insert pre-032 zone %s: %v", zone.name, err)
		}
		zoneID, err := result.LastInsertId()
		if err != nil {
			t.Fatalf("read pre-032 zone %s id: %v", zone.name, err)
		}
		for index := 0; index < zone.records; index++ {
			if _, err := database.Exec(`
				INSERT INTO pdns_records(domain_id, name, type, content, ttl, prio, disabled)
				VALUES (?, ?, 'A', ?, 300, 0, 0)`,
				zoneID, zone.name, fmt.Sprintf("192.0.2.%d", index+1)); err != nil {
				t.Fatalf("insert pre-032 record %d for %s: %v", index, zone.name, err)
			}
		}
	}

	applyEmbeddedMigrationVersion(t, database, 32)

	rows, err := database.Query(`
		SELECT zone_name, source_domain_id, desired_generation,
		       applied_generation, desired_action, desired_zone_type, status
		FROM dns_zone_sync_state
		ORDER BY zone_name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	wantTypes := map[string]string{
		"empty.example": "NATIVE",
		"large.example": "MASTER",
	}
	seen := 0
	for rows.Next() {
		var name, action, zoneType, status string
		var sourceID, desired, applied int64
		if err := rows.Scan(
			&name, &sourceID, &desired, &applied, &action, &zoneType, &status,
		); err != nil {
			t.Fatal(err)
		}
		if sourceID <= 0 || desired != 1 || applied != 0 ||
			action != "sync" || zoneType != wantTypes[name] || status != "pending" {
			t.Fatalf("seeded state for %s = source:%d desired:%d applied:%d %s/%s/%s",
				name, sourceID, desired, applied, action, zoneType, status)
		}
		seen++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if seen != len(wantTypes) {
		t.Fatalf("seeded states=%d want=%d", seen, len(wantTypes))
	}
	var markers int
	if err := database.QueryRow(`SELECT COUNT(*) FROM dns_zone_deletion_markers`).Scan(&markers); err != nil {
		t.Fatal(err)
	}
	if markers != 0 {
		t.Fatalf("migration invented %d deletion markers", markers)
	}
}

func TestDNSZoneSyncMigrationTracksEveryEffectiveZoneAndRecordMutation(t *testing.T) {
	database := newDNSZoneSyncMigrationDB(t)
	result, err := database.Exec(
		`INSERT INTO pdns_domains(name, type) VALUES ('records.example', 'NATIVE')`,
	)
	if err != nil {
		t.Fatal(err)
	}
	zoneID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	assertDNSZoneSyncState(t, database, "records.example", zoneID, 1, 0, "sync", "NATIVE", "pending")

	for range 2 {
		if _, err := database.Exec(`
			INSERT INTO pdns_records(domain_id, name, type, content, ttl, prio, disabled)
			VALUES (?, 'records.example', 'A', '192.0.2.10', 300, 0, 0)`, zoneID); err != nil {
			t.Fatal(err)
		}
	}
	assertDNSZoneSyncState(t, database, "records.example", zoneID, 3, 0, "sync", "NATIVE", "pending")

	var recordID int64
	if err := database.QueryRow(`
		SELECT MIN(id) FROM pdns_records WHERE domain_id = ?`, zoneID,
	).Scan(&recordID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE pdns_records SET content = content WHERE id = ?`, recordID); err != nil {
		t.Fatal(err)
	}
	assertDNSZoneSyncState(t, database, "records.example", zoneID, 3, 0, "sync", "NATIVE", "pending")

	if _, err := database.Exec(`UPDATE pdns_records SET disabled = 1 WHERE id = ?`, recordID); err != nil {
		t.Fatal(err)
	}
	assertDNSZoneSyncState(t, database, "records.example", zoneID, 4, 0, "sync", "NATIVE", "pending")

	// ordername/auth are agent-derived rather than caller-controlled V2 tuple
	// fields, but changing any pdns_records field still makes the zone pending.
	// The generation commits that event even when the projected tuple is equal.
	if _, err := database.Exec(`UPDATE pdns_records SET auth = 0 WHERE id = ?`, recordID); err != nil {
		t.Fatal(err)
	}
	assertDNSZoneSyncState(t, database, "records.example", zoneID, 5, 0, "sync", "NATIVE", "pending")

	if _, err := database.Exec(`DELETE FROM pdns_records WHERE id = ?`, recordID); err != nil {
		t.Fatal(err)
	}
	assertDNSZoneSyncState(t, database, "records.example", zoneID, 6, 0, "sync", "NATIVE", "pending")

	if _, err := database.Exec(`UPDATE pdns_domains SET master = '192.0.2.53', type = 'MASTER' WHERE id = ?`, zoneID); err != nil {
		t.Fatal(err)
	}
	assertDNSZoneSyncState(t, database, "records.example", zoneID, 7, 0, "sync", "MASTER", "pending")
}

func TestDNSZoneSyncMigrationRequiresAnExactAllOrNothingLease(t *testing.T) {
	database := newDNSZoneSyncMigrationDB(t)
	result, err := database.Exec(
		`INSERT INTO pdns_domains(name, type) VALUES ('lease.example', 'NATIVE')`,
	)
	if err != nil {
		t.Fatal(err)
	}
	zoneID, _ := result.LastInsertId()
	requestID := strings.Repeat("a", 32)
	ownerID := strings.Repeat("b", 32)
	qualifier := "dns-zone-sync/v1:sha256:" + strings.Repeat("c", 64)

	requireDNSZoneSyncSQLFailure(t, database, "partial lease", `
		UPDATE dns_zone_sync_state SET lease_request_id = ?
		WHERE zone_name = 'lease.example'`, requestID)
	requireDNSZoneSyncSQLFailure(t, database, "noncanonical request id", `
		UPDATE dns_zone_sync_state
		SET lease_request_id = ?, lease_owner_id = ?, lease_generation = 1,
		    lease_action = 'sync', lease_zone_type = 'NATIVE',
		    lease_qualifier = ?, lease_expires_at = datetime('now', '+2 minutes')
		WHERE zone_name = 'lease.example'`, strings.Repeat("A", 32), ownerID, qualifier)
	requireDNSZoneSyncSQLFailure(t, database, "future lease generation", `
		UPDATE dns_zone_sync_state
		SET lease_request_id = ?, lease_owner_id = ?, lease_generation = 2,
		    lease_action = 'sync', lease_zone_type = 'NATIVE',
		    lease_qualifier = ?, lease_expires_at = datetime('now', '+2 minutes')
		WHERE zone_name = 'lease.example'`, requestID, ownerID, qualifier)
	requireDNSZoneSyncSQLFailure(t, database, "uppercase qualifier", `
		UPDATE dns_zone_sync_state
		SET lease_request_id = ?, lease_owner_id = ?, lease_generation = 1,
		    lease_action = 'sync', lease_zone_type = 'NATIVE',
		    lease_qualifier = ?, lease_expires_at = datetime('now', '+2 minutes')
		WHERE zone_name = 'lease.example'`, requestID, ownerID,
		"dns-zone-sync/v1:sha256:"+strings.Repeat("C", 64))

	if _, err := database.Exec(`
		UPDATE dns_zone_sync_state
		SET lease_request_id = ?, lease_owner_id = ?,
		    lease_generation = desired_generation,
		    lease_action = desired_action,
		    lease_zone_type = desired_zone_type,
		    lease_qualifier = ?,
		    lease_expires_at = datetime('now', '+2 minutes')
		WHERE zone_name = 'lease.example'`, requestID, ownerID, qualifier); err != nil {
		t.Fatalf("store exact lease: %v", err)
	}
	requireDNSZoneSyncSQLFailure(t, database, "applied state with live lease", `
		UPDATE dns_zone_sync_state
		SET applied_generation = desired_generation, status = 'applied'
		WHERE zone_name = 'lease.example'`)

	// Advancing desired state preserves the exact old lease so a late result
	// can be rejected by generation/qualifier instead of being misattributed.
	if _, err := database.Exec(`
		INSERT INTO pdns_records(domain_id, name, type, content, ttl, prio, disabled)
		VALUES (?, 'lease.example', 'A', '192.0.2.8', 300, 0, 0)`, zoneID); err != nil {
		t.Fatal(err)
	}
	var desired, leaseGeneration int64
	var gotRequest, gotOwner, gotQualifier, status string
	if err := database.QueryRow(`
		SELECT desired_generation, lease_generation, lease_request_id,
		       lease_owner_id, lease_qualifier, status
		FROM dns_zone_sync_state WHERE zone_name = 'lease.example'`,
	).Scan(&desired, &leaseGeneration, &gotRequest, &gotOwner, &gotQualifier, &status); err != nil {
		t.Fatal(err)
	}
	if desired != 2 || leaseGeneration != 1 || gotRequest != requestID ||
		gotOwner != ownerID || gotQualifier != qualifier || status != "pending" {
		t.Fatalf("advanced leased state=%d/%d %q/%q %q %q",
			desired, leaseGeneration, gotRequest, gotOwner, gotQualifier, status)
	}

	if _, err := database.Exec(`
		UPDATE dns_zone_sync_state
		SET applied_generation = desired_generation,
		    status = 'applied',
		    lease_request_id = NULL,
		    lease_owner_id = NULL,
		    lease_generation = NULL,
		    lease_action = NULL,
		    lease_zone_type = NULL,
		    lease_qualifier = NULL,
		    lease_expires_at = NULL
		WHERE zone_name = 'lease.example'`); err != nil {
		t.Fatalf("atomically apply and clear exact lease: %v", err)
	}
	assertDNSZoneSyncState(t, database, "lease.example", zoneID, 2, 2, "sync", "NATIVE", "applied")
}

func TestDNSZoneSyncMigrationDeleteMarkerSurvivesCascadesUntilApplied(t *testing.T) {
	database := newDNSZoneSyncMigrationDB(t)
	tenantDomainID := insertDomainDeletionOperationTestDomain(t, database, "delete")
	if _, err := database.Exec(`
		INSERT INTO domain_deletion_operations(domain_id, previous_status)
		VALUES (?, 'active')`, tenantDomainID); err != nil {
		t.Fatal(err)
	}

	result, err := database.Exec(
		`INSERT INTO pdns_domains(name, type) VALUES ('delete.example.test', 'MASTER')`,
	)
	if err != nil {
		t.Fatal(err)
	}
	zoneID, _ := result.LastInsertId()
	for index := 1; index <= 2; index++ {
		if _, err := database.Exec(`
			INSERT INTO pdns_records(domain_id, name, type, content, ttl, prio, disabled)
			VALUES (?, 'delete.example.test', 'A', ?, 300, 0, 0)`,
			zoneID, fmt.Sprintf("192.0.2.%d", index)); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := database.Exec(`DELETE FROM pdns_domains WHERE id = ?`, zoneID); err != nil {
		t.Fatalf("delete zone with record cascade: %v", err)
	}
	// insert + two records + two cascade deletes + final tombstone = 6. The
	// final AFTER DELETE marker must win over every earlier sync generation.
	assertDNSZoneSyncState(t, database, "delete.example.test", 0, 6, 0, "delete", "MASTER", "pending")
	var markerType string
	if err := database.QueryRow(`
		SELECT zone_type FROM dns_zone_deletion_markers
		WHERE zone_name = 'delete.example.test'`,
	).Scan(&markerType); err != nil || markerType != "MASTER" {
		t.Fatalf("deletion marker type=%q err=%v", markerType, err)
	}
	requireDNSZoneSyncSQLFailure(t, database, "duplicate tombstone", `
		INSERT INTO dns_zone_deletion_markers(zone_name, zone_type)
		VALUES ('delete.example.test', 'MASTER')`)
	requireDNSZoneSyncSQLFailure(t, database, "unapplied tombstone removal", `
		DELETE FROM dns_zone_deletion_markers WHERE zone_name = 'delete.example.test'`)

	if _, err := database.Exec(`DELETE FROM domains WHERE id = ?`, tenantDomainID); err != nil {
		t.Fatalf("delete tenant domain and operation cascade: %v", err)
	}
	var operationCount, markerCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM domain_deletion_operations WHERE domain_id = ?`, tenantDomainID).Scan(&operationCount); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM dns_zone_deletion_markers WHERE zone_name = 'delete.example.test'`).Scan(&markerCount); err != nil {
		t.Fatal(err)
	}
	if operationCount != 0 || markerCount != 1 {
		t.Fatalf("after tenant cascade operation=%d marker=%d", operationCount, markerCount)
	}

	if _, err := database.Exec(`
		UPDATE dns_zone_sync_state
		SET applied_generation = desired_generation, status = 'applied'
		WHERE zone_name = 'delete.example.test'`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		DELETE FROM dns_zone_deletion_markers WHERE zone_name = 'delete.example.test'`); err != nil {
		t.Fatalf("retire applied tombstone: %v", err)
	}
	var stateCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM dns_zone_sync_state WHERE zone_name = 'delete.example.test'`).Scan(&stateCount); err != nil {
		t.Fatal(err)
	}
	if stateCount != 0 {
		t.Fatalf("retired deletion state count=%d want=0", stateCount)
	}
}

func TestDNSZoneSyncMigrationResurrectionSupersedesPendingDelete(t *testing.T) {
	database := newDNSZoneSyncMigrationDB(t)
	result, err := database.Exec(
		`INSERT INTO pdns_domains(name, type) VALUES ('again.example', 'NATIVE')`,
	)
	if err != nil {
		t.Fatal(err)
	}
	firstID, _ := result.LastInsertId()
	if _, err := database.Exec(`
		INSERT INTO pdns_records(domain_id, name, type, content, ttl)
		VALUES (?, 'again.example', 'A', '192.0.2.1', 300)`, firstID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`DELETE FROM pdns_domains WHERE id = ?`, firstID); err != nil {
		t.Fatal(err)
	}
	assertDNSZoneSyncState(t, database, "again.example", 0, 4, 0, "delete", "NATIVE", "pending")

	result, err = database.Exec(
		`INSERT INTO pdns_domains(name, type) VALUES ('again.example', 'MASTER')`,
	)
	if err != nil {
		t.Fatalf("resurrect zone: %v", err)
	}
	secondID, _ := result.LastInsertId()
	assertDNSZoneSyncState(t, database, "again.example", secondID, 6, 0, "sync", "MASTER", "pending")
	var markerCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM dns_zone_deletion_markers WHERE zone_name = 'again.example'`).Scan(&markerCount); err != nil {
		t.Fatal(err)
	}
	if markerCount != 0 {
		t.Fatalf("resurrected zone retained %d tombstones", markerCount)
	}
}

func TestDNSZoneSyncMigrationRenameProducesOldDeleteAndNewSync(t *testing.T) {
	database := newDNSZoneSyncMigrationDB(t)
	result, err := database.Exec(
		`INSERT INTO pdns_domains(name, type) VALUES ('old.example', 'MASTER')`,
	)
	if err != nil {
		t.Fatal(err)
	}
	zoneID, _ := result.LastInsertId()
	if _, err := database.Exec(`
		UPDATE pdns_domains SET name = 'new.example' WHERE id = ?`, zoneID); err != nil {
		t.Fatalf("rename DNS zone: %v", err)
	}
	assertDNSZoneSyncState(t, database, "old.example", 0, 2, 0, "delete", "MASTER", "pending")
	assertDNSZoneSyncState(t, database, "new.example", zoneID, 1, 0, "sync", "MASTER", "pending")
	var markerCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM dns_zone_deletion_markers WHERE zone_name = 'old.example'`).Scan(&markerCount); err != nil {
		t.Fatal(err)
	}
	if markerCount != 1 {
		t.Fatalf("rename old-name markers=%d want=1", markerCount)
	}
}

func TestDNSZoneSyncMigrationParticipatesInReferenceSchemaContract(t *testing.T) {
	objects, err := ReferenceSQLiteUserSchema(context.Background(), 32)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"table/dns_zone_deletion_markers":                    false,
		"table/dns_zone_sync_state":                          false,
		"index/idx_dns_zone_sync_state_source_domain":        false,
		"index/idx_dns_zone_sync_state_lease_request":        false,
		"trigger/pdns_domains_dns_sync_delete":               false,
		"trigger/pdns_records_dns_sync_update":               false,
		"trigger/dns_zone_deletion_marker_delete_sync_state": false,
	}
	for _, object := range objects {
		key := object.Type + "/" + object.Name
		if _, expected := want[key]; expected {
			want[key] = true
		}
	}
	for key, found := range want {
		if !found {
			t.Errorf("reference/rescue schema contract is missing %s", key)
		}
	}
}

func newDNSZoneSyncMigrationDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := NewSQLiteDB(filepath.Join(t.TempDir(), "panel.sqlite"))
	if err != nil {
		t.Fatalf("open migrated database: %v", err)
	}
	t.Cleanup(database.Close)
	return database.GetDB()
}

func newPreDNSZoneSyncMigrationDB(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pre-dns-zone-sync.sqlite")
	database, err := sql.Open("sqlite", fmt.Sprintf(
		"%s?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)", path,
	))
	if err != nil {
		t.Fatalf("open pre-032 database: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("close pre-032 database: %v", err)
		}
	})
	migrations, err := loadEmbeddedMigrations()
	if err != nil {
		t.Fatalf("load embedded migrations: %v", err)
	}
	for _, migration := range migrations {
		if migration.version >= 32 {
			break
		}
		if _, err := database.Exec(string(migration.content)); err != nil {
			t.Fatalf("apply pre-032 migration %s: %v", migration.filename, err)
		}
	}
	return database
}

func assertDNSZoneSyncState(
	t *testing.T,
	database *sql.DB,
	zoneName string,
	wantSourceID, wantDesired, wantApplied int64,
	wantAction, wantZoneType, wantStatus string,
) {
	t.Helper()
	var sourceID sql.NullInt64
	var desired, applied int64
	var action, zoneType, status string
	if err := database.QueryRow(`
		SELECT source_domain_id, desired_generation, applied_generation,
		       desired_action, desired_zone_type, status
		FROM dns_zone_sync_state WHERE zone_name = ?`, zoneName,
	).Scan(&sourceID, &desired, &applied, &action, &zoneType, &status); err != nil {
		t.Fatalf("read DNS sync state for %s: %v", zoneName, err)
	}
	gotSourceID := int64(0)
	if sourceID.Valid {
		gotSourceID = sourceID.Int64
	}
	if gotSourceID != wantSourceID || desired != wantDesired || applied != wantApplied ||
		action != wantAction || zoneType != wantZoneType || status != wantStatus {
		t.Fatalf("DNS sync state for %s = source:%d desired:%d applied:%d %s/%s/%s; want source:%d desired:%d applied:%d %s/%s/%s",
			zoneName, gotSourceID, desired, applied, action, zoneType, status,
			wantSourceID, wantDesired, wantApplied, wantAction, wantZoneType, wantStatus)
	}
}

func requireDNSZoneSyncSQLFailure(
	t *testing.T,
	database *sql.DB,
	name, query string,
	args ...any,
) {
	t.Helper()
	if _, err := database.Exec(query, args...); err == nil {
		t.Fatalf("%s unexpectedly succeeded", name)
	}
}
