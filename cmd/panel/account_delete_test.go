package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/core"
)

func accountDeleteTarget(t *testing.T, p *Panel, userID int) *core.User {
	t.Helper()
	target := &core.User{ID: userID}
	if err := p.db.GetDB().QueryRow(`
		SELECT username, email, role
		FROM users
		WHERE id = ?
	`, userID).Scan(&target.Username, &target.Email, &target.Role); err != nil {
		t.Fatalf("load account target: %v", err)
	}
	return target
}

func accountDeleteRequest(t *testing.T, p *Panel, target *core.User) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodDelete, "/api/v1/users/"+target.Username, nil)
	recorder := httptest.NewRecorder()
	p.handleDeleteUser(recorder, request, target)
	return recorder
}

func TestDeleteUserRejectsLiveResourcesWithoutCascading(t *testing.T) {
	p, domainID, _ := newSSLStateFixture(t)
	ctx := context.Background()
	database := p.db.GetDB()

	var userID, subscriptionID int
	if err := database.QueryRowContext(ctx, `
		SELECT s.owner_id, s.id
		FROM subscriptions s
		JOIN domains d ON d.subscription_id = s.id
		WHERE d.id = ?
	`, domainID).Scan(&userID, &subscriptionID); err != nil {
		t.Fatalf("load fixture ownership: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO ftp_accounts (
			subscription_id, username, password_hash, home_dir
		) VALUES (?, 'delete-ftp', 'hash', '/srv/delete-ftp')
	`, subscriptionID); err != nil {
		t.Fatalf("insert FTP account: %v", err)
	}

	var databaseTypeID int
	if err := database.QueryRowContext(ctx,
		`SELECT id FROM database_server_types WHERE name = 'mariadb'`,
	).Scan(&databaseTypeID); err != nil {
		t.Fatalf("load database type: %v", err)
	}
	serverResult, err := database.ExecContext(ctx, `
		INSERT INTO database_servers (
			subscription_id, type_id, name, host, port
		) VALUES (?, ?, 'delete-db-server', '127.0.0.1', 3306)
	`, subscriptionID, databaseTypeID)
	if err != nil {
		t.Fatalf("insert database server: %v", err)
	}
	serverID, err := serverResult.LastInsertId()
	if err != nil {
		t.Fatalf("database server id: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO database_users (
			server_id, subscription_id, username, password
		) VALUES (?, ?, 'delete_db_user', 'sealed')
	`, serverID, subscriptionID); err != nil {
		t.Fatalf("insert database user: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO databases_v2 (
			server_id, subscription_id, domain_id, name
		) VALUES (?, ?, ?, 'delete_managed_db')
	`, serverID, subscriptionID, domainID); err != nil {
		t.Fatalf("insert managed database: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO databases (
			subscription_id, name, db_type, db_user, db_password
		) VALUES (?, 'delete_legacy_db', 'mariadb', 'delete_legacy_user', 'sealed')
	`, subscriptionID); err != nil {
		t.Fatalf("insert legacy database: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO vpn_peers (
			subscription_id, name, public_key, preshared_key, ip
		) VALUES (?, 'delete-peer', 'delete-public-key', 'sealed', '10.8.0.250')
	`, subscriptionID); err != nil {
		t.Fatalf("insert VPN peer: %v", err)
	}

	recorder := accountDeleteRequest(t, p, accountDeleteTarget(t, p, userID))
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d; body=%s",
			recorder.Code, http.StatusConflict, recorder.Body.String())
	}
	for _, resource := range []string{
		"domains",
		"FTP accounts",
		"database users",
		"databases",
		"VPN peers",
	} {
		if !strings.Contains(recorder.Body.String(), resource) {
			t.Errorf("response does not name %q: %s", resource, recorder.Body.String())
		}
	}

	for name, query := range map[string]string{
		"user":     `SELECT COUNT(*) FROM users WHERE id = ?`,
		"domain":   `SELECT COUNT(*) FROM domains WHERE id = ?`,
		"VPN peer": `SELECT COUNT(*) FROM vpn_peers WHERE subscription_id = ?`,
	} {
		argument := any(userID)
		if name == "domain" {
			argument = domainID
		} else if name == "VPN peer" {
			argument = subscriptionID
		}
		var count int
		if err := database.QueryRowContext(ctx, query, argument).Scan(&count); err != nil {
			t.Fatalf("count retained %s: %v", name, err)
		}
		if count != 1 {
			t.Fatalf("retained %s count = %d, want 1", name, count)
		}
	}
}

func TestDeleteUserAllowsOnlyMetadataToCascade(t *testing.T) {
	p := newDNSPanelForTest(t)
	ctx := context.Background()
	database := p.db.GetDB()

	userResult, err := database.ExecContext(ctx, `
		INSERT INTO users (username, password_hash, email, role)
		VALUES ('empty-owner', 'hash', 'empty-owner@example.test', 'customer')
	`)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	userID64, err := userResult.LastInsertId()
	if err != nil {
		t.Fatalf("user id: %v", err)
	}
	userID := int(userID64)
	subscriptionResult, err := database.ExecContext(ctx, `
		INSERT INTO subscriptions (owner_id, name)
		VALUES (?, 'Empty subscription')
	`, userID)
	if err != nil {
		t.Fatalf("insert subscription: %v", err)
	}
	subscriptionID, err := subscriptionResult.LastInsertId()
	if err != nil {
		t.Fatalf("subscription id: %v", err)
	}
	var databaseTypeID int
	if err := database.QueryRowContext(ctx,
		`SELECT id FROM database_server_types WHERE name = 'postgresql'`,
	).Scan(&databaseTypeID); err != nil {
		t.Fatalf("load database type: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO database_servers (
			subscription_id, type_id, name, host, port
		) VALUES (?, ?, 'metadata-only', '127.0.0.1', 5432)
	`, subscriptionID, databaseTypeID); err != nil {
		t.Fatalf("insert metadata-only database server: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO subscription_entitlements (subscription_id, product_id)
		VALUES (?, 'application-installer')
	`, subscriptionID); err != nil {
		t.Fatalf("insert entitlement: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO sessions (token_hash, user_id, expires_at)
		VALUES ('delete-session', ?, '2099-01-01T00:00:00Z')
	`, userID); err != nil {
		t.Fatalf("insert session: %v", err)
	}

	recorder := accountDeleteRequest(t, p, accountDeleteTarget(t, p, userID))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s",
			recorder.Code, http.StatusOK, recorder.Body.String())
	}
	for name, query := range map[string]string{
		"user":         `SELECT COUNT(*) FROM users WHERE id = ?`,
		"subscription": `SELECT COUNT(*) FROM subscriptions WHERE id = ?`,
		"session":      `SELECT COUNT(*) FROM sessions WHERE user_id = ?`,
	} {
		argument := any(userID)
		if name == "subscription" {
			argument = subscriptionID
		}
		var count int
		if err := database.QueryRowContext(ctx, query, argument).Scan(&count); err != nil {
			t.Fatalf("count deleted %s: %v", name, err)
		}
		if count != 0 {
			t.Fatalf("deleted %s count = %d, want 0", name, count)
		}
	}
}

func TestDeleteUserFailsClosedWhenDependencyInspectionFails(t *testing.T) {
	p := newDNSPanelForTest(t)
	database := p.db.GetDB()
	result, err := database.Exec(`
		INSERT INTO users (username, password_hash, email, role)
		VALUES ('inspection-owner', 'hash', 'inspection-owner@example.test', 'customer')
	`)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	userID64, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("user id: %v", err)
	}
	userID := int(userID64)
	target := accountDeleteTarget(t, p, userID)
	if _, err := database.Exec(`DROP TABLE vpn_peers`); err != nil {
		t.Fatalf("break dependency schema: %v", err)
	}

	recorder := accountDeleteRequest(t, p, target)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body=%s",
			recorder.Code, http.StatusInternalServerError, recorder.Body.String())
	}
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM users WHERE id = ?`, userID).Scan(&count); err != nil {
		t.Fatalf("count retained user: %v", err)
	}
	if count != 1 {
		t.Fatalf("retained user count = %d, want 1", count)
	}
}
