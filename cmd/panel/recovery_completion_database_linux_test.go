//go:build linux

package main

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"

	paneldb "github.com/alicelik/celikpanel/internal/db"
)

func TestCompletedUpdateDatabaseWALAwareChecksExactLatestContract(t *testing.T) {
	repository, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	latest, err := paneldb.HighestEmbeddedMigrationVersion()
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"complete", "older-schema", "missing-latest-row", "fake-latest", "future-row", "history-gap", "foreign-table", "changed-schema", "changed-filename", "changed-digest", "legacy-null-identities"} {
		t.Run(kind, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "celikpanel.db")
			version := latest
			if kind == "older-schema" || kind == "fake-latest" {
				version--
			}
			seedRecoveryCheckerDatabase(t, repository, path, version)
			database, err := sql.Open("sqlite", sqliteSnapshotURI(path, false))
			if err != nil {
				t.Fatal(err)
			}
			statement := ""
			switch kind {
			case "missing-latest-row":
				statement = fmt.Sprintf("DELETE FROM schema_migrations WHERE version=%d", latest)
			case "fake-latest":
				statement = fmt.Sprintf("INSERT INTO schema_migrations(version,filename,sha256) VALUES(%d,'fake.sql','fake')", latest)
			case "future-row":
				statement = fmt.Sprintf("INSERT INTO schema_migrations(version,filename,sha256) VALUES(%d,'future.sql','future')", latest+1)
			case "history-gap":
				statement = "DELETE FROM schema_migrations WHERE version=20"
			case "foreign-table":
				statement = "CREATE TABLE owner_extra(value TEXT)"
			case "changed-schema":
				statement = "ALTER TABLE users ADD COLUMN unreviewed TEXT"
			case "changed-filename":
				statement = "UPDATE schema_migrations SET filename='foreign.sql' WHERE version=1"
			case "changed-digest":
				statement = "UPDATE schema_migrations SET sha256=lower(hex(zeroblob(32))) WHERE version=1"
			case "legacy-null-identities":
				statement = "UPDATE schema_migrations SET filename=NULL,sha256=NULL WHERE version=1"
			}
			if statement != "" {
				if _, err := database.Exec(statement); err != nil {
					database.Close()
					t.Fatal(err)
				}
			}
			if err := database.Close(); err != nil {
				t.Fatal(err)
			}
			before := captureSQLiteSourceStates(t, path)
			err = checkCompletedUpdateDatabaseWALAware(path)
			if (err == nil) != (kind == "complete") {
				t.Fatalf("completion result: %v", err)
			}
			if after := captureSQLiteSourceStates(t, path); !reflect.DeepEqual(before, after) {
				t.Fatal("completion proof changed database or source sidecars")
			}
			// Older schemas remain compatible with the original queue-only
			// reader. The new completion contract must not change that API.
			if kind == "older-schema" || kind == "missing-latest-row" {
				if err := checkWALAwareServiceOperationsIdle(path); err != nil {
					t.Fatalf("legacy queue check semantics changed: %v", err)
				}
			}
		})
	}
}

func TestCompletedUpdateDatabaseReadsCommittedWALWithoutChangingSource(t *testing.T) {
	for _, kind := range []string{"complete", "active-in-wal", "schema-change-in-wal", "identity-change-in-wal"} {
		t.Run(kind, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "celikpanel.db")
			database := openWALAwareTestDatabase(t, path)
			defer database.Close()
			writeWALAwareIdleMarker(t, database, "completion-wal-proof")
			switch kind {
			case "active-in-wal":
				panel := &Panel{db: database}
				if _, err := panel.createServiceOperation(context.Background(), serviceOperationKindInstall, "certbot", "", serviceOperationActor{}); err != nil {
					t.Fatal(err)
				}
			case "schema-change-in-wal":
				if _, err := database.GetDB().Exec("CREATE TABLE unreviewed_wal_table(value TEXT)"); err != nil {
					t.Fatal(err)
				}
			case "identity-change-in-wal":
				if _, err := database.GetDB().Exec("UPDATE schema_migrations SET sha256='changed' WHERE version=1"); err != nil {
					t.Fatal(err)
				}
			}
			requireNonEmptyWAL(t, path)
			before := captureSQLiteSourceStates(t, path)
			err := checkCompletedUpdateDatabaseWALAware(path)
			if (err == nil) != (kind == "complete") {
				t.Fatalf("WAL completion result: %v", err)
			}
			if after := captureSQLiteSourceStates(t, path); !reflect.DeepEqual(before, after) {
				t.Fatal("completion proof changed source DB/WAL/SHM")
			}
		})
	}
}
