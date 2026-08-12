package db

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestAdditionalUsersMigrationBackfillsLegacyAccounts(t *testing.T) {
	database := newPreAdditionalUsersMigrationDB(t)

	legacyUserID := insertTestUser(t, database, "legacy-customer", "customer", nil, "")
	applyEmbeddedMigrationVersion(t, database, 29)

	var accountType string
	if err := database.QueryRow(
		`SELECT account_type FROM users WHERE id = ?`, legacyUserID,
	).Scan(&accountType); err != nil {
		t.Fatalf("read legacy user account type: %v", err)
	}
	if accountType != "account" {
		t.Fatalf("legacy account_type = %q, want account", accountType)
	}

	newUserID := insertTestUser(t, database, "new-customer", "customer", nil, "")
	if err := database.QueryRow(
		`SELECT account_type FROM users WHERE id = ?`, newUserID,
	).Scan(&accountType); err != nil {
		t.Fatalf("read new user account type: %v", err)
	}
	if accountType != "account" {
		t.Fatalf("default account_type = %q, want account", accountType)
	}

	assertForeignKeyCheckClean(t, database)
}

func TestAdditionalUsersMigrationRejectsInvalidIdentityAndGrantEnums(t *testing.T) {
	database := newAdditionalUsersMigrationDB(t)
	ownerID := insertTestUser(t, database, "owner", "customer", nil, "account")
	subscriptionID := insertTestSubscription(t, database, ownerID, "owner-subscription")
	domainID := insertTestDomain(t, database, subscriptionID, "owner.example")
	memberID := insertTestUser(t, database, "member", "customer", &ownerID, "additional_user")

	requireSQLFailure(t, database, "invalid account type", "CHECK constraint failed", `
		INSERT INTO users (username, password_hash, email, role, account_type)
		VALUES ('invalid-type', 'hash', 'invalid-type@example.test', 'customer', 'unknown')`)
	requireSQLFailure(t, database, "invalid additional-user role", "additional user role must be customer", `
		INSERT INTO users (username, password_hash, email, role, parent_id, account_type)
		VALUES ('invalid-role', 'hash', 'invalid-role@example.test', 'admin', ?, 'additional_user')`, ownerID)

	grantTests := []struct {
		name  string
		query string
		args  []any
	}{
		{
			name:  "subscription capability",
			query: `INSERT INTO additional_user_subscription_permissions (user_id, subscription_id, capability, mode) VALUES (?, ?, 'shell', 'view')`,
			args:  []any{memberID, subscriptionID},
		},
		{
			name:  "subscription mode",
			query: `INSERT INTO additional_user_subscription_permissions (user_id, subscription_id, capability, mode) VALUES (?, ?, 'files', 'owner')`,
			args:  []any{memberID, subscriptionID},
		},
		{
			name:  "domain capability",
			query: `INSERT INTO additional_user_domain_permissions (user_id, domain_id, capability, mode) VALUES (?, ?, 'shell', 'view')`,
			args:  []any{memberID, domainID},
		},
		{
			name:  "domain mode",
			query: `INSERT INTO additional_user_domain_permissions (user_id, domain_id, capability, mode) VALUES (?, ?, 'files', 'owner')`,
			args:  []any{memberID, domainID},
		},
	}
	for _, test := range grantTests {
		t.Run(test.name, func(t *testing.T) {
			requireSQLFailure(t, database, test.name, "CHECK constraint failed", test.query, test.args...)
		})
	}

	assertForeignKeyCheckClean(t, database)
}

func TestAdditionalUsersMigrationRejectsSubscriptionOwnership(t *testing.T) {
	database := newAdditionalUsersMigrationDB(t)
	ownerID := insertTestUser(t, database, "owner", "customer", nil, "account")
	memberID := insertTestUser(t, database, "member", "customer", &ownerID, "additional_user")
	subscriptionID := insertTestSubscription(t, database, ownerID, "owner-subscription")

	requireSQLFailure(t, database, "member subscription insert", "additional users cannot own subscriptions", `
		INSERT INTO subscriptions (owner_id, name) VALUES (?, 'member-subscription')`, memberID)
	requireSQLFailure(t, database, "member subscription update", "additional users cannot own subscriptions", `
		UPDATE subscriptions SET owner_id = ? WHERE id = ?`, memberID, subscriptionID)

	assertForeignKeyCheckClean(t, database)
}

func TestAdditionalUsersMigrationRejectsCrossCustomerGrants(t *testing.T) {
	database := newAdditionalUsersMigrationDB(t)
	ownerA := insertTestUser(t, database, "owner-a", "customer", nil, "account")
	ownerB := insertTestUser(t, database, "owner-b", "customer", nil, "account")
	memberA := insertTestUser(t, database, "member-a", "customer", &ownerA, "additional_user")
	subscriptionA := insertTestSubscription(t, database, ownerA, "subscription-a")
	subscriptionB := insertTestSubscription(t, database, ownerB, "subscription-b")
	domainA := insertTestDomain(t, database, subscriptionA, "a.example")
	domainB := insertTestDomain(t, database, subscriptionB, "b.example")

	mustExec(t, database, `
		INSERT INTO additional_user_subscription_permissions (user_id, subscription_id, capability, mode)
		VALUES (?, ?, 'files', 'manage')`, memberA, subscriptionA)
	mustExec(t, database, `
		INSERT INTO additional_user_domain_permissions (user_id, domain_id, capability, mode)
		VALUES (?, ?, 'dns', 'view')`, memberA, domainA)

	requireSQLFailure(t, database, "cross-customer subscription grant", "crosses tenancy boundary", `
		INSERT INTO additional_user_subscription_permissions (user_id, subscription_id, capability, mode)
		VALUES (?, ?, 'mail', 'view')`, memberA, subscriptionB)
	requireSQLFailure(t, database, "cross-customer domain grant", "crosses tenancy boundary", `
		INSERT INTO additional_user_domain_permissions (user_id, domain_id, capability, mode)
		VALUES (?, ?, 'ssl', 'view')`, memberA, domainB)
	requireSQLFailure(t, database, "subscription grant retarget", "crosses tenancy boundary", `
		UPDATE additional_user_subscription_permissions
		SET subscription_id = ?
		WHERE user_id = ? AND subscription_id = ? AND capability = 'files'`, subscriptionB, memberA, subscriptionA)
	requireSQLFailure(t, database, "domain grant retarget", "crosses tenancy boundary", `
		UPDATE additional_user_domain_permissions
		SET domain_id = ?
		WHERE user_id = ? AND domain_id = ? AND capability = 'dns'`, domainB, memberA, domainA)

	assertForeignKeyCheckClean(t, database)
}

func TestAdditionalUsersMigrationGuardsOwnerAndDomainMoves(t *testing.T) {
	database := newAdditionalUsersMigrationDB(t)
	ownerA := insertTestUser(t, database, "owner-a", "customer", nil, "account")
	ownerB := insertTestUser(t, database, "owner-b", "customer", nil, "account")
	memberA := insertTestUser(t, database, "member-a", "customer", &ownerA, "additional_user")
	subscriptionA := insertTestSubscription(t, database, ownerA, "subscription-a")
	subscriptionB := insertTestSubscription(t, database, ownerB, "subscription-b")
	domainA := insertTestDomain(t, database, subscriptionA, "a.example")

	mustExec(t, database, `
		INSERT INTO additional_user_subscription_permissions (user_id, subscription_id, capability, mode)
		VALUES (?, ?, 'files', 'manage')`, memberA, subscriptionA)
	mustExec(t, database, `
		INSERT INTO additional_user_domain_permissions (user_id, domain_id, capability, mode)
		VALUES (?, ?, 'dns', 'view')`, memberA, domainA)

	requireSQLFailure(t, database, "subscription owner move", "owner conflicts with additional-user grants", `
		UPDATE subscriptions SET owner_id = ? WHERE id = ?`, ownerB, subscriptionA)
	requireSQLFailure(t, database, "domain subscription move", "domain subscription conflicts with additional-user grants", `
		UPDATE domains SET subscription_id = ? WHERE id = ?`, subscriptionB, domainA)
	requireSQLFailure(t, database, "member owner move", "owner conflicts with granted scope", `
		UPDATE users SET parent_id = ? WHERE id = ?`, ownerB, memberA)

	// A domain-only grant must also pin the subscription to its current owner.
	mustExec(t, database, `
		DELETE FROM additional_user_subscription_permissions
		WHERE user_id = ? AND subscription_id = ?`, memberA, subscriptionA)
	requireSQLFailure(t, database, "subscription owner move with domain-only grant", "owner conflicts with additional-user grants", `
		UPDATE subscriptions SET owner_id = ? WHERE id = ?`, ownerB, subscriptionA)

	assertForeignKeyCheckClean(t, database)
}

func TestAdditionalUsersMigrationRejectsSuspendedParent(t *testing.T) {
	database := newAdditionalUsersMigrationDB(t)
	ownerID := insertTestUser(t, database, "owner", "customer", nil, "account")
	mustExec(t, database, `UPDATE users SET status = 'suspended' WHERE id = ?`, ownerID)

	requireSQLFailure(t, database, "member under suspended owner", "requires an active customer account owner", `
		INSERT INTO users (username, password_hash, email, role, parent_id, account_type)
		VALUES ('member', 'hash', 'member@example.test', 'customer', ?, 'additional_user')`, ownerID)

	assertForeignKeyCheckClean(t, database)
}

func TestAdditionalUsersMigrationKeepsAccountTypeImmutable(t *testing.T) {
	database := newAdditionalUsersMigrationDB(t)
	ownerID := insertTestUser(t, database, "owner", "customer", nil, "account")
	plainID := insertTestUser(t, database, "plain", "customer", &ownerID, "account")
	subscriptionID := insertTestSubscription(t, database, ownerID, "owner-subscription")
	domainID := insertTestDomain(t, database, subscriptionID, "owner.example")
	memberID := insertTestUser(t, database, "member", "customer", &ownerID, "additional_user")
	insertBothTestGrants(t, database, memberID, subscriptionID, domainID)

	requireSQLFailure(t, database, "account conversion", "account_type is immutable",
		"UPDATE users SET account_type = 'additional_user' WHERE id = ?", plainID)
	requireSQLFailure(t, database, "member conversion", "account_type is immutable",
		"UPDATE users SET account_type = 'account' WHERE id = ?", memberID)

	assertGrantCounts(t, database, 1, 1)
	assertForeignKeyCheckClean(t, database)
}

func TestAdditionalUsersMigrationRejectsGrantsForSuspendedAccounts(t *testing.T) {
	database := newAdditionalUsersMigrationDB(t)
	ownerID := insertTestUser(t, database, "owner", "customer", nil, "account")
	memberID := insertTestUser(t, database, "member", "customer", &ownerID, "additional_user")
	subscriptionA := insertTestSubscription(t, database, ownerID, "subscription-a")
	subscriptionB := insertTestSubscription(t, database, ownerID, "subscription-b")
	domainA := insertTestDomain(t, database, subscriptionA, "a.example")
	domainB := insertTestDomain(t, database, subscriptionB, "b.example")
	insertBothTestGrants(t, database, memberID, subscriptionA, domainA)

	assertBlocked := func(t *testing.T) {
		t.Helper()
		requireSQLFailure(t, database, "subscription grant insert", "crosses tenancy boundary",
			"INSERT INTO additional_user_subscription_permissions (user_id, subscription_id, capability, mode) VALUES (?, ?, 'mail', 'view')",
			memberID, subscriptionB)
		requireSQLFailure(t, database, "domain grant insert", "crosses tenancy boundary",
			"INSERT INTO additional_user_domain_permissions (user_id, domain_id, capability, mode) VALUES (?, ?, 'ssl', 'view')",
			memberID, domainB)
		requireSQLFailure(t, database, "subscription grant retarget", "crosses tenancy boundary",
			"UPDATE additional_user_subscription_permissions SET subscription_id = ? WHERE user_id = ? AND subscription_id = ? AND capability = 'files'",
			subscriptionB, memberID, subscriptionA)
		requireSQLFailure(t, database, "domain grant retarget", "crosses tenancy boundary",
			"UPDATE additional_user_domain_permissions SET domain_id = ? WHERE user_id = ? AND domain_id = ? AND capability = 'dns'",
			domainB, memberID, domainA)
	}

	mustExec(t, database, "UPDATE users SET status = 'suspended' WHERE id = ?", memberID)
	t.Run("suspended member", assertBlocked)
	mustExec(t, database, "UPDATE users SET status = 'active' WHERE id = ?", memberID)
	mustExec(t, database, "UPDATE users SET status = 'suspended' WHERE id = ?", ownerID)
	t.Run("suspended owner", assertBlocked)

	assertGrantCounts(t, database, 1, 1)
	assertForeignKeyCheckClean(t, database)
}

func TestAdditionalUsersMigrationCascadesGrants(t *testing.T) {
	database := newAdditionalUsersMigrationDB(t)
	ownerID := insertTestUser(t, database, "owner", "customer", nil, "account")
	subscriptionID := insertTestSubscription(t, database, ownerID, "owner-subscription")
	domainID := insertTestDomain(t, database, subscriptionID, "owner.example")
	memberID := insertTestUser(t, database, "member", "customer", &ownerID, "additional_user")

	insertBothTestGrants(t, database, memberID, subscriptionID, domainID)
	mustExec(t, database, `DELETE FROM users WHERE id = ?`, memberID)
	assertGrantCounts(t, database, 0, 0)

	memberID = insertTestUser(t, database, "member-two", "customer", &ownerID, "additional_user")
	insertBothTestGrants(t, database, memberID, subscriptionID, domainID)
	mustExec(t, database, `DELETE FROM subscriptions WHERE id = ?`, subscriptionID)
	assertGrantCounts(t, database, 0, 0)

	assertForeignKeyCheckClean(t, database)
}

func newAdditionalUsersMigrationDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := NewSQLiteDB(filepath.Join(t.TempDir(), "panel.sqlite"))
	if err != nil {
		t.Fatalf("open migrated database: %v", err)
	}
	t.Cleanup(database.Close)
	return database.GetDB()
}

func newPreAdditionalUsersMigrationDB(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "legacy.sqlite")
	database, err := sql.Open("sqlite", fmt.Sprintf(
		"%s?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)", path,
	))
	if err != nil {
		t.Fatalf("open legacy database: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("close legacy database: %v", err)
		}
	})

	migrations, err := loadEmbeddedMigrations()
	if err != nil {
		t.Fatalf("load embedded migrations: %v", err)
	}
	for _, migration := range migrations {
		if migration.version >= 29 {
			break
		}
		if _, err := database.Exec(string(migration.content)); err != nil {
			t.Fatalf("apply legacy migration %s: %v", migration.filename, err)
		}
	}
	return database
}

func applyEmbeddedMigrationVersion(t *testing.T, database *sql.DB, version int) {
	t.Helper()
	migrations, err := loadEmbeddedMigrations()
	if err != nil {
		t.Fatalf("load embedded migrations: %v", err)
	}
	for _, migration := range migrations {
		if migration.version != version {
			continue
		}
		if _, err := database.Exec(string(migration.content)); err != nil {
			t.Fatalf("apply migration %s: %v", migration.filename, err)
		}
		return
	}
	t.Fatalf("embedded migration version %d not found", version)
}

func insertTestUser(t *testing.T, database *sql.DB, username, role string, parentID *int64, accountType string) int64 {
	t.Helper()
	query := `INSERT INTO users (username, password_hash, email, role, parent_id`
	args := []any{username, "hash", username + "@example.test", role, parentID}
	if accountType == "" {
		query += `) VALUES (?, ?, ?, ?, ?)`
	} else {
		query += `, account_type) VALUES (?, ?, ?, ?, ?, ?)`
		args = append(args, accountType)
	}
	result, err := database.Exec(query, args...)
	if err != nil {
		t.Fatalf("insert user %q: %v", username, err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("read user %q id: %v", username, err)
	}
	return id
}

func insertTestSubscription(t *testing.T, database *sql.DB, ownerID int64, name string) int64 {
	t.Helper()
	result, err := database.Exec(`INSERT INTO subscriptions (owner_id, name) VALUES (?, ?)`, ownerID, name)
	if err != nil {
		t.Fatalf("insert subscription %q: %v", name, err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("read subscription %q id: %v", name, err)
	}
	return id
}

func insertTestDomain(t *testing.T, database *sql.DB, subscriptionID int64, name string) int64 {
	t.Helper()
	result, err := database.Exec(`INSERT INTO domains (subscription_id, name) VALUES (?, ?)`, subscriptionID, name)
	if err != nil {
		t.Fatalf("insert domain %q: %v", name, err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("read domain %q id: %v", name, err)
	}
	return id
}

func insertBothTestGrants(t *testing.T, database *sql.DB, memberID, subscriptionID, domainID int64) {
	t.Helper()
	mustExec(t, database, `
		INSERT INTO additional_user_subscription_permissions (user_id, subscription_id, capability, mode)
		VALUES (?, ?, 'files', 'manage')`, memberID, subscriptionID)
	mustExec(t, database, `
		INSERT INTO additional_user_domain_permissions (user_id, domain_id, capability, mode)
		VALUES (?, ?, 'dns', 'view')`, memberID, domainID)
}

func assertGrantCounts(t *testing.T, database *sql.DB, wantSubscription, wantDomain int) {
	t.Helper()
	var subscriptionCount, domainCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM additional_user_subscription_permissions`).Scan(&subscriptionCount); err != nil {
		t.Fatalf("count subscription grants: %v", err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM additional_user_domain_permissions`).Scan(&domainCount); err != nil {
		t.Fatalf("count domain grants: %v", err)
	}
	if subscriptionCount != wantSubscription || domainCount != wantDomain {
		t.Fatalf("grant counts = subscription:%d domain:%d, want subscription:%d domain:%d", subscriptionCount, domainCount, wantSubscription, wantDomain)
	}
}

func assertForeignKeyCheckClean(t *testing.T, database *sql.DB) {
	t.Helper()
	rows, err := database.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatalf("foreign_key_check: %v", err)
	}
	defer rows.Close()
	if rows.Next() {
		var table string
		var rowID int64
		var parent string
		var foreignKeyID int
		if err := rows.Scan(&table, &rowID, &parent, &foreignKeyID); err != nil {
			t.Fatalf("scan foreign_key_check: %v", err)
		}
		t.Fatalf("foreign key violation: table=%s rowid=%d parent=%s fk=%d", table, rowID, parent, foreignKeyID)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("foreign_key_check rows: %v", err)
	}
}

func requireSQLFailure(t *testing.T, database *sql.DB, name, want string, query string, args ...any) {
	t.Helper()
	if _, err := database.Exec(query, args...); err == nil {
		t.Fatalf("%s unexpectedly succeeded", name)
	} else if !strings.Contains(err.Error(), want) {
		t.Fatalf("%s error = %q, want containing %q", name, err, want)
	}
}

func mustExec(t *testing.T, database *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := database.Exec(query, args...); err != nil {
		t.Fatalf("execute SQL: %v", err)
	}
}
