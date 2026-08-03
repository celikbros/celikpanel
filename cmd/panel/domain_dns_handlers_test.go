package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestNormalizeDNSOwnerNeverEscapesZone(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "apex", input: "@", want: "biovision.health"},
		{name: "relative", input: "www", want: "www.biovision.health"},
		{name: "in zone fqdn", input: "WWW.BIOVISION.HEALTH.", want: "www.biovision.health"},
		{name: "suffix is not a boundary", input: "evilbiovision.health", want: "evilbiovision.health.biovision.health"},
		{name: "srv underscores", input: "_submission._tcp", want: "_submission._tcp.biovision.health"},
		{name: "wildcard", input: "*", want: "*.biovision.health"},
		{name: "absolute outside zone", input: "outside.example.", wantErr: true},
		{name: "embedded wildcard", input: "www.*", wantErr: true},
		{name: "empty label", input: "bad..name", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := normalizeDNSOwner(test.input, "biovision.health")
			if test.wantErr {
				if err == nil {
					t.Fatalf("normalizeDNSOwner(%q) = %q, want error", test.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeDNSOwner(%q): %v", test.input, err)
			}
			if got != test.want {
				t.Fatalf("normalizeDNSOwner(%q) = %q, want %q", test.input, got, test.want)
			}
		})
	}
}

func TestNormalizeDNSRecordRejectsUnsafeOrInvalidData(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		typeName   string
		owner      string
		content    string
		ttl        int
		priority   int
		wantType   string
		wantValue  string
		wantPrio   *int
		shouldFail bool
	}{
		{name: "IPv4", typeName: "a", owner: "www.biovision.health", content: "192.0.2.10", ttl: 300, wantType: "A", wantValue: "192.0.2.10"},
		{name: "wrong address family", typeName: "A", owner: "www.biovision.health", content: "2001:db8::1", ttl: 300, shouldFail: true},
		{name: "apex CNAME", typeName: "CNAME", owner: "biovision.health", content: "target.example", ttl: 300, shouldFail: true},
		{name: "managed apex NS", typeName: "NS", owner: "biovision.health", content: "ns3.example", ttl: 300, shouldFail: true},
		{name: "MX", typeName: "MX", owner: "biovision.health", content: "MAIL.EXAMPLE.", ttl: 300, priority: 10, wantType: "MX", wantValue: "mail.example", wantPrio: intPtr(10)},
		{name: "bad priority", typeName: "MX", owner: "biovision.health", content: "mail.example", ttl: 300, priority: 65536, shouldFail: true},
		{name: "SRV", typeName: "SRV", owner: "_sip._tcp.biovision.health", content: "5 443 SERVICE.EXAMPLE.", ttl: 300, priority: 20, wantType: "SRV", wantValue: "5 443 service.example", wantPrio: intPtr(20)},
		{name: "bad SRV", typeName: "SRV", owner: "_sip._tcp.biovision.health", content: "443 service.example", ttl: 300, shouldFail: true},
		{name: "TXT quoted", typeName: "TXT", owner: "_verify.biovision.health", content: `"alpha" "beta"`, ttl: 300, wantType: "TXT", wantValue: `"alphabeta"`},
		{name: "TXT injection", typeName: "TXT", owner: "_verify.biovision.health", content: "alpha\nbeta", ttl: 300, shouldFail: true},
		{name: "unsupported", typeName: "SOA", owner: "biovision.health", content: "invalid", ttl: 300, shouldFail: true},
		{name: "negative TTL", typeName: "A", owner: "www.biovision.health", content: "192.0.2.10", ttl: -1, shouldFail: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotType, gotValue, gotPrio, err := normalizeDNSRecord(
				test.typeName, test.owner, test.content, test.ttl, test.priority, "biovision.health",
			)
			if test.shouldFail {
				if err == nil {
					t.Fatalf("normalizeDNSRecord() = (%q, %q, %v), want error", gotType, gotValue, gotPrio)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeDNSRecord(): %v", err)
			}
			if gotType != test.wantType || gotValue != test.wantValue {
				t.Fatalf("normalizeDNSRecord() = (%q, %q), want (%q, %q)", gotType, gotValue, test.wantType, test.wantValue)
			}
			if !sameOptionalInt(gotPrio, test.wantPrio) {
				t.Fatalf("priority = %v, want %v", gotPrio, test.wantPrio)
			}
		})
	}
}

func intPtr(value int) *int { return &value }

func sameOptionalInt(left, right *int) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func TestAddDNSRecordRejectsUnknownFieldsWithoutMutation(t *testing.T) {
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "standalone")
	seedStrictDNSZone(t, p, "biovision.health")

	request := httptest.NewRequest(http.MethodPost, "/api/v1/domains/1/dns/records",
		strings.NewReader(`{"name":"www","type":"A","content":"192.0.2.10","ttl":300,"admin":true}`))
	recorder := httptest.NewRecorder()
	p.handleAddDNSRecord(recorder, request, "biovision.health")
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", recorder.Code, recorder.Body.String())
	}
	var count int
	if err := p.db.GetDB().QueryRow(`
		SELECT COUNT(*) FROM pdns_records r
		JOIN pdns_domains d ON d.id = r.domain_id
		WHERE d.name = ? AND r.name = ? AND r.type = 'A'`,
		"biovision.health", "www.biovision.health",
	).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("invalid request inserted %d records", count)
	}
}

func TestAddDNSRecordReportsSavedButUnpublished(t *testing.T) {
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "standalone")
	seedStrictDNSZone(t, p, "biovision.health")
	agent := &strictDNSRPCAgent{failZone: "biovision.health"}
	attachStrictDNSRPCAgent(t, p, agent)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/domains/1/dns/records",
		strings.NewReader(`{"name":"api","type":"A","content":"192.0.2.10","ttl":300,"prio":0}`))
	recorder := httptest.NewRecorder()
	p.handleAddDNSRecord(recorder, request, "biovision.health")
	assertPublicationConflict(t, recorder)

	var count int
	if err := p.db.GetDB().QueryRow(`
		SELECT COUNT(*) FROM pdns_records r
		JOIN pdns_domains d ON d.id = r.domain_id
		WHERE d.name = ? AND r.name = ? AND r.type = 'A' AND r.content = ?`,
		"biovision.health", "api.biovision.health", "192.0.2.10",
	).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("saved record count = %d, want 1", count)
	}
}

func TestDeleteDNSRecordProtectsSOAAndApexNameservers(t *testing.T) {
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "standalone")
	seedStrictDNSZone(t, p, "biovision.health")

	rows, err := p.db.GetDB().Query(`
		SELECT r.id FROM pdns_records r
		JOIN pdns_domains d ON d.id = r.domain_id
		WHERE d.name = ? AND (r.type = 'SOA' OR (r.type = 'NS' AND r.name = d.name))
		ORDER BY r.id`, "biovision.health")
	if err != nil {
		t.Fatal(err)
	}
	var recordIDs []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		recordIDs = append(recordIDs, id)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if len(recordIDs) < 3 {
		t.Fatalf("managed record count = %d, want at least 3", len(recordIDs))
	}
	for _, id := range recordIDs {
		request := httptest.NewRequest(http.MethodDelete,
			"/api/v1/domains/1/dns/records?id="+strconv.Itoa(id), nil)
		recorder := httptest.NewRecorder()
		p.handleDeleteDNSRecord(recorder, request, "biovision.health")
		if recorder.Code != http.StatusConflict {
			t.Fatalf("record %d status = %d, want 409; body=%s", id, recorder.Code, recorder.Body.String())
		}
	}

	var remaining int
	if err := p.db.GetDB().QueryRow(`
		SELECT COUNT(*) FROM pdns_records r
		JOIN pdns_domains d ON d.id = r.domain_id
		WHERE d.name = ? AND (r.type = 'SOA' OR (r.type = 'NS' AND r.name = d.name))`,
		"biovision.health",
	).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != len(recordIDs) {
		t.Fatalf("remaining managed records = %d, want %d", remaining, len(recordIDs))
	}
}

func TestListDNSRecordsFailsClosedOnCorruptRow(t *testing.T) {
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "standalone")
	seedStrictDNSZone(t, p, "biovision.health")
	var zoneID int
	if err := p.db.GetDB().QueryRow(
		`SELECT id FROM pdns_domains WHERE name = ?`, "biovision.health",
	).Scan(&zoneID); err != nil {
		t.Fatal(err)
	}
	if _, err := p.db.GetDB().Exec(`
		INSERT INTO pdns_records (domain_id, name, type, content, ttl)
		VALUES (?, NULL, 'A', '192.0.2.20', 300)`, zoneID); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/domains/1/dns/records", nil)
	recorder := httptest.NewRecorder()
	p.handleListDNSRecords(recorder, request, "biovision.health")
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", recorder.Code, recorder.Body.String())
	}
	var body map[string]interface{}
	if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if body["error"] != "internal server error" {
		t.Fatalf("response did not fail closed: %v", body)
	}
	if strings.Contains(recorder.Body.String(), "Scan error") || strings.Contains(recorder.Body.String(), "NULL") {
		t.Fatalf("response leaked database details: %s", recorder.Body.String())
	}
}

func TestDeleteDNSRecordRejectsInvalidID(t *testing.T) {
	p := newDNSPanelForTest(t)
	request := httptest.NewRequest(http.MethodDelete, "/api/v1/domains/1/dns/records?id=nope", nil)
	recorder := httptest.NewRecorder()
	p.handleDeleteDNSRecord(recorder, request, "biovision.health")
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestDomainNameForDNSPropagatesDatabaseFailure(t *testing.T) {
	p := newDNSPanelForTest(t)
	p.db.Close()
	if _, err := p.domainNameForDNS(context.Background(), 1); err == nil {
		t.Fatal("closed database error was masked as domain not found")
	}
}
