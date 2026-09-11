package db

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestServerSetupFreshProvenanceSurvivesReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "panel.sqlite")
	database, err := NewSQLiteDB(path)
	if err != nil {
		t.Fatal(err)
	}
	var origin, status string
	if err := database.GetDB().QueryRow(`SELECT origin,status FROM server_setup_state WHERE id=1`).Scan(&origin, &status); err != nil {
		t.Fatal(err)
	}
	if origin != "fresh" || status != "new" {
		t.Fatalf("new database: %s/%s", origin, status)
	}
	if _, err := database.GetDB().Exec(`UPDATE server_setup_state SET status='ready', revision=7,completed_at='2026-09-11T00:00:00Z' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	database.Close()
	database, err = NewSQLiteDB(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	var revision int
	if err := database.GetDB().QueryRow(`SELECT origin,status,revision FROM server_setup_state WHERE id=1`).Scan(&origin, &status, &revision); err != nil {
		t.Fatal(err)
	}
	if origin != "fresh" || status != "ready" || revision != 7 {
		t.Fatalf("reopen reset setup: %s/%s/%d", origin, status, revision)
	}
}

func TestServerSetupExistingSchemaWithoutCustomersRemainsLegacy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "panel.sqlite")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`CREATE TABLE existing_installation_marker (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	database, err := NewSQLiteDB(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	var origin, status string
	if err := database.GetDB().QueryRow(`SELECT origin,status FROM server_setup_state WHERE id=1`).Scan(&origin, &status); err != nil {
		t.Fatal(err)
	}
	if origin != "legacy" || status != "legacy" {
		t.Fatalf("existing database forced into setup: %s/%s", origin, status)
	}
}
