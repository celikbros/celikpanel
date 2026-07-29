package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOwnedEntitlementCanBeRevokedDespiteStoreBlocker(t *testing.T) {
	panel := newStorePanelForTest(t)
	if _, err := panel.db.GetDB().Exec(`
		INSERT INTO subscription_entitlements
			(subscription_id, product_id, status)
		VALUES
			(201, 'app_installer', 'active'),
			(201, 'firewall', 'active')`,
	); err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	panel.handleStore(recorder, storeRequest(
		http.MethodGet, "/api/v1/store/app_installer?subscription_id=201", "",
		storeAdminID, roleAdmin,
	))
	item := decodeStoreItem(t, recorder)
	if item.BlockerReason != "component_state_unavailable" ||
		item.PrimaryAction.Type != "remove" || !item.PrimaryAction.Enabled {
		t.Fatalf("owned blocked entitlement cannot be removed: %+v", item)
	}

	recorder = httptest.NewRecorder()
	panel.handleSubscriptionEntitlements(recorder, storeRequest(
		http.MethodDelete, "/api/v1/subscriptions/201/entitlements/firewall", "",
		storeAdminID, roleAdmin,
	))
	if recorder.Code != http.StatusOK {
		t.Fatalf("coming-soon cleanup status = %d; body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestResellerNeverReceivesAdminOnlyActionTypeOrPath(t *testing.T) {
	panel := newStorePanelForTest(t)
	cacheStoreObservations(t, panel, timeNowUTC(), readyAppInstallerObservations()...)
	if _, err := panel.db.GetDB().Exec(`
		INSERT INTO subscription_entitlements
			(subscription_id, product_id, status)
		VALUES (202, 'app_installer', 'active')`,
	); err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	panel.handleStore(recorder, storeRequest(
		http.MethodGet, "/api/v1/store/app_installer?subscription_id=202", "",
		storeResellerID, roleReseller,
	))
	item := decodeStoreItem(t, recorder)
	if item.PrimaryAction.Type != "none" || item.PrimaryAction.Path != "" ||
		item.ManagePath != "" || item.ActionPath != "" || len(item.ComponentIDs) != 0 ||
		item.PrimaryAction.Enabled || item.BlockerReason != "admin_required" {
		t.Fatalf("reseller received admin-only action metadata: %+v", item)
	}
}

func TestIncludedComponentTopologyIsVisibleOnlyToAdmin(t *testing.T) {
	panel := newStorePanelForTest(t)
	observations := append(readyAppInstallerObservations(),
		serviceObservation{ID: "clamav", IsInstalled: true, Status: "active"})
	cacheStoreObservations(t, panel, timeNowUTC(), observations...)

	callers := []struct {
		name string
		id   int
		role string
	}{
		{name: "reseller", id: storeResellerID, role: roleReseller},
		{name: "customer", id: storeCustomerID, role: roleCustomer},
	}
	for _, caller := range callers {
		t.Run(caller.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			panel.handleStore(recorder, storeRequest(
				http.MethodGet, "/api/v1/store/clamav?subscription_id=202", "",
				caller.id, caller.role,
			))
			item := decodeStoreItem(t, recorder)
			if item.PrimaryAction.Type != "none" || item.PrimaryAction.Path != "" ||
				item.ManagePath != "" || item.ActionPath != "" ||
				len(item.ComponentIDs) != 0 || item.PrimaryAction.Enabled {
				t.Fatalf("non-admin received included component topology: %+v", item)
			}
		})
	}

	recorder := httptest.NewRecorder()
	panel.handleStore(recorder, storeRequest(
		http.MethodGet, "/api/v1/store/clamav?subscription_id=201", "",
		storeAdminID, roleAdmin,
	))
	item := decodeStoreItem(t, recorder)
	if item.PrimaryAction.Type != "manage_components" || !item.PrimaryAction.Enabled ||
		item.PrimaryAction.Path != "/services/clamav" ||
		item.ManagePath != "/services/clamav" || item.ActionPath != "/services/clamav" ||
		len(item.ComponentIDs) != 1 || item.ComponentIDs[0] != "clamav" {
		t.Fatalf("admin did not receive authorized component topology: %+v", item)
	}
}
func timeNowUTC() time.Time {
	return time.Now().UTC()
}
