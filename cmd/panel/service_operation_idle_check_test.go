package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	paneldb "github.com/alicelik/celikpanel/internal/db"
)

func TestNormalizeSQLiteSchemaSQLPreservesTokenBoundaries(t *testing.T) {
	withBoundary := normalizeSQLiteSchemaSQL(`CREATE TABLE sample(value TEXT NOT NULL)`)
	withoutBoundary := normalizeSQLiteSchemaSQL(`CREATE TABLE sample(value TEXTNOT NULL)`)
	if withBoundary == withoutBoundary {
		t.Fatalf("semantic token boundary collapsed: %q", withBoundary)
	}
}

func TestNormalizeSQLiteSchemaSQLIgnoresWhitespaceAroundPunctuation(t *testing.T) {
	left := normalizeSQLiteSchemaSQL(`CREATE TABLE sample(value TEXT NOT NULL , other INTEGER )`)
	right := normalizeSQLiteSchemaSQL(`CREATE TABLE sample ( value TEXT NOT NULL,other INTEGER)`)
	if left != right {
		t.Fatalf("left=%q right=%q", left, right)
	}
}

func TestServiceOperationsIdleCheckIsReadOnlyAndFailClosed(t *testing.T) {
	t.Run("idle database", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "panel.sqlite")
		database, err := paneldb.NewSQLiteDB(path)
		if err != nil {
			t.Fatal(err)
		}
		database.Close()
		if err := checkServiceOperationsIdle(path); err != nil {
			t.Fatalf("idle database rejected: %v", err)
		}
	})

	t.Run("queued operation", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "panel.sqlite")
		database, err := paneldb.NewSQLiteDB(path)
		if err != nil {
			t.Fatal(err)
		}
		panel := &Panel{db: database}
		if _, err := panel.createServiceOperation(
			context.Background(), serviceOperationKindInstall, "certbot", "", serviceOperationActor{},
		); err != nil {
			t.Fatal(err)
		}
		database.Close()
		if err := checkServiceOperationsIdle(path); !errors.Is(err, errServiceOperationsNotIdle) {
			t.Fatalf("queued operation err=%v want not idle", err)
		}
	})

	t.Run("terminal operation", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "panel.sqlite")
		database, err := paneldb.NewSQLiteDB(path)
		if err != nil {
			t.Fatal(err)
		}
		panel := &Panel{db: database}
		op, err := panel.createServiceOperation(
			context.Background(), serviceOperationKindInstall, "certbot", "", serviceOperationActor{},
		)
		if err != nil {
			t.Fatal(err)
		}
		if err := panel.markServiceOperationRunning(context.Background(), op.ID, "installing"); err != nil {
			t.Fatal(err)
		}
		if err := panel.finishServiceOperationSucceeded(
			context.Background(), op.ID, serviceOperationResult{"success": true},
		); err != nil {
			t.Fatal(err)
		}
		database.Close()
		if err := checkServiceOperationsIdle(path); err != nil {
			t.Fatalf("terminal database rejected: %v", err)
		}
	})

	t.Run("missing database", func(t *testing.T) {
		err := checkServiceOperationsIdle(filepath.Join(t.TempDir(), "missing.sqlite"))
		if !errors.Is(err, errServiceOperationsNotIdle) {
			t.Fatalf("missing database err=%v want not idle", err)
		}
	})

	t.Run("registered schema version 21", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "panel.sqlite")
		database, err := paneldb.NewSQLiteDB(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := database.GetDB().Exec(`DELETE FROM schema_migrations WHERE version = 22`); err != nil {
			database.Close()
			t.Fatal(err)
		}
		database.Close()
		if err := checkServiceOperationsIdle(path); !errors.Is(err, errServiceOperationsNotIdle) {
			t.Fatalf("version 21 database err=%v want not idle", err)
		}
	})

	t.Run("missing required index", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "panel.sqlite")
		database, err := paneldb.NewSQLiteDB(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := database.GetDB().Exec(`DROP INDEX idx_service_operations_recent`); err != nil {
			database.Close()
			t.Fatal(err)
		}
		database.Close()
		if err := checkServiceOperationsIdle(path); !errors.Is(err, errServiceOperationsNotIdle) {
			t.Fatalf("missing index err=%v want not idle", err)
		}
	})

	t.Run("invalid required index contract", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "panel.sqlite")
		database, err := paneldb.NewSQLiteDB(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := database.GetDB().Exec(`
			DROP INDEX idx_service_operations_recent;
			CREATE UNIQUE INDEX idx_service_operations_recent
			ON service_operations(started_at DESC);
		`); err != nil {
			database.Close()
			t.Fatal(err)
		}
		database.Close()
		if err := checkServiceOperationsIdle(path); !errors.Is(err, errServiceOperationsNotIdle) {
			t.Fatalf("invalid index contract err=%v want not idle", err)
		}
	})
	t.Run("same flags with wrong index column", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "panel.sqlite")
		database, err := paneldb.NewSQLiteDB(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := database.GetDB().Exec(`
			DROP INDEX idx_service_operations_recent;
			CREATE INDEX idx_service_operations_recent
			ON service_operations(updated_at DESC);
		`); err != nil {
			database.Close()
			t.Fatal(err)
		}
		database.Close()
		if err := checkServiceOperationsIdle(path); !errors.Is(err, errServiceOperationsNotIdle) {
			t.Fatalf("wrong index column err=%v want not idle", err)
		}
	})

	t.Run("same flags and keys with wrong partial predicate", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "panel.sqlite")
		database, err := paneldb.NewSQLiteDB(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := database.GetDB().Exec(`
			DROP INDEX idx_service_operations_request_id;
			CREATE UNIQUE INDEX idx_service_operations_request_id
			ON service_operations(request_id)
			WHERE request_id IS NULL;
		`); err != nil {
			database.Close()
			t.Fatal(err)
		}
		database.Close()
		if err := checkServiceOperationsIdle(path); !errors.Is(err, errServiceOperationsNotIdle) {
			t.Fatalf("wrong partial predicate err=%v want not idle", err)
		}
	})

	t.Run("wrong required column type", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "panel.sqlite")
		database, err := paneldb.NewSQLiteDB(path)
		if err != nil {
			t.Fatal(err)
		}
		replaceServiceOperationsTableForIdleCheck(
			t,
			database.GetDB(),
			"INTEGER",
			`status IN ('queued', 'running', 'succeeded', 'failed')`,
			`INTEGER REFERENCES users(id) ON DELETE SET NULL`,
		)
		database.Close()
		if err := checkServiceOperationsIdle(path); !errors.Is(err, errServiceOperationsNotIdle) {
			t.Fatalf("wrong column type err=%v want not idle", err)
		}
	})

	t.Run("status check accepts unknown active state", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "panel.sqlite")
		database, err := paneldb.NewSQLiteDB(path)
		if err != nil {
			t.Fatal(err)
		}
		replaceServiceOperationsTableForIdleCheck(
			t,
			database.GetDB(),
			"TEXT",
			`status IN ('queued', 'running', 'succeeded', 'failed', 'paused')`,
			`INTEGER REFERENCES users(id) ON DELETE SET NULL`,
		)
		database.Close()
		if err := checkServiceOperationsIdle(path); !errors.Is(err, errServiceOperationsNotIdle) {
			t.Fatalf("unknown active status contract err=%v want not idle", err)
		}
	})

	t.Run("wrong requested by foreign key action", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "panel.sqlite")
		database, err := paneldb.NewSQLiteDB(path)
		if err != nil {
			t.Fatal(err)
		}
		replaceServiceOperationsTableForIdleCheck(
			t,
			database.GetDB(),
			"TEXT",
			`status IN ('queued', 'running', 'succeeded', 'failed')`,
			`INTEGER REFERENCES users(id) ON DELETE CASCADE`,
		)
		database.Close()
		if err := checkServiceOperationsIdle(path); !errors.Is(err, errServiceOperationsNotIdle) {
			t.Fatalf("wrong foreign key action err=%v want not idle", err)
		}
	})
}

func TestServiceOperationsIdleAcceptsExactVersionedTableContracts(t *testing.T) {
	tests := []struct {
		name              string
		schemaVersion     int
		withOperationData bool
	}{
		{name: "schema 28 legacy", schemaVersion: 28},
		{name: "schema 30 legacy", schemaVersion: 30},
		{name: "schema 31 current", schemaVersion: 31, withOperationData: true},
		{name: "schema 32 current", schemaVersion: 32, withOperationData: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := createServiceOperationIdleContractDatabase(
				t,
				test.schemaVersion,
				test.withOperationData,
			)
			if err := checkServiceOperationsIdle(path); err != nil {
				t.Fatalf("exact schema %d idle database rejected: %v", test.schemaVersion, err)
			}
		})
	}
}

func TestServiceOperationsIdleRejectsVersionedTableContractMismatch(t *testing.T) {
	tests := []struct {
		name              string
		schemaVersion     int
		withOperationData bool
	}{
		{name: "legacy ledger with current table", schemaVersion: 28, withOperationData: true},
		{name: "current ledger with legacy table", schemaVersion: 32},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := createServiceOperationIdleContractDatabase(
				t,
				test.schemaVersion,
				test.withOperationData,
			)
			if err := checkServiceOperationsIdle(path); !errors.Is(err, errServiceOperationsNotIdle) {
				t.Fatalf("schema/table mismatch err=%v, want not idle", err)
			}
		})
	}
}

func TestServiceOperationsIdleLegacyContractStillChecksActiveRows(t *testing.T) {
	t.Run("terminal", func(t *testing.T) {
		path := createServiceOperationIdleContractDatabase(t, 28, false)
		insertServiceOperationIdleContractRow(t, path, "terminal", serviceOperationSucceeded)
		if err := checkServiceOperationsIdle(path); err != nil {
			t.Fatalf("legacy terminal operation rejected: %v", err)
		}
	})

	for _, status := range []string{serviceOperationQueued, serviceOperationRunning} {
		t.Run(status, func(t *testing.T) {
			path := createServiceOperationIdleContractDatabase(t, 28, false)
			insertServiceOperationIdleContractRow(t, path, "active-"+status, status)
			if err := checkServiceOperationsIdle(path); !errors.Is(err, errServiceOperationsNotIdle) {
				t.Fatalf("legacy %s operation err=%v, want not idle", status, err)
			}
		})
	}
}

func createServiceOperationIdleContractDatabase(
	t *testing.T,
	schemaVersion int,
	withOperationData bool,
) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "panel.sqlite")
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	tableSQL := requiredPreOperationDataServiceOperationTableSQL
	if withOperationData {
		tableSQL = requiredServiceOperationTableSQL
	}
	if _, err := database.Exec(`
		CREATE TABLE schema_migrations (
			version INTEGER PRIMARY KEY,
			filename TEXT,
			sha256 TEXT,
			applied_at TEXT DEFAULT (datetime('now'))
		);
	` + tableSQL); err != nil {
		database.Close()
		t.Fatal(err)
	}
	for _, contract := range requiredServiceOperationIndexes {
		if _, err := database.Exec(contract.schemaSQL); err != nil {
			database.Close()
			t.Fatal(err)
		}
	}
	transaction, err := database.Begin()
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	statement, err := transaction.Prepare(`INSERT INTO schema_migrations(version) VALUES (?)`)
	if err != nil {
		_ = transaction.Rollback()
		database.Close()
		t.Fatal(err)
	}
	for version := 1; version <= schemaVersion; version++ {
		if _, err := statement.Exec(version); err != nil {
			_ = statement.Close()
			_ = transaction.Rollback()
			database.Close()
			t.Fatal(err)
		}
	}
	if err := statement.Close(); err != nil {
		_ = transaction.Rollback()
		database.Close()
		t.Fatal(err)
	}
	if err := transaction.Commit(); err != nil {
		database.Close()
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func insertServiceOperationIdleContractRow(
	t *testing.T,
	path string,
	id string,
	status string,
) {
	t.Helper()
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	insertServiceOperationIdleContractRowDB(t, database, id, status)
}

func insertServiceOperationIdleContractRowDB(
	t *testing.T,
	database *sql.DB,
	id string,
	status string,
) {
	t.Helper()
	const timestamp = "2026-08-13T00:00:00Z"
	if _, err := database.Exec(`
		INSERT INTO service_operations (
			id, kind, service_id, status, phase, started_at, created_at, updated_at
		) VALUES (?, 'install', 'certbot', ?, 'test', ?, ?, ?)
	`, id, status, timestamp, timestamp, timestamp); err != nil {
		t.Fatal(err)
	}
}

func replaceServiceOperationsTableForIdleCheck(
	t *testing.T,
	database *sql.DB,
	phaseType string,
	statusCheck string,
	requestedByContract string,
) {
	t.Helper()
	statement := fmt.Sprintf(`
		DROP TABLE service_operations;
		CREATE TABLE service_operations (
			id TEXT PRIMARY KEY,
			kind TEXT NOT NULL,
			service_id TEXT NOT NULL,
			package_name TEXT,
			status TEXT NOT NULL CHECK (%s),
			phase %s NOT NULL,
			result_json TEXT,
			error_code TEXT,
			error_message TEXT,
			requested_by %s,
			request_ip TEXT,
			user_agent TEXT,
			started_at TEXT NOT NULL,
			finished_at TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			request_id TEXT
		);
		CREATE UNIQUE INDEX idx_service_operations_one_active
			ON service_operations((1))
			WHERE status IN ('queued', 'running');
		CREATE INDEX idx_service_operations_recent
			ON service_operations(started_at DESC);
		CREATE UNIQUE INDEX idx_service_operations_request_id
			ON service_operations(request_id)
			WHERE request_id IS NOT NULL;
	`, statusCheck, phaseType, requestedByContract)
	if _, err := database.Exec(statement); err != nil {
		t.Fatal(err)
	}
}

func TestPinnedPanelDatabaseRejectsPathSwap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "panel.sqlite")
	database, err := paneldb.NewSQLiteDB(path)
	if err != nil {
		t.Fatal(err)
	}
	database.Close()

	replacementPath := filepath.Join(t.TempDir(), "replacement.sqlite")
	replacement, err := paneldb.NewSQLiteDB(replacementPath)
	if err != nil {
		t.Fatal(err)
	}
	replacement.Close()

	pinned, err := pinPanelDatabase(path)
	if err != nil {
		t.Fatal(err)
	}
	defer pinned.close()

	if runtime.GOOS == "windows" {
		pinned.path = replacementPath
	} else {
		if err := os.Rename(path, path+".original"); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(replacementPath, path); err != nil {
			t.Fatal(err)
		}
	}
	if err := pinned.verifyPath(); !errors.Is(err, errServiceOperationsNotIdle) {
		t.Fatalf("path swap err=%v want not idle", err)
	}
}

func TestPinnedPanelDatabaseRejectsSQLiteSidecars(t *testing.T) {
	t.Run("existing sidecar", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "panel.sqlite")
		database, err := paneldb.NewSQLiteDB(path)
		if err != nil {
			t.Fatal(err)
		}
		database.Close()
		if err := os.WriteFile(path+"-wal", []byte("unexpected"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := pinPanelDatabase(path); !errors.Is(err, errServiceOperationsNotIdle) {
			t.Fatalf("existing sidecar err=%v want not idle", err)
		}
	})

	t.Run("sidecar appears after pin", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "panel.sqlite")
		database, err := paneldb.NewSQLiteDB(path)
		if err != nil {
			t.Fatal(err)
		}
		database.Close()

		pinned, err := pinPanelDatabase(path)
		if err != nil {
			t.Fatal(err)
		}
		defer pinned.close()
		if err := os.WriteFile(pinned.siblingPath("-wal"), []byte("unexpected"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := pinned.verifyPath(); !errors.Is(err, errServiceOperationsNotIdle) {
			t.Fatalf("late sidecar err=%v want not idle", err)
		}
	})

	t.Run("stable empty WAL and stale SHM", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "panel.sqlite")
		database, err := paneldb.NewSQLiteDB(path)
		if err != nil {
			t.Fatal(err)
		}
		database.Close()
		if err := os.WriteFile(path+"-wal", nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path+"-shm", []byte("stale"), 0o600); err != nil {
			t.Fatal(err)
		}

		pinned, err := pinPanelDatabase(path)
		if err != nil {
			t.Fatalf("stable empty sidecars rejected: %v", err)
		}
		defer pinned.close()
		if err := pinned.verifyPath(); err != nil {
			t.Fatalf("stable empty sidecars changed: %v", err)
		}
	})
}

func createPreLedgerPanelDatabase(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "panel.sqlite")
	database, err := paneldb.NewSQLiteDB(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.GetDB().Exec(`DROP TABLE service_operations`); err != nil {
		database.Close()
		t.Fatal(err)
	}
	if _, err := database.GetDB().Exec(`DELETE FROM schema_migrations WHERE version > 20`); err != nil {
		database.Close()
		t.Fatal(err)
	}
	database.Close()
	return path
}

func TestPreLedgerServiceOperationsCheckAcceptsOnlyExactVersion20(t *testing.T) {
	t.Run("exact version 20", func(t *testing.T) {
		path := createPreLedgerPanelDatabase(t)
		if err := checkPreLedgerServiceOperationsIdle(path); err != nil {
			t.Fatalf("exact pre-ledger database rejected: %v", err)
		}
		if err := checkServiceOperationsIdle(path); !errors.Is(err, errServiceOperationsNotIdle) {
			t.Fatalf("normal idle check accepted pre-ledger database: %v", err)
		}
	})

	t.Run("missing migration", func(t *testing.T) {
		path := createPreLedgerPanelDatabase(t)
		database, err := sql.Open("sqlite", path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := database.Exec(`DELETE FROM schema_migrations WHERE version = 20`); err != nil {
			database.Close()
			t.Fatal(err)
		}
		database.Close()
		if err := checkPreLedgerServiceOperationsIdle(path); !errors.Is(err, errServiceOperationsNotIdle) {
			t.Fatalf("version 19 database err=%v want not idle", err)
		}
	})

	t.Run("partial service operation migration", func(t *testing.T) {
		path := createPreLedgerPanelDatabase(t)
		database, err := sql.Open("sqlite", path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := database.Exec(`CREATE TABLE service_operations (id TEXT PRIMARY KEY)`); err != nil {
			database.Close()
			t.Fatal(err)
		}
		database.Close()
		if err := checkPreLedgerServiceOperationsIdle(path); !errors.Is(err, errServiceOperationsNotIdle) {
			t.Fatalf("partial migration err=%v want not idle", err)
		}
	})

	t.Run("current schema", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "panel.sqlite")
		database, err := paneldb.NewSQLiteDB(path)
		if err != nil {
			t.Fatal(err)
		}
		database.Close()
		if err := checkPreLedgerServiceOperationsIdle(path); !errors.Is(err, errServiceOperationsNotIdle) {
			t.Fatalf("current schema err=%v want not idle", err)
		}
	})
}
