package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/rpc"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

type MailTLSInspectRPCRequest struct {
	CertPath string
	KeyPath  string
}

type MailTLSInspectRPCResponse struct {
	Valid        bool
	Trusted      bool
	TrustChecked bool
	TrustError   string
	DNSNames     []string
	Error        string
}

type MailTLSSNIRPCEntry struct {
	Names    []string
	CertPath string
	KeyPath  string
}

type MailTLSSecureRPCRequest struct {
	Myhostname string
	SNI        []MailTLSSNIRPCEntry
}

type MailTLSSecureRPCResponse struct {
	Configured bool
	SNICount   int
	Error      string
}

type mailTLSIsolationRPCAgent struct {
	mu                  sync.Mutex
	certificates        map[string]MailTLSInspectRPCResponse
	inspectCalls        []string
	secureCalls         [][]MailTLSSNIRPCEntry
	secureMailErrorOnce string
	applyVhostErrorOnce string
}

type MailTLSApplyVhostRPCRequest struct{}

type MailTLSApplyVhostRPCResponse struct {
	Error string
}

func (a *mailTLSIsolationRPCAgent) ApplyVhost(
	_ *MailTLSApplyVhostRPCRequest,
	resp *MailTLSApplyVhostRPCResponse,
) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	resp.Error = a.applyVhostErrorOnce
	a.applyVhostErrorOnce = ""
	return nil
}

func (a *mailTLSIsolationRPCAgent) InspectInstalledCertificate(
	req *MailTLSInspectRPCRequest,
	resp *MailTLSInspectRPCResponse,
) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.inspectCalls = append(a.inspectCalls, req.CertPath)
	info, ok := a.certificates[req.CertPath]
	if !ok {
		info = MailTLSInspectRPCResponse{Error: "certificate fixture is missing"}
	}
	info.DNSNames = append([]string(nil), info.DNSNames...)
	*resp = info
	return nil
}

func (a *mailTLSIsolationRPCAgent) SecureMailTLS(
	req *MailTLSSecureRPCRequest,
	resp *MailTLSSecureRPCResponse,
) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	snapshot := make([]MailTLSSNIRPCEntry, 0, len(req.SNI))
	for _, item := range req.SNI {
		item.Names = append([]string(nil), item.Names...)
		snapshot = append(snapshot, item)
	}
	a.secureCalls = append(a.secureCalls, snapshot)
	if a.secureMailErrorOnce != "" {
		resp.Error = a.secureMailErrorOnce
		a.secureMailErrorOnce = ""
		return nil
	}
	resp.Configured = true
	resp.SNICount = len(snapshot)
	return nil
}

func attachMailTLSIsolationAgent(t *testing.T, p *Panel, agent *mailTLSIsolationRPCAgent) {
	t.Helper()
	socketFile, err := os.CreateTemp("", "cp-mailtls-*.sock")
	if err != nil {
		t.Fatalf("reserve fake mail TLS agent socket path: %v", err)
	}
	socketPath := socketFile.Name()
	if err := socketFile.Close(); err != nil {
		t.Fatalf("close fake mail TLS socket placeholder: %v", err)
	}
	if err := os.Remove(socketPath); err != nil {
		t.Fatalf("remove fake mail TLS socket placeholder: %v", err)
	}
	listener, err := transport.ListenAgent(socketPath)
	if err != nil {
		t.Fatalf("listen for fake mail TLS agent: %v", err)
	}
	server := rpc.NewServer()
	if err := server.RegisterName("Agent", agent); err != nil {
		t.Fatalf("register fake mail TLS agent: %v", err)
	}
	go func() {
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			go server.ServeConn(conn)
		}
	}()
	connector := func(ctx context.Context) (*rpc.Client, error) {
		dialer := net.Dialer{}
		conn, dialErr := dialer.DialContext(ctx, "unix", socketPath)
		if dialErr != nil {
			return nil, dialErr
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			_ = conn.Close()
			return nil, ctxErr
		}
		return rpc.NewClient(conn), nil
	}
	rawClient, err := connector(context.Background())
	if err != nil {
		_ = listener.Close()
		t.Fatalf("connect fake mail TLS agent: %v", err)
	}
	p.agentClient = transport.NewReconnectingClientWithContextConnector(rawClient, connector)
	t.Cleanup(func() {
		_ = rawClient.Close()
		_ = listener.Close()
		_ = os.Remove(socketPath)
	})
}

func newMailTLSIsolationFixture(t *testing.T) (*Panel, int64) {
	t.Helper()
	p := newDNSPanelForTest(t)
	db := p.db.GetDB()
	userResult, err := db.Exec(`
		INSERT INTO users (username, password_hash, email, role)
		VALUES ('mail-tls-owner', 'hash', 'mail-tls-owner@example.test', 'customer')`)
	if err != nil {
		t.Fatalf("insert mail TLS owner: %v", err)
	}
	userID, err := userResult.LastInsertId()
	if err != nil {
		t.Fatalf("mail TLS owner id: %v", err)
	}
	subscriptionResult, err := db.Exec(`
		INSERT INTO subscriptions (owner_id, name)
		VALUES (?, 'Mail TLS isolation')`, userID)
	if err != nil {
		t.Fatalf("insert mail TLS subscription: %v", err)
	}
	subscriptionID, err := subscriptionResult.LastInsertId()
	if err != nil {
		t.Fatalf("mail TLS subscription id: %v", err)
	}
	return p, subscriptionID
}

func addMailTLSIsolationDomain(
	t *testing.T,
	p *Panel,
	subscriptionID int64,
	name string,
	certPath string,
	status string,
	secureMail bool,
) int {
	t.Helper()
	domainResult, err := p.db.GetDB().Exec(`
		INSERT INTO domains (subscription_id, name) VALUES (?, ?)`, subscriptionID, name)
	if err != nil {
		t.Fatalf("insert domain %s: %v", name, err)
	}
	domainID64, err := domainResult.LastInsertId()
	if err != nil {
		t.Fatalf("domain %s id: %v", name, err)
	}
	if _, err := p.db.GetDB().Exec(`
		INSERT INTO ssl_certificates (
			domain_id, type, cert_path, key_path, chain_path,
			issuer, subject, issued_at, expires_at,
			auto_renew, secure_mail, status
		) VALUES (?, 'custom', ?, ?, '', 'Test CA', ?,
		          '2026-01-01T00:00:00Z', '2027-01-01T00:00:00Z',
		          false, ?, ?)`,
		domainID64, certPath, certPath+".key", name, secureMail, status); err != nil {
		t.Fatalf("insert certificate for %s: %v", name, err)
	}
	return int(domainID64)
}

func validMailTLSCertificate(name string) MailTLSInspectRPCResponse {
	return MailTLSInspectRPCResponse{
		Valid:        true,
		Trusted:      true,
		TrustChecked: true,
		DNSNames:     []string{name, "mail." + name},
	}
}

func mailTLSSnapshotPaths(agent *mailTLSIsolationRPCAgent) []string {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if len(agent.secureCalls) == 0 {
		return nil
	}
	last := agent.secureCalls[len(agent.secureCalls)-1]
	paths := make([]string, 0, len(last))
	for _, item := range last {
		paths = append(paths, item.CertPath)
	}
	sort.Strings(paths)
	return paths
}

func TestSyncCertificateDependentsWebOnlyTargetSkipsGlobalMailTLS(t *testing.T) {
	p, subscriptionID := newMailTLSIsolationFixture(t)
	targetID := addMailTLSIsolationDomain(
		t, p, subscriptionID, "web-only.example", "/certs/web-only", "active", false,
	)
	addMailTLSIsolationDomain(
		t, p, subscriptionID, "broken-mail.example", "/certs/broken-mail", "active", true,
	)
	agent := &mailTLSIsolationRPCAgent{certificates: map[string]MailTLSInspectRPCResponse{
		"/certs/broken-mail": {Error: "broken unrelated certificate"},
	}}
	attachMailTLSIsolationAgent(t, p, agent)

	if err := p.syncCertificateDependents(context.Background(), targetID); err != nil {
		t.Fatalf("web-only target was blocked by unrelated mail tenant: %v", err)
	}
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if len(agent.inspectCalls) != 0 || len(agent.secureCalls) != 0 {
		t.Fatalf("web-only target triggered global mail reconcile: inspect=%v secure=%v",
			agent.inspectCalls, agent.secureCalls)
	}
}

func TestSyncCertificateDependentsStrictTargetOmitsBrokenUnrelatedTenant(t *testing.T) {
	p, subscriptionID := newMailTLSIsolationFixture(t)
	targetID := addMailTLSIsolationDomain(
		t, p, subscriptionID, "target.example", "/certs/target", "active", true,
	)
	addMailTLSIsolationDomain(
		t, p, subscriptionID, "healthy-peer.example", "/certs/healthy-peer", "active", true,
	)
	addMailTLSIsolationDomain(
		t, p, subscriptionID, "broken-peer.example", "/certs/broken-peer", "active", true,
	)
	agent := &mailTLSIsolationRPCAgent{certificates: map[string]MailTLSInspectRPCResponse{
		"/certs/target":       validMailTLSCertificate("target.example"),
		"/certs/healthy-peer": validMailTLSCertificate("healthy-peer.example"),
		"/certs/broken-peer":  {Error: "expired unrelated certificate"},
	}}
	attachMailTLSIsolationAgent(t, p, agent)

	if err := p.syncCertificateDependents(context.Background(), targetID); err != nil {
		t.Fatalf("strict target was blocked by unrelated tenant: %v", err)
	}
	want := []string{"/certs/healthy-peer", "/certs/target"}
	if got := mailTLSSnapshotPaths(agent); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("full safe snapshot paths = %v, want %v", got, want)
	}
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if len(agent.secureCalls) != 1 {
		t.Fatalf("SecureMailTLS calls = %d, want 1", len(agent.secureCalls))
	}
}

func TestSyncCertificateDependentsRejectsBrokenStrictTarget(t *testing.T) {
	p, subscriptionID := newMailTLSIsolationFixture(t)
	targetID := addMailTLSIsolationDomain(
		t, p, subscriptionID, "broken-target.example", "/certs/broken-target", "active", true,
	)
	addMailTLSIsolationDomain(
		t, p, subscriptionID, "healthy-other.example", "/certs/healthy-other", "active", true,
	)
	agent := &mailTLSIsolationRPCAgent{certificates: map[string]MailTLSInspectRPCResponse{
		"/certs/broken-target": {Error: "target certificate is invalid"},
		"/certs/healthy-other": validMailTLSCertificate("healthy-other.example"),
	}}
	attachMailTLSIsolationAgent(t, p, agent)

	err := p.syncCertificateDependents(context.Background(), targetID)
	if err == nil || !strings.Contains(err.Error(), "broken-target.example") {
		t.Fatalf("broken strict target error = %v", err)
	}
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if len(agent.secureCalls) != 0 {
		t.Fatalf("broken strict target published a snapshot: %v", agent.secureCalls)
	}
}

func TestSyncCertificateDependentsHistoricalSecureMailTriggersCleanup(t *testing.T) {
	p, subscriptionID := newMailTLSIsolationFixture(t)
	targetID := addMailTLSIsolationDomain(
		t, p, subscriptionID, "detached.example", "/certs/detached", "revoked", true,
	)
	addMailTLSIsolationDomain(
		t, p, subscriptionID, "remaining.example", "/certs/remaining", "active", true,
	)
	agent := &mailTLSIsolationRPCAgent{certificates: map[string]MailTLSInspectRPCResponse{
		"/certs/remaining": validMailTLSCertificate("remaining.example"),
	}}
	attachMailTLSIsolationAgent(t, p, agent)

	if err := p.syncCertificateDependents(context.Background(), targetID); err != nil {
		t.Fatalf("historical secure-mail cleanup: %v", err)
	}
	want := []string{"/certs/remaining"}
	if got := mailTLSSnapshotPaths(agent); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("cleanup snapshot paths = %v, want %v", got, want)
	}
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if len(agent.inspectCalls) != 1 || agent.inspectCalls[0] != "/certs/remaining" {
		t.Fatalf("cleanup inspected unexpected certificates: %v", agent.inspectCalls)
	}
}

func TestDeleteDomainRemovesSecureMailFromPublishedSNISnapshot(t *testing.T) {
	p, subscriptionID := newMailTLSIsolationFixture(t)
	targetID := addMailTLSIsolationDomain(
		t, p, subscriptionID, "delete-mail.example", "/certs/delete-mail", "active", true,
	)
	addMailTLSIsolationDomain(
		t, p, subscriptionID, "remaining-mail.example", "/certs/remaining-mail", "active", true,
	)
	agent := &mailTLSIsolationRPCAgent{certificates: map[string]MailTLSInspectRPCResponse{
		"/certs/delete-mail":    validMailTLSCertificate("delete-mail.example"),
		"/certs/remaining-mail": validMailTLSCertificate("remaining-mail.example"),
	}}
	attachMailTLSIsolationAgent(t, p, agent)

	request := httptest.NewRequest(
		http.MethodDelete,
		fmt.Sprintf("/api/v1/domains/%d", targetID),
		nil,
	)
	recorder := httptest.NewRecorder()
	p.handleDeleteDomain(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("delete domain status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	var domainCount int
	if err := p.db.GetDB().QueryRow(
		`SELECT COUNT(*) FROM domains WHERE id = ?`, targetID,
	).Scan(&domainCount); err != nil {
		t.Fatalf("count deleted domain: %v", err)
	}
	if domainCount != 0 {
		t.Fatalf("deleted domain count = %d, want 0", domainCount)
	}
	want := []string{"/certs/remaining-mail"}
	if got := mailTLSSnapshotPaths(agent); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("post-delete mail SNI snapshot = %v, want %v", got, want)
	}
}

func TestDeleteDomainAbortsAndRestoresSecureMailWhenSNICleanupFails(t *testing.T) {
	p, subscriptionID := newMailTLSIsolationFixture(t)
	targetID := addMailTLSIsolationDomain(
		t, p, subscriptionID, "keep-mail.example", "/certs/keep-mail", "active", true,
	)
	addMailTLSIsolationDomain(
		t, p, subscriptionID, "other-mail.example", "/certs/other-mail", "active", true,
	)
	agent := &mailTLSIsolationRPCAgent{
		certificates: map[string]MailTLSInspectRPCResponse{
			"/certs/keep-mail":  validMailTLSCertificate("keep-mail.example"),
			"/certs/other-mail": validMailTLSCertificate("other-mail.example"),
		},
		secureMailErrorOnce: "forced SNI publication failure",
	}
	attachMailTLSIsolationAgent(t, p, agent)

	request := httptest.NewRequest(
		http.MethodDelete,
		fmt.Sprintf("/api/v1/domains/%d", targetID),
		nil,
	)
	recorder := httptest.NewRecorder()
	p.handleDeleteDomain(recorder, request)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("failed delete status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	var domainCount, secureMail int
	if err := p.db.GetDB().QueryRow(`
		SELECT COUNT(*), COALESCE(MAX(sc.secure_mail), 0)
		FROM domains d
		LEFT JOIN ssl_certificates sc ON sc.domain_id = d.id AND sc.status = 'active'
		WHERE d.id = ?`, targetID).
		Scan(&domainCount, &secureMail); err != nil {
		t.Fatalf("read aborted delete state: %v", err)
	}
	if domainCount != 1 || secureMail != 1 {
		t.Fatalf("aborted delete state = domains:%d secure_mail:%d, want 1/1", domainCount, secureMail)
	}
	want := []string{"/certs/keep-mail", "/certs/other-mail"}
	if got := mailTLSSnapshotPaths(agent); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("rolled-back mail SNI snapshot = %v, want %v", got, want)
	}
}
