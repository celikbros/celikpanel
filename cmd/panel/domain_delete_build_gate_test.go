package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/hostingpath"
)

func TestDeleteDomainRejectsMismatchedAgentBuildBeforeSiteCleanup(t *testing.T) {
	p := newDNSPanelForTest(t)
	db := p.db.GetDB()

	userResult, err := db.Exec(`
		INSERT INTO users (username, password_hash, email, role)
		VALUES ('delete-pair-owner', 'hash', 'delete-pair@example.test', 'customer')`)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	userID, err := userResult.LastInsertId()
	if err != nil {
		t.Fatalf("user id: %v", err)
	}
	subscriptionResult, err := db.Exec(`
		INSERT INTO subscriptions (owner_id, name)
		VALUES (?, 'Delete build pair')`, userID)
	if err != nil {
		t.Fatalf("insert subscription: %v", err)
	}
	subscriptionID, err := subscriptionResult.LastInsertId()
	if err != nil {
		t.Fatalf("subscription id: %v", err)
	}
	domainResult, err := db.Exec(`
		INSERT INTO domains (subscription_id, name)
		VALUES (?, 'delete-pair.example')`, subscriptionID)
	if err != nil {
		t.Fatalf("insert domain: %v", err)
	}
	domainID64, err := domainResult.LastInsertId()
	if err != nil {
		t.Fatalf("domain id: %v", err)
	}
	domainID := int(domainID64)
	documentRoot, err := hostingpath.DocumentRoot(int(subscriptionID), domainID)
	if err != nil {
		t.Fatalf("derive document root: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO sites (
			domain_id, document_root, project_type, php_version, status
		) VALUES (?, ?, 'static', '', 'active')`,
		domainID, documentRoot,
	); err != nil {
		t.Fatalf("insert site: %v", err)
	}

	withPanelBuildCommit(t, "panel-release-commit")
	attachVersionPairAgent(t, p, "agent-other-commit")

	request := httptest.NewRequest(
		http.MethodDelete,
		fmt.Sprintf("/api/v1/domains/%d", domainID),
		nil,
	)
	recorder := httptest.NewRecorder()
	p.handleDeleteDomain(recorder, request)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("delete status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "panel/agent build mismatch") {
		t.Fatalf("delete body = %q, want explicit build mismatch", recorder.Body.String())
	}
	var domainCount, siteCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM domains WHERE id = ?`, domainID).
		Scan(&domainCount); err != nil {
		t.Fatalf("count domain: %v", err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM sites WHERE domain_id = ?`, domainID).
		Scan(&siteCount); err != nil {
		t.Fatalf("count site: %v", err)
	}
	if domainCount != 1 || siteCount != 1 {
		t.Fatalf(
			"mismatched delete state = domains:%d sites:%d, want 1/1",
			domainCount,
			siteCount,
		)
	}
}
