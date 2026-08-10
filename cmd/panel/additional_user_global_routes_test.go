package main

import (
	"net/http"
	"strings"
	"testing"
)

func TestRequireAuthAdditionalUserRejectsGlobalRouteFamilies(t *testing.T) {
	fixture := newAuthzMatrixFixture(t)
	token := seedAdditionalUserSession(t, &fixture)

	tests := []struct {
		name string
		path string
	}{
		{name: "system stats", path: "/api/v1/system/stats"},
		{name: "hosting capabilities", path: "/api/v1/hosting/capabilities"},
		{name: "products", path: "/api/v1/products"},
		{name: "domain create", path: "/api/v1/domains/create"},
		{name: "store root", path: "/api/v1/store"},
		{name: "store child", path: "/api/v1/store/application-installer"},
		{name: "users", path: "/api/v1/users/123"},
		{name: "team members", path: "/api/v1/team-members/123"},
		{name: "plans", path: "/api/v1/plans/123"},
		{name: "subscription entitlements", path: "/api/v1/subscriptions/9802/entitlements"},
		{name: "vpn status", path: "/api/v1/vpn/status"},
		{name: "vpn peer", path: "/api/v1/vpn/peers/peer-1"},
		{name: "runtime inventory", path: "/api/v1/runtimes/node"},
		{name: "runtime upstream", path: "/api/v1/runtimes/node/lts"},
		{name: "database tool exact", path: "/dbtool"},
		{name: "database tool proxy", path: "/dbtool/phpmyadmin/"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reached := false
			handler := fixture.panel.requireAuth(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				reached = true
				w.WriteHeader(http.StatusNoContent)
			}))

			recorder := requestWithToken(handler, http.MethodGet, test.path, token)
			if recorder.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusForbidden, recorder.Body.String())
			}
			if reached {
				t.Fatal("restricted additional-user request reached the downstream handler")
			}
			if !strings.Contains(recorder.Body.String(), `"code":"`+errCodeAdditionalUserScope+`"`) {
				t.Fatalf("body = %s, want error code %q", recorder.Body.String(), errCodeAdditionalUserScope)
			}
		})
	}
}

func TestAdditionalUserRestrictedPathUsesRouteBoundaries(t *testing.T) {
	restricted := []string{
		"/api/v1/system/stats",
		"/api/v1/hosting/capabilities",
		"/api/v1/products",
		"/api/v1/domains/create",
		"/api/v1/store",
		"/api/v1/store/catalog",
		"/api/v1/users",
		"/api/v1/users/123",
		"/api/v1/team-members",
		"/api/v1/team-members/123",
		"/api/v1/plans",
		"/api/v1/plans/123",
		"/api/v1/subscriptions",
		"/api/v1/subscriptions/9802/entitlements",
		"/api/v1/vpn",
		"/api/v1/vpn/peers",
		"/api/v1/runtimes",
		"/api/v1/runtimes/node/lts",
		"/dbtool",
		"/dbtool/phppgadmin/",
	}
	for _, path := range restricted {
		if !isAdditionalUserRestrictedPath(path) {
			t.Errorf("isAdditionalUserRestrictedPath(%q) = false, want true", path)
		}
	}

	allowed := []string{
		"/api/v1/storefront",
		"/api/v1/users-export",
		"/api/v1/team-membership",
		"/api/v1/plans-preview",
		"/api/v1/subscriptions-export",
		"/api/v1/vpn-status",
		"/api/v1/runtimes-extra",
		"/dbtooling",
		"/api/v1/products-preview",
		"/api/v1/hosting/capabilities-preview",
		"/api/v1/domains/create-preview",
		"/api/v1/domains/9902/files",
		"/api/v1/ssl/providers",
		"/webmail/",
	}
	for _, path := range allowed {
		if isAdditionalUserRestrictedPath(path) {
			t.Errorf("isAdditionalUserRestrictedPath(%q) = true, want false", path)
		}
	}
}

func TestRequireAuthAdditionalUserGlobalGuardPreservesAccountRoles(t *testing.T) {
	fixture := newAuthzMatrixFixture(t)
	handler := fixture.panel.requireAuth(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	roles := []struct {
		name string
		id   int
	}{
		{name: "admin", id: authzMatrixAdminID},
		{name: "reseller", id: authzMatrixResellerID},
		{name: "customer", id: authzMatrixCustomerID},
	}
	paths := []string{
		"/api/v1/system/stats",
		"/api/v1/hosting/capabilities",
		"/api/v1/products",
		"/api/v1/store",
		"/api/v1/users",
		"/api/v1/team-members",
		"/api/v1/plans",
		"/api/v1/subscriptions/9802/entitlements",
		"/api/v1/vpn/status",
		"/api/v1/runtimes/node",
		"/dbtool/phpmyadmin/",
	}

	for _, role := range roles {
		for _, path := range paths {
			t.Run(role.name+" "+path, func(t *testing.T) {
				recorder := requestWithToken(handler, http.MethodGet, path, fixture.tokens[role.id])
				if recorder.Code != http.StatusNoContent {
					t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusNoContent, recorder.Body.String())
				}
			})
		}
	}
}
