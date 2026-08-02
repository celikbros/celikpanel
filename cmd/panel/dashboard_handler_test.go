package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDashboardReturnsAccurateCountsAndCertificateExpiry(t *testing.T) {
	p, domainID, certificateID := newSSLStateFixture(t)
	database := p.db.GetDB()

	var subscriptionID int
	if err := database.QueryRow(`
		SELECT subscription_id FROM domains WHERE id = ?
	`, domainID).Scan(&subscriptionID); err != nil {
		t.Fatalf("load subscription: %v", err)
	}
	var databaseTypeID int
	if err := database.QueryRow(`
		SELECT id FROM database_server_types WHERE name = 'mariadb'
	`).Scan(&databaseTypeID); err != nil {
		t.Fatalf("load database type: %v", err)
	}
	serverResult, err := database.Exec(`
		INSERT INTO database_servers (
			subscription_id, type_id, name, host, port
		) VALUES (?, ?, 'dashboard-mariadb', '127.0.0.1', 3306)
	`, subscriptionID, databaseTypeID)
	if err != nil {
		t.Fatalf("insert database server: %v", err)
	}
	serverID, err := serverResult.LastInsertId()
	if err != nil {
		t.Fatalf("database server id: %v", err)
	}
	if _, err := database.Exec(`
		INSERT INTO databases_v2 (
			server_id, subscription_id, domain_id, name
		) VALUES (?, ?, ?, 'dashboard_db')
	`, serverID, subscriptionID, domainID); err != nil {
		t.Fatalf("insert database: %v", err)
	}
	if _, err := database.Exec(`
		INSERT INTO email_accounts (
			domain_id, address, password_hash
		) VALUES (?, 'dashboard@ssl-state.example', 'hash')
	`, domainID); err != nil {
		t.Fatalf("insert mail account: %v", err)
	}
	expiresAt := time.Now().UTC().Add(10 * 24 * time.Hour).Format(time.RFC3339)
	if _, err := database.Exec(`
		UPDATE ssl_certificates
		SET expires_at = ?
		WHERE id = ?
	`, expiresAt, certificateID); err != nil {
		t.Fatalf("set certificate expiry: %v", err)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard", nil)
	p.handleDashboard(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s",
			recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var response dashboardExtras
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode dashboard: %v", err)
	}
	if response.Databases != 1 {
		t.Fatalf("database count = %d, want 1", response.Databases)
	}
	if response.MailAccounts != 1 {
		t.Fatalf("mail account count = %d, want 1", response.MailAccounts)
	}
	if len(response.ExpiringCerts) != 1 {
		t.Fatalf("expiring certificates = %#v, want one", response.ExpiringCerts)
	}
	certificate := response.ExpiringCerts[0]
	if certificate.DomainName != "ssl-state.example" {
		t.Fatalf("certificate domain = %q", certificate.DomainName)
	}
	if certificate.DaysLeft < 9 || certificate.DaysLeft > 10 {
		t.Fatalf("certificate days left = %d, want 9 or 10", certificate.DaysLeft)
	}
}

func TestDashboardFailsClosedWhenCountQueryFails(t *testing.T) {
	p := newDNSPanelForTest(t)
	if _, err := p.db.GetDB().Exec(`DROP TABLE databases_v2`); err != nil {
		t.Fatalf("break database-count schema: %v", err)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard", nil)
	p.handleDashboard(recorder, request)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body=%s",
			recorder.Code, http.StatusInternalServerError, recorder.Body.String())
	}
}

func TestDashboardFailsClosedForMalformedActiveCertificateExpiry(t *testing.T) {
	p, _, certificateID := newSSLStateFixture(t)
	if _, err := p.db.GetDB().Exec(`
		UPDATE ssl_certificates
		SET expires_at = 'not-a-timestamp'
		WHERE id = ?
	`, certificateID); err != nil {
		t.Fatalf("corrupt certificate expiry: %v", err)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard", nil)
	p.handleDashboard(recorder, request)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body=%s",
			recorder.Code, http.StatusInternalServerError, recorder.Body.String())
	}
}
