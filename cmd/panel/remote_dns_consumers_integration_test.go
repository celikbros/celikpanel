package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/transport"
)

// The network seam dispatches serialized requests through the real machine
// authentication and receiver handlers. Only receiver host authority probes
// and the final paired agent publication are replaced; their exact V3 receipts
// and pinned TLS transport are exercised by their own contract tests.
type remoteConsumerIntegrationFixture struct {
	origin, receiver    *Panel
	connectionID        string
	subscriptionID      int
	published           int
	losePublishResponse bool
}

func newRemoteConsumerIntegrationFixture(t *testing.T) *remoteConsumerIntegrationFixture {
	t.Helper()
	t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.45")
	f := &remoteConsumerIntegrationFixture{origin: newDNSPanelForTest(t), receiver: newDNSPanelForTest(t)}
	f.subscriptionID = seedSetupDNSOwner(t, f.origin)
	attachRemoteConsumerAgent(t, f.origin)
	f.origin.license = testPanelLicense(t, "active")
	f.receiver.license = testPanelLicense(t, "active")
	oldExchange, oldAuthority, oldPublish := remoteDNSExchange, remoteDNSReadLocalAuthority, remoteDNSPublishReceived
	t.Cleanup(func() {
		remoteDNSExchange, remoteDNSReadLocalAuthority, remoteDNSPublishReceived = oldExchange, oldAuthority, oldPublish
	})
	remoteDNSReadLocalAuthority = func(p *Panel, ctx context.Context) (remoteDNSAuthority, error) {
		if p != f.receiver {
			return remoteDNSAuthority{}, errors.New("unexpected local authority probe on hosting origin")
		}
		return remoteDNSAuthority{Ready: true, Engine: "bind", Epoch: 1, Nameservers: []string{"ns1.authority.test", "ns2.authority.test"}}, nil
	}
	remoteDNSPublishReceived = func(p *Panel, ctx context.Context, domain string, deleted bool) error {
		if p != f.receiver {
			return errors.New("hosting origin attempted local DNS publication")
		}
		f.published++
		return nil
	}
	stack := f.receiver.requireRemoteDNSMachineAuth(csrfProtect(f.receiver.requireAuth(http.HandlerFunc(f.receiver.handleRemoteDNSMachine))))
	remoteDNSExchange = func(ctx context.Context, endpoint, path, secret string, request, response any) error {
		if endpoint != "https://dns.authority.test:2083" {
			return errors.New("unexpected receiver endpoint")
		}
		raw, err := json.Marshal(request)
		if err != nil {
			return err
		}
		r := httptest.NewRequest(http.MethodPost, endpoint+path, bytes.NewReader(raw)).WithContext(ctx)
		r.Header.Set("Authorization", "Bearer "+secret)
		w := httptest.NewRecorder()
		stack.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			return fmt.Errorf("receiver status %d: %s", w.Code, w.Body.String())
		}
		if path == "/api/v1/dns/remote/receiver/publish" && f.losePublishResponse {
			f.losePublishResponse = false
			return errors.New("response lost after receiver committed its exact generation")
		}
		return json.Unmarshal(w.Body.Bytes(), response)
	}
	code := strings.Repeat("c", 64)
	if _, err := f.receiver.db.GetDB().Exec(`INSERT INTO remote_dns_enrollments(code_hash,expires_at) VALUES(?,?)`, remoteDNSHash(code), time.Now().Add(time.Minute).Unix()); err != nil {
		t.Fatal(err)
	}
	id, err := f.origin.prepareRemoteDNSConnection(context.Background(), "https://dns.authority.test:2083", code, "Hosting origin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.origin.resumeRemoteDNSConnection(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	f.connectionID = id
	if err = f.origin.saveSetupRemoteDNSConnection(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *remoteConsumerIntegrationFixture) create(t *testing.T, name, kind string) *httptest.ResponseRecorder {
	t.Helper()
	r := serviceOperationAdminRequest(t, http.MethodPost, "/api/v1/domains", fmt.Sprintf(`{"domain":%q,"project_type":%q,"subscription_id":%d}`, name, kind, f.subscriptionID), 1)
	w := httptest.NewRecorder()
	f.origin.handleCreateDomain(w, r)
	return w
}

func remoteConsumerDNSRequest(t *testing.T, p *Panel, domain, method, suffix, body string) *httptest.ResponseRecorder {
	t.Helper()
	var id int
	if err := p.db.GetDB().QueryRow(`SELECT id FROM domains WHERE name=?`, domain).Scan(&id); err != nil {
		t.Fatal(err)
	}
	r := serviceOperationAdminRequest(t, method, fmt.Sprintf("/api/v1/domains/%d/dns/%s", id, suffix), body, 1)
	w := httptest.NewRecorder()
	p.handleDomainDNS(w, r)
	return w
}

func TestRemoteDNSConsumerHostingCRUDAndChildStayOnReviewedAuthority(t *testing.T) {
	f := newRemoteConsumerIntegrationFixture(t)
	ctx := context.Background()
	const parent = "hosting.example.test"
	created := f.create(t, parent, "static")
	if created.Code != http.StatusOK {
		t.Fatalf("remote hosting creation: %d %s", created.Code, created.Body.String())
	}
	var localZones int
	if err := f.origin.db.GetDB().QueryRow(`SELECT count(*) FROM pdns_domains`).Scan(&localZones); err != nil || localZones != 0 {
		t.Fatalf("origin installed local authoritative zone: %d %v", localZones, err)
	}
	var mailRecords int
	if err := f.receiver.db.GetDB().QueryRow(`SELECT count(*) FROM pdns_records WHERE type='MX' OR name=?`, "mail."+parent).Scan(&mailRecords); err != nil || mailRecords != 0 {
		t.Fatalf("plain web creation advertised mail: %d %v", mailRecords, err)
	}
	if f.published != 1 {
		t.Fatalf("initial receiver publications=%d", f.published)
	}
	write := remoteConsumerDNSRequest(t, f.origin, parent, http.MethodPost, "records", `{"name":"@","type":"TXT","content":"verification-token=keep-me","ttl":3600}`)
	if write.Code != http.StatusOK {
		t.Fatalf("remote record create: %d %s", write.Code, write.Body.String())
	}
	read := remoteConsumerDNSRequest(t, f.origin, parent, http.MethodGet, "records", "")
	if read.Code != http.StatusOK || !strings.Contains(read.Body.String(), `"published":true`) || !strings.Contains(read.Body.String(), "verification-token=keep-me") {
		t.Fatalf("remote record read: %d %s", read.Code, read.Body.String())
	}
	second := strings.Repeat("b", 32)
	seedRemoteConsumerConnection(t, f.origin, second)
	if _, err := f.origin.db.GetDB().Exec(`UPDATE panel_settings SET value=? WHERE key=?`, second, settingRemoteDNSConnection); err != nil {
		t.Fatal(err)
	}
	child := f.create(t, "child."+parent, "static")
	if child.Code != http.StatusOK {
		t.Fatalf("child failed to inherit original authority after default changed: %d %s", child.Code, child.Body.String())
	}
	connection, err := f.origin.domainRemoteDNSConnectionID(ctx, "child."+parent)
	if err != nil || connection != f.connectionID {
		t.Fatalf("child connection=%q err=%v", connection, err)
	}
	var receiverZones int
	if err := f.receiver.db.GetDB().QueryRow(`SELECT count(*) FROM pdns_domains`).Scan(&receiverZones); err != nil || receiverZones != 1 {
		t.Fatalf("child acquired separate authority: %d %v", receiverZones, err)
	}
	dnsOnly := f.create(t, "dns."+parent, "dnsonly")
	if dnsOnly.Code != http.StatusConflict || !strings.Contains(dnsOnly.Body.String(), "DNS_ONLY_REQUIRES_LOCAL_AUTHORITY") {
		t.Fatalf("DNS-only inherited remote bypass: %d %s", dnsOnly.Code, dnsOnly.Body.String())
	}
	if err := f.origin.removeDomainDNSForDeletion(ctx, "child."+parent, parent); err != nil {
		t.Fatal(err)
	}
	remaining, err := f.origin.remoteDomainDNSRecords(ctx, parent)
	if err != nil {
		t.Fatal(err)
	}
	foundToken := false
	for _, record := range remaining {
		if remoteDNSNameWithin(record.Name, "child."+parent) {
			t.Fatal("deleted child records remain")
		}
		foundToken = foundToken || strings.Contains(record.Content, "verification-token=keep-me")
	}
	if !foundToken {
		t.Fatal("child deletion removed parent record")
	}
}

func TestRemoteDNSConsumerDeletionReconcilesLostExactReceipt(t *testing.T) {
	f := newRemoteConsumerIntegrationFixture(t)
	const domain = "delete.example.test"
	if w := f.create(t, domain, "static"); w.Code != http.StatusOK {
		t.Fatalf("create %d %s", w.Code, w.Body.String())
	}
	f.losePublishResponse = true
	if err := f.origin.removeDomainDNSForDeletion(context.Background(), domain, ""); err == nil {
		t.Fatal("lost receipt was incorrectly reported as applied")
	}
	var generation, applied int64
	var deleted bool
	if err := f.origin.db.GetDB().QueryRow(`SELECT z.generation,z.applied_generation,z.deleted FROM remote_dns_zones z JOIN domains d ON d.id=z.domain_id WHERE d.name=?`, domain).Scan(&generation, &applied, &deleted); err != nil || !deleted || generation == applied {
		t.Fatalf("pending delete lost its domain/receipt: %d/%d deleted=%v err=%v", generation, applied, deleted, err)
	}
	publications := f.published
	if err := f.origin.removeDomainDNSForDeletion(context.Background(), domain, ""); err != nil {
		t.Fatal(err)
	}
	if f.published != publications {
		t.Fatal("exact deletion replay mutated receiver a second time")
	}
	if err := f.origin.db.GetDB().QueryRow(`SELECT applied_generation FROM remote_dns_zones z JOIN domains d ON d.id=z.domain_id WHERE d.name=?`, domain).Scan(&applied); err != nil || applied != generation {
		t.Fatalf("exact receipt not reconciled: %d/%d %v", generation, applied, err)
	}
}

func TestRemoteDNSConsumerExplicitMailApplyPreservesVerificationAndRejectsRoutingConflict(t *testing.T) {
	f := newRemoteConsumerIntegrationFixture(t)
	const domain = "mail-ready.example.test"
	if w := f.create(t, domain, "static"); w.Code != http.StatusOK {
		t.Fatalf("create %d %s", w.Code, w.Body.String())
	}
	if w := remoteConsumerDNSRequest(t, f.origin, domain, http.MethodPost, "records", `{"name":"@","type":"TXT","content":"verification-token=keep-me","ttl":3600}`); w.Code != http.StatusOK {
		t.Fatal(w.Body.String())
	}
	apply := func() *httptest.ResponseRecorder {
		r := serviceOperationAdminRequest(t, http.MethodPost, "/api/v1/domains/"+domain+"/mail-auth/apply", `{"record":"spf"}`, 1)
		w := httptest.NewRecorder()
		f.origin.handleMailAuthApply(w, r, domain)
		return w
	}
	if w := apply(); w.Code != http.StatusOK {
		t.Fatalf("explicit mail SPF apply: %d %s", w.Code, w.Body.String())
	}
	records, err := f.origin.remoteDomainDNSRecords(context.Background(), domain)
	if err != nil {
		t.Fatal(err)
	}
	var token, spf, mx, mailAddress bool
	for _, record := range records {
		token = token || strings.Contains(record.Content, "verification-token=keep-me")
		spf = spf || strings.Contains(record.Content, "v=spf1")
		mx = mx || record.Type == "MX"
		mailAddress = mailAddress || (record.Name == "mail."+domain && (record.Type == "A" || record.Type == "AAAA"))
	}
	if !token || !spf || !mx || !mailAddress {
		t.Fatalf("mail activation lost token or prerequisites: token=%v spf=%v mx=%v address=%v records=%+v", token, spf, mx, mailAddress, records)
	}
	statusRequest := serviceOperationAdminRequest(t, http.MethodGet, "/api/v1/domains/1/mail/auth", "", 1)
	statusResponse := httptest.NewRecorder()
	f.origin.handleMailAuthStatus(statusResponse, statusRequest, domain)
	var status mailAuthStatus
	if err := json.Unmarshal(statusResponse.Body.Bytes(), &status); err != nil || statusResponse.Code != http.StatusOK || status.SPF.ZoneValue != spfRecommended() || status.SPF.Status != "pending" {
		t.Fatalf("preserved verification TXT obscured pending SPF: %d %s (%v)", statusResponse.Code, statusResponse.Body.String(), err)
	}

	if w := remoteConsumerDNSRequest(t, f.origin, domain, http.MethodPost, "records", `{"name":"@","type":"MX","content":"external-mail.example.test","ttl":3600,"prio":20}`); w.Code != http.StatusOK {
		t.Fatal(w.Body.String())
	}
	before, err := f.origin.remoteDomainDNSRecords(context.Background(), domain)
	if err != nil {
		t.Fatal(err)
	}
	beforeJSON, _ := json.Marshal(before)
	if w := apply(); w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "REMOTE_DNS_MAIL_RECORD_CONFLICT") {
		t.Fatalf("conflicting routing was overwritten: %d %s", w.Code, w.Body.String())
	}
	after, err := f.origin.remoteDomainDNSRecords(context.Background(), domain)
	if err != nil {
		t.Fatal(err)
	}
	afterJSON, _ := json.Marshal(after)
	if !bytes.Equal(beforeJSON, afterJSON) {
		t.Fatal("failed mail apply partially changed operator records")
	}
}

func (a *setupDNSHostingAgent) GetDKIMStatus(_ *transport.DKIMStatusRequest, out *transport.DKIMStatusResponse) error {
	*out = transport.DKIMStatusResponse{}
	return nil
}
