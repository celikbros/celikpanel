//go:build linux

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	paneldb "github.com/alicelik/celikpanel/internal/db"
)

// This builds the exact production list, not the ordinary panel test binary.
// Fixtures apply real migration SQL up to the historical version under test.
func TestRecoveryCheckStandalone(t *testing.T) {
	repository, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(repository, "deploy/recovery/panel-checker.sources"))
	if err != nil {
		t.Fatal(err)
	}
	sources := strings.Fields(string(raw))
	seen := map[string]bool{}
	for _, source := range sources {
		if !strings.HasPrefix(source, "cmd/panel/") || strings.Contains(source, "..") ||
			!strings.HasSuffix(source, ".go") || strings.HasSuffix(source, "_test.go") ||
			source == "cmd/panel/main.go" || source == "cmd/panel/service_operations.go" || seen[source] {
			t.Fatalf("unsafe or duplicate recovery source %q", source)
		}
		seen[source] = true
	}
	if !seen["cmd/panel/recovery_check_entry.go"] || !seen["cmd/panel/service_operation_restore_linux.go"] || !seen["cmd/panel/recovery_completion_database.go"] {
		t.Fatal("standalone entry or real guarded restoration implementation is absent")
	}
	for _, source := range []string{"cmd/panel/recovery_database_migration_linux.go", "cmd/panel/recovery_database_migration_files_linux.go", "cmd/panel/recovery_database_migration_publication_linux.go"} {
		if !seen[source] {
			t.Fatalf("real isolated database recovery source is missing: %s", source)
		}
	}
	entry, err := os.ReadFile(filepath.Join(repository, "cmd/panel/recovery_check_entry.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{serviceOperationQueued, serviceOperationRunning} {
		if !bytes.Contains(entry, []byte(strconv.Quote(value))) {
			t.Fatalf("persisted queue value %q differs from standalone contract", value)
		}
	}
	binary := filepath.Join(t.TempDir(), "recovery-panel-checker")
	build := exec.Command("go", append([]string{"build", "-o", binary}, sources...)...)
	build.Dir = repository
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build standalone checker: %v\n%s", err, output)
	}
	run := func(directory string, wantSuccess bool, args ...string) string {
		t.Helper()
		command := exec.Command(binary, args...)
		command.Env = append(os.Environ(), "CELIKPANEL_DATA_DIR="+directory)
		output, err := command.CombinedOutput()
		if (err == nil) != wantSuccess {
			t.Fatalf("checker %v success=%v expected=%v: %v\n%s", args, err == nil, wantSuccess, err, output)
		}
		return string(output)
	}
	for _, version := range []int{20, 28, 31} {
		t.Run(fmt.Sprintf("real-schema-%d", version), func(t *testing.T) {
			directory := t.TempDir()
			path := filepath.Join(directory, "celikpanel.db")
			seedRecoveryCheckerDatabase(t, repository, path, version)
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			mode := "--check-service-operations-idle"
			if version == 20 {
				mode = "--check-pre-ledger-service-operations-idle"
			}
			run(directory, true, mode)
			run(directory, true, mode+"-wal-aware")
			run(directory, false, "--check-completed-update-database-wal-aware")
			if version == 20 {
				run(directory, false, "--check-service-operations-idle")
			} else {
				run(directory, false, "--check-pre-ledger-service-operations-idle")
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Fatal("read-only checker changed source database bytes")
			}
			for _, suffix := range []string{"-wal", "-shm", "-journal"} {
				if _, err := os.Stat(path + suffix); !os.IsNotExist(err) {
					t.Fatalf("unexpected source sidecar %s: %v", suffix, err)
				}
			}
		})
	}
	t.Run("queued-only-in-wal", func(t *testing.T) {
		directory := t.TempDir()
		path := filepath.Join(directory, "celikpanel.db")
		seedRecoveryCheckerDatabase(t, repository, path, 31)
		database, err := sql.Open("sqlite", path)
		if err != nil {
			t.Fatal(err)
		}
		defer database.Close()
		database.SetMaxOpenConns(1)
		if _, err := database.Exec(`PRAGMA journal_mode=WAL; PRAGMA wal_autocheckpoint=0;`); err != nil {
			t.Fatal(err)
		}
		if _, err := database.Exec(`INSERT INTO service_operations(id,kind,service_id,status,phase,started_at,created_at,updated_at,request_id) VALUES('queued-fixture','service_install','nginx','queued','queued','2026-09-14','2026-09-14','2026-09-14','queued-fixture')`); err != nil {
			t.Fatal(err)
		}
		before := captureRecoveryCheckerSQLiteBytes(t, path)
		if len(before["-wal"]) == 0 {
			t.Fatal("fixture did not retain a real WAL")
		}
		run(directory, false, "--check-service-operations-idle-wal-aware")
		run(directory, false, "--check-service-operations-idle")
		after := captureRecoveryCheckerSQLiteBytes(t, path)
		for suffix, content := range before {
			if !bytes.Equal(content, after[suffix]) {
				t.Fatalf("checker changed source%s", suffix)
			}
		}
		if _, err := database.Exec(`UPDATE service_operations SET status='failed',finished_at='2026-09-14' WHERE id='queued-fixture'`); err != nil {
			t.Fatal(err)
		}
		run(directory, true, "--check-service-operations-idle-wal-aware")
	})
	t.Run("completed-latest-schema", func(t *testing.T) {
		latest, err := paneldb.HighestEmbeddedMigrationVersion()
		if err != nil {
			t.Fatal(err)
		}
		directory := t.TempDir()
		path := filepath.Join(directory, "celikpanel.db")
		seedRecoveryCheckerDatabase(t, repository, path, latest)
		before := captureRecoveryCheckerSQLiteBytes(t, path)
		run(directory, true, "--check-completed-update-database-wal-aware")
		run(directory, false, "--check-completed-update-database-wal-aware", "--check-service-operations-idle")
		run(directory, false, "--check-completed-update-database-wal-aware", "--check-completed-update-database-wal-aware")
		run(directory, false, "--check-completed-update-database-wal-aware", "--release-transaction-fd=9")
		after := captureRecoveryCheckerSQLiteBytes(t, path)
		for suffix, content := range before {
			if !bytes.Equal(content, after[suffix]) {
				t.Fatalf("completion reader changed source%s", suffix)
			}
		}
	})
	t.Run("no-startup-or-unguarded-restore", func(t *testing.T) {
		directory := t.TempDir()
		path := filepath.Join(directory, "celikpanel.db")
		seedRecoveryCheckerDatabase(t, repository, path, 31)
		before, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, args := range [][]string{
			{}, {"--migrate-only"}, {"--create-admin"}, {"--help"},
			{"--check-service-operations-idle", "--check-service-operations-idle"},
			{"--check-service-operations-idle", "--release-transaction-fd=9"},
			{"--check-service-operations-idle", "--check-pre-ledger-service-operations-idle"},
			{"--restore-service-operation-snapshot=" + path, "--snapshot-schema=normal", "--release-transaction-fd=2", "--release-transaction-token=" + strings.Repeat("a", 64), "--release-transaction-operation=rollback", "--release-transaction-snapshot=fixture"},
			{"--restore-service-operation-snapshot=" + path, "--snapshot-schema=normal", "--release-transaction-fd=9", "--release-transaction-token=" + strings.Repeat("a", 64), "--release-transaction-operation=rollback", "--release-transaction-snapshot=fixture"},
		} {
			run(directory, false, args...)
		}
		for _, mode := range []string{"--create-service-operation-snapshot=", "--ensure-service-operation-rescue-snapshot="} {
			for _, operation := range []string{"rollback", "update"} {
				run(directory, false, mode+path, "--snapshot-schema=normal", "--release-transaction-fd=9", "--release-transaction-token="+strings.Repeat("a", 64), "--release-transaction-operation="+operation, "--release-transaction-snapshot=fixture")
			}
		}
		for _, mode := range []string{"prepare-update-database", "publish-update-database", "restore-update-database", "verify-update-database"} {
			// Correctly shaped names still cannot create authority from flags,
			// a caller-selected data directory, or an absent inherited lock.
			exact := "20260915T010000Z-from-unknown-to-" + strings.Repeat("a", 40) + "-" + strings.Repeat("b", 32)
			run(directory, false, "--"+mode+"="+exact)
			run(directory, false, "--"+mode+"="+exact, "--release-transaction-fd=9")
			run(directory, false, "--"+mode+"="+exact, "--"+mode+"="+exact)
			run(directory, false, "--"+mode+"="+path)
			run(directory, false, "--"+mode+"="+exact, "--migrate-only")
		}
		after, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(before, after) {
			t.Fatal("rejected command changed canonical database")
		}
		missing := filepath.Join(directory, "missing")
		run(missing, false, "--check-service-operations-idle")
		if _, err := os.Stat(missing); !os.IsNotExist(err) {
			t.Fatalf("checker started database initialization: %v", err)
		}
	})
}

func captureRecoveryCheckerSQLiteBytes(t *testing.T, path string) map[string][]byte {
	t.Helper()
	result := map[string][]byte{}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		raw, err := os.ReadFile(path + suffix)
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		result[suffix] = raw
	}
	return result
}

func seedRecoveryCheckerDatabase(t *testing.T, repository, path string, version int) {
	t.Helper()
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	database.SetMaxOpenConns(1)
	// Match production migration execution. Placeholder deletion must apply
	// its foreign-key actions instead of leaving invalid historical fixtures.
	if _, err := database.Exec(`PRAGMA foreign_keys=ON`); err != nil {
		t.Fatal(err)
	}
	// Use the writer-owned table SQL, including its exact durable schema text.
	reference, err := paneldb.ReferenceSQLiteUserSchema(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	ledgerSQL := ""
	for _, object := range reference {
		if object.Type == "table" && object.Name == "schema_migrations" {
			ledgerSQL = object.SQL
		}
	}
	if ledgerSQL == "" {
		t.Fatal("reference migration ledger is absent")
	}
	if _, err := database.Exec(ledgerSQL); err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(repository, "internal/db/migrations/*.sql"))
	if err != nil {
		t.Fatal(err)
	}
	applied := 0
	for _, path := range files {
		name := filepath.Base(path)
		number, err := strconv.Atoi(strings.SplitN(name, "_", 2)[0])
		if err != nil {
			t.Fatal(err)
		}
		if number > version {
			break
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		transaction, err := database.Begin()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := transaction.Exec(string(raw)); err != nil {
			transaction.Rollback()
			t.Fatalf("migration %s: %v", name, err)
		}
		if _, err := transaction.Exec(`INSERT INTO schema_migrations(version,filename,sha256) VALUES(?,?,?)`, number, name, fmt.Sprintf("%x", sha256.Sum256(raw))); err != nil {
			transaction.Rollback()
			t.Fatal(err)
		}
		if err := transaction.Commit(); err != nil {
			t.Fatal(err)
		}
		applied++
	}
	if applied != version {
		t.Fatalf("fixture applied %d migrations, expected %d", applied, version)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
}
