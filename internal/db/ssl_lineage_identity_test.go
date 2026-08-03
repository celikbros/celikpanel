package db

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestSSLLineageIdentityMigrationBackfillsLegacyCertificateHistory(t *testing.T) {
	database := newDatabaseAtMigrationVersion(t, 20)
	raw := database.GetDB()

	assertSSLIdentityColumnsAbsent(t, raw)

	userResult, err := raw.Exec(`
		INSERT INTO users (username, password_hash, email, role)
		VALUES ('ssl-migration-owner', 'hash', 'ssl-migration-owner@example.test', 'customer')`)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	userID, err := userResult.LastInsertId()
	if err != nil {
		t.Fatalf("read user id: %v", err)
	}

	subscriptionResult, err := raw.Exec(`
		INSERT INTO subscriptions (owner_id, name)
		VALUES (?, 'SSL migration subscription')`, userID)
	if err != nil {
		t.Fatalf("insert subscription: %v", err)
	}
	subscriptionID, err := subscriptionResult.LastInsertId()
	if err != nil {
		t.Fatalf("read subscription id: %v", err)
	}

	type certificateFamily struct {
		domain     string
		certType   string
		issuer     string
		providerID string
	}
	families := []certificateFamily{
		{
			domain:     "letsencrypt-history.example.test",
			certType:   "letsencrypt",
			issuer:     "  Let's Encrypt  ",
			providerID: "letsencrypt",
		},
		{
			domain:     "zerossl-history.example.test",
			certType:   "letsencrypt",
			issuer:     "  ZEROSSL  ",
			providerID: "zerossl",
		},
		{
			domain:     "google-history.example.test",
			certType:   "letsencrypt",
			issuer:     "  GOOGLE TRUST SERVICES  ",
			providerID: "google",
		},
		{
			domain:   "custom-history.example.test",
			certType: "custom",
			// A custom certificate must stay provider-less even when its
			// display issuer happens to match a supported ACME provider.
			issuer: "Let's Encrypt",
		},
	}

	type expectedCertificate struct {
		id         int64
		domain     string
		status     string
		lineage    sql.NullString
		providerID sql.NullString
	}
	var expected []expectedCertificate
	for _, family := range families {
		domainResult, err := raw.Exec(`
			INSERT INTO domains (subscription_id, name, status)
			VALUES (?, ?, 'active')`, subscriptionID, family.domain)
		if err != nil {
			t.Fatalf("insert domain %q: %v", family.domain, err)
		}
		domainID, err := domainResult.LastInsertId()
		if err != nil {
			t.Fatalf("read domain %q id: %v", family.domain, err)
		}

		for _, status := range []string{"active", "revoked"} {
			certificateResult, err := raw.Exec(`
				INSERT INTO ssl_certificates (
					domain_id, type, cert_path, key_path, issuer, expires_at, status
				)
				VALUES (?, ?, ?, ?, ?, '2030-01-01 00:00:00', ?)`,
				domainID,
				family.certType,
				"/legacy/"+family.domain+"/"+status+"/fullchain.pem",
				"/legacy/"+family.domain+"/"+status+"/privkey.pem",
				family.issuer,
				status,
			)
			if err != nil {
				t.Fatalf("insert %s certificate for %q: %v", status, family.domain, err)
			}
			certificateID, err := certificateResult.LastInsertId()
			if err != nil {
				t.Fatalf("read %s certificate id for %q: %v", status, family.domain, err)
			}

			lineage := sql.NullString{}
			providerID := sql.NullString{}
			if family.certType == "letsencrypt" {
				lineage = sql.NullString{String: family.domain, Valid: true}
				providerID = sql.NullString{String: family.providerID, Valid: true}
			}
			expected = append(expected, expectedCertificate{
				id:         certificateID,
				domain:     family.domain,
				status:     status,
				lineage:    lineage,
				providerID: providerID,
			})
		}
	}

	if err := database.RunMigrations(); err != nil {
		t.Fatalf("run v20 to v21 migration: %v", err)
	}

	var version21 int
	if err := raw.QueryRow(`
		SELECT COUNT(*) FROM schema_migrations WHERE version = 21`).Scan(&version21); err != nil {
		t.Fatalf("query migration 21 state: %v", err)
	}
	if version21 != 1 {
		t.Fatalf("migration 21 applied rows = %d, want 1", version21)
	}

	for _, want := range expected {
		var (
			gotDomain   string
			gotStatus   string
			gotLineage  sql.NullString
			gotProvider sql.NullString
		)
		err := raw.QueryRow(`
			SELECT d.name, c.status, c.lineage_name, c.acme_provider_id
			FROM ssl_certificates c
			JOIN domains d ON d.id = c.domain_id
			WHERE c.id = ?`, want.id,
		).Scan(&gotDomain, &gotStatus, &gotLineage, &gotProvider)
		if err != nil {
			t.Fatalf("query migrated certificate %d: %v", want.id, err)
		}
		if gotDomain != want.domain {
			t.Errorf("certificate %d domain = %q, want %q", want.id, gotDomain, want.domain)
		}
		if gotStatus != want.status {
			t.Errorf("certificate %d status = %q, want %q", want.id, gotStatus, want.status)
		}
		if gotLineage != want.lineage {
			t.Errorf(
				"certificate %d (%s, %s) lineage = %#v, want %#v",
				want.id,
				want.domain,
				want.status,
				gotLineage,
				want.lineage,
			)
		}
		if gotProvider != want.providerID {
			t.Errorf(
				"certificate %d (%s, %s) provider = %#v, want %#v",
				want.id,
				want.domain,
				want.status,
				gotProvider,
				want.providerID,
			)
		}
	}
}

func newDatabaseAtMigrationVersion(t *testing.T, targetVersion int) *SQLiteDB {
	t.Helper()

	path := filepath.Join(t.TempDir(), "panel.sqlite")
	raw, err := sql.Open(
		"sqlite",
		fmt.Sprintf(
			"%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)",
			path,
		),
	)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() {
		_ = raw.Close()
	})
	if err := raw.Ping(); err != nil {
		t.Fatalf("ping database: %v", err)
	}
	if _, err := raw.Exec(`
		CREATE TABLE schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at TEXT DEFAULT (datetime('now'))
		)`); err != nil {
		t.Fatalf("create migration ledger: %v", err)
	}

	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		t.Fatalf("read embedded migrations: %v", err)
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)

	appliedTarget := false
	for _, name := range names {
		var version int
		if _, err := fmt.Sscanf(name, "%d_", &version); err != nil {
			t.Fatalf("parse migration version from %q: %v", name, err)
		}
		if version > targetVersion {
			continue
		}

		content, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatalf("read migration %q: %v", name, err)
		}
		tx, err := raw.Begin()
		if err != nil {
			t.Fatalf("begin migration %q: %v", name, err)
		}
		if _, err := tx.Exec(string(content)); err != nil {
			_ = tx.Rollback()
			t.Fatalf("apply migration %q: %v", name, err)
		}
		if _, err := tx.Exec(
			`INSERT INTO schema_migrations (version) VALUES (?)`,
			version,
		); err != nil {
			_ = tx.Rollback()
			t.Fatalf("record migration %q: %v", name, err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("commit migration %q: %v", name, err)
		}
		if version == targetVersion {
			appliedTarget = true
		}
	}
	if !appliedTarget {
		t.Fatalf("migration version %d was not found", targetVersion)
	}

	return &SQLiteDB{db: raw}
}

func assertSSLIdentityColumnsAbsent(t *testing.T, raw *sql.DB) {
	t.Helper()

	rows, err := raw.Query(`PRAGMA table_info(ssl_certificates)`)
	if err != nil {
		t.Fatalf("inspect v20 ssl_certificates schema: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			columnID  int
			name      string
			columnTyp string
			notNull   int
			defaultV  sql.NullString
			primary   int
		)
		if err := rows.Scan(
			&columnID,
			&name,
			&columnTyp,
			&notNull,
			&defaultV,
			&primary,
		); err != nil {
			t.Fatalf("scan v20 ssl_certificates schema: %v", err)
		}
		if name == "lineage_name" || name == "acme_provider_id" {
			t.Fatalf("v20 unexpectedly contains %q", name)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate v20 ssl_certificates schema: %v", err)
	}
}
