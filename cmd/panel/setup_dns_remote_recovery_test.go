package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRemoteDNSPendingCancelWinsConcurrentReadyTransition(t *testing.T) {
	f := newRemoteDNSHTTPFixture(t)
	exchange := remoteDNSExchange
	interleaved := false
	remoteDNSExchange = func(ctx context.Context, endpoint, path, secret string, request, response any) error {
		if accept, ok := request.(remoteDNSAcceptRequest); ok && accept.Cancel && !interleaved {
			interleaved = true
			// DELETE already read its pending row; an in-flight acceptance now
			// commits ready before the cancellation reaches the receiver.
			f.connect(t)
		}
		return exchange(ctx, endpoint, path, secret, request, response)
	}
	w := httptest.NewRecorder()
	f.origin.handleRemoteDNSAdmin(w, remoteDNSAdminRequest(t, http.MethodDelete, "/api/v1/dns/remote/connections?id="+f.id))
	if w.Code != 200 || !interleaved {
		t.Fatalf("cancellation did not reconcile ready transition: %d", w.Code)
	}
	c, err := f.origin.readRemoteDNSConnection(context.Background(), f.id)
	if err != nil || c.Status != "revoked" || c.credential != "" || c.enrollmentCode != "" {
		t.Fatal("revocation receipt left a ready origin credential")
	}
}

func TestRemoteDNSLostCancellationReplyReconcilesAfterEnrollmentExpiry(t *testing.T) {
	f := newRemoteDNSHTTPFixture(t)
	_, _ = f.receiver.db.GetDB().Exec(`UPDATE remote_dns_enrollments SET expires_at=0`)
	f.lostPath = "/api/v1/dns/remote/accept"
	for attempt, want := range []int{409, 200} {
		w := httptest.NewRecorder()
		f.origin.handleRemoteDNSAdmin(w, remoteDNSAdminRequest(t, http.MethodDelete, "/api/v1/dns/remote/connections?id="+f.id))
		if w.Code != want {
			t.Fatalf("cancellation attempt%d got%d want%d", attempt, w.Code, want)
		}
	}
	var revoked bool
	if err := f.receiver.db.GetDB().QueryRow(`SELECT revoked FROM remote_dns_clients WHERE id=?`, f.id).Scan(&revoked); err != nil || !revoked {
		t.Fatal("cancelled enrollment retained publication access")
	}
}

func TestRemoteDNSPendingGenerationMustConvergeBeforeAnotherEdit(t *testing.T) {
	f := newRemoteDNSHTTPFixture(t)
	f.connect(t)
	f.domain(t, "customer.test", nil)
	t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.44")
	f.failPublication = true
	if err := f.origin.ensureRemoteDomainDNS(context.Background(), "customer.test"); err == nil {
		t.Fatal("unconfirmed authority was success")
	}
	changed := []DNSRecord{{Name: "customer.test", Type: "A", Content: "192.0.2.99", TTL: 300}}
	if err := f.origin.setRemoteDomainDNSRecords(context.Background(), "customer.test", changed); err == nil {
		t.Fatal("new generation replaced an unresolved publication")
	}
	var generation int64
	if err := f.origin.db.GetDB().QueryRow(`SELECT generation FROM remote_dns_zones`).Scan(&generation); err != nil || generation != 1 {
		t.Fatal("pending origin generation was replaced")
	}
	f.failPublication = false
	if err := f.origin.setRemoteDomainDNSRecords(context.Background(), "customer.test", changed); err != nil {
		t.Fatal(err)
	}
	var applied int64
	if err := f.receiver.db.GetDB().QueryRow(`SELECT generation,applied_generation FROM remote_dns_zone_ownership WHERE zone_name='customer.test'`).Scan(&generation, &applied); err != nil || generation != 2 || applied != 2 {
		t.Fatal("generation did not converge in order")
	}
}
