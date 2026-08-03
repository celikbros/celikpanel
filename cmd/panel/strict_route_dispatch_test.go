package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMatchDomainSubrouteExactRoutes(t *testing.T) {
	tests := []struct {
		method string
		path   string
		kind   string
	}{
		{http.MethodGet, "/api/v1/domains/7/hosting", "hosting"},
		{http.MethodPost, "/api/v1/domains/7/app/restart", "app"},
		{http.MethodDelete, "/api/v1/domains/7/aliases/www.example.test", "aliases"},
		{http.MethodPut, "/api/v1/domains/7/dns/records", "dns"},
		{http.MethodPost, "/api/v1/domains/7/mail/auth/dkim", "mail"},
		{http.MethodDelete, "/api/v1/domains/7/databases/19", "database-delete"},
	}
	for _, test := range tests {
		t.Run(test.method+"_"+test.kind, func(t *testing.T) {
			r := httptest.NewRequest(test.method, test.path, nil)
			match, ok := matchDomainSubroute(r)
			if !ok || match.domainID != 7 || match.kind != test.kind {
				t.Fatalf("unexpected match: %#v, ok=%v", match, ok)
			}
			if !routeAllows(test.method, match.methods) {
				t.Fatalf("method %s not allowed by %#v", test.method, match.methods)
			}
		})
	}
}

func TestDomainSubrouteRejectsAmbiguousPathsBeforeAuthorization(t *testing.T) {
	tests := []string{
		"/api/v1/domains/1/hostingevil",
		"/api/v1/domains/1/extra/hosting",
		"/api/v1/domains/1/hosting/",
		"/api/v1/domains/1/logs/access/extra",
		"/api/v1/domains/01/hosting",
		"/api/v1/domains/1/%2e%2e/hosting",
		"/api/v1/domains/1%2f2/hosting",
	}
	for _, path := range tests {
		t.Run(path, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, path, nil)
			w := httptest.NewRecorder()
			(&Panel{}).handleDomainSubroute(w, r)
			if w.Code != http.StatusNotFound {
				t.Fatalf("status=%d, want 404", w.Code)
			}
		})
	}
}

func TestDomainSubrouteRejectsWrongMethodBeforeAuthorization(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/domains/1/hosting", nil)
	w := httptest.NewRecorder()
	(&Panel{}).handleDomainSubroute(w, r)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405", w.Code)
	}
	if allow := w.Header().Get("Allow"); allow != "GET, PUT" {
		t.Fatalf("Allow=%q, want GET, PUT", allow)
	}
}

func TestDatabaseSubrouteExactShapeAndMethod(t *testing.T) {
	valid := httptest.NewRequest(http.MethodPost, "/api/v1/database-servers/3/databases", nil)
	match, ok := matchDatabaseSubroute(valid, "/api/v1/database-servers/")
	if !ok || match.resourceID != 3 || match.kind != "server-databases" || !routeAllows(valid.Method, match.methods) {
		t.Fatalf("unexpected valid match: %#v, ok=%v", match, ok)
	}

	tests := []struct {
		method string
		path   string
		want   int
	}{
		{http.MethodGet, "/api/v1/database-servers/3/databases-extra", http.StatusNotFound},
		{http.MethodGet, "/api/v1/database-servers/3/databases/extra", http.StatusNotFound},
		{http.MethodGet, "/api/v1/databases/4/grants/", http.StatusNotFound},
		{http.MethodDelete, "/api/v1/databases/4/grants", http.StatusMethodNotAllowed},
		{http.MethodGet, "/api/v1/database-users/4%2f5", http.StatusNotFound},
	}
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			prefix := "/api/v1/database-servers/"
			if strings.Contains(test.path, "/databases/") {
				prefix = "/api/v1/databases/"
			} else if strings.Contains(test.path, "/database-users/") {
				prefix = "/api/v1/database-users/"
			}
			r := httptest.NewRequest(test.method, test.path, nil)
			w := httptest.NewRecorder()
			(&Panel{}).handleDatabaseSubroute(w, r, prefix)
			if w.Code != test.want {
				t.Fatalf("status=%d, want %d", w.Code, test.want)
			}
		})
	}
}
