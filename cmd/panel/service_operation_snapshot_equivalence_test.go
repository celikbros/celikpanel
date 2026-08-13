package main

import (
	"crypto/sha256"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServiceOperationSnapshotLogicalEquivalenceIgnoresFileLayout(t *testing.T) {
	leftPath := filepath.Join(t.TempDir(), "left.db")
	createLogicalEquivalenceFixture(t, leftPath)
	rightPath := copyLogicalEquivalenceFixture(t, leftPath)

	before, err := os.ReadFile(rightPath)
	if err != nil {
		t.Fatal(err)
	}
	database := openLogicalEquivalenceFixture(t, rightPath)
	if _, err := database.Exec(`
		INSERT INTO logical_values(id, text_value) VALUES (999, 'temporary-layout-change');
		DELETE FROM logical_values WHERE id = 999;
	`); err != nil {
		database.Close()
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(rightPath)
	if err != nil {
		t.Fatal(err)
	}
	if sha256.Sum256(before) == sha256.Sum256(after) {
		t.Fatal("fixture rewrite did not produce distinct SQLite file bytes")
	}

	if err := compareServiceOperationSnapshotLogicalContents(leftPath, rightPath); err != nil {
		t.Fatalf("logically equivalent databases rejected: %v", err)
	}
}

func TestServiceOperationSnapshotLogicalEquivalenceRejectsValueDifferenceWithoutLeakingValue(t *testing.T) {
	leftPath := filepath.Join(t.TempDir(), "left.db")
	createLogicalEquivalenceFixture(t, leftPath)
	rightPath := copyLogicalEquivalenceFixture(t, leftPath)

	const sensitiveDifference = "must-not-appear-in-proof-error"
	database := openLogicalEquivalenceFixture(t, rightPath)
	if _, err := database.Exec(
		`UPDATE logical_values SET text_value = ? WHERE id = 1`,
		sensitiveDifference,
	); err != nil {
		database.Close()
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	err := compareServiceOperationSnapshotLogicalContents(leftPath, rightPath)
	if err == nil || !strings.Contains(err.Error(), "logical value differs") {
		t.Fatalf("logical difference err=%v", err)
	}
	if strings.Contains(err.Error(), sensitiveDifference) {
		t.Fatalf("logical proof error leaked a database value: %v", err)
	}
}

func TestServiceOperationSnapshotLogicalEquivalenceIncludesRowID(t *testing.T) {
	leftPath := filepath.Join(t.TempDir(), "left.db")
	createLogicalEquivalenceFixture(t, leftPath)
	rightPath := copyLogicalEquivalenceFixture(t, leftPath)

	database := openLogicalEquivalenceFixture(t, rightPath)
	if _, err := database.Exec(`DELETE FROM implicit_rowids WHERE value = 'first'`); err != nil {
		database.Close()
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO implicit_rowids(value) VALUES ('first')`); err != nil {
		database.Close()
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	err := compareServiceOperationSnapshotLogicalContents(leftPath, rightPath)
	if err == nil || !strings.Contains(err.Error(), "$rowid") {
		t.Fatalf("rowid difference err=%v", err)
	}
}

func TestServiceOperationSnapshotLogicalEquivalenceIncludesSQLiteSequence(t *testing.T) {
	leftPath := filepath.Join(t.TempDir(), "left.db")
	createLogicalEquivalenceFixture(t, leftPath)
	rightPath := copyLogicalEquivalenceFixture(t, leftPath)

	database := openLogicalEquivalenceFixture(t, rightPath)
	result, err := database.Exec(`INSERT INTO automatic_ids(value) VALUES ('temporary')`)
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	if _, err := database.Exec(`DELETE FROM automatic_ids WHERE id = ?`, id); err != nil {
		database.Close()
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	err = compareServiceOperationSnapshotLogicalContents(leftPath, rightPath)
	if err == nil || !strings.Contains(err.Error(), "sqlite_sequence") {
		t.Fatalf("sqlite_sequence difference err=%v", err)
	}
}

func createLogicalEquivalenceFixture(t *testing.T, path string) {
	t.Helper()
	database := openLogicalEquivalenceFixture(t, path)
	defer database.Close()
	if _, err := database.Exec(`
		CREATE TABLE logical_values (
			id INTEGER PRIMARY KEY,
			null_value,
			integer_value,
			real_value,
			text_value,
			blob_value
		);
		CREATE TABLE implicit_rowids (value TEXT NOT NULL);
		CREATE TABLE automatic_ids (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			value TEXT NOT NULL
		);
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(
		`INSERT INTO logical_values(
			id, null_value, integer_value, real_value, text_value, blob_value
		) VALUES (?, ?, ?, ?, ?, ?)`,
		1,
		nil,
		int64(-9223372036854775807),
		1.25,
		"text\x00with-nul",
		[]byte{0x00, 0x7f, 0xff},
	); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(
		`INSERT INTO implicit_rowids(value) VALUES ('first'), ('second')`,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(
		`INSERT INTO automatic_ids(value) VALUES ('durable')`,
	); err != nil {
		t.Fatal(err)
	}
}

func copyLogicalEquivalenceFixture(t *testing.T, sourcePath string) string {
	t.Helper()
	content, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	destinationPath := filepath.Join(t.TempDir(), "right.db")
	if err := os.WriteFile(destinationPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return destinationPath
}

func openLogicalEquivalenceFixture(t *testing.T, path string) *sql.DB {
	t.Helper()
	database, err := sql.Open("sqlite", sqliteSnapshotURI(path, false))
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

func TestValidatePreLedgerSnapshotEquivalenceRequestRequiresFixedSchema(t *testing.T) {
	transaction := serviceOperationReleaseTransaction{
		fd:        3,
		token:     strings.Repeat("a", 64),
		operation: "update",
		snapshot:  "snapshot-name",
	}
	_, err := validatePreLedgerSnapshotEquivalenceRequest(
		"not-used-when-schema-conflicts",
		"pre-ledger",
		transaction,
		false,
	)
	if err == nil || !strings.Contains(err.Error(), "must not use --snapshot-schema") {
		t.Fatalf("schema conflict err=%v", err)
	}
}

func TestValidatePreLedgerSnapshotEquivalenceRequestRejectsOtherDatabaseMode(t *testing.T) {
	transaction := serviceOperationReleaseTransaction{
		fd:        3,
		token:     strings.Repeat("a", 64),
		operation: "update",
		snapshot:  "snapshot-name",
	}
	_, err := validatePreLedgerSnapshotEquivalenceRequest(
		"not-used-when-mode-conflicts",
		"",
		transaction,
		true,
	)
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("mode conflict err=%v", err)
	}
}

func TestValidatePreLedgerSnapshotEquivalenceRequestNotRequested(t *testing.T) {
	requested, err := validatePreLedgerSnapshotEquivalenceRequest(
		"",
		"",
		serviceOperationReleaseTransaction{},
		false,
	)
	if err != nil || requested {
		t.Fatalf("requested=%v err=%v", requested, err)
	}
}
