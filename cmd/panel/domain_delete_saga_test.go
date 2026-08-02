package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/rpc"
	"sync"
	"testing"

	"github.com/alicelik/celikpanel/internal/hostingpath"
	"github.com/alicelik/celikpanel/internal/transport"
)

type domainDeletionRPCAgent struct {
	durableMutationRPCFixture

	callsMu sync.Mutex
	commit  string

	deleteSiteCalls int
	syncRequests    []transport.SyncDNSZoneRequest
	certRequests    []transport.DeleteCertLineageRequest

	syncErrorsRemaining int
	certErrorsRemaining int
}

func (a *domainDeletionRPCAgent) BeginServiceMutation(
	req *ServiceOperationMutationBeginRequest,
	resp *ServiceOperationMutationResponse,
) error {
	return a.durableMutationRPCFixture.BeginServiceMutation(req, resp)
}

func (a *domainDeletionRPCAgent) HeartbeatServiceMutation(
	req *ServiceOperationMutationHeartbeatRequest,
	resp *ServiceOperationMutationResponse,
) error {
	return a.durableMutationRPCFixture.HeartbeatServiceMutation(req, resp)
}

func (a *domainDeletionRPCAgent) FinishServiceMutation(
	req *ServiceOperationMutationFinishRequest,
	resp *ServiceOperationMutationResponse,
) error {
	return a.durableMutationRPCFixture.FinishServiceMutation(req, resp)
}

func (a *domainDeletionRPCAgent) Version(
	_ *transport.Empty,
	resp *transport.AgentVersionResponse,
) error {
	resp.Commit = a.commit
	return nil
}

func (a *domainDeletionRPCAgent) DeleteSite(
	_ *transport.DeleteSiteRequest,
	resp *transport.DeleteSiteResponse,
) error {
	a.callsMu.Lock()
	a.deleteSiteCalls++
	a.callsMu.Unlock()
	resp.Success = true
	return nil
}

func (a *domainDeletionRPCAgent) SyncDNSZone(
	req *transport.SyncDNSZoneRequest,
	resp *transport.SyncDNSZoneResponse,
) error {
	a.callsMu.Lock()
	a.syncRequests = append(a.syncRequests, *req)
	fail := a.syncErrorsRemaining > 0
	if fail {
		a.syncErrorsRemaining--
	}
	a.callsMu.Unlock()
	if fail {
		resp.Error = "forced DNS deletion failure"
		return nil
	}
	resp.Synced = true
	return nil
}

func (a *domainDeletionRPCAgent) DeleteCertLineage(
	req *transport.DeleteCertLineageRequest,
	resp *transport.DeleteCertLineageResponse,
) error {
	a.callsMu.Lock()
	copied := *req
	copied.LineageNames = append([]string(nil), req.LineageNames...)
	a.certRequests = append(a.certRequests, copied)
	fail := a.certErrorsRemaining > 0
	if fail {
		a.certErrorsRemaining--
	}
	a.callsMu.Unlock()
	if fail {
		resp.Error = "forced certificate deletion failure"
		return nil
	}
	resp.Deleted = true
	return nil
}

func attachDomainDeletionRPCAgent(
	t *testing.T,
	p *Panel,
	agent *domainDeletionRPCAgent,
) {
	t.Helper()
	server := rpc.NewServer()
	if err := server.RegisterName("Agent", agent); err != nil {
		t.Fatalf("register domain deletion agent: %v", err)
	}
	connector := func(ctx context.Context) (*rpc.Client, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		serverConn, clientConn := net.Pipe()
		go server.ServeConn(serverConn)
		return rpc.NewClient(clientConn), nil
	}
	rawClient, err := connector(context.Background())
	if err != nil {
		t.Fatalf("connect domain deletion agent: %v", err)
	}
	p.agentClient = transport.NewReconnectingClientWithContextConnector(
		rawClient,
		connector,
	)
	t.Cleanup(func() { _ = rawClient.Close() })
}

func seedDomainDeletionLedger(
	t *testing.T,
	p *Panel,
	domain string,
	projectType string,
) (int, int) {
	t.Helper()
	db := p.db.GetDB()
	userResult, err := db.Exec(`
		INSERT INTO users (username, password_hash, email, role)
		VALUES ('delete-owner', 'hash', 'delete-owner@example.test', 'customer')`)
	if err != nil {
		t.Fatalf("insert deletion owner: %v", err)
	}
	userID, err := userResult.LastInsertId()
	if err != nil {
		t.Fatalf("deletion owner id: %v", err)
	}
	subscriptionResult, err := db.Exec(`
		INSERT INTO subscriptions (owner_id, name)
		VALUES (?, 'Domain deletion saga')`, userID)
	if err != nil {
		t.Fatalf("insert deletion subscription: %v", err)
	}
	subscriptionID64, err := subscriptionResult.LastInsertId()
	if err != nil {
		t.Fatalf("deletion subscription id: %v", err)
	}
	domainResult, err := db.Exec(`
		INSERT INTO domains (subscription_id, name, status)
		VALUES (?, ?, 'active')`, subscriptionID64, domain)
	if err != nil {
		t.Fatalf("insert deletion domain: %v", err)
	}
	domainID64, err := domainResult.LastInsertId()
	if err != nil {
		t.Fatalf("deletion domain id: %v", err)
	}
	domainID := int(domainID64)
	subscriptionID := int(subscriptionID64)
	if projectType != "dnsonly" {
		documentRoot, err := hostingpath.DocumentRoot(subscriptionID, domainID)
		if err != nil {
			t.Fatalf("derive deletion document root: %v", err)
		}
		if _, err := db.Exec(`
			INSERT INTO sites (
				domain_id, document_root, project_type, php_version, status
			) VALUES (?, ?, ?, '', 'active')`,
			domainID,
			documentRoot,
			projectType,
		); err != nil {
			t.Fatalf("insert deletion site: %v", err)
		}
	}
	return domainID, subscriptionID
}

func deleteDomainForSagaTest(t *testing.T, p *Panel, domainID int) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(
		http.MethodDelete,
		fmt.Sprintf("/api/v1/domains/%d", domainID),
		nil,
	)
	recorder := httptest.NewRecorder()
	p.handleDeleteDomain(recorder, request)
	return recorder
}

func requireDeletionPendingStage(
	t *testing.T,
	recorder *httptest.ResponseRecorder,
	stage string,
) {
	t.Helper()
	if recorder.Code != http.StatusAccepted {
		t.Fatalf(
			"delete status = %d, want 202; body=%s",
			recorder.Code,
			recorder.Body.String(),
		)
	}
	var response struct {
		Status string `json:"status"`
		Stage  string `json:"stage"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode pending deletion response: %v", err)
	}
	if response.Status != domainDeletionPendingStatus || response.Stage != stage {
		t.Fatalf("pending deletion response = %+v, want status=%q stage=%q",
			response, domainDeletionPendingStatus, stage)
	}
}

func TestDeleteDomainFinalLedgerFailureIsDurableAndRetryable(t *testing.T) {
	p := newDNSPanelForTest(t)
	domainID, _ := seedDomainDeletionLedger(
		t,
		p,
		"ledger-retry.example",
		"static",
	)
	withPanelBuildCommit(t, "domain-delete-test")
	agent := &domainDeletionRPCAgent{commit: "domain-delete-test"}
	attachDomainDeletionRPCAgent(t, p, agent)

	if _, err := p.db.GetDB().Exec(fmt.Sprintf(`
		CREATE TRIGGER reject_domain_delete
		BEFORE DELETE ON domains
		WHEN OLD.id = %d
		BEGIN
			SELECT RAISE(ABORT, 'forced domain delete failure');
		END`, domainID)); err != nil {
		t.Fatalf("create domain delete failure trigger: %v", err)
	}

	first := deleteDomainForSagaTest(t, p, domainID)
	requireDeletionPendingStage(t, first, "ledger_delete")

	var status string
	if err := p.db.GetDB().QueryRow(
		`SELECT status FROM domains WHERE id = ?`, domainID,
	).Scan(&status); err != nil {
		t.Fatalf("read pending domain status: %v", err)
	}
	if status != domainDeletionLedgerStatus {
		t.Fatalf("domain status = %q, want %q", status, domainDeletionLedgerStatus)
	}
	var siteCount int
	if err := p.db.GetDB().QueryRow(
		`SELECT COUNT(*) FROM sites WHERE domain_id = ?`, domainID,
	).Scan(&siteCount); err != nil {
		t.Fatalf("count retained site ledger: %v", err)
	}
	if siteCount != 1 {
		t.Fatalf("retained site rows = %d, want 1", siteCount)
	}

	if _, err := p.db.GetDB().Exec(`DROP TRIGGER reject_domain_delete`); err != nil {
		t.Fatalf("drop domain delete failure trigger: %v", err)
	}
	second := deleteDomainForSagaTest(t, p, domainID)
	if second.Code != http.StatusOK {
		t.Fatalf("retry status = %d, want 200; body=%s",
			second.Code, second.Body.String())
	}
	var domainCount int
	if err := p.db.GetDB().QueryRow(
		`SELECT COUNT(*) FROM domains WHERE id = ?`, domainID,
	).Scan(&domainCount); err != nil {
		t.Fatalf("count deleted domain: %v", err)
	}
	if domainCount != 0 {
		t.Fatalf("domain rows after retry = %d, want 0", domainCount)
	}
	agent.callsMu.Lock()
	deleteSiteCalls := agent.deleteSiteCalls
	agent.callsMu.Unlock()
	if deleteSiteCalls != 2 {
		t.Fatalf("idempotent site cleanup calls = %d, want 2", deleteSiteCalls)
	}
}

func TestDeleteDomainDNSFailureRetainsZoneAndRetryHandle(t *testing.T) {
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "standalone")
	domain := "dns-retry.example"
	domainID, _ := seedDomainDeletionLedger(t, p, domain, "dnsonly")
	seedStrictDNSZone(t, p, domain)
	agent := &domainDeletionRPCAgent{syncErrorsRemaining: 1}
	attachDomainDeletionRPCAgent(t, p, agent)

	first := deleteDomainForSagaTest(t, p, domainID)
	requireDeletionPendingStage(t, first, "dns_cleanup")

	var domainStatus string
	if err := p.db.GetDB().QueryRow(
		`SELECT status FROM domains WHERE id = ?`, domainID,
	).Scan(&domainStatus); err != nil {
		t.Fatalf("read retained domain: %v", err)
	}
	if domainStatus != domainDeletionLedgerStatus {
		t.Fatalf("retained domain status = %q, want %q",
			domainStatus, domainDeletionLedgerStatus)
	}
	var zoneCount int
	if err := p.db.GetDB().QueryRow(
		`SELECT COUNT(*) FROM pdns_domains WHERE name = ?`, domain,
	).Scan(&zoneCount); err != nil {
		t.Fatalf("count retained DNS zone: %v", err)
	}
	if zoneCount != 1 {
		t.Fatalf("DNS zone rows after failed publication = %d, want 1", zoneCount)
	}

	second := deleteDomainForSagaTest(t, p, domainID)
	if second.Code != http.StatusOK {
		t.Fatalf("DNS retry status = %d, want 200; body=%s",
			second.Code, second.Body.String())
	}
	agent.callsMu.Lock()
	requests := append([]transport.SyncDNSZoneRequest(nil), agent.syncRequests...)
	agent.callsMu.Unlock()
	if len(requests) != 2 || !requests[0].Delete || !requests[1].Delete {
		t.Fatalf("DNS deletion requests = %+v, want two delete publications", requests)
	}
	if err := p.db.GetDB().QueryRow(
		`SELECT COUNT(*) FROM pdns_domains WHERE name = ?`, domain,
	).Scan(&zoneCount); err != nil {
		t.Fatalf("count removed DNS zone: %v", err)
	}
	if zoneCount != 0 {
		t.Fatalf("DNS zone rows after retry = %d, want 0", zoneCount)
	}
}

func TestDeleteDomainCertificateFailureRetainsLedgerForRetry(t *testing.T) {
	p := newDNSPanelForTest(t)
	domain := "certificate-retry.example"
	domainID, _ := seedDomainDeletionLedger(t, p, domain, "dnsonly")
	if _, err := p.db.GetDB().Exec(`
		INSERT INTO ssl_certificates (
			domain_id, type, cert_path, key_path, chain_path,
			issuer, subject, issued_at, expires_at,
			auto_renew, secure_mail, status, lineage_name
		) VALUES (
			?, 'letsencrypt', '/certs/cert.pem', '/certs/key.pem', '',
			'Test CA', ?, '2026-01-01T00:00:00Z', '2027-01-01T00:00:00Z',
			1, 0, 'active', ?
		)`, domainID, domain, domain); err != nil {
		t.Fatalf("insert managed certificate: %v", err)
	}
	agent := &domainDeletionRPCAgent{certErrorsRemaining: 1}
	attachDomainDeletionRPCAgent(t, p, agent)

	first := deleteDomainForSagaTest(t, p, domainID)
	requireDeletionPendingStage(t, first, "certificate_cleanup")

	var certCount int
	if err := p.db.GetDB().QueryRow(
		`SELECT COUNT(*) FROM ssl_certificates WHERE domain_id = ?`,
		domainID,
	).Scan(&certCount); err != nil {
		t.Fatalf("count retained certificate: %v", err)
	}
	if certCount != 1 {
		t.Fatalf("certificate rows after failed cleanup = %d, want 1", certCount)
	}

	second := deleteDomainForSagaTest(t, p, domainID)
	if second.Code != http.StatusOK {
		t.Fatalf("certificate retry status = %d, want 200; body=%s",
			second.Code, second.Body.String())
	}
	agent.callsMu.Lock()
	requests := append(
		[]transport.DeleteCertLineageRequest(nil),
		agent.certRequests...,
	)
	agent.callsMu.Unlock()
	if len(requests) != 2 {
		t.Fatalf("certificate cleanup calls = %d, want 2", len(requests))
	}
	for _, request := range requests {
		if request.Domain != domain || !request.DeleteCanonical {
			t.Fatalf("certificate cleanup request = %+v", request)
		}
	}
	if err := p.db.GetDB().QueryRow(
		`SELECT COUNT(*) FROM domains WHERE id = ?`, domainID,
	).Scan(&certCount); err != nil {
		t.Fatalf("count deleted certificate domain: %v", err)
	}
	if certCount != 0 {
		t.Fatalf("domain rows after certificate retry = %d, want 0", certCount)
	}
}

func TestDeleteSubdomainDNSPublicationFailureIsDurableAndRetryable(t *testing.T) {
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "standalone")
	parentID, subscriptionID := seedDomainDeletionLedger(
		t,
		p,
		"example.test",
		"dnsonly",
	)
	seedStrictDNSZone(t, p, "example.test")

	childResult, err := p.db.GetDB().Exec(`
		INSERT INTO domains (subscription_id, name, parent_domain_id, status)
		VALUES (?, 'api.example.test', ?, 'active')`,
		subscriptionID,
		parentID,
	)
	if err != nil {
		t.Fatalf("insert child domain: %v", err)
	}
	childID64, err := childResult.LastInsertId()
	if err != nil {
		t.Fatalf("child domain id: %v", err)
	}
	childID := int(childID64)
	zoneID := strictDNSZoneID(t, p, "example.test")
	if _, err := p.db.GetDB().Exec(`
		INSERT INTO pdns_records (domain_id, name, type, content, ttl)
		VALUES (?, 'api.example.test', 'A', '2.25.80.4', 3600)`, zoneID); err != nil {
		t.Fatalf("insert child DNS record: %v", err)
	}

	agent := &domainDeletionRPCAgent{syncErrorsRemaining: 1}
	attachDomainDeletionRPCAgent(t, p, agent)

	first := deleteDomainForSagaTest(t, p, childID)
	requireDeletionPendingStage(t, first, "dns_cleanup")
	assertSubdomainRecordCount(
		t,
		p,
		"example.test",
		"api.example.test",
		"A",
		"2.25.80.4",
		0,
	)
	var status string
	if err := p.db.GetDB().QueryRow(
		`SELECT status FROM domains WHERE id = ?`,
		childID,
	).Scan(&status); err != nil {
		t.Fatalf("read pending child status: %v", err)
	}
	if status != domainDeletionLedgerStatus {
		t.Fatalf("child status = %q, want %q", status, domainDeletionLedgerStatus)
	}

	second := deleteDomainForSagaTest(t, p, childID)
	if second.Code != http.StatusOK {
		t.Fatalf("child DNS retry status = %d, want 200; body=%s",
			second.Code, second.Body.String())
	}
	var remaining int
	if err := p.db.GetDB().QueryRow(
		`SELECT COUNT(*) FROM domains WHERE id = ?`,
		childID,
	).Scan(&remaining); err != nil {
		t.Fatalf("count deleted child domain: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("child domain rows after retry = %d, want 0", remaining)
	}

	agent.callsMu.Lock()
	syncRequests := append([]transport.SyncDNSZoneRequest(nil), agent.syncRequests...)
	agent.callsMu.Unlock()
	if len(syncRequests) != 2 {
		t.Fatalf("parent DNS publication attempts = %d, want 2", len(syncRequests))
	}
	for _, request := range syncRequests {
		if request.Domain != "example.test" || request.Delete {
			t.Fatalf("parent DNS publication request = %+v", request)
		}
	}
}
