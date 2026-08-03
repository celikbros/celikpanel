package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDomainGeneralSettingsPOSTIsReadOnly(t *testing.T) {
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/domains/42/general",
		strings.NewReader(`{"document_root":"/var/www/other-tenant","web_server":"nginx"}`),
	)
	rec := httptest.NewRecorder()

	(&Panel{}).handleDomainGeneralSettings(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusMethodNotAllowed, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "managed by the hosting layout") {
		t.Fatalf("response does not explain the read-only contract: %s", rec.Body.String())
	}
}
