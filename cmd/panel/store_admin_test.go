package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/secrets"
)

func storeAdminRequest(method, path, body string, callerID int, role string) *http.Request {
	request := storeRequest(method, path, body, callerID, role)
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	return request
}

func loadAdminCatalogForTest(t *testing.T, panel *Panel) struct {
	Offerings       []storeAdminOfferingResponse  `json:"offerings"`
	Components      []storeAdminComponentResponse `json:"components"`
	OperationPolicy storeAdminOperationPolicy     `json:"operation_policy"`
} {
	t.Helper()
	recorder := httptest.NewRecorder()
	panel.handleStoreCatalogAdmin(recorder, storeAdminRequest(
		http.MethodGet, storeCatalogAdminPath, "", storeAdminID, roleAdmin,
	))
	if recorder.Code != http.StatusOK {
		t.Fatalf("admin catalog status = %d; body=%s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Cache-Control"); got != "private, no-store" {
		t.Fatalf("Cache-Control = %q", got)
	}
	var response struct {
		Offerings       []storeAdminOfferingResponse  `json:"offerings"`
		Components      []storeAdminComponentResponse `json:"components"`
		OperationPolicy storeAdminOperationPolicy     `json:"operation_policy"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode admin catalog: %v", err)
	}
	return response
}

func adminOfferingForTest(t *testing.T, panel *Panel, offeringID string) storeAdminOfferingResponse {
	t.Helper()
	for _, offering := range loadAdminCatalogForTest(t, panel).Offerings {
		if offering.ID == offeringID {
			return offering
		}
	}
	t.Fatalf("offering %q not found", offeringID)
	return storeAdminOfferingResponse{}
}

func encodeAdminOfferingPatch(t *testing.T, offering storeAdminOfferingResponse, acknowledge bool) string {
	t.Helper()
	category := offering.Category
	vendor := offering.Vendor
	releaseState := offering.ReleaseState
	metadata := offering.Metadata
	componentIDs := append([]string{}, offering.ComponentIDs...)
	sortOrder := offering.SortOrder
	expectedUpdatedAt := offering.UpdatedAt
	body, err := json.Marshal(storeAdminUpdateRequest{
		Category:                     &category,
		Vendor:                       &vendor,
		ReleaseState:                 &releaseState,
		Metadata:                     &metadata,
		ComponentIDs:                 &componentIDs,
		SortOrder:                    &sortOrder,
		ExpectedUpdatedAt:            &expectedUpdatedAt,
		AcknowledgeEntitlementImpact: acknowledge,
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func patchAdminOfferingForTest(
	t *testing.T,
	panel *Panel,
	offering storeAdminOfferingResponse,
	acknowledge bool,
) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	panel.handleStoreCatalogAdmin(recorder, storeAdminRequest(
		http.MethodPatch,
		storeCatalogAdminPath+"/"+offering.ID,
		encodeAdminOfferingPatch(t, offering, acknowledge),
		storeAdminID,
		roleAdmin,
	))
	if got := recorder.Header().Get("Cache-Control"); got != "private, no-store" {
		t.Fatalf("Cache-Control = %q", got)
	}
	return recorder
}

func seedStoreVPNLifecycleForTest(
	t *testing.T,
	panel *Panel,
) *serviceOperationTestAgent {
	t.Helper()
	box, err := secrets.LoadOrCreate(filepath.Join(t.TempDir(), "secrets.key"))
	if err != nil {
		t.Fatalf("create VPN secret box: %v", err)
	}
	panel.secrets = box
	agent := newServiceOperationTestAgent()
	attachServiceOperationTestAgent(t, panel, agent)
	_, err = panel.db.GetDB().Exec(
		"INSERT INTO subscription_entitlements (subscription_id, product_id, status) "+
			"VALUES (?, 'vpn', 'active'); "+
			"INSERT INTO vpn_peers "+
			"(subscription_id, name, public_key, preshared_key, ip, created_by) "+
			"VALUES (?, 'lifecycle-peer', 'lifecycle-public-key', "+
			"'lifecycle-preshared-key', '10.8.0.2', ?)",
		storeAdminSubID, storeAdminSubID, storeAdminID,
	)
	if err != nil {
		t.Fatalf("seed VPN lifecycle state: %v", err)
	}
	return agent
}

func TestStoreCatalogAdminListsEditableDataAndReadOnlyReleasePolicy(t *testing.T) {
	panel := newStorePanelForTest(t)
	response := loadAdminCatalogForTest(t, panel)
	if len(response.Offerings) < 10 || len(response.Components) < 20 {
		t.Fatalf("offerings=%d components=%d", len(response.Offerings), len(response.Components))
	}
	if response.OperationPolicy.Management != "release_managed" ||
		response.OperationPolicy.Mode != "read_only" ||
		response.OperationPolicy.BrowserEditable ||
		response.OperationPolicy.CatalogFormat != "manifest_v2_signed_sqlite" ||
		response.OperationPolicy.Verification != "implemented" ||
		response.OperationPolicy.RuntimeActivation != "pending" {
		t.Fatalf("dishonest operation policy: %+v", response.OperationPolicy)
	}
	for _, component := range response.Components {
		if component.Editable || component.PolicySource != "release_managed" {
			t.Fatalf("mutable operation component: %+v", component)
		}
	}
	serialized, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"shell_command", "sql_statement", "recipe_steps", "filesystem_write"} {
		if strings.Contains(string(serialized), forbidden) {
			t.Fatalf("admin response exposed forbidden operation field %q", forbidden)
		}
	}
}

func TestStoreCatalogAdminIsStrictAndAdminOnly(t *testing.T) {
	panel := newStorePanelForTest(t)
	cases := []struct {
		name   string
		method string
		path   string
		id     int
		role   string
		want   int
	}{
		{name: "anonymous", method: http.MethodGet, path: storeCatalogAdminPath, want: http.StatusForbidden},
		{name: "reseller", method: http.MethodGet, path: storeCatalogAdminPath, id: storeResellerID, role: roleReseller, want: http.StatusForbidden},
		{name: "query", method: http.MethodGet, path: storeCatalogAdminPath + "?debug=1", id: storeAdminID, role: roleAdmin, want: http.StatusBadRequest},
		{name: "collection mutation", method: http.MethodPatch, path: storeCatalogAdminPath, id: storeAdminID, role: roleAdmin, want: http.StatusMethodNotAllowed},
		{name: "item read", method: http.MethodGet, path: storeCatalogAdminPath + "/vpn", id: storeAdminID, role: roleAdmin, want: http.StatusMethodNotAllowed},
		{name: "extra segment", method: http.MethodPatch, path: storeCatalogAdminPath + "/vpn/extra", id: storeAdminID, role: roleAdmin, want: http.StatusNotFound},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			panel.handleStoreCatalogAdmin(recorder, storeAdminRequest(
				test.method, test.path, "", test.id, test.role,
			))
			if recorder.Code != test.want {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, test.want, recorder.Body.String())
			}
		})
	}
	if !isAdminOnlyPath(storeCatalogAdminPath) || !isAdminOnlyPath(storeCatalogAdminPath+"/vpn") {
		t.Fatal("Store catalog administration is not middleware-protected")
	}
}

func TestStoreCatalogAdminUpdateIsAtomicTypedAcknowledgedAndAudited(t *testing.T) {
	panel := newStorePanelForTest(t)
	if _, err := panel.db.GetDB().Exec(`
		INSERT INTO subscription_entitlements (subscription_id, product_id, status)
		VALUES (?, 'app_installer', 'active')`, storeAdminSubID); err != nil {
		t.Fatal(err)
	}
	offering := adminOfferingForTest(t, panel, "app_installer")
	if offering.ActiveEntitlements != 1 {
		t.Fatalf("active entitlements = %d", offering.ActiveEntitlements)
	}
	offering.Category = "automation"
	offering.Vendor = "CelikPanel Labs"
	offering.ReleaseState = "coming_soon"
	offering.SortOrder = 42
	offering.Metadata = storeMetadata{
		Name:        storeLocalizedText{EN: "Curated installer", TR: "Seçilmiş kurucu"},
		Description: storeLocalizedText{EN: "Managed application access.", TR: "Yönetilen uygulama erişimi."},
		Icon:        "package-check",
		Tags:        []string{"apps", "managed"},
	}
	offering.ComponentIDs = []string{"nginx", "php-fpm"}

	withoutAcknowledgement := patchAdminOfferingForTest(t, panel, offering, false)
	if withoutAcknowledgement.Code != http.StatusConflict {
		t.Fatalf("missing acknowledgement status = %d; body=%s", withoutAcknowledgement.Code, withoutAcknowledgement.Body.String())
	}
	recorder := patchAdminOfferingForTest(t, panel, offering, true)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Offering storeAdminOfferingResponse `json:"offering"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Offering.ActiveEntitlements != 1 {
		t.Fatalf("response active entitlements = %d", response.Offering.ActiveEntitlements)
	}

	var category, vendor, releaseState, rawMetadata string
	var sortOrder int
	if err := panel.db.GetDB().QueryRow(`
		SELECT category, vendor, release_state, metadata_json, sort_order
		FROM store_offerings WHERE id = 'app_installer'`,
	).Scan(&category, &vendor, &releaseState, &rawMetadata, &sortOrder); err != nil {
		t.Fatal(err)
	}
	if category != "automation" || vendor != "CelikPanel Labs" || releaseState != "coming_soon" || sortOrder != 42 {
		t.Fatalf("typed fields = %q %q %q %d", category, vendor, releaseState, sortOrder)
	}
	metadata, err := decodeStoreMetadata(rawMetadata)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Name.TR != "Seçilmiş kurucu" || !slices.Equal(metadata.Tags, []string{"apps", "managed"}) {
		t.Fatalf("metadata = %+v", metadata)
	}

	var auditAction string
	if err := panel.db.GetDB().QueryRow(`
		SELECT action FROM audit_logs
		WHERE resource_type = 'store_offering' AND user_id = ?`, storeAdminID,
	).Scan(&auditAction); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(auditAction, "store.catalog.update:app_installer:fields=") ||
		!strings.Contains(auditAction, "release_state") ||
		!strings.Contains(auditAction, "state=available->coming_soon") ||
		!strings.Contains(auditAction, "impact_ack=true,count=1") ||
		!strings.Contains(auditAction, ":before=") ||
		!strings.Contains(auditAction, ":after=") {
		t.Fatalf("audit action = %q", auditAction)
	}
}

func TestStoreCatalogAdminClosingVPNRevokesPeersBeforeSuccess(t *testing.T) {
	panel := newStorePanelForTest(t)
	seedStoreVPNLifecycleForTest(t, panel)
	offering := adminOfferingForTest(t, panel, vpnProductID)
	if offering.ActiveEntitlements != 1 {
		t.Fatalf("active VPN entitlements = %d", offering.ActiveEntitlements)
	}
	offering.ReleaseState = "coming_soon"

	recorder := patchAdminOfferingForTest(t, panel, offering, true)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", recorder.Code, recorder.Body.String())
	}
	var peerCount, entitlementCount, desiredGeneration, appliedGeneration int
	var syncStatus string
	if err := panel.db.GetDB().QueryRow("SELECT COUNT(*) FROM vpn_peers").Scan(&peerCount); err != nil {
		t.Fatal(err)
	}
	if err := panel.db.GetDB().QueryRow(
		"SELECT COUNT(*) FROM subscription_entitlements "+
			"WHERE subscription_id = ? AND product_id = 'vpn' AND status = 'active'",
		storeAdminSubID,
	).Scan(&entitlementCount); err != nil {
		t.Fatal(err)
	}
	if err := panel.db.GetDB().QueryRow(
		"SELECT status, desired_generation, applied_generation "+
			"FROM vpn_sync_state WHERE id = 1",
	).Scan(&syncStatus, &desiredGeneration, &appliedGeneration); err != nil {
		t.Fatal(err)
	}
	if peerCount != 0 {
		t.Fatalf("VPN peers remained after acknowledged catalog closure: %d", peerCount)
	}
	if entitlementCount != 1 {
		t.Fatalf("catalog closure destroyed the recorded entitlement: %d", entitlementCount)
	}
	if syncStatus != "applied" || appliedGeneration != desiredGeneration {
		t.Fatalf(
			"VPN sync state = %q generation=%d/%d",
			syncStatus, appliedGeneration, desiredGeneration,
		)
	}
}

func TestStoreCatalogAdminClosedVPNRetryFinishesFailedImmediateRevocation(t *testing.T) {
	panel := newStorePanelForTest(t)
	agent := seedStoreVPNLifecycleForTest(t, panel)
	agent.peerError = "forced peer sync failure"
	offering := adminOfferingForTest(t, panel, vpnProductID)
	offering.ReleaseState = "retired"

	failed := patchAdminOfferingForTest(t, panel, offering, true)
	if failed.Code != http.StatusInternalServerError {
		t.Fatalf("failed sync status = %d; body=%s", failed.Code, failed.Body.String())
	}
	var releaseState, desiredState, syncState string
	if err := panel.db.GetDB().QueryRow(
		"SELECT release_state FROM store_offerings WHERE id = 'vpn'",
	).Scan(&releaseState); err != nil {
		t.Fatal(err)
	}
	if err := panel.db.GetDB().QueryRow(
		"SELECT desired_state, sync_state FROM vpn_peers WHERE subscription_id = ?",
		storeAdminSubID,
	).Scan(&desiredState, &syncState); err != nil {
		t.Fatal(err)
	}
	if releaseState != "retired" || desiredState != "revoked" || syncState != "error" {
		t.Fatalf(
			"failed closure was not fail-closed: release=%q desired=%q sync=%q",
			releaseState, desiredState, syncState,
		)
	}

	agent.mu.Lock()
	agent.peerError = ""
	agent.mu.Unlock()
	closed := adminOfferingForTest(t, panel, vpnProductID)
	retry := patchAdminOfferingForTest(t, panel, closed, true)
	if retry.Code != http.StatusOK || !strings.Contains(retry.Body.String(), "unchanged") {
		t.Fatalf("closed retry status = %d; body=%s", retry.Code, retry.Body.String())
	}
	var peerCount int
	if err := panel.db.GetDB().QueryRow("SELECT COUNT(*) FROM vpn_peers").Scan(&peerCount); err != nil {
		t.Fatal(err)
	}
	if peerCount != 0 {
		t.Fatalf("closed exact retry did not finish immediate peer removal: %d", peerCount)
	}
}

func TestStoreCatalogAdminExactRetryIsNoopAndStaleDifferentEditConflicts(t *testing.T) {
	panel := newStorePanelForTest(t)
	original := adminOfferingForTest(t, panel, "app_installer")
	updated := original
	updated.Vendor = "First vendor"
	first := patchAdminOfferingForTest(t, panel, updated, false)
	if first.Code != http.StatusOK {
		t.Fatalf("first status = %d; body=%s", first.Code, first.Body.String())
	}

	// The exact network retry carries the old token. It is safe and must not
	// rewrite updated_at or create a duplicate audit record.
	// Aynı ağ tekrarı eski token'ı taşır. Güvenlidir; updated_at'i yeniden
	// yazmamalı veya ikinci denetim kaydı oluşturmamalıdır.
	retry := patchAdminOfferingForTest(t, panel, updated, false)
	if retry.Code != http.StatusOK || !strings.Contains(retry.Body.String(), `"unchanged":true`) {
		t.Fatalf("retry status = %d; body=%s", retry.Code, retry.Body.String())
	}

	staleDifferent := updated
	staleDifferent.Vendor = "Stale second vendor"
	conflict := patchAdminOfferingForTest(t, panel, staleDifferent, false)
	if conflict.Code != http.StatusConflict {
		t.Fatalf("stale different status = %d; body=%s", conflict.Code, conflict.Body.String())
	}
	var vendor string
	var auditCount int
	if err := panel.db.GetDB().QueryRow(`SELECT vendor FROM store_offerings WHERE id='app_installer'`).Scan(&vendor); err != nil {
		t.Fatal(err)
	}
	if err := panel.db.GetDB().QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE action LIKE 'store.catalog.update:app_installer:%'`).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if vendor != "First vendor" || auditCount != 1 {
		t.Fatalf("vendor=%q auditCount=%d", vendor, auditCount)
	}
}

func TestStoreCatalogAdminRefusesBrowserPublishAndOperationFields(t *testing.T) {
	panel := newStorePanelForTest(t)
	comingSoon := adminOfferingForTest(t, panel, "ai_agent")
	comingSoon.ReleaseState = "available"
	recorder := patchAdminOfferingForTest(t, panel, comingSoon, false)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("browser publish status = %d; body=%s; request=%s", recorder.Code, recorder.Body.String(), encodeAdminOfferingPatch(t, comingSoon, false))
	}

	base := adminOfferingForTest(t, panel, "app_installer")
	valid := encodeAdminOfferingPatch(t, base, false)
	unknown := strings.TrimSuffix(valid, "}") + `,"shell_command":"rm -rf /"}`
	recorder = httptest.NewRecorder()
	panel.handleStoreCatalogAdmin(recorder, storeAdminRequest(
		http.MethodPatch, storeCatalogAdminPath+"/app_installer", unknown, storeAdminID, roleAdmin,
	))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("operation field status = %d; body=%s", recorder.Code, recorder.Body.String())
	}

	var auditCount int
	if err := panel.db.GetDB().QueryRow(`SELECT COUNT(*) FROM audit_logs`).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if auditCount != 0 {
		t.Fatalf("audit count after refused requests = %d", auditCount)
	}
}

func TestStoreAndEntitlementResponsesArePrivateNoStore(t *testing.T) {
	panel := newStorePanelForTest(t)
	storeRecorder := httptest.NewRecorder()
	panel.handleStore(storeRecorder, storeRequest(
		http.MethodGet, "/api/v1/store?subscription_id=201", "", storeAdminID, roleAdmin,
	))
	if got := storeRecorder.Header().Get("Cache-Control"); got != "private, no-store" {
		t.Fatalf("Store Cache-Control = %q", got)
	}

	entitlementRecorder := httptest.NewRecorder()
	panel.handleSubscriptionEntitlements(entitlementRecorder, storeRequest(
		http.MethodGet, "/api/v1/subscriptions/201/entitlements", "", storeAdminID, roleAdmin,
	))
	if got := entitlementRecorder.Header().Get("Cache-Control"); got != "private, no-store" {
		t.Fatalf("entitlement Cache-Control = %q", got)
	}
}
