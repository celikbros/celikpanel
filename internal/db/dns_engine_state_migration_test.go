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

func TestDNSEngineMigrationEnforcesOperationModeAndTopology(t *testing.T) {
	tests := []struct {
		name                     string
		mode                     string
		source, target           any
		sourceEpoch, targetEpoch int64
		topology                 string
		wantValid                bool
	}{
		{
			name: "fresh standalone switch", mode: "switch",
			source: nil, target: "bind", sourceEpoch: 0, targetEpoch: 1,
			topology: "standalone", wantValid: true,
		},
		{
			name: "standalone PowerDNS adoption", mode: "adopt",
			source: nil, target: "pdns", sourceEpoch: 0, targetEpoch: 1,
			topology: "standalone", wantValid: true,
		},
		{
			name: "paired PowerDNS adoption", mode: "adopt",
			source: nil, target: "pdns", sourceEpoch: 0, targetEpoch: 1,
			topology: "paired", wantValid: true,
		},
		{
			name: "unknown mode", mode: "repair",
			source: nil, target: "pdns", sourceEpoch: 0, targetEpoch: 1,
			topology: "standalone",
		},
		{
			name: "paired switch", mode: "switch",
			source: nil, target: "bind", sourceEpoch: 0, targetEpoch: 1,
			topology: "paired",
		},
		{
			name: "adopt BIND", mode: "adopt",
			source: nil, target: "bind", sourceEpoch: 0, targetEpoch: 1,
			topology: "standalone",
		},
		{
			name: "adopt from resolved source", mode: "adopt",
			source: "bind", target: "pdns", sourceEpoch: 1, targetEpoch: 2,
			topology: "standalone",
		},
		{
			name: "adopt nonzero source epoch", mode: "adopt",
			source: nil, target: "pdns", sourceEpoch: 1, targetEpoch: 2,
			topology: "standalone",
		},
		{
			name: "adopt skips target epoch", mode: "adopt",
			source: nil, target: "pdns", sourceEpoch: 0, targetEpoch: 2,
			topology: "standalone",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			database := newDNSZoneSyncMigrationDB(t)
			err := insertDNSEngineSnapshotForTest(
				database, strings.Repeat("1", 32), strings.Repeat("2", 32),
				strings.Repeat("3", 32), test.mode, test.source, test.target,
				test.sourceEpoch, test.targetEpoch, 0, test.topology,
			)
			if test.wantValid && err != nil {
				t.Fatalf("valid snapshot rejected: %v", err)
			}
			if !test.wantValid && err == nil {
				t.Fatal("invalid snapshot unexpectedly accepted")
			}
		})
	}
}

func TestDNSEngineMigrationCommitsPairedPowerDNSAdoption(t *testing.T) {
	database := newDNSZoneSyncMigrationDB(t)
	if _, err := database.Exec(
		`INSERT INTO pdns_domains(name, type) VALUES ('adopt.example', 'NATIVE')`,
	); err != nil {
		t.Fatal(err)
	}
	switchID := strings.Repeat("4", 32)
	if err := insertDNSEngineSnapshotForTest(
		database, switchID, strings.Repeat("5", 32), strings.Repeat("6", 32),
		"adopt", nil, "pdns", 0, 1, 0, "paired",
	); err != nil {
		t.Fatal(err)
	}
	requireDNSEngineSQLFailure(t, database, "operation mode mutation", `
		UPDATE dns_engine_switch_snapshots SET mode = 'switch'
		WHERE switch_id = ?`, switchID)
	requireDNSEngineSQLFailure(t, database, "unattached phase transition", `
		UPDATE dns_engine_switch_snapshots
		SET phase = 'staging', updated_at = datetime('now')
		WHERE switch_id = ?`, switchID)
	if _, err := database.Exec(`
		UPDATE dns_engine_state
		SET current_switch_id = ?, revision = 1, updated_at = datetime('now')
		WHERE singleton_id = 1`, switchID); err != nil {
		t.Fatal(err)
	}
	requireDNSEngineSQLFailure(t, database, "early topology mutation", `
		UPDATE dns_engine_state
		SET topology = 'paired', revision = 2, updated_at = datetime('now')
		WHERE singleton_id = 1`)
	requireDNSEngineSQLFailure(t, database, "adoption ledger freeze",
		`INSERT INTO pdns_domains(name, type) VALUES ('blocked.example', 'NATIVE')`)
	for _, phase := range []string{
		"staging", "staged", "activating", "verifying", "committed",
	} {
		if _, err := database.Exec(`
			UPDATE dns_engine_switch_snapshots
			SET phase = ?, updated_at = datetime('now')
			WHERE switch_id = ?`, phase, switchID); err != nil {
			t.Fatalf("advance adoption to %s: %v", phase, err)
		}
	}
	if _, err := database.Exec(`
		UPDATE dns_engine_state
		SET active_engine = 'pdns', active_epoch = 1, topology = 'paired',
		    current_switch_id = NULL, revision = 2, updated_at = datetime('now')
		WHERE singleton_id = 1`); err != nil {
		t.Fatalf("commit paired adoption: %v", err)
	}
	var engine, topology string
	var epoch, revision int64
	if err := database.QueryRow(`
		SELECT active_engine, active_epoch, revision, topology
		FROM dns_engine_state WHERE singleton_id = 1`,
	).Scan(&engine, &epoch, &revision, &topology); err != nil {
		t.Fatal(err)
	}
	if engine != "pdns" || epoch != 1 || revision != 2 || topology != "paired" {
		t.Fatalf("adopted state=%s/%d/%d/%s", engine, epoch, revision, topology)
	}
	requireDNSEngineSQLFailure(t, database, "direct topology downgrade", `
		UPDATE dns_engine_state
		SET topology = 'standalone', revision = 3, updated_at = datetime('now')
		WHERE singleton_id = 1`)

	secondSwitchID := strings.Repeat("7", 32)
	if err := insertDNSEngineSnapshotForTest(
		database, secondSwitchID, strings.Repeat("8", 32), strings.Repeat("9", 32),
		"switch", "pdns", "bind", 1, 2, 2, "standalone",
	); err != nil {
		t.Fatal(err)
	}
	requireDNSEngineSQLFailure(t, database, "switch from paired topology", `
		UPDATE dns_engine_state
		SET current_switch_id = ?, revision = 3, updated_at = datetime('now')
		WHERE singleton_id = 1`, secondSwitchID)
}

func TestDNSEngineMigrationRequiresPublishedSagaForPowerDNSTopology(t *testing.T) {
	database := newDNSZoneSyncMigrationDB(t)
	switchID := strings.Repeat("a", 32)
	if err := insertDNSEngineSnapshotForTest(
		database, switchID, strings.Repeat("b", 32), strings.Repeat("c", 32),
		"switch", nil, "pdns", 0, 1, 0, "standalone",
	); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		UPDATE dns_engine_state
		SET current_switch_id = ?, revision = 1, updated_at = datetime('now')
		WHERE singleton_id = 1`, switchID); err != nil {
		t.Fatal(err)
	}
	for _, phase := range []string{
		"staging", "staged", "activating", "verifying", "committed",
	} {
		if _, err := database.Exec(`
			UPDATE dns_engine_switch_snapshots
			SET phase = ?, updated_at = datetime('now')
			WHERE switch_id = ?`, phase, switchID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := database.Exec(`
		UPDATE dns_engine_state
		SET active_engine = 'pdns', active_epoch = 1,
		    current_switch_id = NULL, revision = 2,
		    updated_at = datetime('now')
		WHERE singleton_id = 1`); err != nil {
		t.Fatal(err)
	}
	requireDNSEngineSQLFailure(t, database, "topology without saga", `
		UPDATE dns_engine_state
		SET topology = 'paired', revision = 3, updated_at = datetime('now')
		WHERE singleton_id = 1`)
	if _, err := database.Exec(`
		INSERT INTO panel_settings(key, value)
		VALUES ('dns_cluster_saga_v1',
		        '{"version":1,"phase":"published","previous":{"role":"standalone"},"desired":{"role":"paired"}}')
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		UPDATE dns_engine_state
		SET topology = 'paired', revision = 3, updated_at = datetime('now')
		WHERE singleton_id = 1`); err != nil {
		t.Fatalf("published topology transition rejected: %v", err)
	}
	if _, err := database.Exec(`
		UPDATE panel_settings SET value = '' WHERE key = 'dns_cluster_saga_v1'`); err != nil {
		t.Fatal(err)
	}
	requireDNSEngineSQLFailure(t, database, "topology after saga clear", `
		UPDATE dns_engine_state
		SET topology = 'standalone', revision = 4, updated_at = datetime('now')
		WHERE singleton_id = 1`)
}

func TestDNSEngineMigration033AttachesModeBoundPairedAdoption(t *testing.T) {
	database := newPreDNSEngineMigrationDB(t)
	applyEmbeddedMigrationVersion(t, database, 33)
	switchID := strings.Repeat("f", 32)
	if err := insertDNSEngineSnapshotForTest(
		database, switchID, strings.Repeat("1", 32), strings.Repeat("2", 32),
		"adopt", nil, "pdns", 0, 1, 0, "paired",
	); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		UPDATE dns_engine_state
		SET current_switch_id = ?, revision = 1, updated_at = datetime('now')
		WHERE singleton_id = 1`, switchID); err != nil {
		t.Fatalf("migration 033 rejected exact paired adoption: %v", err)
	}
	if _, err := database.Exec(`
		UPDATE dns_engine_switch_snapshots
		SET phase = 'staging', updated_at = datetime('now')
		WHERE switch_id = ?`, switchID); err != nil {
		t.Fatalf("migration 033 phase guard rejected attached adoption: %v", err)
	}
}

func TestDNSEngineMigration034PreservesPreEngineLedgerAdoption(t *testing.T) {
	database := newPreDNSEngineMigrationDB(t)
	if _, err := database.Exec(
		`INSERT INTO pdns_domains(name, type) VALUES ('legacy.example', 'NATIVE')`,
	); err != nil {
		t.Fatal(err)
	}
	applyEmbeddedMigrationVersion(t, database, 33)
	applyEmbeddedMigrationVersion(t, database, 34)

	var desiredGeneration int64
	if err := database.QueryRow(`
		SELECT desired_generation FROM dns_zone_sync_state
		WHERE zone_name = 'legacy.example'`,
	).Scan(&desiredGeneration); err != nil {
		t.Fatalf("pre-engine zone ledger was not preserved: %v", err)
	}
	if desiredGeneration != 1 {
		t.Fatalf("legacy desired generation=%d want=1", desiredGeneration)
	}

	switchID := strings.Repeat("a", 32)
	if err := insertDNSEngineSnapshotForTest(
		database, switchID, strings.Repeat("b", 32), strings.Repeat("c", 32),
		"adopt", nil, "pdns", 0, 1, 0, "paired",
	); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		INSERT INTO dns_zone_engine_leases (
			zone_name, engine, engine_epoch, request_id, owner_id,
			desired_generation, desired_action, desired_zone_type,
			qualifier, expires_at
		) VALUES (
			'legacy.example', 'pdns', 1, ?, ?, 1, 'sync', 'NATIVE', ?,
			datetime('now', '+2 minutes')
		)`, strings.Repeat("d", 32), strings.Repeat("e", 32),
		testDNSV3Qualifier); err != nil {
		t.Fatal(err)
	}
	requireDNSEngineSQLFailure(t, database, "034 publication lease gate", `
		UPDATE dns_engine_state
		SET current_switch_id = ?, revision = 1, updated_at = datetime('now')
		WHERE singleton_id = 1`, switchID)
	if _, err := database.Exec(
		`DELETE FROM dns_zone_engine_leases WHERE zone_name = 'legacy.example'`,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		UPDATE dns_engine_state
		SET current_switch_id = ?, revision = 1, updated_at = datetime('now')
		WHERE singleton_id = 1`, switchID); err != nil {
		t.Fatalf("034 rejected exact paired adoption after lease release: %v", err)
	}
	assertForeignKeyCheckClean(t, database)
}

func TestDNSEngineMigrationCommitsOnlyAnExactFrozenSwitch(t *testing.T) {
	database := newDNSZoneSyncMigrationDB(t)
	switchID := strings.Repeat("1", 32)
	requestID := strings.Repeat("2", 32)
	ownerID := strings.Repeat("3", 32)
	recordsJSON := `[{"name":"example.test","type":"A","content":"192.0.2.2","ttl":300,"prio":0,"disabled":false}]`
	if _, err := database.Exec(`
		INSERT INTO dns_engine_switch_snapshots (
			switch_id, request_id, owner_id, mode, source_engine, target_engine,
			source_epoch, target_epoch, source_state_revision, topology,
			phase, manifest_qualifier, zone_count, snapshot_bytes
		) VALUES (?, ?, ?, 'switch', NULL, 'bind', 0, 1, 0, 'standalone',
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
		  switch_id, request_id, owner_id, mode, source_engine, target_engine,
		  source_epoch, target_epoch, source_state_revision, topology, phase,
		  manifest_qualifier, zone_count, snapshot_bytes
		) VALUES (?, ?, ?, 'switch', NULL, 'bind', 0, 1, 0, 'standalone',
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
				  switch_id, request_id, owner_id, mode, source_engine, target_engine,
				  source_epoch, target_epoch, source_state_revision, topology,
				  phase, manifest_qualifier, zone_count, snapshot_bytes
				) VALUES (?, ?, ?, 'switch', NULL, 'bind', 0, 1, 0, 'standalone',
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

func insertDNSEngineSnapshotForTest(
	database *sql.DB,
	switchID, requestID, ownerID, mode string,
	sourceEngine, targetEngine any,
	sourceEpoch, targetEpoch, sourceRevision int64,
	topology string,
) error {
	peerIP, peerNS := "", ""
	if topology == "paired" {
		peerIP, peerNS = "192.0.2.53", "ns2.example.test"
	}
	_, err := database.Exec(`
		INSERT INTO dns_engine_switch_snapshots (
			switch_id, request_id, owner_id, mode, source_engine, target_engine,
			source_epoch, target_epoch, source_state_revision, topology, peer_ip, peer_ns,
			phase, manifest_qualifier, zone_count, snapshot_bytes
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'planned', ?, 0, 0)`,
		switchID, requestID, ownerID, mode, sourceEngine, targetEngine,
		sourceEpoch, targetEpoch, sourceRevision, topology, peerIP, peerNS,
		testDNSSwitchQualifier,
	)
	return err
}

func newPreDNSEngineMigrationDB(t *testing.T) *sql.DB {
	t.Helper()
	path := t.TempDir() + "/pre-dns-engine.sqlite"
	database, err := sql.Open(
		"sqlite",
		path+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)",
	)
	if err != nil {
		t.Fatalf("open pre-DNS-engine database: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("close pre-DNS-engine database: %v", err)
		}
	})
	migrations, err := loadEmbeddedMigrations()
	if err != nil {
		t.Fatalf("load embedded migrations: %v", err)
	}
	for _, migration := range migrations {
		if migration.version >= 33 {
			break
		}
		if _, err := database.Exec(string(migration.content)); err != nil {
			t.Fatalf("apply pre-DNS-engine migration %s: %v",
				migration.filename, err)
		}
	}
	return database
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
	objects, err := ReferenceSQLiteUserSchema(context.Background(), 35)
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
		"trigger/dns_engine_switch_freeze_peer_setting_insert":    false,
		"trigger/dns_engine_switch_freeze_peer_setting_update":    false,
		"trigger/dns_engine_switch_freeze_peer_setting_delete":    false,
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

func TestDNSEnginePeerSnapshotMigratesReleased032ToCurrent(t *testing.T) {
	database := newPreDNSEngineMigrationDB(t)
	for _, version := range []int{33, 34, 35} {
		applyEmbeddedMigrationVersion(t, database, version)
	}

	requireDNSEngineSQLFailure(t, database, "paired snapshot without peer tuple", `
		INSERT INTO dns_engine_switch_snapshots (
			switch_id, request_id, owner_id, mode, source_engine, target_engine,
			source_epoch, target_epoch, source_state_revision, topology,
			phase, manifest_qualifier, zone_count, snapshot_bytes
		) VALUES (?, ?, ?, 'adopt', NULL, 'pdns', 0, 1, 0, 'paired',
		          'planned', ?, 0, 0)`,
		strings.Repeat("1", 32), strings.Repeat("2", 32),
		strings.Repeat("3", 32), testDNSSwitchQualifier,
	)

	switchID := strings.Repeat("4", 32)
	if err := insertDNSEngineSnapshotForTest(
		database, switchID, strings.Repeat("5", 32), strings.Repeat("6", 32),
		"adopt", nil, "pdns", 0, 1, 0, "paired",
	); err != nil {
		t.Fatalf("insert exact paired snapshot after released-032 migration: %v", err)
	}
	var peerIP, peerNS string
	if err := database.QueryRow(`
		SELECT peer_ip, peer_ns FROM dns_engine_switch_snapshots
		WHERE switch_id = ?`, switchID,
	).Scan(&peerIP, &peerNS); err != nil {
		t.Fatal(err)
	}
	if peerIP != "192.0.2.53" || peerNS != "ns2.example.test" {
		t.Fatalf("persisted peer tuple=%q/%q", peerIP, peerNS)
	}
	if _, err := database.Exec(`
		INSERT INTO panel_settings(key, value) VALUES
		('dns_role', 'paired'), ('dns_peer_ip', '192.0.2.53')
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		UPDATE dns_engine_state SET current_switch_id = ?, revision = 1,
		       updated_at = datetime('now') WHERE singleton_id = 1`, switchID,
	); err != nil {
		t.Fatal(err)
	}
	requireDNSEngineSQLFailure(t, database, "peer setting update while attached", `
		UPDATE panel_settings SET value = '192.0.2.54' WHERE key = 'dns_peer_ip'`)
	requireDNSEngineSQLFailure(t, database, "peer setting delete while attached", `
		DELETE FROM panel_settings WHERE key = 'dns_role'`)
	requireDNSEngineSQLFailure(t, database, "peer setting insert while attached", `
		INSERT INTO panel_settings(key, value) VALUES ('dns_peer_ns', 'other.test')`)
	requireDNSEngineSQLFailure(t, database, "peer tuple identity mutation", `
		UPDATE dns_engine_switch_snapshots SET peer_ip = '192.0.2.54'
		WHERE switch_id = ?`, switchID)
	assertForeignKeyCheckClean(t, database)
}

func TestDNSEngineMigration034RetainsModeAwareAttachmentGuard(t *testing.T) {
	objects, err := ReferenceSQLiteUserSchema(context.Background(), 34)
	if err != nil {
		t.Fatal(err)
	}
	for _, object := range objects {
		if object.Type != "trigger" ||
			object.Name != "dns_engine_state_attach_switch_guard" {
			continue
		}
		for _, fragment := range []string{
			"snapshot.mode = 'switch'",
			"snapshot.mode = 'adopt'",
			"snapshot.topology IN ('standalone', 'paired')",
			"EXISTS (SELECT 1 FROM dns_zone_engine_leases)",
		} {
			if !strings.Contains(object.SQL, fragment) {
				t.Errorf("migration 034 attachment guard is missing %q", fragment)
			}
		}
		return
	}
	t.Fatal("migration 034 attachment guard is unavailable")
}

func TestBINDPairedMigrationBindsExactSwitchAndActiveEpoch(t *testing.T) {
	database := newDNSZoneSyncMigrationDB(t)
	switchID := strings.Repeat("7", 32)
	if err := insertDNSEngineSnapshotForTest(
		database, switchID, strings.Repeat("8", 32), strings.Repeat("9", 32),
		"switch", nil, "bind", 0, 1, 0, "standalone",
	); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		INSERT INTO dns_bind_pair_switches (
		  switch_id, pair_role, local_ip, local_ns, peer_ip, peer_ns
		) VALUES (?, 'primary', '192.0.2.10', 'ns1.example.test',
		          '192.0.2.20', 'ns2.example.test')`, switchID,
	); err != nil {
		t.Fatalf("insert exact BIND pair identity: %v", err)
	}
	if _, err := database.Exec(`
		UPDATE dns_engine_state SET current_switch_id = ?, revision = 1,
		       updated_at = datetime('now') WHERE singleton_id = 1`, switchID,
	); err != nil {
		t.Fatalf("attach exact BIND pair switch: %v", err)
	}
	requireDNSEngineSQLFailure(t, database, "pair identity mutation", `
		UPDATE dns_bind_pair_switches SET peer_ip = '192.0.2.21'
		WHERE switch_id = ?`, switchID)
	for _, phase := range []string{"staging", "staged", "activating", "verifying", "committed"} {
		if _, err := database.Exec(`
			UPDATE dns_engine_switch_snapshots SET phase = ?,
			       updated_at = datetime('now') WHERE switch_id = ?`, phase, switchID,
		); err != nil {
			t.Fatalf("advance %s: %v", phase, err)
		}
	}
	if _, err := database.Exec(`
		INSERT INTO dns_bind_pair_state (
		  singleton_id, active_epoch, pair_role, local_ip, local_ns,
		  peer_ip, peer_ns, source_switch_id
		) VALUES (1, 1, 'primary', '192.0.2.10', 'ns1.example.test',
		          '192.0.2.20', 'ns2.example.test', ?)`, switchID,
	); err != nil {
		t.Fatalf("publish exact BIND pair state: %v", err)
	}
	if _, err := database.Exec(`
		UPDATE dns_engine_state
		SET active_engine = 'bind', active_epoch = 1,
		    current_switch_id = NULL, revision = 2,
		    updated_at = datetime('now')
		WHERE singleton_id = 1`,
	); err != nil {
		t.Fatalf("finalize paired BIND engine: %v", err)
	}
	var role, localNS, peerNS string
	var epoch int64
	if err := database.QueryRow(`
		SELECT active_epoch, pair_role, local_ns, peer_ns
		FROM dns_bind_pair_state WHERE singleton_id = 1`,
	).Scan(&epoch, &role, &localNS, &peerNS); err != nil {
		t.Fatal(err)
	}
	if epoch != 1 || role != "primary" || localNS != "ns1.example.test" ||
		peerNS != "ns2.example.test" {
		t.Fatalf("paired state=%d/%s/%s/%s", epoch, role, localNS, peerNS)
	}
	requireDNSEngineSQLFailure(t, database, "active pair state mutation", `
		UPDATE dns_bind_pair_state SET pair_role = 'secondary' WHERE singleton_id = 1`)
	assertForeignKeyCheckClean(t, database)
}

func TestBINDPairedMigrationParticipatesInReferenceSchema(t *testing.T) {
	objects, err := ReferenceSQLiteUserSchema(context.Background(), 36)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"table/dns_bind_pair_switches":                 false,
		"table/dns_bind_pair_state":                    false,
		"trigger/dns_bind_pair_switch_insert_guard":    false,
		"trigger/dns_bind_pair_switch_immutable":       false,
		"trigger/dns_bind_pair_state_insert_guard":     false,
		"trigger/dns_engine_state_attach_switch_guard": false,
	}
	for _, object := range objects {
		key := object.Type + "/" + object.Name
		if _, ok := want[key]; ok {
			want[key] = true
		}
	}
	for key, found := range want {
		if !found {
			t.Errorf("reference schema is missing %s", key)
		}
	}
}

func TestDNSPairIdentityMigration037UpgradesReleasedAlpha27Schema(t *testing.T) {
	database := newDatabaseAtMigrationVersion(t, 27)
	if err := database.RunMigrations(); err != nil {
		t.Fatalf("upgrade released alpha.27 schema: %v", err)
	}

	var version int
	if err := database.db.QueryRow(
		`SELECT max(version) FROM schema_migrations`,
	).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 37 {
		t.Fatalf("upgraded schema version=%d, want 37", version)
	}

	for _, trigger := range []string{
		"dns_bind_pair_switch_preserve_active_identity",
		"dns_bind_pair_state_identity_immutable",
	} {
		var count int
		if err := database.db.QueryRow(`
			SELECT count(*) FROM sqlite_schema
			WHERE type = 'trigger' AND name = ?`, trigger,
		).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("upgraded schema trigger %s count=%d, want 1", trigger, count)
		}
	}
	assertForeignKeyCheckClean(t, database.db)
}

func TestDNSPairIdentityMigration037ParticipatesInReferenceSchema(t *testing.T) {
	objects, err := ReferenceSQLiteUserSchema(context.Background(), 37)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"trigger/dns_bind_pair_switch_preserve_active_identity": false,
		"trigger/dns_bind_pair_state_identity_immutable":        false,
	}
	for _, object := range objects {
		key := object.Type + "/" + object.Name
		if _, ok := want[key]; ok {
			want[key] = true
		}
	}
	for key, found := range want {
		if !found {
			t.Errorf("reference schema is missing %s", key)
		}
	}
}

func TestDNSPairIdentityMigration037RejectsPreUpgradeChangedSwitch(t *testing.T) {
	database := newDatabaseAtMigrationVersion(t, 36)
	raw := database.db
	advance := func(id string) {
		t.Helper()
		for _, phase := range []string{"staging", "staged", "activating", "verifying", "committed"} {
			if _, err := raw.Exec(
				`UPDATE dns_engine_switch_snapshots SET phase=? WHERE switch_id=?`,
				phase, id,
			); err != nil {
				t.Fatal(err)
			}
		}
	}

	first := strings.Repeat("1", 32)
	if err := insertDNSEngineSnapshotForTest(
		raw, first, strings.Repeat("2", 32), strings.Repeat("3", 32),
		"switch", nil, "bind", 0, 1, 0, "standalone",
	); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`
		INSERT INTO dns_bind_pair_switches
		(switch_id,pair_role,local_ip,local_ns,peer_ip,peer_ns)
		VALUES(?, 'primary', '192.0.2.10', 'ns1.example.test',
		       '192.0.2.20', 'ns2.example.test')`, first); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(
		`UPDATE dns_engine_state SET current_switch_id=?,revision=1 WHERE singleton_id=1`,
		first,
	); err != nil {
		t.Fatal(err)
	}
	advance(first)
	if _, err := raw.Exec(`
		INSERT INTO dns_bind_pair_state
		(singleton_id,active_epoch,pair_role,local_ip,local_ns,peer_ip,peer_ns,source_switch_id)
		VALUES(1,1,'primary','192.0.2.10','ns1.example.test',
		       '192.0.2.20','ns2.example.test',?)`, first); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`
		UPDATE dns_engine_state
		SET active_engine='bind',active_epoch=1,current_switch_id=NULL,revision=2
		WHERE singleton_id=1`); err != nil {
		t.Fatal(err)
	}

	second := strings.Repeat("4", 32)
	source := "bind"
	if err := insertDNSEngineSnapshotForTest(
		raw, second, strings.Repeat("5", 32), strings.Repeat("6", 32),
		"switch", &source, "pdns", 1, 2, 2, "standalone",
	); err != nil {
		t.Fatal(err)
	}
	// Migration 036 allowed this conflicting planned receipt. Migration 037
	// must also protect a host that upgrades while such a switch is attached.
	if _, err := raw.Exec(`
		INSERT INTO dns_bind_pair_switches
		(switch_id,pair_role,local_ip,local_ns,peer_ip,peer_ns)
		VALUES(?, 'secondary', '192.0.2.20', 'ns2.example.test',
		       '192.0.2.10', 'ns1.example.test')`, second); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(
		`UPDATE dns_engine_state SET current_switch_id=?,revision=3 WHERE singleton_id=1`,
		second,
	); err != nil {
		t.Fatal(err)
	}
	advance(second)

	if err := database.RunMigrations(); err != nil {
		t.Fatalf("apply migration 037 over attached switch: %v", err)
	}
	requireDNSEngineSQLFailure(t, raw, "pre-upgrade changed pair finalization", `
		UPDATE dns_bind_pair_state
		SET active_epoch=2,pair_role='secondary',
		    local_ip='192.0.2.20',local_ns='ns2.example.test',
		    peer_ip='192.0.2.10',peer_ns='ns1.example.test',
		    source_switch_id=?
		WHERE singleton_id=1`, second)

	var epoch int64
	var role, sourceSwitch string
	if err := raw.QueryRow(`
		SELECT active_epoch,pair_role,source_switch_id
		FROM dns_bind_pair_state WHERE singleton_id=1`,
	).Scan(&epoch, &role, &sourceSwitch); err != nil {
		t.Fatal(err)
	}
	if epoch != 1 || role != "primary" || sourceSwitch != first {
		t.Fatalf("pair identity changed after rejected upgrade finalization: %d/%s/%s",
			epoch, role, sourceSwitch)
	}
	assertForeignKeyCheckClean(t, raw)
}

func TestPairedMigrationAllowsEngineChangeWithoutLosingIdentity(t *testing.T) {
	database := newDNSZoneSyncMigrationDB(t)
	first := strings.Repeat("a", 32)
	if err := insertDNSEngineSnapshotForTest(
		database, first, strings.Repeat("b", 32), strings.Repeat("c", 32),
		"switch", nil, "bind", 0, 1, 0, "standalone",
	); err != nil {
		t.Fatal(err)
	}
	insertPair := func(id string) {
		t.Helper()
		if _, err := database.Exec(`
			INSERT INTO dns_bind_pair_switches
			(switch_id,pair_role,local_ip,local_ns,peer_ip,peer_ns)
			VALUES(?, 'primary', '192.0.2.10', 'ns1.example.test',
			       '192.0.2.20', 'ns2.example.test')`, id); err != nil {
			t.Fatal(err)
		}
	}
	advance := func(id string) {
		t.Helper()
		for _, phase := range []string{"staging", "staged", "activating", "verifying", "committed"} {
			if _, err := database.Exec(`UPDATE dns_engine_switch_snapshots SET phase=? WHERE switch_id=?`, phase, id); err != nil {
				t.Fatal(err)
			}
		}
	}
	insertPair(first)
	if _, err := database.Exec(`UPDATE dns_engine_state SET current_switch_id=?,revision=1 WHERE singleton_id=1`, first); err != nil {
		t.Fatal(err)
	}
	advance(first)
	if _, err := database.Exec(`
		INSERT INTO dns_bind_pair_state
		(singleton_id,active_epoch,pair_role,local_ip,local_ns,peer_ip,peer_ns,source_switch_id)
		VALUES(1,1,'primary','192.0.2.10','ns1.example.test','192.0.2.20','ns2.example.test',?)`, first); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE dns_engine_state SET active_engine='bind',active_epoch=1,current_switch_id=NULL,revision=2 WHERE singleton_id=1`); err != nil {
		t.Fatal(err)
	}
	second := strings.Repeat("d", 32)
	source := "bind"
	if err := insertDNSEngineSnapshotForTest(
		database, second, strings.Repeat("e", 32), strings.Repeat("f", 32),
		"switch", &source, "pdns", 1, 2, 2, "standalone",
	); err != nil {
		t.Fatal(err)
	}
	for _, changed := range []struct {
		name                   string
		role, localIP, localNS string
		peerIP, peerNS         string
	}{
		{
			name: "role", role: "secondary", localIP: "192.0.2.10",
			localNS: "ns1.example.test", peerIP: "192.0.2.20", peerNS: "ns2.example.test",
		},
		{
			name: "local IP", role: "primary", localIP: "192.0.2.11",
			localNS: "ns1.example.test", peerIP: "192.0.2.20", peerNS: "ns2.example.test",
		},
		{
			name: "local NS", role: "primary", localIP: "192.0.2.10",
			localNS: "ns3.example.test", peerIP: "192.0.2.20", peerNS: "ns2.example.test",
		},
		{
			name: "peer IP", role: "primary", localIP: "192.0.2.10",
			localNS: "ns1.example.test", peerIP: "192.0.2.21", peerNS: "ns2.example.test",
		},
		{
			name: "peer NS", role: "primary", localIP: "192.0.2.10",
			localNS: "ns1.example.test", peerIP: "192.0.2.20", peerNS: "ns3.example.test",
		},
	} {
		t.Run("reject changed "+changed.name, func(t *testing.T) {
			requireDNSEngineSQLFailure(t, database, changed.name, `
				INSERT INTO dns_bind_pair_switches
				(switch_id,pair_role,local_ip,local_ns,peer_ip,peer_ns)
				VALUES(?, ?, ?, ?, ?, ?)`,
				second, changed.role, changed.localIP, changed.localNS,
				changed.peerIP, changed.peerNS,
			)
		})
	}
	insertPair(second)
	if _, err := database.Exec(`UPDATE dns_engine_state SET current_switch_id=?,revision=3 WHERE singleton_id=1`, second); err != nil {
		t.Fatal(err)
	}
	advance(second)
	if _, err := database.Exec(`
		UPDATE dns_bind_pair_state
		SET active_epoch=2, source_switch_id=?, updated_at=datetime('now')
		WHERE singleton_id=1`, second); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE dns_engine_state SET active_engine='pdns',active_epoch=2,current_switch_id=NULL,revision=4 WHERE singleton_id=1`); err != nil {
		t.Fatal(err)
	}
	var engine, role, sourceSwitch string
	var epoch int64
	if err := database.QueryRow(`
		SELECT state.active_engine,pairing.active_epoch,pairing.pair_role,pairing.source_switch_id
		FROM dns_engine_state state JOIN dns_bind_pair_state pairing ON pairing.singleton_id=state.singleton_id
	`).Scan(&engine, &epoch, &role, &sourceSwitch); err != nil {
		t.Fatal(err)
	}
	if engine != "pdns" || epoch != 2 || role != "primary" || sourceSwitch != second {
		t.Fatalf("reverse pair=%s/%d/%s/%s", engine, epoch, role, sourceSwitch)
	}
	assertForeignKeyCheckClean(t, database)
}
