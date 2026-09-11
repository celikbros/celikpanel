package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRemoteDNSRootDeleteRecreateRetainsGenerationAndRejectsStaleDelete(t *testing.T) {
	f := newRemoteDNSHTTPFixture(t)
	c := f.connect(t)
	t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.44")
	domainID := f.domain(t, "customer.test", nil)
	ctx := context.Background()
	if err := f.origin.ensureRemoteDomainDNS(ctx, "customer.test"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.origin.db.GetDB().Exec(`DELETE FROM domains WHERE id=?`, domainID); err == nil {
		t.Fatal("domain deletion discarded a live remote DNS zone without receipt")
	}
	if err := f.origin.syncRemoteDomainDNS(ctx, "customer.test", true); err != nil {
		t.Fatal(err)
	}
	var raw string
	if err := f.origin.db.GetDB().QueryRow(`SELECT payload_json FROM remote_dns_zones WHERE domain_id=?`, domainID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var staleDelete remoteDNSPublication
	if json.Unmarshal([]byte(raw), &staleDelete) != nil || !staleDelete.Deleted || staleDelete.Generation != 2 {
		t.Fatal("invalid deletion fixture")
	}
	if _, err := f.origin.db.GetDB().Exec(`DELETE FROM domains WHERE id=?`, domainID); err != nil {
		t.Fatal(err)
	}
	var historyGeneration, historyApplied int64
	if err := f.origin.db.GetDB().QueryRow(`SELECT generation,applied_generation FROM remote_dns_origin_history WHERE connection_id=? AND zone_name='customer.test'`, f.id).Scan(&historyGeneration, &historyApplied); err != nil || historyGeneration != 2 || historyApplied != 2 {
		t.Fatal("domain removal erased exact deletion history")
	}
	f.domain(t, "customer.test", nil)
	f.lostPath = "/api/v1/dns/remote/receiver/publish"
	if err := f.origin.ensureRemoteDomainDNS(ctx, "customer.test"); err == nil {
		t.Fatal("lost recreated publication receipt became success")
	}
	if err := f.origin.ensureRemoteDomainDNS(ctx, "customer.test"); err != nil {
		t.Fatal(err)
	}
	var generation, applied int64
	var deleted bool
	if err := f.receiver.db.GetDB().QueryRow(`SELECT generation,applied_generation,deleted FROM remote_dns_zone_ownership WHERE zone_name='customer.test'`).Scan(&generation, &applied, &deleted); err != nil || generation != 3 || applied != 3 || deleted {
		t.Fatalf("recreated ownership did not continue its generation: %d/%d deleted=%v error=%v", applied, generation, deleted, err)
	}
	var receipt remoteDNSPublicationReceipt
	if err := remoteDNSExchange(ctx, c.Endpoint, "/api/v1/dns/remote/receiver/publish", c.ID+"."+c.credential, staleDelete, &receipt); err == nil {
		t.Fatal("old deletion removed recreated zone")
	}
	var zones int
	if err := f.receiver.db.GetDB().QueryRow(`SELECT COUNT(*) FROM pdns_domains WHERE name='customer.test'`).Scan(&zones); err != nil || zones != 1 {
		t.Fatal("stale deletion changed served recreated zone")
	}
}

func TestRemoteDNSGenericCancellation401PreservesPossiblyLiveCredential(t *testing.T) {
	f := newRemoteDNSHTTPFixture(t)
	f.lostPath = "/api/v1/dns/remote/accept"
	if _, err := f.origin.resumeRemoteDNSConnection(context.Background(), f.id); err == nil {
		t.Fatal("expected lost enrollment reply")
	}
	before, err := f.origin.readRemoteDNSConnection(context.Background(), f.id)
	if err != nil {
		t.Fatal(err)
	}
	exchange := remoteDNSExchange
	remoteDNSExchange = func(ctx context.Context, endpoint, path, secret string, request, response any) error {
		if accept, ok := request.(remoteDNSAcceptRequest); ok && accept.Cancel {
			return &remoteDNSHTTPError{StatusCode: http.StatusUnauthorized}
		}
		return exchange(ctx, endpoint, path, secret, request, response)
	}
	w := httptest.NewRecorder()
	f.origin.handleRemoteDNSAdmin(w, remoteDNSAdminRequest(t, http.MethodDelete, "/api/v1/dns/remote/connections?id="+f.id))
	if w.Code != http.StatusConflict {
		t.Fatal("generic HTTP denial became cancellation proof")
	}
	after, err := f.origin.readRemoteDNSConnection(context.Background(), f.id)
	if err != nil || after.Status != "pending" || after.credential != before.credential || after.enrollmentCode != before.enrollmentCode {
		t.Fatal("uncertain cancellation discarded recovery credentials")
	}
	if !f.receiver.remoteDNSClientAuthorized(context.Background(), f.id, remoteDNSHash(before.credential)) {
		t.Fatal("test did not retain a live receiver grant")
	}
}
