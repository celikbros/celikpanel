package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	paneldb "github.com/alicelik/celikpanel/internal/db"
	_ "modernc.org/sqlite"
)

func TestValidateServiceOperationSnapshotRequest(t *testing.T) {
	tests := []struct {
		name        string
		destination string
		schema      string
		conflict    bool
		wantMode    serviceOperationSnapshotSchema
		wantRequest bool
		wantError   string
	}{
		{name: "not requested"},
		{
			name:        "normal",
			destination: "/secure/celikpanel.db",
			schema:      "normal",
			wantMode:    serviceOperationSnapshotSchemaNormal,
			wantRequest: true,
		},
		{
			name:        "pre ledger",
			destination: "/secure/celikpanel.db",
			schema:      "pre-ledger",
			wantMode:    serviceOperationSnapshotSchemaPreLedger,
			wantRequest: true,
		},
		{name: "missing destination", schema: "normal", wantRequest: true, wantError: "destination is required"},
		{name: "missing schema", destination: "/secure/celikpanel.db", wantRequest: true, wantError: "schema is required"},
		{name: "wrong schema", destination: "/secure/celikpanel.db", schema: "future", wantRequest: true, wantError: "exactly normal or pre-ledger"},
		{name: "conflicting mode", destination: "/secure/celikpanel.db", schema: "normal", conflict: true, wantRequest: true, wantError: "mutually exclusive"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mode, requested, err := validateServiceOperationSnapshotRequest(test.destination, test.schema, test.conflict)
			if requested != test.wantRequest {
				t.Fatalf("requested=%v want %v", requested, test.wantRequest)
			}
			if mode != test.wantMode {
				t.Fatalf("mode=%q want %q", mode, test.wantMode)
			}
			if test.wantError == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("error=%v want substring %q", err, test.wantError)
			}
		})
	}
}

func TestServiceOperationSnapshotOnlineBackupIsConsistent(t *testing.T) {
	testRoot := newSecureSnapshotTestRoot(t)
	sourceDirectory := filepath.Join(testRoot, "source")
	destinationDirectory := filepath.Join(testRoot, "destination")
	mustMkdirSnapshotTestDirectory(t, sourceDirectory, 0o700)
	mustMkdirSnapshotTestDirectory(t, destinationDirectory, 0o700)

	sourcePath := filepath.Join(sourceDirectory, serviceOperationSnapshotBasename)
	database, err := paneldb.NewSQLiteDB(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.GetDB().Exec(`
		PRAGMA wal_autocheckpoint=0;
		INSERT INTO panel_settings(key, value) VALUES ('snapshot_consistency_left', '0');
		INSERT INTO panel_settings(key, value) VALUES ('snapshot_consistency_right', '0');
		INSERT INTO panel_settings(key, value) VALUES ('snapshot_wal_marker', 'present-in-online-backup');
	`); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(sourcePath + "-wal"); err != nil || info.Size() == 0 {
		t.Fatalf("source WAL is unavailable or empty: info=%v err=%v", info, err)
	}

	var writes atomic.Int64
	stopWriter := make(chan struct{})
	writerDone := make(chan error, 1)
	go func() {
		for value := int64(1); ; value++ {
			select {
			case <-stopWriter:
				writerDone <- nil
				return
			default:
			}
			transaction, err := database.GetDB().BeginTx(context.Background(), nil)
			if err != nil {
				writerDone <- err
				return
			}
			if _, err := transaction.Exec(
				`UPDATE panel_settings SET value=? WHERE key='snapshot_consistency_left'`,
				value,
			); err != nil {
				_ = transaction.Rollback()
				writerDone <- err
				return
			}
			if _, err := transaction.Exec(
				`UPDATE panel_settings SET value=? WHERE key='snapshot_consistency_right'`,
				value,
			); err != nil {
				_ = transaction.Rollback()
				writerDone <- err
				return
			}
			if err := transaction.Commit(); err != nil {
				writerDone <- err
				return
			}
			writes.Add(1)
			time.Sleep(100 * time.Microsecond)
		}
	}()
	deadline := time.Now().Add(5 * time.Second)
	for writes.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if writes.Load() == 0 {
		close(stopWriter)
		<-writerDone
		t.Fatal("concurrent writer did not start")
	}

	destinationPath := filepath.Join(destinationDirectory, serviceOperationSnapshotBasename)
	snapshotErr := createServiceOperationSnapshot(sourcePath, destinationPath, serviceOperationSnapshotSchemaNormal)
	close(stopWriter)
	if writerErr := <-writerDone; writerErr != nil {
		t.Fatalf("concurrent writer failed: %v", writerErr)
	}
	if snapshotErr != nil {
		t.Fatal(snapshotErr)
	}
	assertStandaloneSnapshot(t, destinationPath)

	snapshot, err := sql.Open("sqlite", sqliteSnapshotURI(destinationPath, true))
	if err != nil {
		t.Fatal(err)
	}
	var leftValue, rightValue int64
	if err := snapshot.QueryRow(`
		SELECT
			CAST((SELECT value FROM panel_settings WHERE key='snapshot_consistency_left') AS INTEGER),
			CAST((SELECT value FROM panel_settings WHERE key='snapshot_consistency_right') AS INTEGER)
	`).Scan(&leftValue, &rightValue); err != nil {
		t.Fatal(err)
	}
	if leftValue != rightValue {
		t.Fatalf("snapshot split a writer transaction: left=%d right=%d", leftValue, rightValue)
	}
	var marker string
	if err := snapshot.QueryRow(`SELECT value FROM panel_settings WHERE key='snapshot_wal_marker'`).Scan(&marker); err != nil {
		t.Fatal(err)
	}
	if marker != "present-in-online-backup" {
		t.Fatalf("snapshot marker=%q", marker)
	}
	if err := snapshot.Close(); err != nil {
		t.Fatal(err)
	}
	assertStandaloneSnapshot(t, destinationPath)
}

func TestServiceOperationSnapshotAcceptsExactPreLedgerDatabase(t *testing.T) {
	testRoot := newSecureSnapshotTestRoot(t)
	sourcePath := createPreLedgerPanelDatabaseInDirectory(t, filepath.Join(testRoot, "source"))
	destinationDirectory := filepath.Join(testRoot, "destination")
	mustMkdirSnapshotTestDirectory(t, destinationDirectory, 0o700)
	destinationPath := filepath.Join(destinationDirectory, serviceOperationSnapshotBasename)
	if err := createServiceOperationSnapshot(
		sourcePath,
		destinationPath,
		serviceOperationSnapshotSchemaPreLedger,
	); err != nil {
		t.Fatal(err)
	}
	assertStandaloneSnapshot(t, destinationPath)
}

func TestServiceOperationSnapshotRejectsUnsafeDestinations(t *testing.T) {
	testRoot := newSecureSnapshotTestRoot(t)
	sourceDirectory := filepath.Join(testRoot, "source")
	mustMkdirSnapshotTestDirectory(t, sourceDirectory, 0o700)
	sourcePath := filepath.Join(sourceDirectory, serviceOperationSnapshotBasename)
	database, err := paneldb.NewSQLiteDB(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	database.Close()

	t.Run("existing destination", func(t *testing.T) {
		parent := filepath.Join(testRoot, "existing")
		mustMkdirSnapshotTestDirectory(t, parent, 0o700)
		destinationPath := filepath.Join(parent, serviceOperationSnapshotBasename)
		if err := os.WriteFile(destinationPath, []byte("do-not-replace"), 0o600); err != nil {
			t.Fatal(err)
		}
		err := createServiceOperationSnapshot(sourcePath, destinationPath, serviceOperationSnapshotSchemaNormal)
		if err == nil || !strings.Contains(err.Error(), "already exists") {
			t.Fatalf("error=%v want existing destination rejection", err)
		}
		content, readErr := os.ReadFile(destinationPath)
		if readErr != nil || string(content) != "do-not-replace" {
			t.Fatalf("existing destination changed: content=%q err=%v", content, readErr)
		}
	})

	t.Run("destination symlink", func(t *testing.T) {
		parent := filepath.Join(testRoot, "symlink")
		mustMkdirSnapshotTestDirectory(t, parent, 0o700)
		targetPath := filepath.Join(parent, "target")
		if err := os.WriteFile(targetPath, []byte("target"), 0o600); err != nil {
			t.Fatal(err)
		}
		destinationPath := filepath.Join(parent, serviceOperationSnapshotBasename)
		if err := os.Symlink(targetPath, destinationPath); err != nil {
			t.Fatal(err)
		}
		err := createServiceOperationSnapshot(sourcePath, destinationPath, serviceOperationSnapshotSchemaNormal)
		if err == nil || !strings.Contains(err.Error(), "already exists") {
			t.Fatalf("error=%v want symlink rejection", err)
		}
		content, readErr := os.ReadFile(targetPath)
		if readErr != nil || string(content) != "target" {
			t.Fatalf("symlink target changed: content=%q err=%v", content, readErr)
		}
	})

	t.Run("existing destination journal", func(t *testing.T) {
		parent := filepath.Join(testRoot, "existing-journal")
		mustMkdirSnapshotTestDirectory(t, parent, 0o700)
		destinationPath := filepath.Join(parent, serviceOperationSnapshotBasename)
		journalPath := destinationPath + "-journal"
		if err := os.WriteFile(journalPath, []byte("do-not-consume"), 0o600); err != nil {
			t.Fatal(err)
		}
		err := createServiceOperationSnapshot(sourcePath, destinationPath, serviceOperationSnapshotSchemaNormal)
		if err == nil || !strings.Contains(err.Error(), "-journal") {
			t.Fatalf("error=%v want existing journal rejection", err)
		}
		content, readErr := os.ReadFile(journalPath)
		if readErr != nil || string(content) != "do-not-consume" {
			t.Fatalf("existing journal changed: content=%q err=%v", content, readErr)
		}
	})

	t.Run("insecure parent", func(t *testing.T) {
		insecureParent := filepath.Join(testRoot, "insecure")
		mustMkdirSnapshotTestDirectory(t, insecureParent, 0o770)
		secureChild := filepath.Join(insecureParent, "child")
		mustMkdirSnapshotTestDirectory(t, secureChild, 0o700)
		err := createServiceOperationSnapshot(
			sourcePath,
			filepath.Join(secureChild, serviceOperationSnapshotBasename),
			serviceOperationSnapshotSchemaNormal,
		)
		if err == nil || !strings.Contains(err.Error(), "must not be writable") {
			t.Fatalf("error=%v want insecure parent rejection", err)
		}
	})

	t.Run("wrong basename", func(t *testing.T) {
		parent := filepath.Join(testRoot, "wrong-basename")
		mustMkdirSnapshotTestDirectory(t, parent, 0o700)
		err := createServiceOperationSnapshot(
			sourcePath,
			filepath.Join(parent, "snapshot.db"),
			serviceOperationSnapshotSchemaNormal,
		)
		if err == nil || !strings.Contains(err.Error(), "basename") {
			t.Fatalf("error=%v want basename rejection", err)
		}
	})

	t.Run("relative destination", func(t *testing.T) {
		err := createServiceOperationSnapshot(
			sourcePath,
			serviceOperationSnapshotBasename,
			serviceOperationSnapshotSchemaNormal,
		)
		if err == nil || !strings.Contains(err.Error(), "absolute") {
			t.Fatalf("error=%v want absolute path rejection", err)
		}
	})
}

func TestServiceOperationSnapshotWrongModeCleansPartialOutput(t *testing.T) {
	testRoot := newSecureSnapshotTestRoot(t)
	sourceDirectory := filepath.Join(testRoot, "source")
	destinationDirectory := filepath.Join(testRoot, "destination")
	mustMkdirSnapshotTestDirectory(t, sourceDirectory, 0o700)
	mustMkdirSnapshotTestDirectory(t, destinationDirectory, 0o700)
	sourcePath := filepath.Join(sourceDirectory, serviceOperationSnapshotBasename)
	database, err := paneldb.NewSQLiteDB(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	database.Close()
	destinationPath := filepath.Join(destinationDirectory, serviceOperationSnapshotBasename)
	err = createServiceOperationSnapshot(
		sourcePath,
		destinationPath,
		serviceOperationSnapshotSchemaPreLedger,
	)
	if err == nil || !strings.Contains(err.Error(), "pre-ledger") {
		t.Fatalf("error=%v want wrong mode rejection", err)
	}
	entries, readErr := os.ReadDir(destinationDirectory)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("partial snapshot entries remain: %v", entryNames(entries))
	}
}

func TestServiceOperationSnapshotSourceVerificationFailureDoesNotPublish(t *testing.T) {
	testRoot := newSecureSnapshotTestRoot(t)
	sourceDirectory := filepath.Join(testRoot, "source")
	destinationDirectory := filepath.Join(testRoot, "destination")
	mustMkdirSnapshotTestDirectory(t, sourceDirectory, 0o700)
	mustMkdirSnapshotTestDirectory(t, destinationDirectory, 0o700)
	sourcePath := filepath.Join(sourceDirectory, serviceOperationSnapshotBasename)
	database, err := paneldb.NewSQLiteDB(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	database.Close()
	destinationPath := filepath.Join(destinationDirectory, serviceOperationSnapshotBasename)
	err = createServiceOperationSnapshotWithCopyAndVerify(
		sourcePath,
		destinationPath,
		serviceOperationSnapshotSchemaNormal,
		copySQLiteDatabaseOnline,
		func() error { return errors.New("injected source verification failure") },
	)
	if err == nil || !strings.Contains(err.Error(), "injected source verification failure") {
		t.Fatalf("error=%v want source verification failure", err)
	}
	assertSnapshotDirectoryEmpty(t, destinationDirectory)
}

func TestServiceOperationSnapshotRejectsFutureMigrationAndCleansPartialOutput(t *testing.T) {
	testRoot := newSecureSnapshotTestRoot(t)
	sourceDirectory := filepath.Join(testRoot, "source")
	destinationDirectory := filepath.Join(testRoot, "destination")
	mustMkdirSnapshotTestDirectory(t, sourceDirectory, 0o700)
	mustMkdirSnapshotTestDirectory(t, destinationDirectory, 0o700)
	sourcePath := filepath.Join(sourceDirectory, serviceOperationSnapshotBasename)
	database, err := paneldb.NewSQLiteDB(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	var latestVersion int
	if err := database.GetDB().QueryRow(
		`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`,
	).Scan(&latestVersion); err != nil {
		database.Close()
		t.Fatal(err)
	}
	futureVersion := latestVersion + 1
	if _, err := database.GetDB().Exec(`
		CREATE TABLE synthetic_future_snapshot_table (
			id INTEGER PRIMARY KEY
		);
		INSERT INTO schema_migrations(version) VALUES (?);
	`, futureVersion); err != nil {
		database.Close()
		t.Fatal(err)
	}
	database.Close()

	destinationPath := filepath.Join(destinationDirectory, serviceOperationSnapshotBasename)
	err = createServiceOperationSnapshot(
		sourcePath,
		destinationPath,
		serviceOperationSnapshotSchemaNormal,
	)
	wantError := fmt.Sprintf("embedded migration version %d is unavailable", futureVersion)
	if err == nil || !strings.Contains(err.Error(), wantError) {
		t.Fatalf("error=%v want %q", err, wantError)
	}
	assertSnapshotDirectoryEmpty(t, destinationDirectory)
}

func TestServiceOperationSnapshotCleanupWithoutStagePreservesUnrelatedEntries(t *testing.T) {
	testRoot := newSecureSnapshotTestRoot(t)
	destinationDirectory := filepath.Join(testRoot, "destination")
	mustMkdirSnapshotTestDirectory(t, destinationDirectory, 0o700)
	for _, name := range []string{"-wal", "-shm", "-journal"} {
		if err := os.WriteFile(filepath.Join(destinationDirectory, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	destinationPath := filepath.Join(destinationDirectory, serviceOperationSnapshotBasename)
	destination, err := prepareServiceOperationSnapshotDestination(destinationPath)
	if err != nil {
		t.Fatal(err)
	}
	cleanupSnapshotDestinationForTest(t, destination)
	for _, name := range []string{"-wal", "-shm", "-journal"} {
		content, err := os.ReadFile(filepath.Join(destinationDirectory, name))
		if err != nil || string(content) != name {
			t.Fatalf("unrelated entry %s changed: content=%q err=%v", name, content, err)
		}
	}
}

func TestServiceOperationSnapshotRejectsSchemaDriftAndCleansPartialOutput(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*sql.DB) error
	}{
		{
			name: "missing base table",
			mutate: func(database *sql.DB) error {
				_, err := database.Exec(`DROP TABLE metrics_samples`)
				return err
			},
		},
		{
			name: "missing base index",
			mutate: func(database *sql.DB) error {
				_, err := database.Exec(`DROP INDEX idx_metrics_ts`)
				return err
			},
		},
		{
			name: "extra trigger",
			mutate: func(database *sql.DB) error {
				_, err := database.Exec(`
					CREATE TRIGGER unexpected_snapshot_trigger
					AFTER INSERT ON metrics_samples
					BEGIN
						SELECT 1;
					END`)
				return err
			},
		},
		{
			name: "syntactically valid token boundary collision",
			mutate: func(database *sql.DB) error {
				var schemaSQL string
				if err := database.QueryRow(`
					SELECT sql FROM sqlite_master
					WHERE type='table' AND name='metrics_samples'
				`).Scan(&schemaSQL); err != nil {
					return err
				}
				mutatedSQL := strings.Replace(schemaSQL, "TEXT    NOT NULL", "TEXTNOT NULL", 1)
				if mutatedSQL == schemaSQL {
					return fmt.Errorf("metrics schema does not contain the expected token boundary")
				}
				if _, err := database.Exec(`PRAGMA writable_schema=ON`); err != nil {
					return err
				}
				if _, err := database.Exec(
					`UPDATE sqlite_master SET sql=? WHERE type='table' AND name='metrics_samples'`,
					mutatedSQL,
				); err != nil {
					return err
				}
				var schemaVersion int
				if err := database.QueryRow(`PRAGMA schema_version`).Scan(&schemaVersion); err != nil {
					return err
				}
				if _, err := database.Exec(fmt.Sprintf(`PRAGMA schema_version=%d`, schemaVersion+1)); err != nil {
					return err
				}
				_, err := database.Exec(`PRAGMA writable_schema=OFF`)
				return err
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testRoot := newSecureSnapshotTestRoot(t)
			sourceDirectory := filepath.Join(testRoot, "source")
			destinationDirectory := filepath.Join(testRoot, "destination")
			mustMkdirSnapshotTestDirectory(t, sourceDirectory, 0o700)
			mustMkdirSnapshotTestDirectory(t, destinationDirectory, 0o700)
			sourcePath := filepath.Join(sourceDirectory, serviceOperationSnapshotBasename)
			database, err := paneldb.NewSQLiteDB(sourcePath)
			if err != nil {
				t.Fatal(err)
			}
			if err := test.mutate(database.GetDB()); err != nil {
				database.Close()
				t.Fatal(err)
			}
			database.Close()

			destinationPath := filepath.Join(destinationDirectory, serviceOperationSnapshotBasename)
			err = createServiceOperationSnapshot(
				sourcePath,
				destinationPath,
				serviceOperationSnapshotSchemaNormal,
			)
			if err == nil || !strings.Contains(err.Error(), "schema contract") {
				t.Fatalf("error=%v want exact schema contract rejection", err)
			}
			assertSnapshotDirectoryEmpty(t, destinationDirectory)
		})
	}
}

func TestServiceOperationSnapshotRejectsForeignKeyViolationAndCleansPartialOutput(t *testing.T) {
	testRoot := newSecureSnapshotTestRoot(t)
	sourceDirectory := filepath.Join(testRoot, "source")
	destinationDirectory := filepath.Join(testRoot, "destination")
	mustMkdirSnapshotTestDirectory(t, sourceDirectory, 0o700)
	mustMkdirSnapshotTestDirectory(t, destinationDirectory, 0o700)
	sourcePath := filepath.Join(sourceDirectory, serviceOperationSnapshotBasename)
	database, err := paneldb.NewSQLiteDB(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	connection, err := database.GetDB().Conn(context.Background())
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	if _, err := connection.ExecContext(context.Background(), `PRAGMA foreign_keys=OFF`); err != nil {
		connection.Close()
		database.Close()
		t.Fatal(err)
	}
	if _, err := connection.ExecContext(
		context.Background(),
		`INSERT INTO subscriptions(owner_id, name) VALUES (999999, 'orphan snapshot row')`,
	); err != nil {
		connection.Close()
		database.Close()
		t.Fatal(err)
	}
	if err := connection.Close(); err != nil {
		database.Close()
		t.Fatal(err)
	}
	database.Close()

	destinationPath := filepath.Join(destinationDirectory, serviceOperationSnapshotBasename)
	err = createServiceOperationSnapshot(
		sourcePath,
		destinationPath,
		serviceOperationSnapshotSchemaNormal,
	)
	if err == nil || !strings.Contains(err.Error(), "foreign key check") {
		t.Fatalf("error=%v want foreign key violation rejection", err)
	}
	assertSnapshotDirectoryEmpty(t, destinationDirectory)
}

func TestServiceOperationSnapshotAtomicPublishFaultsAndRetries(t *testing.T) {
	testRoot := newSecureSnapshotTestRoot(t)
	sourceDirectory := filepath.Join(testRoot, "source")
	mustMkdirSnapshotTestDirectory(t, sourceDirectory, 0o700)
	sourcePath := filepath.Join(sourceDirectory, serviceOperationSnapshotBasename)
	database, err := paneldb.NewSQLiteDB(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	database.Close()

	t.Run("pre rename fault cleans stage and retry succeeds", func(t *testing.T) {
		parent := filepath.Join(testRoot, "pre-rename")
		mustMkdirSnapshotTestDirectory(t, parent, 0o700)
		destinationPath := filepath.Join(parent, serviceOperationSnapshotBasename)
		destination := stageValidatedSnapshotForPublish(t, sourcePath, destinationPath)
		destination.beforeRename = func() error { return errors.New("injected pre-rename fault") }
		if err := destination.publish(); err == nil || !strings.Contains(err.Error(), "pre-rename") {
			t.Fatalf("publish error=%v want injected pre-rename fault", err)
		}
		cleanupSnapshotDestinationForTest(t, destination)
		assertSnapshotDirectoryEmpty(t, parent)
		if err := createServiceOperationSnapshot(sourcePath, destinationPath, serviceOperationSnapshotSchemaNormal); err != nil {
			t.Fatalf("retry after pre-rename fault failed: %v", err)
		}
		assertStandaloneSnapshot(t, destinationPath)
	})

	t.Run("pre rename stage mutation is rejected and retry succeeds", func(t *testing.T) {
		parent := filepath.Join(testRoot, "pre-rename-stage-mutation")
		mustMkdirSnapshotTestDirectory(t, parent, 0o700)
		destinationPath := filepath.Join(parent, serviceOperationSnapshotBasename)
		destination := stageValidatedSnapshotForPublish(t, sourcePath, destinationPath)
		destination.beforeRename = func() error {
			return os.WriteFile(destination.stagePath, []byte("mutated after validation"), 0o600)
		}
		if err := destination.publish(); err == nil || !strings.Contains(err.Error(), "changed after validation") {
			t.Fatalf("publish error=%v want validated stage mutation rejection", err)
		}
		cleanupSnapshotDestinationForTest(t, destination)
		assertSnapshotDirectoryEmpty(t, parent)
		if err := createServiceOperationSnapshot(sourcePath, destinationPath, serviceOperationSnapshotSchemaNormal); err != nil {
			t.Fatalf("retry after stage mutation failed: %v", err)
		}
		assertStandaloneSnapshot(t, destinationPath)
	})

	t.Run("rename no replace preserves racing destination", func(t *testing.T) {
		parent := filepath.Join(testRoot, "no-clobber")
		mustMkdirSnapshotTestDirectory(t, parent, 0o700)
		destinationPath := filepath.Join(parent, serviceOperationSnapshotBasename)
		destination := stageValidatedSnapshotForPublish(t, sourcePath, destinationPath)
		destination.beforeRename = func() error {
			return os.WriteFile(destinationPath, []byte("racing-destination"), 0o600)
		}
		if err := destination.publish(); err == nil || !strings.Contains(err.Error(), "without replacement") {
			t.Fatalf("publish error=%v want no-clobber rejection", err)
		}
		cleanupSnapshotDestinationForTest(t, destination)
		content, err := os.ReadFile(destinationPath)
		if err != nil || string(content) != "racing-destination" {
			t.Fatalf("racing destination changed: content=%q err=%v", content, err)
		}
		if err := os.Remove(destinationPath); err != nil {
			t.Fatal(err)
		}
		if err := createServiceOperationSnapshot(sourcePath, destinationPath, serviceOperationSnapshotSchemaNormal); err != nil {
			t.Fatalf("retry after no-clobber rejection failed: %v", err)
		}
		assertStandaloneSnapshot(t, destinationPath)
	})

	t.Run("post rename fault removes incomplete final and retry succeeds", func(t *testing.T) {
		parent := filepath.Join(testRoot, "post-rename")
		mustMkdirSnapshotTestDirectory(t, parent, 0o700)
		destinationPath := filepath.Join(parent, serviceOperationSnapshotBasename)
		destination := stageValidatedSnapshotForPublish(t, sourcePath, destinationPath)
		destination.afterRename = func() error { return errors.New("injected post-rename fault") }
		if err := destination.publish(); err == nil || !strings.Contains(err.Error(), "post-rename") {
			t.Fatalf("publish error=%v want injected post-rename fault", err)
		}
		cleanupSnapshotDestinationForTest(t, destination)
		assertSnapshotDirectoryEmpty(t, parent)
		if err := createServiceOperationSnapshot(sourcePath, destinationPath, serviceOperationSnapshotSchemaNormal); err != nil {
			t.Fatalf("retry after post-rename fault failed: %v", err)
		}
		assertStandaloneSnapshot(t, destinationPath)
	})

	t.Run("post rename journal is rejected and retry succeeds", func(t *testing.T) {
		parent := filepath.Join(testRoot, "post-rename-journal")
		mustMkdirSnapshotTestDirectory(t, parent, 0o700)
		destinationPath := filepath.Join(parent, serviceOperationSnapshotBasename)
		destination := stageValidatedSnapshotForPublish(t, sourcePath, destinationPath)
		destination.afterRename = func() error {
			return os.WriteFile(destinationPath+"-journal", []byte("injected journal"), 0o600)
		}
		if err := destination.publish(); err == nil || !strings.Contains(err.Error(), "-journal") {
			t.Fatalf("publish error=%v want post-rename journal rejection", err)
		}
		cleanupSnapshotDestinationForTest(t, destination)
		assertSnapshotDirectoryEmpty(t, parent)
		if err := createServiceOperationSnapshot(sourcePath, destinationPath, serviceOperationSnapshotSchemaNormal); err != nil {
			t.Fatalf("retry after post-rename journal failed: %v", err)
		}
		assertStandaloneSnapshot(t, destinationPath)
	})
}

func TestSnapshotCleanupPreservesExchangedStagePath(t *testing.T) {
	testRoot := newSecureSnapshotTestRoot(t)
	sourceDirectory := filepath.Join(testRoot, t.Name())
	mustMkdirSnapshotTestDirectory(t, sourceDirectory, 0o700)
	sourcePath := filepath.Join(sourceDirectory, serviceOperationSnapshotBasename)
	database, err := paneldb.NewSQLiteDB(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	database.Close()
	destinationDirectory := filepath.Join(testRoot, t.Name()+t.Name())
	mustMkdirSnapshotTestDirectory(t, destinationDirectory, 0o700)
	destinationPath := filepath.Join(destinationDirectory, serviceOperationSnapshotBasename)
	destination := stageValidatedSnapshotForPublish(t, sourcePath, destinationPath)
	displacedPath := destination.stagePath + t.Name()
	replacement := []byte{13, 14, 15, 16}
	destination.beforeRename = func() error {
		if err := os.Rename(destination.stagePath, displacedPath); err != nil {
			return err
		}
		return os.WriteFile(destination.stagePath, replacement, 0o600)
	}
	if err := destination.publish(); err == nil {
		t.Fatal(err)
	}
	if err := destination.cleanup(); err == nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(destination.stagePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(content) != len(replacement) || content[0] != replacement[0] {
		t.Fatal(content)
	}
	if _, err := os.Stat(displacedPath); err != nil {
		t.Fatal(err)
	}
	if err := destination.close(); err != nil {
		t.Fatal(err)
	}
}

func TestSnapshotCleanupPreservesExchangedPublishedPath(t *testing.T) {
	testRoot := newSecureSnapshotTestRoot(t)
	sourceDirectory := filepath.Join(testRoot, t.Name())
	mustMkdirSnapshotTestDirectory(t, sourceDirectory, 0o700)
	sourcePath := filepath.Join(sourceDirectory, serviceOperationSnapshotBasename)
	database, err := paneldb.NewSQLiteDB(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	database.Close()
	destinationDirectory := filepath.Join(testRoot, t.Name()+t.Name())
	mustMkdirSnapshotTestDirectory(t, destinationDirectory, 0o700)
	destinationPath := filepath.Join(destinationDirectory, serviceOperationSnapshotBasename)
	displacedPath := destinationPath + t.Name()
	destination := stageValidatedSnapshotForPublish(t, sourcePath, destinationPath)
	replacement := []byte{1, 2, 3, 4}
	destination.afterRename = func() error {
		if err := os.Rename(destinationPath, displacedPath); err != nil {
			return err
		}
		return os.WriteFile(destinationPath, replacement, 0o600)
	}
	if err := destination.publish(); err == nil {
		t.Fatal(err)
	}
	if err := destination.cleanup(); err == nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(destinationPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(content) != len(replacement) || content[0] != replacement[0] {
		t.Fatal(content)
	}
	if _, err := os.Stat(displacedPath); err != nil {
		t.Fatal(err)
	}
	if err := destination.close(); err != nil {
		t.Fatal(err)
	}
}

func stageValidatedSnapshotForPublish(
	t *testing.T,
	sourcePath string,
	destinationPath string,
) *serviceOperationSnapshotDestination {
	t.Helper()
	destination, err := prepareServiceOperationSnapshotDestination(destinationPath)
	if err != nil {
		t.Fatal(err)
	}
	stagePath, err := destination.createStage()
	if err != nil {
		destination.close()
		t.Fatal(err)
	}
	if err := copySQLiteDatabaseOnline(sourcePath, stagePath); err != nil {
		destination.cleanup()
		destination.close()
		t.Fatal(err)
	}
	if err := destination.syncAndVerifyStage(); err != nil {
		destination.cleanup()
		destination.close()
		t.Fatal(err)
	}
	if err := destination.validateStage(serviceOperationSnapshotSchemaNormal); err != nil {
		destination.cleanup()
		destination.close()
		t.Fatal(err)
	}
	return destination
}

func cleanupSnapshotDestinationForTest(t *testing.T, destination *serviceOperationSnapshotDestination) {
	t.Helper()
	if err := destination.cleanup(); err != nil {
		t.Errorf("cleanup snapshot destination: %v", err)
	}
	if err := destination.close(); err != nil {
		t.Errorf("close snapshot destination: %v", err)
	}
}

func assertSnapshotDirectoryEmpty(t *testing.T, directory string) {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("snapshot directory contains incomplete entries: %v", entryNames(entries))
	}
}

func newSecureSnapshotTestRoot(t *testing.T) string {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("root-owned secure snapshot destination tests require Linux")
	}
	if os.Geteuid() != 0 || os.Getegid() != 0 {
		t.Skip("root-owned secure snapshot destination tests require uid 0 gid 0")
	}
	root, err := os.MkdirTemp("/root", "celikpanel-snapshot-test-")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(root); err != nil {
			t.Errorf("remove test root: %v", err)
		}
	})
	return root
}

func mustMkdirSnapshotTestDirectory(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	if err := os.Mkdir(path, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func createPreLedgerPanelDatabaseInDirectory(t *testing.T, directory string) string {
	t.Helper()
	mustMkdirSnapshotTestDirectory(t, directory, 0o700)
	path := filepath.Join(directory, serviceOperationSnapshotBasename)
	database, err := paneldb.NewSQLiteDB(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.GetDB().Exec(`
		DROP TRIGGER vpn_offering_sync_update;
		DROP TRIGGER vpn_entitlements_sync_delete;
		DROP TRIGGER vpn_entitlements_sync_update;
		DROP TRIGGER vpn_entitlements_sync_insert;
		DROP TRIGGER vpn_peers_sync_delete;
		DROP TRIGGER vpn_peers_sync_update;
		DROP TRIGGER vpn_peers_sync_insert;
		DROP INDEX idx_vpn_peers_desired_sync;
		DROP TABLE vpn_sync_state;
		ALTER TABLE vpn_peers DROP COLUMN delivery_expires_at;
		ALTER TABLE vpn_peers DROP COLUMN delivery_token_hash;
		ALTER TABLE vpn_peers DROP COLUMN provisioning_state;
		ALTER TABLE vpn_peers DROP COLUMN updated_at;
		ALTER TABLE vpn_peers DROP COLUMN sync_error;
		ALTER TABLE vpn_peers DROP COLUMN sync_state;
		ALTER TABLE vpn_peers DROP COLUMN desired_state;

		DROP INDEX idx_store_offering_components_component;
		DROP INDEX idx_store_offerings_release;
		DROP TABLE store_offering_components;
		DROP TABLE store_offerings;

		DROP INDEX idx_service_operations_request_id;
		ALTER TABLE service_operations DROP COLUMN request_id;
		DROP INDEX idx_service_operations_recent;
		DROP INDEX idx_service_operations_one_active;
		DROP TABLE service_operations;

		DELETE FROM schema_migrations WHERE version > 20;
	`); err != nil {
		database.Close()
		t.Fatal(err)
	}
	database.Close()
	return path
}

func assertStandaloneSnapshot(t *testing.T, destinationPath string) {
	t.Helper()
	info, err := os.Stat(destinationPath)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Fatalf("snapshot mode=%v want 0600 regular file", info.Mode())
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		exists, err := snapshotPathExists(destinationPath + suffix)
		if err != nil {
			t.Fatal(err)
		}
		if exists {
			t.Fatalf("standalone snapshot has sidecar %s", suffix)
		}
	}
}

func entryNames(entries []os.DirEntry) []string {
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}
