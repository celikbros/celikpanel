package db

import (
	"path/filepath"
	"strings"
	"testing"
)

// Migration 44 (D-031): one ledger row per file the Panel writes for a site.
// Göç 44 (D-031): Panel'in bir site için yazdığı her dosya için bir defter satırı.

const managedSiteFilesMigrationVersion = 44

func seedManagedSiteFilesSite(t *testing.T, database *SQLiteDB) (siteID, domainID int64) {
	t.Helper()
	db := database.GetDB()
	result, err := db.Exec(`INSERT INTO users (username, password_hash, email, role) VALUES ('owner44', 'h', 'owner44@example.test', 'admin')`)
	if err != nil {
		t.Fatal(err)
	}
	userID, _ := result.LastInsertId()
	result, err = db.Exec(`INSERT INTO subscriptions (owner_id, name) VALUES (?, 'sub44')`, userID)
	if err != nil {
		t.Fatalf("subscription: %v", err)
	}
	subscriptionID, _ := result.LastInsertId()
	result, err = db.Exec(`INSERT INTO domains (subscription_id, name) VALUES (?, 'site44.example')`, subscriptionID)
	if err != nil {
		t.Fatalf("domain: %v", err)
	}
	domainID, _ = result.LastInsertId()
	result, err = db.Exec(`INSERT INTO sites (domain_id, document_root) VALUES (?, '/var/www/x')`, domainID)
	if err != nil {
		t.Fatalf("site: %v", err)
	}
	siteID, _ = result.LastInsertId()
	return siteID, domainID
}

func TestManagedSiteFilesMigrationContracts(t *testing.T) {
	database, err := NewSQLiteDB(filepath.Join(t.TempDir(), "panel.sqlite"))
	if err != nil {
		t.Fatalf("open migrated database: %v", err)
	}
	t.Cleanup(database.Close)
	db := database.GetDB()

	var filename string
	if err := db.QueryRow(`SELECT filename FROM schema_migrations WHERE version = ?`,
		managedSiteFilesMigrationVersion).Scan(&filename); err != nil {
		t.Fatalf("ledger entry %d: %v", managedSiteFilesMigrationVersion, err)
	}
	if filename != "044_managed_site_files.sql" {
		t.Fatalf("ledger filename=%q", filename)
	}

	siteID, domainID := seedManagedSiteFilesSite(t, database)
	digest := strings.Repeat("a", 64)
	insert := `INSERT INTO managed_site_files (site_id, domain_id, kind, path, state, body_sha256)
		VALUES (?, ?, ?, ?, ?, ?)`
	if _, err := db.Exec(insert, siteID, domainID, "nginx_vhost", "/etc/nginx/sites-available/site44.example.conf", "managed_unchanged", digest); err != nil {
		t.Fatalf("insert: %v", err)
	}
	// Defaults: nothing decided, nothing pending, nothing kept aside.
	var format, release, decision, pending, backup string
	var writtenAt, decidedAt int64
	if err := db.QueryRow(`SELECT file_format, written_release, written_at, decision, decided_at, pending_path, backup_path
		FROM managed_site_files WHERE site_id = ?`, siteID).Scan(&format, &release, &writtenAt, &decision, &decidedAt, &pending, &backup); err != nil {
		t.Fatal(err)
	}
	if format != "" || release != "" || writtenAt != 0 || decision != "" || decidedAt != 0 || pending != "" || backup != "" {
		t.Fatalf("defaults: %q %q %d %q %d %q %q", format, release, writtenAt, decision, decidedAt, pending, backup)
	}

	for name, args := range map[string][]any{
		"the same file twice": {siteID, domainID, "nginx_vhost", "/etc/nginx/sites-available/site44.example.conf", "managed_unchanged", digest},
		"an unknown kind":     {siteID, domainID, "apache_vhost", "/x", "managed_unchanged", ""},
		"an unknown state":    {siteID, domainID, "php_pool", "/y", "edited", ""},
		"a short digest":      {siteID, domainID, "php_pool", "/z", "managed_unchanged", strings.Repeat("a", 63)},
		"an empty path":       {siteID, domainID, "php_pool", "", "managed_unchanged", ""},
		"a site that is not":  {siteID + 100, domainID, "php_pool", "/w", "managed_unchanged", ""},
	} {
		if _, err := db.Exec(insert, args...); err == nil {
			t.Fatalf("%s was accepted", name)
		}
	}
	for _, statement := range []string{
		`UPDATE managed_site_files SET decision = 'overwrite' WHERE site_id = ?`,
		`UPDATE managed_site_files SET file_format = 'v1' WHERE site_id = ?`,
		`UPDATE managed_site_files SET pending_sha256 = 'x' WHERE site_id = ?`,
	} {
		if _, err := db.Exec(statement, siteID); err == nil {
			t.Fatalf("accepted: %s", statement)
		}
	}
	for _, state := range []string{"absent", "owner_edited", "foreign", "unreadable", "unknown_origin", "missing", "unknown"} {
		if _, err := db.Exec(`UPDATE managed_site_files SET state = ? WHERE site_id = ?`, state, siteID); err != nil {
			t.Fatalf("state %q: %v", state, err)
		}
	}
	for _, decision := range []string{"keep_mine", "take_celikpanel", "recreate", ""} {
		if _, err := db.Exec(`UPDATE managed_site_files SET decision = ? WHERE site_id = ?`, decision, siteID); err != nil {
			t.Fatalf("decision %q: %v", decision, err)
		}
	}

	// Deleting the site removes its rows; the files are the Agent's business.
	if _, err := db.Exec(`DELETE FROM sites WHERE id = ?`, siteID); err != nil {
		t.Fatal(err)
	}
	var rows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM managed_site_files`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("rows after the site was deleted: %d", rows)
	}

	var index string
	if err := db.QueryRow(`SELECT name FROM sqlite_master
		WHERE type = 'index' AND tbl_name = 'managed_site_files' AND name = 'managed_site_files_domain'`).Scan(&index); err != nil {
		t.Fatalf("domain index: %v", err)
	}
}

// A database at ledger entry 43 (the published alpha.82 schema) gains the
// empty table on the next start and keeps everything else; a second start
// changes nothing. The migration writes no row: the files are classified at
// the first start that renders them.
func TestManagedSiteFilesMigrationAppliesToASchema43Database(t *testing.T) {
	path := filepath.Join(t.TempDir(), "panel.sqlite")
	database, err := NewSQLiteDB(path)
	if err != nil {
		t.Fatal(err)
	}
	siteID, _ := seedManagedSiteFilesSite(t, database)
	db := database.GetDB()
	if _, err := db.Exec(`DROP TABLE managed_site_files`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM schema_migrations WHERE version = ?`, managedSiteFilesMigrationVersion); err != nil {
		t.Fatal(err)
	}
	database.Close()

	for start := 0; start < 2; start++ {
		database, err = NewSQLiteDB(path)
		if err != nil {
			t.Fatalf("start %d: %v", start, err)
		}
		db = database.GetDB()
		var rows, sites, ledger int
		if err := db.QueryRow(`SELECT COUNT(*) FROM managed_site_files`).Scan(&rows); err != nil {
			t.Fatalf("start %d: table missing: %v", start, err)
		}
		if err := db.QueryRow(`SELECT COUNT(*) FROM sites WHERE id = ?`, siteID).Scan(&sites); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = ?`,
			managedSiteFilesMigrationVersion).Scan(&ledger); err != nil {
			t.Fatal(err)
		}
		if rows != 0 || sites != 1 || ledger != 1 {
			t.Fatalf("start %d: rows=%d sites=%d ledger=%d", start, rows, sites, ledger)
		}
		database.Close()
	}
}

// The published alpha.82 (schema 43) refuses a database that carries ledger
// entry 44; a return to it is the pre-update snapshot restore, as for every
// earlier migration, and with it the protection of D-031 is gone (the release
// notes say so).
func TestOlderReleaseRefusesALedgerWithTheManagedSiteFilesEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "panel.sqlite")
	database, err := NewSQLiteDB(path)
	if err != nil {
		t.Fatal(err)
	}
	embedded, err := loadEmbeddedMigrations()
	if err != nil {
		t.Fatal(err)
	}
	older := make([]embeddedMigration, 0, len(embedded))
	for _, migration := range embedded {
		if migration.version < managedSiteFilesMigrationVersion {
			older = append(older, migration)
		}
	}
	if len(older) != len(embedded)-1 {
		t.Fatalf("migration %d is not the newest embedded migration", managedSiteFilesMigrationVersion)
	}
	err = database.verifyMigrationLedgerCoverage(t.Context(), older)
	database.Close()
	if err == nil || !strings.Contains(err.Error(), "no matching embedded migration") {
		t.Fatalf("an older release accepted the newer ledger: %v", err)
	}
}
