package db

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

func TestDomainDeletionOperationsMigrationContracts(t *testing.T) {
	database := newDomainDeletionOperationsMigrationDB(t)

	domainIDs := make(map[string]int64)
	for _, status := range []string{"active", "suspended", "pending"} {
		domainID := insertDomainDeletionOperationTestDomain(t, database, status)
		domainIDs[status] = domainID
		if _, err := database.Exec(`
			INSERT INTO domain_deletion_operations (domain_id, previous_status)
			VALUES (?, ?)`, domainID, status); err != nil {
			t.Fatalf("insert operation with previous_status %q: %v", status, err)
		}
	}

	invalidDomainID := insertDomainDeletionOperationTestDomain(t, database, "invalid")
	requireDomainDeletionOperationSQLFailure(t, database, "invalid previous_status", "CHECK constraint failed", `
		INSERT INTO domain_deletion_operations (domain_id, previous_status)
		VALUES (?, 'deleting')`, invalidDomainID)

	requireDomainDeletionOperationSQLFailure(t, database, "second operation for domain", "UNIQUE constraint failed", `
		INSERT INTO domain_deletion_operations (domain_id, previous_status)
		VALUES (?, 'pending')`, domainIDs["active"])

	if _, err := database.Exec(`DELETE FROM domains WHERE id = ?`, domainIDs["active"]); err != nil {
		t.Fatalf("delete domain: %v", err)
	}
	var operationCount int
	if err := database.QueryRow(`
		SELECT COUNT(*) FROM domain_deletion_operations WHERE domain_id = ?`,
		domainIDs["active"],
	).Scan(&operationCount); err != nil {
		t.Fatalf("count cascaded domain deletion operations: %v", err)
	}
	if operationCount != 0 {
		t.Fatalf("operation count after domain delete = %d, want 0", operationCount)
	}
}

func newDomainDeletionOperationsMigrationDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := NewSQLiteDB(filepath.Join(t.TempDir(), "panel.sqlite"))
	if err != nil {
		t.Fatalf("open migrated database: %v", err)
	}
	t.Cleanup(database.Close)
	return database.GetDB()
}

func insertDomainDeletionOperationTestDomain(t *testing.T, database *sql.DB, suffix string) int64 {
	t.Helper()
	userResult, err := database.Exec(`
		INSERT INTO users (username, password_hash, email, role)
		VALUES (?, 'hash', ?, 'customer')`, "owner-"+suffix, "owner-"+suffix+"@example.test")
	if err != nil {
		t.Fatalf("insert owner %q: %v", suffix, err)
	}
	ownerID, err := userResult.LastInsertId()
	if err != nil {
		t.Fatalf("read owner %q id: %v", suffix, err)
	}

	subscriptionResult, err := database.Exec(`
		INSERT INTO subscriptions (owner_id, name) VALUES (?, ?)`, ownerID, "subscription-"+suffix)
	if err != nil {
		t.Fatalf("insert subscription %q: %v", suffix, err)
	}
	subscriptionID, err := subscriptionResult.LastInsertId()
	if err != nil {
		t.Fatalf("read subscription %q id: %v", suffix, err)
	}

	domainResult, err := database.Exec(`
		INSERT INTO domains (subscription_id, name) VALUES (?, ?)`, subscriptionID, suffix+".example.test")
	if err != nil {
		t.Fatalf("insert domain %q: %v", suffix, err)
	}
	domainID, err := domainResult.LastInsertId()
	if err != nil {
		t.Fatalf("read domain %q id: %v", suffix, err)
	}
	return domainID
}

func requireDomainDeletionOperationSQLFailure(
	t *testing.T,
	database *sql.DB,
	name string,
	want string,
	query string,
	args ...any,
) {
	t.Helper()
	if _, err := database.Exec(query, args...); err == nil {
		t.Fatalf("%s unexpectedly succeeded", name)
	} else if !strings.Contains(err.Error(), want) {
		t.Fatalf("%s error = %q, want containing %q", name, err, want)
	}
}
