package db

import (
	"context"
	"database/sql"
	"strings"
	"testing"
)

const (
	testDNSV3Qualifier = "dns-zone-sync/v3:sha256:" +
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testDNSSwitchQualifier = "dns-engine-switch/v1:sha256:" +
		"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func TestDNSEngineMigrationStartsUnresolvedWithoutGuessingAuthority(t *testing.T) {
	database := newDNSZoneSyncMigrationDB(t)
	var active sql.NullString
	var epoch, revision int64
	var topology string
	var switchID sql.NullString
	if err := database.QueryRow(`
		SELECT active_engine, active_epoch, revision, topology, current_switch_id
		FROM dns_engine_state WHERE singleton_id = 1`,
	).Scan(&active, &epoch, &revision, &topology, &switchID); err != nil {
		t.Fatal(err)
	}
	if active.Valid || epoch != 0 || revision != 0 || topology != "standalone" || switchID.Valid {
		t.Fatalf("unexpected initial engine state: engine=%v epoch=%d revision=%d topology=%q switch=%v",
			active, epoch, revision, topology, switchID)
	}
	for _, table := range []string{
		"dns_engine_switch_snapshots", "dns_engine_switch_zones",
		"dns_zone_engine_leases", "dns_zone_engine_applications",
	} {
		var count int
		if err := database.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("migration invented %d rows in %s", count, table)
		}
	}
	requireDNSEngineSQLFailure(t, database, "direct authority guess", `
		UPDATE dns_engine_state
		SET active_engine = 'pdns', active_epoch = 1, revision = 1,
		    updated_at = datetime('now')
		WHERE singleton_id = 1`)
}

func TestDNSEngineMigrationCommitsOnlyAnExactFrozenSwitch(t *testing.T) {
	database := newDNSZoneSyncMigrationDB(t)
	switchID := strings.Repeat("1", 32)
	requestID := strings.Repeat("2", 32)
	ownerID := strings.Repeat("3", 32)
	recordsJSON := `[{"name":"example.test","type":"A","content":"192.0.2.2","ttl":300,"prio":0,"disabled":false}]`
	if _, err := database.Exec(`
		INSERT INTO dns_engine_switch_snapshots (
			switch_id, request_id, owner_id, source_engine, target_engine,
			source_epoch, target_epoch, source_state_revision, topology,
			phase, manifest_qualifier, zone_count, snapshot_bytes
		) VALUES (?, ?, ?, NULL, 'bind', 0, 1, 0, 'standalone',
		          'planned', ?, 1, ?)`, switchID, requestID, ownerID,
		testDNSSwitchQualifier, len([]byte(recordsJSON))); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		UPDATE dns_engine_state
		SET current_switch_id = ?, revision = 1, updated_at = datetime('now')
		WHERE singleton_id = 1`, switchID); err != nil {
		t.Fatal(err)
	}
	requireDNSEngineSQLFailure(t, database, "incomplete snapshot", `
		UPDATE dns_engine_switch_snapshots
		SET phase = 'staging', updated_at = datetime('now')
		WHERE switch_id = ?`, switchID)
	if _, err := database.Exec(`
		INSERT INTO dns_engine_switch_zones (
			switch_id, ordinal, zone_name, desired_generation,
			desired_action, desired_zone_type, zone_qualifier,
			records_json, records_bytes
		) VALUES (?, 0, 'example.test', 7, 'sync', 'NATIVE', ?, ?, ?)`,
		switchID, testDNSV3Qualifier, recordsJSON, len([]byte(recordsJSON))); err != nil {
		t.Fatal(err)
	}
	for _, phase := range []string{"staging", "staged", "activating", "verifying", "committed"} {
		if _, err := database.Exec(`
			UPDATE dns_engine_switch_snapshots
			SET phase = ?, updated_at = datetime('now') WHERE switch_id = ?`, phase, switchID); err != nil {
			t.Fatalf("advance to %s: %v", phase, err)
		}
	}
	if _, err := database.Exec(`
		UPDATE dns_engine_state
		SET active_engine = 'bind', active_epoch = 1,
		    current_switch_id = NULL, revision = 2, updated_at = datetime('now')
		WHERE singleton_id = 1`); err != nil {
		t.Fatal(err)
	}
	var engine string
	var epoch, revision int64
	if err := database.QueryRow(`
		SELECT active_engine, active_epoch, revision FROM dns_engine_state WHERE singleton_id = 1`,
	).Scan(&engine, &epoch, &revision); err != nil {
		t.Fatal(err)
	}
	if engine != "bind" || epoch != 1 || revision != 2 {
		t.Fatalf("committed state=%s/%d/%d", engine, epoch, revision)
	}
	if _, err := database.Exec(`
		INSERT INTO dns_zone_engine_applications (
			zone_name, engine, engine_epoch, applied_generation,
			applied_action, applied_zone_type, qualifier,
			mutation_request_id, mutation_owner_id, switch_id, revision
		) VALUES (
			'example.test', 'bind', 1, 7, 'sync', 'NATIVE', ?, ?, ?, ?, 1
		)`, testDNSV3Qualifier, strings.Repeat("8", 32), ownerID, switchID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		INSERT INTO dns_zone_engine_applications (
			zone_name, engine, engine_epoch, applied_generation,
			applied_action, applied_zone_type, qualifier,
			mutation_request_id, mutation_owner_id, switch_id, revision
		) VALUES (
			'second.example.test', 'bind', 1, 3, 'delete', 'MASTER', ?, ?, ?, ?, 1
		)`, testDNSV3Qualifier, strings.Repeat("8", 32), ownerID, switchID); err != nil {
		t.Fatalf("reuse one switch request across zones: %v", err)
	}
	requireDNSEngineSQLFailure(t, database, "application generation rollback", `
		UPDATE dns_zone_engine_applications
		SET applied_generation = 6, revision = 2, updated_at = datetime('now')
		WHERE zone_name = 'example.test' AND engine = 'bind'`)
	requireDNSEngineSQLFailure(t, database, "terminal phase rewind", `
		UPDATE dns_engine_switch_snapshots SET phase = 'staged' WHERE switch_id = ?`, switchID)
}

func TestDNSEngineMigrationSeparatesLegacyAndV3LeaseAuthority(t *testing.T) {
	database := newDNSZoneSyncMigrationDB(t)
	result, err := database.Exec(`INSERT INTO pdns_domains(name, type) VALUES ('lease.example', 'NATIVE')`)
	if err != nil {
		t.Fatal(err)
	}
	zoneID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	requestID := strings.Repeat("4", 32)
	ownerID := strings.Repeat("5", 32)
	if _, err := database.Exec(`
		INSERT INTO dns_zone_engine_leases (
			zone_name, engine, engine_epoch, request_id, owner_id,
			desired_generation, desired_action, desired_zone_type,
			qualifier, expires_at
		) VALUES (
			'lease.example', 'bind', 1, ?, ?, 1, 'sync', 'NATIVE', ?,
			datetime('now', '+2 minutes')
		)`, requestID, ownerID, testDNSV3Qualifier); err != nil {
		t.Fatal(err)
	}
	requireDNSEngineSQLFailure(t, database, "concurrent legacy lease", `
		UPDATE dns_zone_sync_state
		SET lease_request_id = ?, lease_owner_id = ?, lease_generation = 1,
		    lease_action = 'sync', lease_zone_type = 'NATIVE',
		    lease_qualifier = ?, lease_expires_at = datetime('now', '+2 minutes')
		WHERE zone_name = 'lease.example'`,
		strings.Repeat("6", 32), strings.Repeat("7", 32),
		"dns-zone-sync/v1:sha256:"+strings.Repeat("c", 64))
	if _, err := database.Exec(`
		INSERT INTO pdns_records(domain_id, name, type, content, ttl)
		VALUES (?, 'lease.example', 'A', '192.0.2.2', 300)`, zoneID); err != nil {
		t.Fatal(err)
	}
	requireDNSEngineSQLFailure(t, database, "stale V3 lease renewal", `
		UPDATE dns_zone_engine_leases
		SET expires_at = datetime('now', '+3 minutes'), updated_at = datetime('now')
		WHERE zone_name = 'lease.example'`)
	if _, err := database.Exec(`DELETE FROM dns_zone_engine_leases WHERE zone_name = 'lease.example'`); err != nil {
		t.Fatal(err)
	}
}

func TestDNSEngineMigrationFreezesZoneLedgerUntilTerminalDetach(t *testing.T) {
	database := newDNSZoneSyncMigrationDB(t)
	domainResult, err := database.Exec(`
		INSERT INTO pdns_domains(name, type) VALUES ('freeze.example', 'NATIVE')`)
	if err != nil {
		t.Fatal(err)
	}
	domainID, _ := domainResult.LastInsertId()
	recordResult, err := database.Exec(`
		INSERT INTO pdns_records(domain_id, name, type, content, ttl, prio, disabled)
		VALUES (?, 'freeze.example', 'A', '192.0.2.10', 300, 0, 0)`, domainID)
	if err != nil {
		t.Fatal(err)
	}
	recordID, _ := recordResult.LastInsertId()
	switchID, requestID, ownerID := strings.Repeat("9", 32),
		strings.Repeat("a", 32), strings.Repeat("b", 32)
	recordsJSON := `[{"name":"freeze.example","type":"A","content":"192.0.2.10","ttl":300,"prio":0,"disabled":false}]`
	if _, err := database.Exec(`
		INSERT INTO dns_engine_switch_snapshots (
		  switch_id, request_id, owner_id, source_engine, target_engine,
		  source_epoch, target_epoch, source_state_revision, topology, phase,
		  manifest_qualifier, zone_count, snapshot_bytes
		) VALUES (?, ?, ?, NULL, 'bind', 0, 1, 0, 'standalone',
		          'planned', ?, 1, ?)`,
		switchID, requestID, ownerID, testDNSSwitchQualifier,
		len([]byte(recordsJSON)),
	); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		INSERT INTO dns_engine_switch_zones (
		  switch_id, ordinal, zone_name, desired_generation, desired_action,
		  desired_zone_type, zone_qualifier, records_json, records_bytes
		) VALUES (?, 0, 'freeze.example', 2, 'sync', 'NATIVE', ?, ?, ?)`,
		switchID, testDNSV3Qualifier, recordsJSON, len([]byte(recordsJSON)),
	); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		UPDATE dns_engine_state SET current_switch_id = ?, revision = 1,
		       updated_at = datetime('now') WHERE singleton_id = 1`,
		switchID,
	); err != nil {
		t.Fatal(err)
	}
	assertFrozen := func(phase string) {
		t.Helper()
		requireDNSEngineSQLFailure(t, database, phase+" domain insert",
			`INSERT INTO pdns_domains(name, type) VALUES ('blocked.example', 'NATIVE')`)
		requireDNSEngineSQLFailure(t, database, phase+" domain update",
			`UPDATE pdns_domains SET type = 'MASTER' WHERE id = ?`, domainID)
		requireDNSEngineSQLFailure(t, database, phase+" domain delete",
			`DELETE FROM pdns_domains WHERE id = ?`, domainID)
		requireDNSEngineSQLFailure(t, database, phase+" record insert",
			`INSERT INTO pdns_records(domain_id, name, type, content, ttl)
			 VALUES (?, 'freeze.example', 'TXT', 'blocked', 300)`, domainID)
		requireDNSEngineSQLFailure(t, database, phase+" record update",
			`UPDATE pdns_records SET content = '192.0.2.11' WHERE id = ?`, recordID)
		requireDNSEngineSQLFailure(t, database, phase+" record delete",
			`DELETE FROM pdns_records WHERE id = ?`, recordID)
	}
	assertFrozen("planned")
	for _, phase := range []string{"staging", "staged", "activating", "verifying"} {
		if _, err := database.Exec(`
			UPDATE dns_engine_switch_snapshots SET phase = ?,
			       updated_at = datetime('now') WHERE switch_id = ?`,
			phase, switchID,
		); err != nil {
			t.Fatalf("advance %s: %v", phase, err)
		}
		assertFrozen(phase)
	}
	if _, err := database.Exec(`
		UPDATE dns_engine_switch_snapshots SET phase = 'committed',
		       updated_at = datetime('now') WHERE switch_id = ?`, switchID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		UPDATE dns_engine_state
		SET active_engine = 'bind', active_epoch = 1, current_switch_id = NULL,
		    revision = 2, updated_at = datetime('now')
		WHERE singleton_id = 1`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(
		`UPDATE pdns_records SET content = '192.0.2.12' WHERE id = ?`, recordID,
	); err != nil {
		t.Fatalf("record remained frozen after terminal detach: %v", err)
	}
	if _, err := database.Exec(
		`INSERT INTO pdns_domains(name, type) VALUES ('allowed.example', 'NATIVE')`,
	); err != nil {
		t.Fatalf("domain remained frozen after terminal detach: %v", err)
	}
}

func TestDNSEngineMigrationUnfreezesAfterRollbackOrFailureDetach(t *testing.T) {
	for _, terminal := range []string{"rolled_back", "failed"} {
		t.Run(terminal, func(t *testing.T) {
			database := newDNSZoneSyncMigrationDB(t)
			result, err := database.Exec(`
				INSERT INTO pdns_domains(name, type)
				VALUES ('terminal.example', 'NATIVE')`)
			if err != nil {
				t.Fatal(err)
			}
			domainID, _ := result.LastInsertId()
			if _, err := database.Exec(`
				INSERT INTO pdns_records(domain_id, name, type, content, ttl)
				VALUES (?, 'terminal.example', 'A', '192.0.2.20', 300)`,
				domainID,
			); err != nil {
				t.Fatal(err)
			}
			switchID := strings.Repeat("d", 32)
			requestID := strings.Repeat("e", 32)
			ownerID := strings.Repeat("f", 32)
			if _, err := database.Exec(`
				INSERT INTO dns_engine_switch_snapshots (
				  switch_id, request_id, owner_id, source_engine, target_engine,
				  source_epoch, target_epoch, source_state_revision, topology,
				  phase, manifest_qualifier, zone_count, snapshot_bytes
				) VALUES (?, ?, ?, NULL, 'bind', 0, 1, 0, 'standalone',
				          'planned', ?, 0, 0)`,
				switchID, requestID, ownerID, testDNSSwitchQualifier,
			); err != nil {
				t.Fatal(err)
			}
			if _, err := database.Exec(`
				UPDATE dns_engine_state SET current_switch_id = ?, revision = 1,
				       updated_at = datetime('now') WHERE singleton_id = 1`,
				switchID,
			); err != nil {
				t.Fatal(err)
			}
			requireDNSEngineSQLFailure(t, database, terminal+" attached",
				`UPDATE pdns_records SET content = '192.0.2.21' WHERE domain_id = ?`,
				domainID)
			if terminal == "rolled_back" {
				for _, transition := range []struct {
					phase     string
					lastError any
				}{
					{phase: "staging"},
					{phase: "rolling_back", lastError: "switch failed"},
					{phase: "rolled_back", lastError: "switch failed"},
				} {
					if _, err := database.Exec(`
						UPDATE dns_engine_switch_snapshots
						SET phase = ?, last_error = ?, updated_at = datetime('now')
						WHERE switch_id = ?`,
						transition.phase, transition.lastError, switchID,
					); err != nil {
						t.Fatalf("advance %s: %v", transition.phase, err)
					}
				}
			} else {
				if _, err := database.Exec(`
					UPDATE dns_engine_switch_snapshots
					SET phase = 'failed', last_error = 'switch failed',
					    updated_at = datetime('now')
					WHERE switch_id = ?`, switchID,
				); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := database.Exec(`
				UPDATE dns_engine_state
				SET current_switch_id = NULL, revision = 2,
				    updated_at = datetime('now')
				WHERE singleton_id = 1`); err != nil {
				t.Fatal(err)
			}
			if _, err := database.Exec(`
				UPDATE pdns_records SET content = '192.0.2.22'
				WHERE domain_id = ?`, domainID,
			); err != nil {
				t.Fatalf("record remained frozen after %s detach: %v",
					terminal, err)
			}
		})
	}
}

func requireDNSEngineSQLFailure(
	t *testing.T, database *sql.DB, name, query string, args ...any,
) {
	t.Helper()
	if _, err := database.Exec(query, args...); err == nil {
		t.Fatalf("%s unexpectedly succeeded", name)
	}
}

func TestDNSEngineMigrationParticipatesInReferenceSchemaContract(t *testing.T) {
	objects, err := ReferenceSQLiteUserSchema(context.Background(), 33)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"table/dns_engine_state":                                  false,
		"table/dns_engine_switch_snapshots":                       false,
		"table/dns_engine_switch_zones":                           false,
		"table/dns_zone_engine_leases":                            false,
		"table/dns_zone_engine_applications":                      false,
		"index/idx_dns_engine_switch_one_active":                  false,
		"trigger/dns_engine_state_engine_change_guard":            false,
		"trigger/dns_zone_sync_state_legacy_lease_conflict_guard": false,
		"trigger/dns_engine_switch_freeze_domain_insert":          false,
		"trigger/dns_engine_switch_freeze_record_update":          false,
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
