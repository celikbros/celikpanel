package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	paneldb "github.com/alicelik/celikpanel/internal/db"
)

const (
	storeAdminID       = 101
	storeResellerID    = 102
	storeCustomerID    = 103
	storeOutsiderID    = 104
	storeAdminSubID    = 201
	storeResellerSubID = 202
)

func newStorePanelForTest(t *testing.T) *Panel {
	t.Helper()
	database, err := paneldb.NewSQLiteDB(filepath.Join(t.TempDir(), "panel.sqlite"))
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(database.Close)
	_, err = database.GetDB().Exec(`
		INSERT INTO users (id, username, password_hash, email, role, status)
		VALUES
			(101, 'store_admin', 'x', 'store-admin@example.test', 'admin', 'active'),
			(102, 'store_reseller', 'x', 'store-reseller@example.test', 'reseller', 'active'),
			(103, 'store_customer', 'x', 'store-customer@example.test', 'customer', 'active'),
			(104, 'store_outsider', 'x', 'store-outsider@example.test', 'customer', 'active');
		UPDATE users SET parent_id = 102 WHERE id = 103;
		INSERT INTO subscriptions (id, owner_id, name, status)
		VALUES
			(201, 101, 'Admin subscription', 'active'),
			(202, 103, 'Reseller customer subscription', 'active');
	`)
	if err != nil {
		t.Fatalf("seed Store identities: %v", err)
	}
	return &Panel{db: database, pkgFamilyVal: "apt"}
}

func storeRequest(method, path, body string, callerID int, role string) *http.Request {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if callerID == 0 {
		return request
	}
	return request.WithContext(context.WithValue(
		request.Context(), callerKey, &Caller{ID: callerID, Role: role},
	))
}

func cacheStoreObservations(t *testing.T, panel *Panel, scannedAt time.Time, observations ...serviceObservation) {
	t.Helper()
	raw, err := json.Marshal(scanCacheDoc{Observations: observations})
	if err != nil {
		t.Fatalf("encode service observations: %v", err)
	}
	_, err = panel.db.GetDB().Exec(`
		INSERT INTO service_scan_cache (id, data, scanned_at)
		VALUES (1, ?, ?)
		ON CONFLICT(id) DO UPDATE SET data = excluded.data, scanned_at = excluded.scanned_at`,
		string(raw), scannedAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		t.Fatalf("cache service observations: %v", err)
	}
}

func readyAppInstallerObservations() []serviceObservation {
	return []serviceObservation{
		{ID: "nginx", IsInstalled: true, Status: "active"},
		{ID: "php-fpm", IsInstalled: true, Status: "active"},
		{ID: "mariadb", IsInstalled: true, Status: "active"},
	}
}

func decodeStoreItem(t *testing.T, recorder *httptest.ResponseRecorder) storeItemResponse {
	t.Helper()
	var response struct {
		Item storeItemResponse `json:"item"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode Store item: %v; body=%s", err, recorder.Body.String())
	}
	return response.Item
}

func TestStoreMethodPathLocaleAndOwnershipAreStrict(t *testing.T) {
	panel := newStorePanelForTest(t)
	cases := []struct {
		name   string
		method string
		path   string
		id     int
		role   string
		want   int
	}{
		{name: "method", method: http.MethodPost, path: "/api/v1/store", id: storeAdminID, role: roleAdmin, want: http.StatusMethodNotAllowed},
		{name: "trailing slash", method: http.MethodGet, path: "/api/v1/store/", id: storeAdminID, role: roleAdmin, want: http.StatusNotFound},
		{name: "extra segment", method: http.MethodGet, path: "/api/v1/store/app_installer/extra", id: storeAdminID, role: roleAdmin, want: http.StatusNotFound},
		{name: "locale", method: http.MethodGet, path: "/api/v1/store?locale=de", id: storeAdminID, role: roleAdmin, want: http.StatusBadRequest},
		{name: "unknown query", method: http.MethodGet, path: "/api/v1/store?debug=1", id: storeAdminID, role: roleAdmin, want: http.StatusBadRequest},
		{name: "duplicate locale", method: http.MethodGet, path: "/api/v1/store?locale=en&locale=tr", id: storeAdminID, role: roleAdmin, want: http.StatusBadRequest},
		{name: "duplicate subscription", method: http.MethodGet, path: "/api/v1/store?subscription_id=201&subscription_id=202", id: storeAdminID, role: roleAdmin, want: http.StatusBadRequest},
		{name: "invisible subscription", method: http.MethodGet, path: "/api/v1/store?subscription_id=202", id: storeOutsiderID, role: roleCustomer, want: http.StatusNotFound},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			panel.handleStore(recorder, storeRequest(test.method, test.path, "", test.id, test.role))
			if recorder.Code != test.want {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, test.want, recorder.Body.String())
			}
		})
	}
}

func TestStoreUsesLocalizedDatabaseOfferingAndComingSoonState(t *testing.T) {
	panel := newStorePanelForTest(t)
	recorder := httptest.NewRecorder()
	panel.handleStore(recorder, storeRequest(
		http.MethodGet, "/api/v1/store/firewall?locale=tr", "", storeAdminID, roleAdmin,
	))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", recorder.Code, recorder.Body.String())
	}
	item := decodeStoreItem(t, recorder)
	if item.Name != "Yönetilen site güvenlik duvarı" {
		t.Fatalf("localized name = %q", item.Name)
	}
	if item.ReleaseState != "coming_soon" || item.State != "coming_soon" ||
		item.PrimaryAction.Enabled || item.PrimaryAction.Type != "none" {
		t.Fatalf("dishonest coming-soon item: %+v", item)
	}
}

func TestStoreCatalogGrantRequiresSubscription(t *testing.T) {
	panel := newStorePanelForTest(t)
	cacheStoreObservations(t, panel, time.Now(), readyAppInstallerObservations()...)

	recorder := httptest.NewRecorder()
	panel.handleStore(recorder, storeRequest(
		http.MethodGet, "/api/v1/store/app_installer?locale=tr", "", storeAdminID, roleAdmin,
	))
	item := decodeStoreItem(t, recorder)
	if item.BlockerReason != "subscription_required" || item.PrimaryAction.Type != "none" ||
		item.PrimaryAction.Enabled || item.Action != "none" {
		t.Fatalf("grant offering was actionable without a subscription: %+v", item)
	}
	if item.StateReason != "Bu teklifi yönetmek için bir abonelik seçin." {
		t.Fatalf("localized subscription reason = %q", item.StateReason)
	}
}

func TestStoreFreshnessAndResellerActionsFailClosed(t *testing.T) {
	panel := newStorePanelForTest(t)
	cacheStoreObservations(t, panel, time.Now().Add(-storeScanFreshness-time.Minute), readyAppInstallerObservations()...)

	recorder := httptest.NewRecorder()
	panel.handleStore(recorder, storeRequest(
		http.MethodGet, "/api/v1/store/app_installer?subscription_id=202", "",
		storeResellerID, roleReseller,
	))
	item := decodeStoreItem(t, recorder)
	if item.PlatformState != "unknown" || item.BlockerReason != "component_state_stale" ||
		item.PrimaryAction.Enabled {
		t.Fatalf("stale Store item was actionable: %+v", item)
	}

	cacheStoreObservations(t, panel, time.Now(), readyAppInstallerObservations()...)
	recorder = httptest.NewRecorder()
	panel.handleStore(recorder, storeRequest(
		http.MethodGet, "/api/v1/store/app_installer?subscription_id=202", "",
		storeResellerID, roleReseller,
	))
	item = decodeStoreItem(t, recorder)
	if item.PlatformState != "supported" || item.BlockerReason != "admin_required" ||
		item.PrimaryAction.Enabled {
		t.Fatalf("reseller received server-level action: %+v", item)
	}

	recorder = httptest.NewRecorder()
	panel.handleSubscriptionEntitlements(recorder, storeRequest(
		http.MethodPost, "/api/v1/subscriptions/202/entitlements",
		`{"product_id":"app_installer"}`, storeResellerID, roleReseller,
	))
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("reseller mutation status = %d, want 403; body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestGrantIsStrictCacheOnlyAtomicAndIdempotent(t *testing.T) {
	panel := newStorePanelForTest(t)
	cacheStoreObservations(t, panel, time.Now(), readyAppInstallerObservations()...)

	for attempt := 0; attempt < 2; attempt++ {
		recorder := httptest.NewRecorder()
		panel.handleSubscriptionEntitlements(recorder, storeRequest(
			http.MethodPost, "/api/v1/subscriptions/201/entitlements",
			`{"product_id":"app_installer"}`, storeAdminID, roleAdmin,
		))
		if recorder.Code != http.StatusOK {
			t.Fatalf("attempt %d status = %d; body=%s", attempt+1, recorder.Code, recorder.Body.String())
		}
	}

	var entitlements, audits int
	if err := panel.db.GetDB().QueryRow(`
		SELECT COUNT(*) FROM subscription_entitlements
		WHERE subscription_id = 201 AND product_id = 'app_installer'`,
	).Scan(&entitlements); err != nil {
		t.Fatal(err)
	}
	if err := panel.db.GetDB().QueryRow(`
		SELECT COUNT(*) FROM audit_logs
		WHERE action = 'entitlement.grant:app_installer' AND resource_id = 201`,
	).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if entitlements != 1 || audits != 1 {
		t.Fatalf("entitlements=%d audits=%d, want 1/1", entitlements, audits)
	}
	// panel.agentClient is nil. Reaching this point proves acquire only wrote
	// the entitlement ledger and made no agent/install call.
	// panel.agentClient nil'dir. Buraya ulaşmak, edinmenin yalnız hak defterini
	// yazdığını ve agent/kurulum çağrısı yapmadığını kanıtlar.
}

func TestConcurrentExactGrantWritesOneAudit(t *testing.T) {
	panel := newStorePanelForTest(t)
	cacheStoreObservations(t, panel, time.Now(), readyAppInstallerObservations()...)

	const workers = 4
	start := make(chan struct{})
	type grantResult struct {
		code int
		body string
	}
	results := make(chan grantResult, workers)
	var wg sync.WaitGroup
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			<-start
			recorder := httptest.NewRecorder()
			panel.handleSubscriptionEntitlements(recorder, storeRequest(
				http.MethodPost, "/api/v1/subscriptions/201/entitlements",
				`{"product_id":"app_installer"}`, storeAdminID, roleAdmin,
			))
			results <- grantResult{code: recorder.Code, body: recorder.Body.String()}
		}()
	}
	close(start)
	wg.Wait()
	close(results)

	for result := range results {
		if result.code != http.StatusOK {
			t.Fatalf("concurrent grant status = %d; body=%s", result.code, result.body)
		}
	}

	var entitlements, audits int
	if err := panel.db.GetDB().QueryRow(`
		SELECT COUNT(*) FROM subscription_entitlements
		WHERE subscription_id = 201 AND product_id = 'app_installer'`,
	).Scan(&entitlements); err != nil {
		t.Fatal(err)
	}
	if err := panel.db.GetDB().QueryRow(`
		SELECT COUNT(*) FROM audit_logs
		WHERE action = 'entitlement.grant:app_installer' AND resource_id = 201`,
	).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if entitlements != 1 || audits != 1 {
		t.Fatalf("entitlements=%d audits=%d, want 1/1", entitlements, audits)
	}
}

func TestGrantExactRetrySurvivesStaleCacheAndRetirement(t *testing.T) {
	panel := newStorePanelForTest(t)
	cacheStoreObservations(t, panel, time.Now(), readyAppInstallerObservations()...)

	recorder := httptest.NewRecorder()
	panel.handleSubscriptionEntitlements(recorder, storeRequest(
		http.MethodPost, "/api/v1/subscriptions/201/entitlements",
		`{"product_id":"app_installer"}`, storeAdminID, roleAdmin,
	))
	if recorder.Code != http.StatusOK {
		t.Fatalf("initial grant status = %d; body=%s", recorder.Code, recorder.Body.String())
	}

	if _, err := panel.db.GetDB().Exec(`
		UPDATE store_offerings SET release_state = 'retired' WHERE id = 'app_installer';
		DELETE FROM service_scan_cache WHERE id = 1;
	`); err != nil {
		t.Fatalf("retire offering and clear cache: %v", err)
	}

	recorder = httptest.NewRecorder()
	panel.handleSubscriptionEntitlements(recorder, storeRequest(
		http.MethodPost, "/api/v1/subscriptions/201/entitlements",
		`{"product_id":"app_installer"}`, storeAdminID, roleAdmin,
	))
	if recorder.Code != http.StatusOK {
		t.Fatalf("exact retry status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	var retry struct {
		Success    bool `json:"success"`
		Idempotent bool `json:"idempotent"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&retry); err != nil {
		t.Fatalf("decode exact retry: %v", err)
	}
	if !retry.Success || !retry.Idempotent {
		t.Fatalf("exact retry response = %+v", retry)
	}

	changedExpiry := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
	recorder = httptest.NewRecorder()
	panel.handleSubscriptionEntitlements(recorder, storeRequest(
		http.MethodPost, "/api/v1/subscriptions/201/entitlements",
		`{"product_id":"app_installer","expires_at":"`+changedExpiry+`"}`,
		storeAdminID, roleAdmin,
	))
	if recorder.Code != http.StatusConflict {
		t.Fatalf("retired state change status = %d, want 409; body=%s", recorder.Code, recorder.Body.String())
	}

	var entitlements, audits int
	if err := panel.db.GetDB().QueryRow(`
		SELECT COUNT(*) FROM subscription_entitlements
		WHERE subscription_id = 201 AND product_id = 'app_installer'`,
	).Scan(&entitlements); err != nil {
		t.Fatal(err)
	}
	if err := panel.db.GetDB().QueryRow(`
		SELECT COUNT(*) FROM audit_logs
		WHERE action = 'entitlement.grant:app_installer' AND resource_id = 201`,
	).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if entitlements != 1 || audits != 1 {
		t.Fatalf("entitlements=%d audits=%d, want 1/1", entitlements, audits)
	}
}

func TestGrantRejectsBodyOfferingExpiryAndStaleState(t *testing.T) {
	panel := newStorePanelForTest(t)
	cacheStoreObservations(t, panel, time.Now(), readyAppInstallerObservations()...)
	past := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	cases := []struct {
		name string
		body string
		want int
	}{
		{name: "unknown field", body: `{"product_id":"app_installer","install":true}`, want: http.StatusBadRequest},
		{name: "trailing body", body: `{"product_id":"app_installer"} {}`, want: http.StatusBadRequest},
		{name: "unknown offering", body: `{"product_id":"does_not_exist"}`, want: http.StatusBadRequest},
		{name: "coming soon", body: `{"product_id":"firewall"}`, want: http.StatusConflict},
		{name: "included", body: `{"product_id":"backups"}`, want: http.StatusConflict},
		{name: "invalid expiry", body: `{"product_id":"app_installer","expires_at":"tomorrow"}`, want: http.StatusBadRequest},
		{name: "past expiry", body: `{"product_id":"app_installer","expires_at":"` + past + `"}`, want: http.StatusBadRequest},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			panel.handleSubscriptionEntitlements(recorder, storeRequest(
				http.MethodPost, "/api/v1/subscriptions/201/entitlements",
				test.body, storeAdminID, roleAdmin,
			))
			if recorder.Code != test.want {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, test.want, recorder.Body.String())
			}
		})
	}

	cacheStoreObservations(t, panel, time.Now().Add(-storeScanFreshness-time.Minute), readyAppInstallerObservations()...)
	recorder := httptest.NewRecorder()
	panel.handleSubscriptionEntitlements(recorder, storeRequest(
		http.MethodPost, "/api/v1/subscriptions/201/entitlements",
		`{"product_id":"app_installer"}`, storeAdminID, roleAdmin,
	))
	if recorder.Code != http.StatusConflict {
		t.Fatalf("stale grant status = %d, want 409; body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestStoredInvalidExpiryFailsClosed(t *testing.T) {
	panel := newStorePanelForTest(t)
	cacheStoreObservations(t, panel, time.Now(), readyAppInstallerObservations()...)
	if _, err := panel.db.GetDB().Exec(`
		INSERT INTO subscription_entitlements
			(subscription_id, product_id, status, expires_at)
		VALUES (201, 'app_installer', 'active', 'not-rfc3339')`,
	); err != nil {
		t.Fatal(err)
	}
	if panel.hasEntitlement(context.Background(), storeAdminSubID, "app_installer") {
		t.Fatal("malformed stored expiry was accepted")
	}

	recorder := httptest.NewRecorder()
	panel.handleStore(recorder, storeRequest(
		http.MethodGet, "/api/v1/store/app_installer?subscription_id=201", "",
		storeAdminID, roleAdmin,
	))
	item := decodeStoreItem(t, recorder)
	if item.EntitlementState != "expired" {
		t.Fatalf("entitlement state = %q, want expired", item.EntitlementState)
	}
}

func TestEntitlementPathAndDeleteAreIdempotent(t *testing.T) {
	panel := newStorePanelForTest(t)
	cases := []string{
		"/api/v1/subscriptions/201/entitlements/",
		"/api/v1/subscriptions/201/entitlements/app_installer/extra",
	}
	for _, path := range cases {
		recorder := httptest.NewRecorder()
		panel.handleSubscriptionEntitlements(recorder, storeRequest(
			http.MethodDelete, path, "", storeAdminID, roleAdmin,
		))
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("%s status = %d, want 404", path, recorder.Code)
		}
	}

	for attempt := 0; attempt < 2; attempt++ {
		recorder := httptest.NewRecorder()
		panel.handleSubscriptionEntitlements(recorder, storeRequest(
			http.MethodDelete, "/api/v1/subscriptions/201/entitlements/app_installer",
			"", storeAdminID, roleAdmin,
		))
		if recorder.Code != http.StatusOK {
			t.Fatalf("delete attempt %d status = %d; body=%s", attempt+1, recorder.Code, recorder.Body.String())
		}
	}
	var audits int
	if err := panel.db.GetDB().QueryRow(`
		SELECT COUNT(*) FROM audit_logs
		WHERE action = 'entitlement.revoke:app_installer'`,
	).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if audits != 0 {
		t.Fatalf("no-op deletes wrote %d audit rows", audits)
	}
}

func TestWordPressSubscriptionLookupErrorDoesNotBypassGate(t *testing.T) {
	panel := newStorePanelForTest(t)
	panel.db.Close()
	recorder := httptest.NewRecorder()
	request := storeRequest(
		http.MethodPost, "/api/v1/domains/999/apps/install",
		`{"app":"wordpress"}`, storeAdminID, roleAdmin,
	)
	panel.handleAppInstall(recorder, request, 999)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestStrictJSONLimit(t *testing.T) {
	panel := newStorePanelForTest(t)
	cacheStoreObservations(t, panel, time.Now(), readyAppInstallerObservations()...)
	body := bytes.Repeat([]byte("x"), (64<<10)+1)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost, "/api/v1/subscriptions/201/entitlements", bytes.NewReader(body),
	).WithContext(context.WithValue(context.Background(), callerKey, &Caller{ID: storeAdminID, Role: roleAdmin}))
	panel.handleSubscriptionEntitlements(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("oversized body status = %d, want 400", recorder.Code)
	}
}
