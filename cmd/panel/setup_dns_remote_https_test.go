package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/repositories"
)

// Exercise the transport and authenticated receiver together over actual TLS.
// Only DNS/IP routing, trust and the receiver's host authority/publication are
// isolated fixtures; JSON, credentials, middleware and durable SQL are real.
func TestRemoteDNSHTTPSProtocolEnrollPublishAndRevoke(t *testing.T) {
	ctx := context.Background()
	origin, receiver := newDNSPanelForTest(t), newDNSPanelForTest(t)
	origin.license, receiver.license = testPanelLicense(t, "active"), testPanelLicense(t, "active")
	subscription := seedSetupDNSOwner(t, origin)
	oldExchange, oldAuthority, oldPublish := remoteDNSExchange, remoteDNSReadLocalAuthority, remoteDNSPublishReceived
	t.Cleanup(func() {
		remoteDNSExchange, remoteDNSReadLocalAuthority, remoteDNSPublishReceived = oldExchange, oldAuthority, oldPublish
	})
	remoteDNSReadLocalAuthority = func(p *Panel, _ context.Context) (remoteDNSAuthority, error) {
		if p != receiver {
			t.Error("origin tried to become a local DNS authority")
		}
		return remoteDNSAuthority{Ready: true, Engine: "bind", Epoch: 1, Nameservers: []string{"ns1.authority.test", "ns2.authority.test"}}, nil
	}
	var publishes atomic.Int32
	remoteDNSPublishReceived = func(p *Panel, _ context.Context, domain string, deleted bool) error {
		if p != receiver || domain != "tls-domain.example" || deleted {
			t.Error("unexpected received publication")
		}
		publishes.Add(1)
		return nil
	}
	stack := receiver.requireRemoteDNSMachineAuth(csrfProtect(receiver.requireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil || !remoteDNSMachineVerified(r) || currentCaller(r) != nil {
			t.Error("TLS machine request did not retain its narrow identity")
		}
		receiver.handleRemoteDNSMachine(w, r)
	}))))
	endpoint, deps, dials := remoteDNSTestTransport(t, stack)
	remoteDNSExchange = func(ctx context.Context, target, path, credential string, request, response any) error {
		return remoteDNSHTTPJSONWith(ctx, target, path, credential, request, response, deps)
	}
	code := strings.Repeat("a", 64)
	if _, err := receiver.db.GetDB().Exec(`INSERT INTO remote_dns_enrollments(code_hash,expires_at) VALUES(?,?)`, remoteDNSHash(code), time.Now().Add(time.Minute).Unix()); err != nil {
		t.Fatal(err)
	}
	id, err := origin.prepareRemoteDNSConnection(ctx, endpoint, code, "TLS fixture origin")
	if err != nil {
		t.Fatal(err)
	}
	connection, err := origin.resumeRemoteDNSConnection(ctx, id)
	if err != nil || connection.Status != "ready" {
		t.Fatalf("real HTTPS enrollment: %v", err)
	}
	domain := &core.Domain{SubscriptionID: subscription, Name: "tls-domain.example", Status: "active", DNSManagement: setupDNSModeExisting, DNSRemoteConnectionID: id}
	if err = repositories.NewPostgresDomainRepository(origin.db.GetDB()).Create(ctx, domain); err != nil {
		t.Fatal(err)
	}
	if err = origin.setRemoteDomainDNSRecords(ctx, domain.Name, []DNSRecord{{Name: domain.Name, Type: "A", Content: "192.0.2.42", TTL: 300}}); err != nil {
		t.Fatal(err)
	}
	var generation, applied int64
	if err = receiver.db.GetDB().QueryRow(`SELECT generation,applied_generation FROM remote_dns_zone_ownership WHERE zone_name=? AND client_id=?`, domain.Name, id).Scan(&generation, &applied); err != nil || generation != 1 || applied != 1 {
		t.Fatalf("HTTPS publication receipt %d/%d: %v", generation, applied, err)
	}
	w := httptest.NewRecorder()
	origin.handleRemoteDNSAdmin(w, remoteDNSAdminRequest(t, http.MethodDelete, "/api/v1/dns/remote/connections?id="+id))
	if w.Code != http.StatusOK {
		t.Fatalf("HTTPS revocation status %d", w.Code)
	}
	if err = origin.setRemoteDomainDNSRecords(ctx, domain.Name, []DNSRecord{{Name: domain.Name, Type: "A", Content: "192.0.2.43", TTL: 300}}); err == nil {
		t.Fatal("revoked connection still changed records")
	}
	var content string
	if err = receiver.db.GetDB().QueryRow(`SELECT content FROM pdns_records WHERE name=? AND type='A'`, domain.Name).Scan(&content); err != nil || content != "192.0.2.42" {
		t.Fatalf("revocation changed the already served record: %q %v", content, err)
	}
	if publishes.Load() != 1 || dials.Load() < 4 {
		t.Fatalf("expected one publication over separate TLS exchanges: publishes=%d dials=%d", publishes.Load(), dials.Load())
	}
}
