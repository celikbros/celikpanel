package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/auth"
	paneldb "github.com/alicelik/celikpanel/internal/db"
	"github.com/alicelik/celikpanel/internal/repositories"
	"github.com/alicelik/celikpanel/internal/secrets"
	"github.com/alicelik/celikpanel/internal/services"
)

const (
	authzMatrixAdminID          = 9701
	authzMatrixResellerID       = 9702
	authzMatrixCustomerID       = 9703
	authzMatrixOutsiderID       = 9704
	authzMatrixSuspendedID      = 9705
	authzMatrixResellerSubID    = 9801
	authzMatrixCustomerSubID    = 9802
	authzMatrixOutsiderSubID    = 9803
	authzMatrixResellerDomainID = 9901
	authzMatrixCustomerDomainID = 9902
	authzMatrixOutsiderDomainID = 9903
	authzMatrixDatabaseServerID = 9911
	authzMatrixDatabaseID       = 9912
	authzMatrixForeignDBUserID  = 9913
)

type authzGrantIsolationDriver struct {
	grantCalls int
}

func (d *authzGrantIsolationDriver) TestConnection() error                 { return nil }
func (d *authzGrantIsolationDriver) CreateDatabase(string) error           { return nil }
func (d *authzGrantIsolationDriver) DeleteDatabase(string) error           { return nil }
func (d *authzGrantIsolationDriver) ListDatabases() ([]string, error)      { return nil, nil }
func (d *authzGrantIsolationDriver) CreateUser(string, string) error       { return nil }
func (d *authzGrantIsolationDriver) DeleteUser(string) error               { return nil }
func (d *authzGrantIsolationDriver) ChangePassword(string, string) error   { return nil }
func (d *authzGrantIsolationDriver) ListUsers() ([]string, error)          { return nil, nil }
func (d *authzGrantIsolationDriver) RevokePrivileges(string, string) error { return nil }
func (d *authzGrantIsolationDriver) GrantPrivileges(string, string, string) error {
	d.grantCalls++
	return nil
}

type authzMatrixFixture struct {
	panel   *Panel
	handler http.Handler
	tokens  map[int]string
}

func newAuthzMatrixFixture(t *testing.T) authzMatrixFixture {
	t.Helper()
	database, err := paneldb.NewSQLiteDB(filepath.Join(t.TempDir(), "panel.sqlite"))
	if err != nil {
		t.Fatalf("open authz matrix database: %v", err)
	}
	t.Cleanup(database.Close)

	sqlDB := database.GetDB()
	_, err = sqlDB.Exec(`
		INSERT INTO users (id, username, password_hash, email, role, parent_id, status)
		VALUES
			(9701, 'matrix-admin', 'x', 'matrix-admin@example.test', 'admin', NULL, 'active'),
			(9702, 'matrix-reseller', 'x', 'matrix-reseller@example.test', 'reseller', NULL, 'active'),
			(9703, 'matrix-customer', 'x', 'matrix-customer@example.test', 'customer', 9702, 'active'),
			(9704, 'matrix-outsider', 'x', 'matrix-outsider@example.test', 'customer', NULL, 'active'),
			(9705, 'matrix-suspended', 'x', 'matrix-suspended@example.test', 'customer', NULL, 'suspended');
		INSERT INTO subscriptions (id, owner_id, name, status)
		VALUES
			(9801, 9702, 'reseller subscription', 'active'),
			(9802, 9703, 'customer subscription', 'active'),
			(9803, 9704, 'outsider subscription', 'active');
		INSERT INTO domains (id, subscription_id, name, status)
		VALUES
			(9901, 9801, 'reseller.matrix.test', 'active'),
			(9902, 9802, 'customer.matrix.test', 'active'),
			(9903, 9803, 'outsider.matrix.test', 'active');
	`)
	if err != nil {
		t.Fatalf("seed authz matrix: %v", err)
	}

	sessions := auth.NewSessionStore(sqlDB)
	panel := &Panel{
		db:       database,
		sessions: sessions,
		users:    repositories.NewPostgresUserRepository(sqlDB),
	}
	tokens := make(map[int]string)
	for _, id := range []int{
		authzMatrixAdminID,
		authzMatrixResellerID,
		authzMatrixCustomerID,
		authzMatrixOutsiderID,
		authzMatrixSuspendedID,
	} {
		token, createErr := sessions.Create(context.Background(), id)
		if createErr != nil {
			t.Fatalf("create session for user %d: %v", id, createErr)
		}
		tokens[id] = token
	}

	mux := http.NewServeMux()
	pass := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isPublicPath(r) && currentCaller(r) == nil {
			t.Error("protected route reached handler without caller")
			http.Error(w, "caller missing", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	for _, path := range []string{
		"/",
		"/api/v1/auth/login",
		"/api/v1/auth/login/totp",
		"/api/v1/auth/demo",
		"/api/v1/auth/me",
		"/api/v1/system/stats",
		"/api/v1/service/install",
		"/api/v1/system-databases",
		storeCatalogAdminPath,
	} {
		mux.Handle(path, pass)
	}
	mux.HandleFunc("/api/v1/subscriptions/", panel.handleSubscriptionEntitlements)
	mux.HandleFunc("/api/v1/databases/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/grants") {
			panel.handleGrantDatabaseAccess(w, r)
			return
		}
		http.NotFound(w, r)
	})
	mux.HandleFunc("/api/v1/domains/", func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, "/api/v1/domains/")
		idPart := strings.SplitN(rest, "/", 2)[0]
		domainID, parseErr := strconv.Atoi(idPart)
		if parseErr != nil {
			http.NotFound(w, r)
			return
		}
		if !panel.authorizeDomain(w, r, domainID) {
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	return authzMatrixFixture{
		panel:   panel,
		handler: panel.requireAuth(mux),
		tokens:  tokens,
	}
}

func (f authzMatrixFixture) request(t *testing.T, method, path string, userID int) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, nil)
	if userID != 0 {
		request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: f.tokens[userID]})
	}
	recorder := httptest.NewRecorder()
	f.handler.ServeHTTP(recorder, request)
	return recorder
}

func TestRequireAuthRoleEndpointMatrix(t *testing.T) {
	fixture := newAuthzMatrixFixture(t)
	tests := []struct {
		name   string
		method string
		path   string
		userID int
		want   int
	}{
		{name: "SPA remains public", method: http.MethodGet, path: "/settings", want: http.StatusNoContent},
		{name: "login remains public", method: http.MethodPost, path: "/api/v1/auth/login", want: http.StatusNoContent},
		{name: "TOTP login remains public", method: http.MethodPost, path: "/api/v1/auth/login/totp", want: http.StatusNoContent},
		{name: "demo metadata remains public", method: http.MethodGet, path: "/api/v1/auth/demo", want: http.StatusNoContent},
		{name: "protected API rejects anonymous", method: http.MethodGet, path: "/api/v1/auth/me", want: http.StatusUnauthorized},
		{name: "protected API rejects invalid cookie", method: http.MethodGet, path: "/api/v1/system/stats", userID: -1, want: http.StatusUnauthorized},
		{name: "admin may use authenticated endpoint", method: http.MethodGet, path: "/api/v1/system/stats", userID: authzMatrixAdminID, want: http.StatusNoContent},
		{name: "reseller may use authenticated endpoint", method: http.MethodGet, path: "/api/v1/system/stats", userID: authzMatrixResellerID, want: http.StatusNoContent},
		{name: "customer may use authenticated endpoint", method: http.MethodGet, path: "/api/v1/system/stats", userID: authzMatrixCustomerID, want: http.StatusNoContent},
		{name: "suspended session is rejected", method: http.MethodGet, path: "/api/v1/system/stats", userID: authzMatrixSuspendedID, want: http.StatusForbidden},
		{name: "admin may install services", method: http.MethodPost, path: "/api/v1/service/install", userID: authzMatrixAdminID, want: http.StatusNoContent},
		{name: "reseller may not install services", method: http.MethodPost, path: "/api/v1/service/install", userID: authzMatrixResellerID, want: http.StatusForbidden},
		{name: "customer may not install services", method: http.MethodPost, path: "/api/v1/service/install", userID: authzMatrixCustomerID, want: http.StatusForbidden},
		{name: "reseller may not maintain system SQLite", method: http.MethodGet, path: "/api/v1/system-databases", userID: authzMatrixResellerID, want: http.StatusForbidden},
		{name: "customer may not edit Store catalog", method: http.MethodGet, path: storeCatalogAdminPath, userID: authzMatrixCustomerID, want: http.StatusForbidden},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.path, nil)
			if test.userID == -1 {
				request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "invalid-token"})
			} else if test.userID != 0 {
				request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: fixture.tokens[test.userID]})
			}
			recorder := httptest.NewRecorder()
			fixture.handler.ServeHTTP(recorder, request)
			if recorder.Code != test.want {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, test.want, recorder.Body.String())
			}
		})
	}
}

func TestRequireAuthCrossTenantDomainMatrix(t *testing.T) {
	fixture := newAuthzMatrixFixture(t)
	tests := []struct {
		name     string
		userID   int
		domainID int
		want     int
	}{
		{name: "anonymous is stopped by middleware", domainID: authzMatrixCustomerDomainID, want: http.StatusUnauthorized},
		{name: "admin sees every tenant", userID: authzMatrixAdminID, domainID: authzMatrixOutsiderDomainID, want: http.StatusNoContent},
		{name: "reseller sees own domain", userID: authzMatrixResellerID, domainID: authzMatrixResellerDomainID, want: http.StatusNoContent},
		{name: "reseller sees direct customer domain", userID: authzMatrixResellerID, domainID: authzMatrixCustomerDomainID, want: http.StatusNoContent},
		{name: "reseller cannot probe outsider domain", userID: authzMatrixResellerID, domainID: authzMatrixOutsiderDomainID, want: http.StatusNotFound},
		{name: "customer sees own domain", userID: authzMatrixCustomerID, domainID: authzMatrixCustomerDomainID, want: http.StatusNoContent},
		{name: "customer cannot probe reseller domain", userID: authzMatrixCustomerID, domainID: authzMatrixResellerDomainID, want: http.StatusNotFound},
		{name: "customer cannot probe outsider domain", userID: authzMatrixCustomerID, domainID: authzMatrixOutsiderDomainID, want: http.StatusNotFound},
		{name: "missing and foreign domain are indistinguishable", userID: authzMatrixCustomerID, domainID: 999999, want: http.StatusNotFound},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := fmt.Sprintf("/api/v1/domains/%d/general", test.domainID)
			recorder := fixture.request(t, http.MethodGet, path, test.userID)
			if recorder.Code != test.want {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, test.want, recorder.Body.String())
			}
		})
	}
}

func TestRequireAuthCrossTenantSubscriptionMatrix(t *testing.T) {
	fixture := newAuthzMatrixFixture(t)
	tests := []struct {
		name           string
		userID         int
		subscriptionID int
		want           int
	}{
		{name: "anonymous is stopped by middleware", subscriptionID: authzMatrixCustomerSubID, want: http.StatusUnauthorized},
		{name: "admin sees every tenant", userID: authzMatrixAdminID, subscriptionID: authzMatrixOutsiderSubID, want: http.StatusOK},
		{name: "reseller sees own subscription", userID: authzMatrixResellerID, subscriptionID: authzMatrixResellerSubID, want: http.StatusOK},
		{name: "reseller sees direct customer subscription", userID: authzMatrixResellerID, subscriptionID: authzMatrixCustomerSubID, want: http.StatusOK},
		{name: "reseller cannot probe outsider subscription", userID: authzMatrixResellerID, subscriptionID: authzMatrixOutsiderSubID, want: http.StatusNotFound},
		{name: "customer sees own subscription", userID: authzMatrixCustomerID, subscriptionID: authzMatrixCustomerSubID, want: http.StatusOK},
		{name: "customer cannot probe reseller subscription", userID: authzMatrixCustomerID, subscriptionID: authzMatrixResellerSubID, want: http.StatusNotFound},
		{name: "customer cannot probe outsider subscription", userID: authzMatrixCustomerID, subscriptionID: authzMatrixOutsiderSubID, want: http.StatusNotFound},
		{name: "missing and foreign subscription are indistinguishable", userID: authzMatrixCustomerID, subscriptionID: 999999, want: http.StatusNotFound},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := fmt.Sprintf("/api/v1/subscriptions/%d/entitlements", test.subscriptionID)
			recorder := fixture.request(t, http.MethodGet, path, test.userID)
			if recorder.Code != test.want {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, test.want, recorder.Body.String())
			}
		})
	}
}

func TestSubscriptionMutationRemainsAdministratorOnlyAfterOwnershipCheck(t *testing.T) {
	fixture := newAuthzMatrixFixture(t)
	for _, test := range []struct {
		name   string
		userID int
	}{
		{name: "reseller", userID: authzMatrixResellerID},
		{name: "customer", userID: authzMatrixCustomerID},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(
				http.MethodPost,
				fmt.Sprintf("/api/v1/subscriptions/%d/entitlements", authzMatrixCustomerSubID),
				strings.NewReader(`{"product_id":"vpn"}`),
			)
			request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: fixture.tokens[test.userID]})
			recorder := httptest.NewRecorder()
			fixture.handler.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403; body=%s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestDatabaseGrantRejectsCrossSubscriptionUserOnSameServer(t *testing.T) {
	fixture := newAuthzMatrixFixture(t)

	box, err := secrets.LoadOrCreate(filepath.Join(t.TempDir(), "database-secrets.key"))
	if err != nil {
		t.Fatalf("create database secret box: %v", err)
	}
	rootSecret, err := box.Encrypt("root-secret")
	if err != nil {
		t.Fatalf("encrypt database root secret: %v", err)
	}
	userSecret, err := box.Encrypt("foreign-user-secret")
	if err != nil {
		t.Fatalf("encrypt database user secret: %v", err)
	}
	fixture.panel.secrets = box

	var databaseTypeID int
	if err := fixture.panel.db.GetDB().QueryRow(
		`SELECT id FROM database_server_types WHERE name = 'mariadb'`,
	).Scan(&databaseTypeID); err != nil {
		t.Fatalf("resolve database type: %v", err)
	}
	if _, err := fixture.panel.db.GetDB().Exec(`
		INSERT INTO database_servers
			(id, subscription_id, type_id, name, version, host, port, root_password_encrypted, status)
		VALUES (?, ?, ?, 'matrix-logical-server', 'test', '127.0.0.1', 3306, ?, 'active')`,
		authzMatrixDatabaseServerID,
		authzMatrixCustomerSubID,
		databaseTypeID,
		rootSecret,
	); err != nil {
		t.Fatalf("seed database server: %v", err)
	}
	if _, err := fixture.panel.db.GetDB().Exec(`
		INSERT INTO databases_v2 (id, server_id, subscription_id, name)
		VALUES (?, ?, ?, 'matrix_database')`,
		authzMatrixDatabaseID,
		authzMatrixDatabaseServerID,
		authzMatrixCustomerSubID,
	); err != nil {
		t.Fatalf("seed database: %v", err)
	}
	if _, err := fixture.panel.db.GetDB().Exec(`
		INSERT INTO database_users (id, server_id, subscription_id, username, password)
		VALUES (?, ?, ?, 'foreign_matrix_user', ?)`,
		authzMatrixForeignDBUserID,
		authzMatrixDatabaseServerID,
		authzMatrixOutsiderSubID,
		userSecret,
	); err != nil {
		t.Fatalf("seed cross-subscription database user: %v", err)
	}

	driver := &authzGrantIsolationDriver{}
	previousFactory := newDatabaseDriver
	newDatabaseDriver = func(services.DriverConfig) (services.DatabaseDriver, error) {
		return driver, nil
	}
	t.Cleanup(func() { newDatabaseDriver = previousFactory })

	request := httptest.NewRequest(
		http.MethodPost,
		fmt.Sprintf("/api/v1/databases/%d/grants", authzMatrixDatabaseID),
		strings.NewReader(fmt.Sprintf(`{"user_id":%d}`, authzMatrixForeignDBUserID)),
	)
	request.AddCookie(&http.Cookie{
		Name:  sessionCookieName,
		Value: fixture.tokens[authzMatrixCustomerID],
	})
	recorder := httptest.NewRecorder()
	fixture.handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 without revealing the foreign user; body=%s", recorder.Code, recorder.Body.String())
	}
	if strings.TrimSpace(recorder.Body.String()) != `{"error":"invalid request"}` {
		t.Errorf("body = %q, want non-enumerating invalid request", recorder.Body.String())
	}
	if driver.grantCalls != 0 {
		t.Errorf("physical database grant calls = %d, want 0", driver.grantCalls)
	}
	var storedGrants int
	if err := fixture.panel.db.GetDB().QueryRow(
		`SELECT count(*) FROM database_user_grants WHERE database_id = ? AND user_id = ?`,
		authzMatrixDatabaseID,
		authzMatrixForeignDBUserID,
	).Scan(&storedGrants); err != nil {
		t.Fatalf("count stored database grants: %v", err)
	}
	if storedGrants != 0 {
		t.Errorf("stored cross-subscription grants = %d, want 0", storedGrants)
	}
}
