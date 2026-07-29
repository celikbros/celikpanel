package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestExact17SnapshotMigrateAndRestoreRoundTrip(t *testing.T) {
	root := t.TempDir()
	databasePath := filepath.Join(root, "celikpanel.db")
	createDatabaseAtVersion(t, databasePath, sourceSchemaVersion)

	if err := checkDatabaseFile(databasePath, sourceSchemaVersion); err != nil {
		t.Fatalf("check exact schema 17: %v", err)
	}
	snapshotPath := filepath.Join(root, "snapshot", "celikpanel.db")
	if err := os.Mkdir(filepath.Dir(snapshotPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := createSnapshot(databasePath, snapshotPath); err != nil {
		t.Fatalf("create exact schema-17 snapshot: %v", err)
	}
	if err := checkStandaloneSQLite(snapshotPath); err != nil {
		t.Fatalf("snapshot is not standalone: %v", err)
	}
	if err := checkDatabaseFile(snapshotPath, sourceSchemaVersion); err != nil {
		t.Fatalf("snapshot is not exact schema 17: %v", err)
	}

	if err := migrate(databasePath, migrationRoot(t)); err != nil {
		t.Fatalf("bridge exact schema 17 to exact schema 20: %v", err)
	}
	if err := checkDatabaseFile(databasePath, bridgeSchemaVersion); err != nil {
		t.Fatalf("migrated database is not exact schema 20: %v", err)
	}
	if err := checkDatabaseFile(snapshotPath, sourceSchemaVersion); err != nil {
		t.Fatalf("migration changed the snapshot: %v", err)
	}

	if err := restoreSnapshot(databasePath, snapshotPath); err != nil {
		t.Fatalf("restore exact schema-17 snapshot: %v", err)
	}
	if err := checkDatabaseFile(databasePath, sourceSchemaVersion); err != nil {
		t.Fatalf("restored database is not exact schema 17: %v", err)
	}
}

func TestExact17CheckRejectsUnknownNewerGappedAndPartialShapes(t *testing.T) {
	t.Run("newer ledger", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "panel.db")
		createDatabaseAtVersion(t, path, 18)
		err := checkDatabaseFile(path, sourceSchemaVersion)
		if err == nil || !strings.Contains(err.Error(), "exact schema 17") {
			t.Fatalf("newer database error = %v", err)
		}
	})

	t.Run("gapped ledger", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "panel.db")
		createDatabaseAtVersion(t, path, sourceSchemaVersion)
		execSQLite(t, path, `DELETE FROM schema_migrations WHERE version = 9`)
		err := checkDatabaseFile(path, sourceSchemaVersion)
		if err == nil || !strings.Contains(err.Error(), "not contiguous") {
			t.Fatalf("gapped database error = %v", err)
		}
	})

	t.Run("partial object", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "panel.db")
		createDatabaseAtVersion(t, path, sourceSchemaVersion)
		execSQLite(t, path, `CREATE TABLE hostname_reservations (hostname TEXT)`)
		err := checkDatabaseFile(path, sourceSchemaVersion)
		if err == nil || !strings.Contains(err.Error(), "partial post-17") {
			t.Fatalf("partial object error = %v", err)
		}
	})

	t.Run("partial column", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "panel.db")
		createDatabaseAtVersion(t, path, sourceSchemaVersion)
		execSQLite(t, path, `ALTER TABLE sites ADD COLUMN hsts_retire_after TEXT`)
		err := checkDatabaseFile(path, sourceSchemaVersion)
		if err == nil || !strings.Contains(err.Error(), "partial post-17 column") {
			t.Fatalf("partial column error = %v", err)
		}
	})

	t.Run("partial newer store object", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "panel.db")
		createDatabaseAtVersion(t, path, sourceSchemaVersion)
		execSQLite(t, path, `CREATE TABLE store_offerings (id TEXT PRIMARY KEY)`)
		err := checkDatabaseFile(path, sourceSchemaVersion)
		if err == nil || !strings.Contains(err.Error(), "partial post-17") {
			t.Fatalf("partial newer object error = %v", err)
		}
	})

	t.Run("partial newer VPN column", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "panel.db")
		createDatabaseAtVersion(t, path, sourceSchemaVersion)
		execSQLite(t, path, `ALTER TABLE vpn_peers ADD COLUMN desired_state TEXT`)
		err := checkDatabaseFile(path, sourceSchemaVersion)
		if err == nil || !strings.Contains(err.Error(), "partial post-17 column") {
			t.Fatalf("partial newer column error = %v", err)
		}
	})
}

func TestExact20CheckRejectsPartialLaterShapes(t *testing.T) {
	t.Run("partial store object", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "panel.db")
		createDatabaseAtVersion(t, path, bridgeSchemaVersion)
		execSQLite(t, path, `CREATE TABLE store_offerings (id TEXT PRIMARY KEY)`)
		err := checkDatabaseFile(path, bridgeSchemaVersion)
		if err == nil || !strings.Contains(err.Error(), "partial post-bridge") {
			t.Fatalf("partial post-bridge object error = %v", err)
		}
	})

	t.Run("partial VPN column", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "panel.db")
		createDatabaseAtVersion(t, path, bridgeSchemaVersion)
		execSQLite(t, path, `ALTER TABLE vpn_peers ADD COLUMN desired_state TEXT`)
		err := checkDatabaseFile(path, bridgeSchemaVersion)
		if err == nil || !strings.Contains(err.Error(), "partial post-bridge column") {
			t.Fatalf("partial post-bridge column error = %v", err)
		}
	})
}

func TestBridgeMigrationFailureLeavesExact17Database(t *testing.T) {
	path := filepath.Join(t.TempDir(), "panel.db")
	createDatabaseAtVersion(t, path, sourceSchemaVersion)
	database := openTestDatabase(t, path)

	userResult, err := database.Exec(`
		INSERT INTO users (username, password_hash, email, role)
		VALUES ('owner', 'hash', 'owner@example.test', 'customer')`)
	if err != nil {
		t.Fatal(err)
	}
	userID, err := userResult.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	subscriptionResult, err := database.Exec(
		`INSERT INTO subscriptions (owner_id, name) VALUES (?, 'Bridge fixture')`,
		userID,
	)
	if err != nil {
		t.Fatal(err)
	}
	subscriptionID, err := subscriptionResult.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	domainResult, err := database.Exec(
		`INSERT INTO domains (subscription_id, name) VALUES (?, 'owner.example')`,
		subscriptionID,
	)
	if err != nil {
		t.Fatal(err)
	}
	domainID, err := domainResult.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(
		`INSERT INTO domain_aliases (domain_id, alias) VALUES (?, 'OWNER.EXAMPLE')`,
		domainID,
	); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	err = migrate(path, migrationRoot(t))
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "hostname namespace conflict") {
		t.Fatalf("bridge collision error = %v", err)
	}
	if err := checkDatabaseFile(path, sourceSchemaVersion); err != nil {
		t.Fatalf("failed bridge did not roll back to exact schema 17: %v", err)
	}
}

func TestSnapshotAndRestoreRefuseUnsafeTargets(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "panel.db")
	createDatabaseAtVersion(t, path, sourceSchemaVersion)

	existingOutput := filepath.Join(root, "existing.db")
	if err := os.WriteFile(existingOutput, []byte("do not overwrite"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := createSnapshot(path, existingOutput); err == nil ||
		!strings.Contains(err.Error(), "already exists") {
		t.Fatalf("existing output error = %v", err)
	}
	content, err := os.ReadFile(existingOutput)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "do not overwrite" {
		t.Fatal("snapshot overwrote an existing output")
	}

	newerSnapshot := filepath.Join(root, "newer.db")
	createDatabaseAtVersion(t, newerSnapshot, bridgeSchemaVersion)
	if err := restoreSnapshot(path, newerSnapshot); err == nil ||
		!strings.Contains(err.Error(), "exact schema 17") {
		t.Fatalf("newer restore snapshot error = %v", err)
	}
	if err := checkDatabaseFile(path, sourceSchemaVersion); err != nil {
		t.Fatalf("rejected restore changed destination: %v", err)
	}
}

func TestCommandParserIsClosedOverFourExactOperations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "panel.db")
	createDatabaseAtVersion(t, path, sourceSchemaVersion)
	if err := run([]string{"check", "--db", path}); err != nil {
		t.Fatalf("run check: %v", err)
	}
	for _, args := range [][]string{
		nil,
		{"check"},
		{"check", "--db", path, "extra"},
		{"migrate", "--db", path},
		{"restore", "--db", path},
		{"shell", "--db", path},
	} {
		if err := run(args); err == nil || !strings.Contains(err.Error(), "usage:") {
			t.Fatalf("run(%q) error = %v", args, err)
		}
	}
}

func migrationRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "internal", "db", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Clean(root)
}

func createDatabaseAtVersion(t *testing.T, path string, targetVersion int) {
	t.Helper()
	database := openTestDatabase(t, path)
	defer database.Close()
	if _, err := database.Exec(`
		CREATE TABLE schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at TEXT DEFAULT (datetime('now'))
		)`); err != nil {
		t.Fatalf("create migration ledger: %v", err)
	}
	entries, err := os.ReadDir(migrationRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	applied := 0
	for _, name := range names {
		var version int
		if _, err := fmt.Sscanf(name, "%d_", &version); err != nil {
			t.Fatalf("parse migration name %q: %v", name, err)
		}
		if version > targetVersion {
			continue
		}
		content, err := os.ReadFile(filepath.Join(migrationRoot(t), name))
		if err != nil {
			t.Fatal(err)
		}
		tx, err := database.BeginTx(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(string(content)); err != nil {
			_ = tx.Rollback()
			t.Fatalf("apply migration %d: %v", version, err)
		}
		if _, err := tx.Exec(
			`INSERT INTO schema_migrations (version) VALUES (?)`,
			version,
		); err != nil {
			_ = tx.Rollback()
			t.Fatalf("record migration %d: %v", version, err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("commit migration %d: %v", version, err)
		}
		applied = version
	}
	if applied != targetVersion {
		t.Fatalf("applied through migration %d, want %d", applied, targetVersion)
	}
}

func openTestDatabase(t *testing.T, path string) *sql.DB {
	t.Helper()
	database, err := sql.Open(
		"sqlite",
		fmt.Sprintf(
			"%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)",
			path,
		),
	)
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	if err := database.Ping(); err != nil {
		database.Close()
		t.Fatal(err)
	}
	return database
}

func execSQLite(t *testing.T, path, statement string) {
	t.Helper()
	database := openTestDatabase(t, path)
	defer database.Close()
	if _, err := database.Exec(statement); err != nil {
		t.Fatal(err)
	}
}

func TestNoUnexpectedErrorsPackageContract(t *testing.T) {
	// Keep errors imported for assertions that use errors.Is as the helper
	// evolves; this also documents that missing paths are classified, not
	// silently converted into empty databases.
	_, _, err := canonicalRegularFile(filepath.Join(t.TempDir(), "missing.db"), "database")
	if !errors.Is(err, os.ErrNotExist) && (err == nil || !strings.Contains(err.Error(), "inspect database")) {
		t.Fatalf("missing database error = %v", err)
	}
}
