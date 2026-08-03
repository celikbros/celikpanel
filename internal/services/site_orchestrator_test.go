package services

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/alicelik/celikpanel/internal/core"
	paneldb "github.com/alicelik/celikpanel/internal/db"
	"github.com/alicelik/celikpanel/internal/hostingpath"
)

func newOrchestratorTestDB(t *testing.T) (*paneldb.SQLiteDB, int) {
	t.Helper()
	database, err := paneldb.NewSQLiteDB(filepath.Join(t.TempDir(), "panel.sqlite"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(database.Close)
	raw := database.GetDB()

	userResult, err := raw.Exec(`
		INSERT INTO users (username, password_hash, email, role)
		VALUES ('orchestrator-owner', 'hash', 'orchestrator-owner@example.test', 'customer')`)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	userID, _ := userResult.LastInsertId()
	subscriptionResult, err := raw.Exec(`
		INSERT INTO subscriptions (owner_id, name)
		VALUES (?, 'orchestrator subscription')`, userID)
	if err != nil {
		t.Fatalf("insert subscription: %v", err)
	}
	subscriptionID, _ := subscriptionResult.LastInsertId()
	return database, int(subscriptionID)
}

func TestCreateSiteDNSOnlyHasNoFilesystemIdentityOrAgentCall(t *testing.T) {
	database, subscriptionID := newOrchestratorTestDB(t)
	orchestrator := NewSiteOrchestrator(database.GetDB(), nil)

	response, err := orchestrator.CreateSite(context.Background(), &CreateSiteRequest{
		SubscriptionID: subscriptionID,
		Domain:         "dns-only.example.test",
		ProjectType:    "dnsonly",
	})
	if err != nil {
		t.Fatalf("create DNS-only domain: %v", err)
	}
	if response.ProjectType != "dnsonly" || response.DocumentRoot != "" {
		t.Fatalf("DNS-only response = %#v, want no document root", response)
	}

	var documentRoot, projectType string
	if err := database.GetDB().QueryRow(`
		SELECT document_root, project_type
		FROM sites WHERE id = ?`, response.SiteID,
	).Scan(&documentRoot, &projectType); err != nil {
		t.Fatalf("read DNS-only site: %v", err)
	}
	if projectType != "dnsonly" || documentRoot != "" {
		t.Fatalf("DNS-only row project_type=%q document_root=%q", projectType, documentRoot)
	}
}

func TestCreateSiteRecordDerivesHostedRootFromDatabaseIdentity(t *testing.T) {
	database, subscriptionID := newOrchestratorTestDB(t)
	raw := database.GetDB()
	domainResult, err := raw.Exec(`
		INSERT INTO domains (subscription_id, name, status)
		VALUES (?, 'hosted.example.test', 'active')`, subscriptionID)
	if err != nil {
		t.Fatalf("insert domain: %v", err)
	}
	domainID64, _ := domainResult.LastInsertId()
	domainID := int(domainID64)

	orchestrator := NewSiteOrchestrator(raw, nil)
	site := &core.Site{
		DomainID:     domainID,
		DocumentRoot: "/attacker/controlled/path",
		ProjectType:  "static",
		Status:       "active",
	}
	siteID, err := orchestrator.createSiteRecord(context.Background(), site)
	if err != nil {
		t.Fatalf("create hosted site record: %v", err)
	}

	expected, err := hostingpath.DocumentRoot(subscriptionID, domainID)
	if err != nil {
		t.Fatalf("derive expected root: %v", err)
	}
	if site.DocumentRoot != expected {
		t.Fatalf("site document root = %q, want %q", site.DocumentRoot, expected)
	}
	var stored string
	if err := raw.QueryRow(`SELECT document_root FROM sites WHERE id = ?`, siteID).Scan(&stored); err != nil {
		t.Fatalf("read hosted site: %v", err)
	}
	if stored != expected {
		t.Fatalf("stored document root = %q, want %q", stored, expected)
	}
}
