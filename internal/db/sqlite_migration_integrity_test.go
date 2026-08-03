package db

import (
	"strings"
	"testing"
)

func TestMigrationLedgerRecordsEmbeddedIdentity(t *testing.T) {
	database := newMigrationIntegrityTestDB(t)
	migrations, err := loadEmbeddedMigrations()
	if err != nil {
		t.Fatalf("load embedded migrations: %v", err)
	}
	if len(migrations) == 0 {
		t.Fatal("no embedded migrations")
	}

	var filename, digest string
	if err := database.db.QueryRow(
		`SELECT filename, sha256 FROM schema_migrations WHERE version = ?`,
		migrations[0].version,
	).Scan(&filename, &digest); err != nil {
		t.Fatalf("read migration identity: %v", err)
	}
	if filename != migrations[0].filename {
		t.Fatalf("filename = %q, want %q", filename, migrations[0].filename)
	}
	if digest != migrations[0].sha256 {
		t.Fatalf("sha256 = %q, want %q", digest, migrations[0].sha256)
	}
}

func TestMigrationLedgerBackfillsLegacyIdentity(t *testing.T) {
	database := newMigrationIntegrityTestDB(t)
	migrations, err := loadEmbeddedMigrations()
	if err != nil {
		t.Fatalf("load embedded migrations: %v", err)
	}
	target := migrations[0]

	if _, err := database.db.Exec(
		`UPDATE schema_migrations SET filename = NULL, sha256 = NULL WHERE version = ?`,
		target.version,
	); err != nil {
		t.Fatalf("simulate legacy migration row: %v", err)
	}
	if err := database.RunMigrations(); err != nil {
		t.Fatalf("backfill legacy migration identity: %v", err)
	}

	var filename, digest string
	if err := database.db.QueryRow(
		`SELECT filename, sha256 FROM schema_migrations WHERE version = ?`,
		target.version,
	).Scan(&filename, &digest); err != nil {
		t.Fatalf("read backfilled migration identity: %v", err)
	}
	if filename != target.filename || digest != target.sha256 {
		t.Fatalf(
			"backfilled identity = %q/%q, want %q/%q",
			filename,
			digest,
			target.filename,
			target.sha256,
		)
	}
}

func TestMigrationLedgerRejectsPublishedMigrationIdentityChanges(t *testing.T) {
	tests := []struct {
		name   string
		update string
		want   string
	}{
		{
			name:   "filename",
			update: `UPDATE schema_migrations SET filename = '001_rewritten.sql' WHERE version = 1`,
			want:   "migration integrity mismatch",
		},
		{
			name:   "digest",
			update: `UPDATE schema_migrations SET sha256 = 'deadbeef' WHERE version = 1`,
			want:   "migration integrity mismatch",
		},
		{
			name:   "partial identity",
			update: `UPDATE schema_migrations SET filename = NULL WHERE version = 1`,
			want:   "incomplete identity",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			database := newMigrationIntegrityTestDB(t)
			if _, err := database.db.Exec(test.update); err != nil {
				t.Fatalf("tamper with migration ledger: %v", err)
			}
			err := database.RunMigrations()
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("RunMigrations() error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestMigrationLedgerRejectsAppliedVersionMissingFromRelease(t *testing.T) {
	database := newMigrationIntegrityTestDB(t)
	if _, err := database.db.Exec(`
		INSERT INTO schema_migrations (version, filename, sha256)
		VALUES (999, '999_removed.sql', 'deadbeef')`); err != nil {
		t.Fatalf("insert unknown applied migration: %v", err)
	}

	err := database.RunMigrations()
	if err == nil || !strings.Contains(err.Error(), "no matching embedded migration") {
		t.Fatalf(
			"RunMigrations() error = %v, want missing embedded migration failure",
			err,
		)
	}
}

func TestParseMigrationFilename(t *testing.T) {
	version, err := parseMigrationFilename("027_backup_schedule_job_key.sql")
	if err != nil {
		t.Fatalf("parse valid migration filename: %v", err)
	}
	if version != 27 {
		t.Fatalf("version = %d, want 27", version)
	}

	for _, filename := range []string{
		"migration.sql",
		"0_initial.sql",
		"-1_initial.sql",
		"001_.sql",
		"_initial.sql",
		"001_initial.txt",
	} {
		t.Run(filename, func(t *testing.T) {
			if _, err := parseMigrationFilename(filename); err == nil {
				t.Fatalf("parseMigrationFilename(%q) unexpectedly succeeded", filename)
			}
		})
	}
}

func newMigrationIntegrityTestDB(t *testing.T) *SQLiteDB {
	t.Helper()
	database, err := NewSQLiteDB(t.TempDir() + "/panel.sqlite")
	if err != nil {
		t.Fatalf("open migration integrity test database: %v", err)
	}
	t.Cleanup(database.Close)
	return database
}
