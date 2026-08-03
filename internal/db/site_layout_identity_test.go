package db_test

import (
	"path/filepath"
	"strings"
	"testing"

	paneldb "github.com/alicelik/celikpanel/internal/db"
	"github.com/alicelik/celikpanel/internal/hostingpath"
)

type siteLayoutFixture struct {
	database       *paneldb.SQLiteDB
	subscriptionID int
	otherSubID     int
	hostedDomainID int
	dnsDomainID    int
	otherDomainID  int
	expectedRoot   string
}

func newSiteLayoutFixture(t *testing.T) siteLayoutFixture {
	t.Helper()
	database, err := paneldb.NewSQLiteDB(filepath.Join(t.TempDir(), "panel.sqlite"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(database.Close)
	raw := database.GetDB()

	userResult, err := raw.Exec(`
		INSERT INTO users (username, password_hash, email, role)
		VALUES ('layout-owner', 'hash', 'layout-owner@example.test', 'customer')`)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	userID, _ := userResult.LastInsertId()

	insertSubscription := func(name string) int {
		t.Helper()
		result, err := raw.Exec(`
			INSERT INTO subscriptions (owner_id, name)
			VALUES (?, ?)`, userID, name)
		if err != nil {
			t.Fatalf("insert subscription %q: %v", name, err)
		}
		id, _ := result.LastInsertId()
		return int(id)
	}
	subscriptionID := insertSubscription("layout subscription")
	otherSubID := insertSubscription("other layout subscription")

	insertDomain := func(subscriptionID int, name string) int {
		t.Helper()
		result, err := raw.Exec(`
			INSERT INTO domains (subscription_id, name, status)
			VALUES (?, ?, 'active')`, subscriptionID, name)
		if err != nil {
			t.Fatalf("insert domain %q: %v", name, err)
		}
		id, _ := result.LastInsertId()
		return int(id)
	}
	hostedDomainID := insertDomain(subscriptionID, "hosted-layout.example.test")
	dnsDomainID := insertDomain(subscriptionID, "dns-layout.example.test")
	otherDomainID := insertDomain(subscriptionID, "other-layout.example.test")

	expectedRoot, err := hostingpath.DocumentRoot(subscriptionID, hostedDomainID)
	if err != nil {
		t.Fatalf("derive document root: %v", err)
	}
	return siteLayoutFixture{
		database:       database,
		subscriptionID: subscriptionID,
		otherSubID:     otherSubID,
		hostedDomainID: hostedDomainID,
		dnsDomainID:    dnsDomainID,
		otherDomainID:  otherDomainID,
		expectedRoot:   expectedRoot,
	}
}

func TestSiteDocumentRootFollowsHostingRoleAndIdentity(t *testing.T) {
	fixture := newSiteLayoutFixture(t)
	raw := fixture.database.GetDB()

	if _, err := raw.Exec(`
		INSERT INTO sites (domain_id, document_root, project_type)
		VALUES (?, '/var/www/celikpanel/subscriptions/999/sites/999/public_html', 'static')`,
		fixture.hostedDomainID,
	); err == nil || !strings.Contains(err.Error(), "identity-derived") {
		t.Fatalf("unsafe hosted insert error = %v, want identity-derived rejection", err)
	}
	if _, err := raw.Exec(`
		INSERT INTO sites (domain_id, document_root, project_type)
		VALUES (?, '', 'static')`,
		fixture.hostedDomainID,
	); err == nil || !strings.Contains(err.Error(), "identity-derived") {
		t.Fatalf("empty hosted root error = %v, want identity-derived rejection", err)
	}
	if _, err := raw.Exec(`
		INSERT INTO sites (domain_id, document_root, project_type)
		VALUES (?, ?, 'static')`, fixture.hostedDomainID, fixture.expectedRoot,
	); err != nil {
		t.Fatalf("insert canonical hosted site: %v", err)
	}
	if _, err := raw.Exec(`
		INSERT INTO sites (domain_id, document_root, project_type)
		VALUES (?, '', 'dnsonly')`, fixture.dnsDomainID,
	); err != nil {
		t.Fatalf("insert DNS-only site without filesystem: %v", err)
	}
	if _, err := raw.Exec(`
		INSERT INTO sites (domain_id, document_root, project_type)
		VALUES (?, ?, 'dnsonly')`,
		fixture.otherDomainID, fixture.expectedRoot,
	); err == nil || !strings.Contains(err.Error(), "identity-derived") {
		t.Fatalf("DNS-only path error = %v, want identity-derived rejection", err)
	}

	for _, unsafeRoot := range []string{
		"/var/www/celikpanel/subscriptions/999/sites/999/public_html",
		fixture.expectedRoot + "-sibling",
		fixture.expectedRoot + "/../victim",
		fixture.expectedRoot + "\nroot /tmp;",
		"",
	} {
		if _, err := raw.Exec(`
			UPDATE sites SET document_root = ? WHERE domain_id = ?`,
			unsafeRoot, fixture.hostedDomainID,
		); err == nil || !strings.Contains(err.Error(), "identity-derived") {
			t.Errorf("unsafe update %q error = %v, want identity-derived rejection", unsafeRoot, err)
		}
	}
	if _, err := raw.Exec(`
		UPDATE sites SET status = 'suspended' WHERE domain_id = ?`,
		fixture.hostedDomainID,
	); err != nil {
		t.Fatalf("unrelated site update must remain valid: %v", err)
	}
}

func TestSiteAndSubscriptionFilesystemIdentitiesAreImmutable(t *testing.T) {
	fixture := newSiteLayoutFixture(t)
	raw := fixture.database.GetDB()
	if _, err := raw.Exec(`
		INSERT INTO sites (domain_id, document_root, project_type)
		VALUES (?, ?, 'php')`, fixture.hostedDomainID, fixture.expectedRoot,
	); err != nil {
		t.Fatalf("insert hosted site: %v", err)
	}
	if _, err := raw.Exec(`
		INSERT INTO sites (domain_id, document_root, project_type)
		VALUES (?, '', 'dnsonly')`, fixture.dnsDomainID,
	); err != nil {
		t.Fatalf("insert DNS-only site: %v", err)
	}

	if _, err := raw.Exec(`
		UPDATE sites SET domain_id = ? WHERE domain_id = ?`,
		fixture.otherDomainID, fixture.hostedDomainID,
	); err == nil || !strings.Contains(err.Error(), "domain_id is immutable") {
		t.Fatalf("domain identity update error = %v, want immutable rejection", err)
	}
	if _, err := raw.Exec(`
		UPDATE domains SET subscription_id = ? WHERE id = ?`,
		fixture.otherSubID, fixture.hostedDomainID,
	); err == nil || !strings.Contains(err.Error(), "subscription_id is immutable") {
		t.Fatalf("hosted subscription update error = %v, want immutable rejection", err)
	}
	if _, err := raw.Exec(`
		UPDATE domains SET subscription_id = ? WHERE id = ?`,
		fixture.otherSubID, fixture.dnsDomainID,
	); err != nil {
		t.Fatalf("DNS-only subscription reassignment must remain valid: %v", err)
	}
	if _, err := raw.Exec(`
		UPDATE sites SET project_type = 'dnsonly', document_root = ''
		WHERE domain_id = ?`, fixture.hostedDomainID,
	); err == nil || !strings.Contains(err.Error(), "hosting role cannot be changed") {
		t.Fatalf("hosted to DNS-only transition error = %v, want orchestrated-transition rejection", err)
	}
	if _, err := raw.Exec(`
		UPDATE sites SET project_type = 'static'
		WHERE domain_id = ?`, fixture.hostedDomainID,
	); err != nil {
		t.Fatalf("hosted project type change must remain valid: %v", err)
	}
}

func TestSiteLayoutMigrationFailsClosedForLegacyPoisonedRow(t *testing.T) {
	fixture := newSiteLayoutFixture(t)
	raw := fixture.database.GetDB()
	if _, err := raw.Exec(`
		INSERT INTO sites (domain_id, document_root, project_type)
		VALUES (?, ?, 'static')`, fixture.hostedDomainID, fixture.expectedRoot,
	); err != nil {
		t.Fatalf("insert hosted site: %v", err)
	}

	for _, statement := range []string{
		`DROP TRIGGER trg_sites_document_root_identity_insert`,
		`DROP TRIGGER trg_sites_document_root_identity_update`,
		`DROP TRIGGER trg_sites_hosting_role_immutable`,
		`DROP TRIGGER trg_sites_domain_identity_immutable`,
		`DROP TRIGGER trg_domains_hosted_subscription_identity_immutable`,
		`DELETE FROM schema_migrations WHERE version = 20`,
	} {
		if _, err := raw.Exec(statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
	if _, err := raw.Exec(`
		UPDATE sites
		SET document_root = '/var/www/celikpanel/subscriptions/999/sites/999/public_html'
		WHERE domain_id = ?`, fixture.hostedDomainID,
	); err != nil {
		t.Fatalf("poison legacy row: %v", err)
	}

	err := fixture.database.RunMigrations()
	if err == nil || !strings.Contains(err.Error(), "CHECK constraint failed") {
		t.Fatalf("migration error = %v, want fail-closed preflight", err)
	}
	var applied int
	if err := raw.QueryRow(`
		SELECT COUNT(*) FROM schema_migrations WHERE version = 20`).Scan(&applied); err != nil {
		t.Fatalf("query migration state: %v", err)
	}
	if applied != 0 {
		t.Fatalf("migration 20 marked applied after preflight failure")
	}
	var triggers int
	if err := raw.QueryRow(`
		SELECT COUNT(*) FROM sqlite_master
		WHERE type = 'trigger'
		  AND name IN (
		      'trg_sites_document_root_identity_insert',
		      'trg_sites_document_root_identity_update',
		      'trg_sites_hosting_role_immutable',
		      'trg_sites_domain_identity_immutable',
		      'trg_domains_hosted_subscription_identity_immutable'
		  )`).Scan(&triggers); err != nil {
		t.Fatalf("query trigger state: %v", err)
	}
	if triggers != 0 {
		t.Fatalf("migration left %d partial triggers after rollback", triggers)
	}
}
