package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func assertDNSEngineWorkflowRefusal(t *testing.T, recorder *httptest.ResponseRecorder) {
	t.Helper()
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var body apiErrorBody
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode refusal: %v", err)
	}
	if body.Code != errCodeDNSEngineWorkflowRequired {
		t.Fatalf("code=%q body=%s", body.Code, recorder.Body.String())
	}
	if body.Action != "/settings?section=dns" {
		t.Fatalf("action=%q", body.Action)
	}
}

func TestDNSManagedServicesRequireDedicatedInstallPreflight(t *testing.T) {
	panel := &Panel{}
	for _, serviceID := range []string{"bind", "pdns"} {
		t.Run(serviceID, func(t *testing.T) {
			err := panel.preflightManagedServiceInstall(context.Background(), serviceID, "")
			if !errors.Is(err, errDNSEngineWorkflowRequired) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestDNSManagedServiceHandlersRefuseGenericMutationsBeforePersistence(t *testing.T) {
	fixture := newServiceOperationTestFixture(t)
	requests := []struct {
		name   string
		target string
		body   string
		call   func(http.ResponseWriter, *http.Request)
	}{
		{
			name:   "install BIND",
			target: "/api/v1/service/install",
			body:   `{"service_id":"bind","request_id":"00112233445566778899aabbccddeeff"}`,
			call:   fixture.panel.handleServiceInstall,
		},
		{
			name:   "uninstall PowerDNS",
			target: "/api/v1/service/uninstall",
			body:   `{"service_id":"pdns"}`,
			call:   fixture.panel.handleServiceUninstall,
		},
		{
			name:   "start BIND",
			target: "/api/v1/service/action",
			body:   `{"service_name":"bind9","action":"start"}`,
			call:   fixture.panel.handleServiceAction,
		},
		{
			name:   "restart PowerDNS",
			target: "/api/v1/service/action",
			body:   `{"service_name":"pdns","action":"restart"}`,
			call:   fixture.panel.handleServiceAction,
		},
	}

	for _, test := range requests {
		t.Run(test.name, func(t *testing.T) {
			beforeCalls := fixture.agent.installCalls.Load()
			var beforeRows int
			if err := fixture.database.GetDB().QueryRow(`SELECT COUNT(*) FROM service_operations`).Scan(&beforeRows); err != nil {
				t.Fatal(err)
			}
			recorder := httptest.NewRecorder()
			test.call(recorder, serviceOperationAdminRequest(t, http.MethodPost, test.target, test.body, fixture.userID))
			assertDNSEngineWorkflowRefusal(t, recorder)
			if got := fixture.agent.installCalls.Load(); got != beforeCalls {
				t.Fatalf("agent install calls changed: %d -> %d", beforeCalls, got)
			}
			var afterRows int
			if err := fixture.database.GetDB().QueryRow(`SELECT COUNT(*) FROM service_operations`).Scan(&afterRows); err != nil {
				t.Fatal(err)
			}
			if afterRows != beforeRows {
				t.Fatalf("service operation rows changed: %d -> %d", beforeRows, afterRows)
			}
		})
	}
}

func TestDNSRecordMutationRequiresVerifiedActiveEngineBeforeLedgerLookup(t *testing.T) {
	fixture := newServiceOperationTestFixture(t)
	var before int
	if err := fixture.database.GetDB().QueryRow(`SELECT COUNT(*) FROM pdns_records`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	fixture.panel.handleDomainDNS(
		recorder,
		serviceOperationAdminRequest(
			t,
			http.MethodPost,
			"/api/v1/domains/999/dns/records",
			`{"name":"www","type":"A","content":"192.0.2.1","ttl":300}`,
			fixture.userID,
		),
	)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var body apiErrorBody
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != errCodeDNSServerRequired || body.Action != "/settings?section=dns" {
		t.Fatalf("unexpected refusal: %+v", body)
	}
	var after int
	if err := fixture.database.GetDB().QueryRow(`SELECT COUNT(*) FROM pdns_records`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("DNS ledger changed: %d -> %d", before, after)
	}
}
