package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPanelVersionRouteRemainsAdminOnly(t *testing.T) {
	if !isAdminOnlyPath("/api/v1/panel/version") {
		t.Fatal("panel version route is not protected by the admin-only path policy")
	}
}

func TestHandleVersionReportsAppliedSchemaVersion(t *testing.T) {
	t.Setenv("CELIKPANEL_SERVER_IP", "198.51.100.42")
	p := newDNSPanelForTest(t)
	attachVersionPairAgent(t, p, buildCommit)

	var wantSchema int64
	if err := p.db.GetDB().QueryRow(
		`SELECT MAX(version) FROM schema_migrations`,
	).Scan(&wantSchema); err != nil {
		t.Fatalf("read expected schema version: %v", err)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/panel/version", nil)
	p.handleVersion(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get(`Cache-Control`); got != `no-store` {
		t.Fatalf(`Cache-Control = %q, want no-store`, got)
	}
	if got := recorder.Header().Get(`Pragma`); got != `no-cache` {
		t.Fatalf(`Pragma = %q, want no-cache`, got)
	}
	var response struct {
		SchemaVersion int64  `json:"schema_version"`
		Hostname      string `json:"hostname"`
		IPv4          string `json:"ipv4"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.SchemaVersion != wantSchema {
		t.Fatalf(
			"schema_version = %d, want %d",
			response.SchemaVersion,
			wantSchema,
		)
	}
	if response.Hostname != hostnameOrEmpty() {
		t.Fatalf("hostname = %q, want %q", response.Hostname, hostnameOrEmpty())
	}
	if response.IPv4 != "198.51.100.42" {
		t.Fatalf("ipv4 = %q, want 198.51.100.42", response.IPv4)
	}
}

func TestHandleVersionDoesNotPublishInvalidAdvertisedIPv4(t *testing.T) {
	t.Setenv("CELIKPANEL_SERVER_IP", "not-an-ip-address")
	p := newDNSPanelForTest(t)
	attachVersionPairAgent(t, p, buildCommit)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/panel/version", nil)
	p.handleVersion(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		IPv4 string `json:"ipv4"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.IPv4 != "" {
		t.Fatalf("invalid advertised ipv4 was published: %q", response.IPv4)
	}
}

func TestHandleVersionFailsClosedWhenSchemaVersionCannotBeRead(t *testing.T) {
	p := newDNSPanelForTest(t)
	p.db.Close()

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/panel/version", nil)
	p.handleVersion(recorder, request)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf(
			"status = %d, want 500; body=%s",
			recorder.Code,
			recorder.Body.String(),
		)
	}
	if strings.Contains(recorder.Body.String(), "schema_version") {
		t.Fatalf(
			"failed schema query returned a misleading version: %s",
			recorder.Body.String(),
		)
	}
}
