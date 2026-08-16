package db

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestDNSZoneGenerationHighWaterSurvivesRetiredDeleteAndResurrection(t *testing.T) {
	database := newDNSZoneSyncMigrationDB(t)
	result, err := database.Exec(`
		INSERT INTO pdns_domains(name, type)
		VALUES ('resurrect.example', 'NATIVE')`)
	if err != nil {
		t.Fatal(err)
	}
	domainID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	for _, content := range []string{"one", "two", "three"} {
		if _, err := database.Exec(`
			INSERT INTO pdns_records(domain_id, name, type, content, ttl)
			VALUES (?, 'resurrect.example', 'TXT', ?, 300)`,
			domainID, content,
		); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := database.Exec(
		`DELETE FROM pdns_domains WHERE id = ?`, domainID,
	); err != nil {
		t.Fatal(err)
	}
	var deletedGeneration int64
	if err := database.QueryRow(`
		SELECT desired_generation
		FROM dns_zone_sync_state
		WHERE zone_name = 'resurrect.example'
		  AND desired_action = 'delete' AND status = 'pending'
	`).Scan(&deletedGeneration); err != nil {
		t.Fatal(err)
	}
	if deletedGeneration <= 1 {
		t.Fatalf("delete generation=%d, want greater than one", deletedGeneration)
	}
	if _, err := database.Exec(`
		UPDATE dns_zone_sync_state
		SET applied_generation = desired_generation, status = 'applied'
		WHERE zone_name = 'resurrect.example'
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		DELETE FROM dns_zone_deletion_markers
		WHERE zone_name = 'resurrect.example'
	`); err != nil {
		t.Fatal(err)
	}
	var stateCount int
	if err := database.QueryRow(`
		SELECT count(*) FROM dns_zone_sync_state
		WHERE zone_name = 'resurrect.example'
	`).Scan(&stateCount); err != nil {
		t.Fatal(err)
	}
	if stateCount != 0 {
		t.Fatalf("retired delete retained %d desired-state rows", stateCount)
	}
	var highWater int64
	if err := database.QueryRow(`
		SELECT generation FROM dns_zone_generation_high_water
		WHERE zone_name = 'resurrect.example'
	`).Scan(&highWater); err != nil {
		t.Fatal(err)
	}
	if highWater != deletedGeneration {
		t.Fatalf("high-water=%d, want delete generation %d",
			highWater, deletedGeneration)
	}
	if _, err := database.Exec(`
		INSERT INTO pdns_domains(name, type)
		VALUES ('resurrect.example', 'NATIVE')
	`); err != nil {
		t.Fatal(err)
	}
	var recreatedGeneration, recreatedApplied int64
	var action, status string
	if err := database.QueryRow(`
		SELECT desired_generation, applied_generation, desired_action, status
		FROM dns_zone_sync_state
		WHERE zone_name = 'resurrect.example'
	`).Scan(
		&recreatedGeneration, &recreatedApplied, &action, &status,
	); err != nil {
		t.Fatal(err)
	}
	if recreatedGeneration != deletedGeneration+1 || recreatedApplied != 0 ||
		action != "sync" || status != "pending" {
		t.Fatalf("recreated state=%d/%d/%s/%s, want %d/0/sync/pending",
			recreatedGeneration, recreatedApplied, action, status,
			deletedGeneration+1)
	}
}

func TestDNSZoneGenerationHighWaterSeedsFromExistingEngineApplications(t *testing.T) {
	database := newPreDNSZoneGenerationHighWaterMigrationDB(t)
	if _, err := database.Exec(`
		INSERT INTO dns_zone_engine_applications (
		  zone_name, engine, engine_epoch, applied_generation,
		  applied_action, applied_zone_type, qualifier,
		  mutation_request_id, mutation_owner_id, revision
		) VALUES (
		  'receipt-only.example', 'pdns', 4, 19, 'delete', 'NATIVE',
		  'dns-zone-sync/v3:sha256:' || lower(hex(randomblob(32))),
		  lower(hex(randomblob(16))), lower(hex(randomblob(16))), 1
		)
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		INSERT INTO pdns_domains(name, type)
		VALUES ('receipt-only.example', 'NATIVE')
	`); err != nil {
		t.Fatal(err)
	}
	var stuckGeneration int64
	if err := database.QueryRow(`
		SELECT desired_generation FROM dns_zone_sync_state
		WHERE zone_name = 'receipt-only.example'
	`).Scan(&stuckGeneration); err != nil {
		t.Fatal(err)
	}
	if stuckGeneration != 1 {
		t.Fatalf("pre-035 fixture generation=%d, want stuck generation 1",
			stuckGeneration)
	}
	migrations, err := loadEmbeddedMigrations()
	if err != nil {
		t.Fatal(err)
	}
	for _, migration := range migrations {
		if migration.version != 35 {
			continue
		}
		if _, err := database.Exec(string(migration.content)); err != nil {
			t.Fatalf("apply migration 035: %v", err)
		}
		break
	}
	var generation, highWater int64
	var action, status string
	if err := database.QueryRow(`
		SELECT state.desired_generation, state.desired_action, state.status,
		       high_water.generation
		FROM dns_zone_sync_state AS state
		JOIN dns_zone_generation_high_water AS high_water
		  ON high_water.zone_name = state.zone_name
		WHERE state.zone_name = 'receipt-only.example'
	`).Scan(&generation, &action, &status, &highWater); err != nil {
		t.Fatal(err)
	}
	if generation != 20 || highWater != 20 || action != "sync" || status != "pending" {
		t.Fatalf("receipt-seeded migration state=%d/%d/%s/%s, want 20/20/sync/pending",
			generation, highWater, action, status)
	}
}

func newPreDNSZoneGenerationHighWaterMigrationDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := sql.Open(
		"sqlite",
		filepath.Join(t.TempDir(), "pre-035.sqlite")+
			"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)",
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("close pre-035 database: %v", err)
		}
	})
	migrations, err := loadEmbeddedMigrations()
	if err != nil {
		t.Fatal(err)
	}
	for _, migration := range migrations {
		if migration.version >= 35 {
			break
		}
		if _, err := database.Exec(string(migration.content)); err != nil {
			t.Fatalf("apply pre-035 migration %s: %v", migration.filename, err)
		}
	}
	return database
}
