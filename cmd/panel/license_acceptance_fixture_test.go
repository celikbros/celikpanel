//go:build acceptance_license && linux

package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/licensing"
)

const acceptancePanelTestCell = "pair-accept__bind-arch__bind-debian13__t1"

// acceptancePanelLicense builds the tagged manager exactly as NewServer does,
// with the guest marker in a temporary directory instead of /etc.
func acceptancePanelLicense(t *testing.T, withMarker bool) (*licensing.Manager, string) {
	t.Helper()
	dir := t.TempDir()
	marker := filepath.Join(dir, "celikpanel-dns-kill-matrix")
	if withMarker {
		content := "schema=" + licensing.AcceptanceGuestSchema + "\ncell_id=" + acceptancePanelTestCell + "\nnode=arch\n"
		if err := os.WriteFile(marker, []byte(content), 0444); err != nil {
			t.Fatal(err)
		}
	}
	key, err := hex.DecodeString(licensing.PublicKeyHex)
	if err != nil {
		t.Fatal(err)
	}
	m, err := licensing.NewAcceptanceFixtureManager(filepath.Join(dir, "data", "license.json"), key, strings.Repeat("b", 64),
		licensing.AcceptanceGuest{MarkerPath: marker, MarkerOwner: uint32(os.Getuid())})
	if err != nil {
		t.Fatal(err)
	}
	return m, marker
}

func acceptanceLicenseRequest(t *testing.T, fixture authzMatrixFixture, method, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	handler := fixture.panel.requireAuth(http.HandlerFunc(fixture.panel.handleLicense))
	var request *http.Request
	if body == "" {
		request = httptest.NewRequest(method, panelLicensePath, nil)
	} else {
		request = httptest.NewRequest(method, panelLicensePath, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
	}
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: fixture.tokens[authzMatrixAdminID]})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	var decoded map[string]any
	_ = json.Unmarshal(recorder.Body.Bytes(), &decoded)
	return recorder, decoded
}

func acceptanceGatedPaths() []struct{ method, path string } {
	return []struct{ method, path string }{
		{"GET", "/api/v1/domains"}, {"POST", "/api/v1/domains"}, {"GET", "/api/v2/databases"},
		{"POST", "/api/v1/firewall"}, {"POST", "/api/v1/service/install"}, {"GET", "/api/v1/future-management-route"},
	}
}

func TestAcceptanceFixtureLicenseOpensGatedRoutesThroughTheLicenseScreen(t *testing.T) {
	fixture := newAuthzMatrixFixture(t)
	manager, _ := acceptancePanelLicense(t, true)
	fixture.panel.license = manager
	dials := licensing.AcceptanceLicenseServiceDialAttempts()
	reached := 0
	gated := fixture.panel.requireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached++; w.WriteHeader(204) }))

	for _, route := range acceptanceGatedPaths() {
		if w := requestWithToken(gated, route.method, route.path, fixture.tokens[authzMatrixAdminID]); w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "license_required") {
			t.Fatalf("before activation %s %s: %d %s", route.method, route.path, w.Code, w.Body.String())
		}
	}
	w, status := acceptanceLicenseRequest(t, fixture, http.MethodGet, "")
	if w.Code != 200 || status["state"] != "missing" || status["license_kind"] != licensing.AcceptanceFixtureKind ||
		status["license_label"] != licensing.AcceptanceFixtureHolder || status["acceptance_guest"] != "verified" {
		t.Fatalf("license screen before activation: %d %v", w.Code, status)
	}
	w, problem := acceptanceLicenseRequest(t, fixture, http.MethodPost, `{"action":"activate","key":"CPK-`+strings.Repeat("1", 64)+`"}`)
	if w.Code != 400 || problem["code"] != "license_action_failed" || !strings.Contains(w.Body.String(), "acceptance test build") {
		t.Fatalf("customer key on the acceptance build: %d %s", w.Code, w.Body.String())
	}
	w, status = acceptanceLicenseRequest(t, fixture, http.MethodPost, `{"action":"activate","key":"`+licensing.AcceptanceFixtureKey+`"}`)
	if w.Code != 200 || status["state"] != "active" || status["can_provision"] != true || status["product"] != licensing.AcceptanceFixtureProduct ||
		status["license_label"] != licensing.AcceptanceFixtureHolder || status["license_service"] != licensing.AcceptanceLicenseService ||
		status["acceptance_cell"] != acceptancePanelTestCell || status["acceptance_node"] != "arch" {
		t.Fatalf("fixture activation through the license screen: %d %v", w.Code, status)
	}
	for _, route := range acceptanceGatedPaths() {
		before := reached
		if w := requestWithToken(gated, route.method, route.path, fixture.tokens[authzMatrixAdminID]); w.Code != 204 || reached != before+1 {
			t.Fatalf("fixture did not open %s %s: %d %s", route.method, route.path, w.Code, w.Body.String())
		}
	}
	// The browser's access observation keeps its exact shape; the web accepts it.
	access := httptest.NewRecorder()
	fixture.panel.handleLicenseAccess(access, httptest.NewRequest("GET", panelLicenseAccessPath, nil))
	var observed map[string]any
	if err := json.Unmarshal(access.Body.Bytes(), &observed); err != nil || len(observed) != 4 || observed["can_use_panel"] != true || observed["state"] != "active" {
		t.Fatalf("access observation: %v %s", err, access.Body.String())
	}
	if err := fixture.panel.requireServerSetupAdmission(context.Background()); err != nil {
		t.Fatalf("setup admission with the fixture: %v", err)
	}
	if got := licensing.AcceptanceLicenseServiceDialAttempts(); got != dials {
		t.Fatalf("acceptance build attempted %d license service connections", got-dials)
	}
}

func TestAcceptanceFixtureLicenseKeepsGatedRoutesClosedWithoutTheGuestMarker(t *testing.T) {
	fixture := newAuthzMatrixFixture(t)
	gated := fixture.panel.requireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	t.Run("never a guest", func(t *testing.T) {
		manager, _ := acceptancePanelLicense(t, false)
		fixture.panel.license = manager
		w, problem := acceptanceLicenseRequest(t, fixture, http.MethodPost, `{"action":"activate","key":"`+licensing.AcceptanceFixtureKey+`"}`)
		if w.Code != 400 || problem["code"] != "license_action_failed" || !strings.Contains(w.Body.String(), "disposable acceptance guest") {
			t.Fatalf("activation off a guest: %d %s", w.Code, w.Body.String())
		}
		_, status := acceptanceLicenseRequest(t, fixture, http.MethodGet, "")
		if status["state"] != "invalid" || status["can_provision"] != false || !strings.HasPrefix(status["acceptance_guest"].(string), "refused: ") ||
			status["license_label"] != licensing.AcceptanceFixtureHolder {
			t.Fatalf("status off a guest: %v", status)
		}
		for _, route := range acceptanceGatedPaths() {
			if w := requestWithToken(gated, route.method, route.path, fixture.tokens[authzMatrixAdminID]); w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "license_required") {
				t.Fatalf("off a guest %s %s: %d", route.method, route.path, w.Code)
			}
		}
		if err := fixture.panel.requireServerSetupAdmission(context.Background()); !errors.Is(err, errServerSetupLicenseRequired) {
			t.Fatalf("setup admission off a guest: %v", err)
		}
	})
	t.Run("marker removed after activation", func(t *testing.T) {
		manager, marker := acceptancePanelLicense(t, true)
		fixture.panel.license = manager
		if w, _ := acceptanceLicenseRequest(t, fixture, http.MethodPost, `{"action":"activate","key":"`+licensing.AcceptanceFixtureKey+`"}`); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		if w := requestWithToken(gated, "GET", "/api/v1/domains", fixture.tokens[authzMatrixAdminID]); w.Code != 204 {
			t.Fatal("fixture did not open the panel", w.Code)
		}
		if err := os.Remove(marker); err != nil {
			t.Fatal(err)
		}
		for _, route := range acceptanceGatedPaths() {
			if w := requestWithToken(gated, route.method, route.path, fixture.tokens[authzMatrixAdminID]); w.Code != http.StatusForbidden {
				t.Fatalf("marker removed %s %s: %d", route.method, route.path, w.Code)
			}
		}
	})
}
