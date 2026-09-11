package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/repositories"
)

type remoteDNSHTTPFixture struct {
	origin, receiver *Panel
	sub              int
	code, id         string
	lostPath         string
	publishes        int
	failPublication  bool
}

func newRemoteDNSHTTPFixture(t *testing.T) *remoteDNSHTTPFixture {
	t.Helper()
	f := &remoteDNSHTTPFixture{origin: newDNSPanelForTest(t), receiver: newDNSPanelForTest(t), code: strings.Repeat("a", 64)}
	f.origin.license = testPanelLicense(t, "active")
	f.receiver.license = testPanelLicense(t, "active")
	f.sub = seedSetupDNSOwner(t, f.origin)
	_, err := f.receiver.db.GetDB().Exec(`INSERT INTO remote_dns_enrollments(code_hash,expires_at) VALUES(?,?)`, remoteDNSHash(f.code), time.Now().Add(10*time.Minute).Unix())
	if err != nil {
		t.Fatal(err)
	}
	oldExchange, oldAuthority, oldPublish := remoteDNSExchange, remoteDNSReadLocalAuthority, remoteDNSPublishReceived
	t.Cleanup(func() {
		remoteDNSExchange = oldExchange
		remoteDNSReadLocalAuthority = oldAuthority
		remoteDNSPublishReceived = oldPublish
	})
	remoteDNSReadLocalAuthority = func(*Panel, context.Context) (remoteDNSAuthority, error) {
		return remoteDNSAuthority{Ready: true, Engine: "bind", Epoch: 1, Nameservers: []string{"ns1.remote.test", "ns2.remote.test"}}, nil
	}
	remoteDNSPublishReceived = func(*Panel, context.Context, string, bool) error {
		f.publishes++
		if f.failPublication {
			return errors.New("secondary propagation pending")
		}
		return nil
	}
	remoteDNSExchange = func(ctx context.Context, endpoint, path, secret string, request, response any) error {
		if endpoint != "https://dns.remote.test:2083" {
			return errors.New("unexpected configured endpoint")
		}
		raw, err := json.Marshal(request)
		if err != nil {
			return err
		}
		r := httptest.NewRequest(http.MethodPost, endpoint+path, bytes.NewReader(raw)).WithContext(ctx)
		r.Header.Set("Authorization", "Bearer "+secret)
		w := httptest.NewRecorder()
		f.receiver.handleRemoteDNSMachine(w, r)
		if w.Code != http.StatusOK {
			return &remoteDNSHTTPError{StatusCode: w.Code}
		}
		if f.lostPath == path {
			f.lostPath = ""
			return errors.New("response lost after receiver commit")
		}
		return json.Unmarshal(w.Body.Bytes(), response)
	}
	f.id, err = f.origin.prepareRemoteDNSConnection(context.Background(), "https://dns.remote.test:2083", f.code, "fixture origin")
	if err != nil {
		t.Fatal(err)
	}
	return f
}
func (f *remoteDNSHTTPFixture) connect(t *testing.T) remoteDNSConnection {
	t.Helper()
	c, err := f.origin.resumeRemoteDNSConnection(context.Background(), f.id)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func (f *remoteDNSHTTPFixture) domain(t *testing.T, name string, parent *int) int {
	t.Helper()
	d := &core.Domain{SubscriptionID: f.sub, Name: name, Status: "active", DNSManagement: setupDNSModeExisting, DNSRemoteConnectionID: f.id, ParentDomainID: parent}
	if err := repositories.NewPostgresDomainRepository(f.origin.db.GetDB()).Create(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	return d.ID
}
func remoteDNSAdminRequest(t *testing.T, method, path string) *http.Request {
	t.Helper()
	r := httptest.NewRequest(method, path, nil)
	return r.WithContext(context.WithValue(r.Context(), callerKey, &Caller{ID: 1, Role: roleAdmin}))
}

func TestRemoteDNSEnrollmentReplaysLostReplyAndDeduplicatesCompletedPost(t *testing.T) {
	f := newRemoteDNSHTTPFixture(t)
	f.lostPath = "/api/v1/dns/remote/accept"
	if _, err := f.origin.resumeRemoteDNSConnection(context.Background(), f.id); err == nil {
		t.Fatal("lost response was called success")
	}
	pending, err := f.origin.readRemoteDNSConnection(context.Background(), f.id)
	if err != nil || pending.Status != "pending" {
		t.Fatal("pending identity was lost")
	}
	_, _ = f.receiver.db.GetDB().Exec(`UPDATE remote_dns_enrollments SET expires_at=0`)
	c := f.connect(t)
	if c.Status != "ready" || c.credential != pending.credential || c.enrollmentCode != "" {
		t.Fatal("resumption replaced identity or retained one-time code")
	}
	duplicate, err := f.origin.prepareRemoteDNSConnection(context.Background(), c.Endpoint, f.code, "fixture origin")
	if err != nil || duplicate != f.id {
		t.Fatal("repeated admin request created another client")
	}
	var count int
	var hash string
	if err = f.receiver.db.GetDB().QueryRow(`SELECT COUNT(*),credential_hash FROM remote_dns_clients`).Scan(&count, &hash); err != nil || count != 1 || hash == c.credential || hash != remoteDNSHash(c.credential) {
		t.Fatal("receiver did not retain only one hashed credential")
	}
	var proof remoteDNSAuthority
	err = remoteDNSExchange(context.Background(), c.Endpoint, "/api/v1/dns/remote/accept", f.code, remoteDNSAcceptRequest{ClientID: strings.Repeat("b", 32), Credential: strings.Repeat("c", 64), Label: "another client"}, &proof)
	if err == nil {
		t.Fatal("consumed enrollment authorized another client")
	}
	list := httptest.NewRecorder()
	f.origin.handleRemoteDNSAdmin(list, remoteDNSAdminRequest(t, http.MethodGet, "/api/v1/dns/remote/connections"))
	if list.Code != 200 || strings.Contains(list.Body.String(), c.credential) || strings.Contains(list.Body.String(), f.code) {
		t.Fatal("public list disclosed enrollment material")
	}
}

func TestRemoteDNSExpiredPendingCancellationAndRevocationLostReply(t *testing.T) {
	t.Run("expired unconsumed enrollment", func(t *testing.T) {
		f := newRemoteDNSHTTPFixture(t)
		_, _ = f.receiver.db.GetDB().Exec(`UPDATE remote_dns_enrollments SET expires_at=0`)
		if _, err := f.origin.resumeRemoteDNSConnection(context.Background(), f.id); err == nil {
			t.Fatal("expired enrollment granted publication")
		}
		w := httptest.NewRecorder()
		f.origin.handleRemoteDNSAdmin(w, remoteDNSAdminRequest(t, http.MethodDelete, "/api/v1/dns/remote/connections?id="+f.id))
		if w.Code != 200 {
			t.Fatalf("expired enrollment could not be cancelled: %d", w.Code)
		}
		c, err := f.origin.readRemoteDNSConnection(context.Background(), f.id)
		if err != nil || c.Status != "revoked" || c.credential != "" {
			t.Fatal("cancelled pending identity remained live")
		}
		var revoked bool
		if err = f.receiver.db.GetDB().QueryRow(`SELECT revoked FROM remote_dns_clients WHERE id=?`, f.id).Scan(&revoked); err != nil || !revoked {
			t.Fatal("expired cancellation created a publication grant")
		}
	})
	t.Run("lost revocation response", func(t *testing.T) {
		f := newRemoteDNSHTTPFixture(t)
		c := f.connect(t)
		f.lostPath = "/api/v1/dns/remote/receiver/status"
		for i, want := range []int{409, 200} {
			w := httptest.NewRecorder()
			f.origin.handleRemoteDNSAdmin(w, remoteDNSAdminRequest(t, http.MethodDelete, "/api/v1/dns/remote/connections?id="+f.id))
			if w.Code != want {
				t.Fatalf("revoke attempt%d: %d want%d", i, w.Code, want)
			}
		}
		var proof remoteDNSAuthority
		if err := remoteDNSExchange(context.Background(), c.Endpoint, "/api/v1/dns/remote/receiver/status", c.ID+"."+c.credential, struct{}{}, &proof); err == nil {
			t.Fatal("revoked token retained authority proof access")
		}
	})
}

func TestRemoteDNSOutboxReconcilesExactGenerationAndPreservesForeignZones(t *testing.T) {
	f := newRemoteDNSHTTPFixture(t)
	c := f.connect(t)
	t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.44")
	f.domain(t, "customer.test", nil)
	f.lostPath = "/api/v1/dns/remote/receiver/publish"
	if err := f.origin.ensureRemoteDomainDNS(context.Background(), "customer.test"); err == nil {
		t.Fatal("lost publication reply was success")
	}
	var generation, applied int64
	var payload string
	if err := f.origin.db.GetDB().QueryRow(`SELECT generation,applied_generation,payload_json FROM remote_dns_zones`).Scan(&generation, &applied, &payload); err != nil || generation != 1 || applied != 0 {
		t.Fatal("exact pending outbox missing")
	}
	if err := f.origin.syncRemoteDomainDNS(context.Background(), "customer.test", false); err != nil {
		t.Fatal(err)
	}
	if f.publishes != 1 {
		t.Fatal("lost reply replayed already committed publication")
	}
	var localZones int
	_ = f.origin.db.GetDB().QueryRow(`SELECT COUNT(*) FROM pdns_domains`).Scan(&localZones)
	if localZones != 0 {
		t.Fatal("consumer installed local shadow authority")
	}
	var request remoteDNSPublication
	if json.Unmarshal([]byte(payload), &request) != nil {
		t.Fatal("bad test payload")
	}
	request.Records[0].Content = "192.0.2.99"
	changed, err := canonicalRemoteDNSPublication(request)
	if err != nil {
		t.Fatal(err)
	}
	var receipt remoteDNSPublicationReceipt
	if err = remoteDNSExchange(context.Background(), c.Endpoint, "/api/v1/dns/remote/receiver/publish", c.ID+"."+c.credential, changed, &receipt); err == nil {
		t.Fatal("same generation accepted changed records")
	}
	otherID, otherCredential := strings.Repeat("d", 32), strings.Repeat("e", 64)
	_, err = f.receiver.db.GetDB().Exec(`INSERT INTO remote_dns_clients(id,credential_hash,label,created_at) VALUES(?,?,?,'now')`, otherID, remoteDNSHash(otherCredential), "other")
	if err != nil {
		t.Fatal(err)
	}
	if err = remoteDNSExchange(context.Background(), c.Endpoint, "/api/v1/dns/remote/receiver/publish", otherID+"."+otherCredential, request, &receipt); err == nil {
		t.Fatal("another client claimed an existing remote zone")
	}
	_, err = f.receiver.db.GetDB().Exec(`INSERT INTO pdns_domains(name,type) VALUES('local.test','MASTER')`)
	if err != nil {
		t.Fatal(err)
	}
	local, err := canonicalRemoteDNSPublication(remoteDNSPublication{Domain: "local.test", Generation: 1, Records: []DNSRecord{{Name: "local.test", Type: "A", Content: "192.0.2.44", TTL: 300}}})
	if err != nil {
		t.Fatal(err)
	}
	if err = remoteDNSExchange(context.Background(), c.Endpoint, "/api/v1/dns/remote/receiver/publish", c.ID+"."+c.credential, local, &receipt); err == nil {
		t.Fatal("client adopted an existing local zone")
	}
	if _, err = f.receiver.db.GetDB().Exec(`UPDATE remote_dns_clients SET revoked=1 WHERE id=?`, c.ID); err != nil {
		t.Fatal(err)
	}
	var served int
	_ = f.receiver.db.GetDB().QueryRow(`SELECT COUNT(*) FROM pdns_domains WHERE name='customer.test'`).Scan(&served)
	if served != 1 {
		t.Fatal("revocation removed served workload")
	}
	if err = remoteDNSExchange(context.Background(), c.Endpoint, "/api/v1/dns/remote/receiver/publish", c.ID+"."+c.credential, request, &receipt); err == nil {
		t.Fatal("revoked credential published")
	}
}

func TestRemoteDNSChildAndMailMutationsPreserveExistingRecords(t *testing.T) {
	f := newRemoteDNSHTTPFixture(t)
	f.connect(t)
	t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.44")
	parent := f.domain(t, "customer.test", nil)
	f.domain(t, "shop.customer.test", &parent)
	ctx := context.Background()
	base := []DNSRecord{{Name: "customer.test", Type: "A", Content: "192.0.2.44", TTL: 300}, {Name: "customer.test", Type: "TXT", Content: "provider-verification=keep", TTL: 300}, {Name: "customer.test", Type: "TXT", Content: "v=spf1 -all", TTL: 300}}
	if err := f.origin.setRemoteDomainDNSRecords(ctx, "customer.test", base); err != nil {
		t.Fatal(err)
	}
	if err := f.origin.ensureRemoteDomainDNS(ctx, "shop.customer.test"); err != nil {
		t.Fatal(err)
	}
	desired := []DNSRecord{{Name: "customer.test", Type: "TXT", Content: "v=spf1 mx -all", TTL: 300}, {Name: "customer.test", Type: "MX", Content: "mail.customer.test", TTL: 300, Prio: 10}, {Name: "mail.customer.test", Type: "A", Content: "192.0.2.44", TTL: 300}}
	if err := f.origin.remoteDNSMailRecords(ctx, "customer.test", desired); err != nil {
		t.Fatal(err)
	}
	records, err := f.origin.remoteDomainDNSRecords(ctx, "customer.test")
	if err != nil {
		t.Fatal(err)
	}
	verification, child, spf := false, false, 0
	for _, record := range records {
		if strings.Contains(record.Content, "provider-verification=keep") {
			verification = true
		}
		if record.Name == "shop.customer.test" {
			child = true
		}
		if remoteDNSMailTXTGroup(record) == "v=spf1" {
			spf++
		}
	}
	if !verification || !child || spf != 1 {
		t.Fatal("mail apply removed unrelated TXT/child or retained duplicate SPF")
	}
	conflicting := append([]DNSRecord(nil), desired...)
	conflicting[2].Content = "192.0.2.88"
	if err = f.origin.remoteDNSMailRecords(ctx, "customer.test", conflicting); !errors.Is(err, errRemoteDNSMailConflict) {
		t.Fatalf("mail address conflict not preserved: %v", err)
	}
	if err = f.origin.syncRemoteDomainDNS(ctx, "shop.customer.test", true); err != nil {
		t.Fatal(err)
	}
	records, err = f.origin.remoteDomainDNSRecords(ctx, "customer.test")
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range records {
		if remoteDNSNameWithin(record.Name, "shop.customer.test") {
			t.Fatal("deleted child records remain")
		}
	}
	var zones int
	_ = f.receiver.db.GetDB().QueryRow(`SELECT COUNT(*) FROM pdns_domains WHERE name='customer.test'`).Scan(&zones)
	if zones != 1 {
		t.Fatal("child deletion removed parent authority")
	}
}

func TestRemoteDNSCanonicalRecordsRejectAmbiguousCNAME(t *testing.T) {
	_, err := canonicalRemoteDNSRecords("customer.test", []DNSRecord{{Name: "www.customer.test", Type: "CNAME", Content: "first.test", TTL: 300}, {Name: "www.customer.test", Type: "CNAME", Content: "second.test", TTL: 300}})
	if err == nil {
		t.Fatal("multiple CNAME targets accepted")
	}
}
