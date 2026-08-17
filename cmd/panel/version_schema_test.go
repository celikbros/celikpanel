package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleVersionReportsAppliedSchemaVersion(t *testing.T) {
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
		SchemaVersion int64 `json:"schema_version"`
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
