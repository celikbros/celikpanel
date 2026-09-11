package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRemoteDNSRecordValidationDoesNotCreatePendingPublication(t *testing.T) {
	f := newRemoteDNSHTTPFixture(t)
	f.connect(t)
	t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.44")
	f.domain(t, "customer.test", nil)
	if err := f.origin.ensureRemoteDomainDNS(context.Background(), "customer.test"); err != nil {
		t.Fatal(err)
	}
	records, err := f.origin.remoteDomainDNSRecords(context.Background(), "customer.test")
	if err != nil {
		t.Fatal(err)
	}
	var addressID int
	for _, record := range records {
		if record.Type == "A" && record.Name == "customer.test" {
			addressID = record.ID
		}
	}
	if addressID == 0 {
		t.Fatal("fixture has no address")
	}
	request := func(method, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/api/v1/domains/1/dns/records", strings.NewReader(body))
		w := httptest.NewRecorder()
		f.origin.handleRemoteDomainDNS(w, r, "customer.test")
		return w
	}
	for _, body := range []string{
		fmt.Sprintf(`{"id":%d,"content":"not-an-ip","ttl":300}`, addressID),
		fmt.Sprintf(`{"id":%d,"content":"192.0.2.44","ttl":-1}`, addressID),
	} {
		response := request(http.MethodPut, body)
		if response.Code != http.StatusBadRequest || strings.Contains(response.Body.String(), "PUBLICATION_PENDING") {
			t.Fatalf("invalid local record became publication failure: %d %s", response.Code, response.Body.String())
		}
	}
	valid := request(http.MethodPost, `{"name":"alias","type":"CNAME","content":"one.customer.test","ttl":300}`)
	if valid.Code != http.StatusOK {
		t.Fatalf("valid CNAME: %d %s", valid.Code, valid.Body.String())
	}
	before := f.publishes
	invalid := request(http.MethodPost, `{"name":"alias","type":"CNAME","content":"two.customer.test","ttl":300}`)
	if invalid.Code != http.StatusBadRequest || !strings.Contains(invalid.Body.String(), "CNAME") {
		t.Fatalf("ambiguous CNAME: %d %s", invalid.Code, invalid.Body.String())
	}
	var generation, applied int64
	if err := f.origin.db.GetDB().QueryRow(`SELECT generation,applied_generation FROM remote_dns_zones`).Scan(&generation, &applied); err != nil || generation != 2 || applied != 2 || before != f.publishes {
		t.Fatalf("invalid records changed committed DNS: generation=%d applied=%d publications=%d/%d err=%v", generation, applied, f.publishes, before, err)
	}
}
