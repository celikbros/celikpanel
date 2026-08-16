package main

import (
	"context"
	"crypto/sha256"
	"errors"
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
	"time"

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
	Configured  bool
	DefaultCert string
	SNICount    int
	Error       string
}

type mailTLSIsolationRPCAgent struct {
	*serviceOperationTestAgent

	mu                  sync.Mutex
	certificates        map[string]MailTLSInspectRPCResponse
	inspectErrors       map[string]string
	inspectCalls        []string
	secureCalls         [][]MailTLSSNIRPCEntry
	secureMailErrorOnce string
	syncCalls           []transport.SyncMailTLSV2Request
	reconcileResponse   *transport.SecureMailTLSResponse
	reconcileRPCError   string
	terminalizeSync     bool
	cancelMailAsSuccess bool
	leaveMailIntent     bool
	finishMailLoss      bool
	applyVhostErrorOnce string
	syncStarted         chan struct{}
	releaseSync         <-chan struct{}
	syncStartOnce       sync.Once
	syncHook            func(int, *transport.SyncMailTLSV2Request, *transport.SecureMailTLSResponse) (bool, error)
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
	fixturePath := a.fixturePathLocked(req.CertPath)
	a.inspectCalls = append(a.inspectCalls, fixturePath)
	if message := a.inspectErrors[fixturePath]; message != "" {
		return fmt.Errorf("%s", message)
	}
	info, ok := a.certificates[fixturePath]
	if !ok {
		info = MailTLSInspectRPCResponse{Error: "certificate fixture is missing"}
	}
	info.DNSNames = append([]string(nil), info.DNSNames...)
	*resp = info
	return nil
}

func (a *mailTLSIsolationRPCAgent) fixturePathLocked(canonicalPath string) string {
	if _, ok := a.certificates[canonicalPath]; ok {
		return canonicalPath
	}
	if _, ok := a.inspectErrors[canonicalPath]; ok {
		return canonicalPath
	}
	relative := strings.TrimPrefix(canonicalPath, panelMailTLSManagedRoot+"/")
	domain := strings.SplitN(relative, "/", 2)[0]
	label := strings.SplitN(domain, ".", 2)[0]
	for candidate := range a.certificates {
		if strings.TrimSuffix(candidate[strings.LastIndex(candidate, "/")+1:], ".pem") == label {
			return candidate
		}
	}
	for candidate := range a.inspectErrors {
		if strings.TrimSuffix(candidate[strings.LastIndex(candidate, "/")+1:], ".pem") == label {
			return candidate
		}
	}
	return canonicalPath
}

func (a *mailTLSIsolationRPCAgent) SyncMailTLSV2(
	req *transport.SyncMailTLSV2Request,
	resp *transport.SecureMailTLSResponse,
) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	cloned := *req
	cloned.SNI = make([]transport.MailSNIEntry, 0, len(req.SNI))
	for _, item := range req.SNI {
		item.Names = append([]string(nil), item.Names...)
		cloned.SNI = append(cloned.SNI, item)
	}
	a.syncCalls = append(a.syncCalls, cloned)
	callIndex := len(a.syncCalls)
	if a.syncHook != nil {
		handled, err := a.syncHook(callIndex, req, resp)
		if handled || err != nil {
			return err
		}
	}
	if a.syncStarted != nil {
		a.syncStartOnce.Do(func() { close(a.syncStarted) })
	}
	if a.releaseSync != nil {
		<-a.releaseSync
	}
	snapshot := make([]MailTLSSNIRPCEntry, 0, len(req.SNI))
	for _, item := range req.SNI {
		snapshot = append(snapshot, MailTLSSNIRPCEntry{
			Names: append([]string(nil), item.Names...), CertPath: item.CertPath, KeyPath: item.KeyPath,
		})
	}
	a.secureCalls = append(a.secureCalls, snapshot)
	if a.secureMailErrorOnce != "" {
		resp.Error = a.secureMailErrorOnce
		a.secureMailErrorOnce = ""
		return nil
	}
	if a.reconcileRPCError != "" {
		a.markMailTLSSyncIntentLocked(req)
		return fmt.Errorf("%s", a.reconcileRPCError)
	}
	if a.reconcileResponse != nil {
		*resp = *a.reconcileResponse
		a.markMailTLSSyncTerminalLocked(req)
		return nil
	}
	resp.Configured = true
	resp.DefaultCert = transport.DefaultMailTLSCertificatePath
	resp.SNICount = len(req.SNI)
	a.markMailTLSSyncTerminalLocked(req)
	return nil
}

func (a *mailTLSIsolationRPCAgent) markMailTLSSyncIntentLocked(
	req *transport.SyncMailTLSV2Request,
) {
	if !a.leaveMailIntent {
		return
	}
	a.serviceOperationTestAgent.mu.Lock()
	defer a.serviceOperationTestAgent.mu.Unlock()
	job := a.mutationJobs[req.MutationRequestID]
	if job == nil || job.OwnerID != req.MutationOwnerID {
		return
	}
	job.Phase = "commit/mail-tls-sync/v1/intent/" +
		req.MutationRequestID + "/" + job.PackageName
}

func (a *mailTLSIsolationRPCAgent) markMailTLSSyncTerminalLocked(
	req *transport.SyncMailTLSV2Request,
) {
	if !a.terminalizeSync {
		return
	}
	a.serviceOperationTestAgent.mu.Lock()
	defer a.serviceOperationTestAgent.mu.Unlock()
	job := a.mutationJobs[req.MutationRequestID]
	if job == nil || job.OwnerID != req.MutationOwnerID {
		return
	}
	job.Status = agentMutationSucceeded
	job.Phase = "commit/mail-tls-sync/v1/published/" +
		req.MutationRequestID + "/" + job.PackageName
	if a.mutationActive == req.MutationRequestID {
		a.mutationActive = ""
	}
}

func (a *mailTLSIsolationRPCAgent) FinishServiceMutation(
	req *ServiceOperationMutationFinishRequest,
	resp *ServiceOperationMutationResponse,
) error {
	a.serviceOperationTestAgent.mu.Lock()
	job := a.mutationJobs[req.RequestID]
	if a.finishMailLoss && job != nil && job.OwnerID == req.OwnerID &&
		job.Kind == "mail_tls_sync" && agentMutationActive(job.Status) {
		resp.Job = cloneServiceOperationMutationJob(job)
		a.serviceOperationTestAgent.mu.Unlock()
		return errors.New("simulated FinishServiceMutation response loss during Mail TLS intent")
	}
	if job != nil && job.OwnerID == req.OwnerID &&
		job.Status == agentMutationSucceeded {
		resp.Job = cloneServiceOperationMutationJob(job)
		a.serviceOperationTestAgent.mu.Unlock()
		return nil
	}
	a.serviceOperationTestAgent.mu.Unlock()
	return a.serviceOperationTestAgent.FinishServiceMutation(req, resp)
}

func (a *mailTLSIsolationRPCAgent) CancelServiceMutation(
	req *ServiceOperationMutationCancelRequest,
	resp *ServiceOperationMutationResponse,
) error {
	if !a.cancelMailAsSuccess {
		return a.serviceOperationTestAgent.CancelServiceMutation(req, resp)
	}
	a.serviceOperationTestAgent.mu.Lock()
	defer a.serviceOperationTestAgent.mu.Unlock()
	job := a.mutationJobs[req.RequestID]
	if job == nil || job.OwnerID != req.ExpectedOwner {
		resp.Error = "service mutation owner mismatch"
		resp.Job = cloneServiceOperationMutationJob(job)
		return nil
	}
	job.Status = agentMutationSucceeded
	job.Phase = "commit/mail-tls-sync/v1/published/" +
		job.RequestID + "/" + job.PackageName
	if a.mutationActive == req.RequestID {
		a.mutationActive = ""
	}
	a.mutationEvents = append(a.mutationEvents, "cancel:"+job.Kind)
	resp.Job = cloneServiceOperationMutationJob(job)
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
	resp.DefaultCert = transport.DefaultMailTLSCertificatePath
	resp.SNICount = len(snapshot)
	return nil
}

func (a *mailTLSIsolationRPCAgent) DeleteMailDomain(
	_ *transport.DeleteMailDomainRequest,
	resp *transport.DeleteMailDomainResponse,
) error {
	resp.Applied = true
	return nil
}

func (a *mailTLSIsolationRPCAgent) DNSBackendReadiness(
	_ *transport.Empty,
	resp *transport.DNSBackendReadinessResponse,
) error {
	resp.Engines = []transport.DNSBackendRuntimeState{
		{
			Engine: transport.DNSEnginePowerDNS, Installed: true,
			Running: true, Managed: true, Unit: "pdns.service",
		},
		{Engine: transport.DNSEngineBIND, Unit: "bind9.service"},
	}
	return nil
}

func attachMailTLSIsolationAgent(t *testing.T, p *Panel, agent *mailTLSIsolationRPCAgent) {
	t.Helper()
	ensureActiveDNSEngineForTest(t, p, transport.DNSEnginePowerDNS)
	if agent.serviceOperationTestAgent == nil {
		agent.serviceOperationTestAgent = newServiceOperationTestAgent()
	}
	p.pkgFamilyVal = "apt"
	previousHostname := readMailTLSHostname
	readMailTLSHostname = func() (string, error) { return "MAIL.PANEL.TEST.", nil }
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
		readMailTLSHostname = previousHostname
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
	keyPath := certPath + ".key"
	if strings.HasPrefix(certPath, "/certs/") {
		digest := sha256.Sum256([]byte(certPath))
		directory := fmt.Sprintf(
			"/etc/ssl/celikpanel/%s/sha256-%x",
			name,
			digest,
		)
		certPath = directory + "/fullchain.pem"
		keyPath = directory + "/privkey.pem"
	}
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
		domainID64, certPath, keyPath, name, secureMail, status); err != nil {
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
		paths = append(paths, agent.fixturePathLocked(item.CertPath))
	}
	sort.Strings(paths)
	return paths
}

func mailTLSMutationBoundContext() context.Context {
	return withPanelMutationBinding(context.Background(), agentMutationBinding{
		MutationRequestID: "11111111111111111111111111111111",
		MutationOwnerID:   "22222222222222222222222222222222",
	})
}

func mailTLSReconcileSnapshotPaths(agent *mailTLSIsolationRPCAgent) []string {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if len(agent.syncCalls) == 0 {
		return nil
	}
	last := agent.syncCalls[len(agent.syncCalls)-1]
	paths := make([]string, 0, len(last.SNI))
	for _, item := range last.SNI {
		paths = append(paths, agent.fixturePathLocked(item.CertPath))
	}
	sort.Strings(paths)
	return paths
}

func TestMailTLSV2RejectsBoundContextBeforeRPC(t *testing.T) {
	p := &Panel{}
	agent := &mailTLSIsolationRPCAgent{}
	attachMailTLSIsolationAgent(t, p, agent)

	resp, err := p.resyncMailTLSForTargetLocked(mailTLSMutationBoundContext(), 0, "", "")
	if err == nil || !strings.Contains(err.Error(), "cannot reuse a bound mutation context") {
		t.Fatalf("bound context error = %v", err)
	}
	if resp != (transport.SecureMailTLSResponse{}) {
		t.Fatalf("missing binding response = %+v, want zero", resp)
	}
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if len(agent.inspectCalls) != 0 || len(agent.syncCalls) != 0 {
		t.Fatalf(
			"missing binding reached agent RPCs: inspect=%v reconcile=%v",
			agent.inspectCalls,
			agent.syncCalls,
		)
	}
}

func TestMailTLSV2CapabilityAndPlatformGatesPrecedeSnapshotAndLease(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*Panel, *mailTLSIsolationRPCAgent)
	}{
		{
			name: "agent capability missing",
			setup: func(_ *Panel, agent *mailTLSIsolationRPCAgent) {
				capabilities := []string{
					transport.AgentCapabilityFirewallApplyV2,
					transport.AgentCapabilityDNSZoneSyncV2,
					transport.AgentCapabilityPanelCertificateIssueV2,
				}
				agent.versionCapabilities = &capabilities
			},
		},
		{
			name: "RHEL preview remains closed",
			setup: func(panel *Panel, _ *mailTLSIsolationRPCAgent) {
				panel.pkgFamilyVal = "dnf"
				panel.hostPlatformKnown = true
				panel.hostPlatformVal = rhelPolicyTestIdentity()
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			panel := &Panel{pkgFamilyVal: "apt"}
			agent := &mailTLSIsolationRPCAgent{}
			attachMailTLSIsolationAgent(t, panel, agent)
			test.setup(panel, agent)
			if err := panel.resyncMailTLS(context.Background()); err == nil {
				t.Fatal("Mail TLS V2 gate unexpectedly passed")
			}
			agent.serviceOperationTestAgent.mu.Lock()
			jobs := len(agent.mutationJobs)
			agent.serviceOperationTestAgent.mu.Unlock()
			agent.mu.Lock()
			inspectCalls, syncCalls := len(agent.inspectCalls), len(agent.syncCalls)
			agent.mu.Unlock()
			if jobs != 0 || inspectCalls != 0 || syncCalls != 0 {
				t.Fatalf("rejected publication touched host/lease: jobs=%d inspect=%d sync=%d", jobs, inspectCalls, syncCalls)
			}
		})
	}
}

func TestMailTLSV2PreflightPrecedesSecureMailLedgerMutation(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*Panel, *mailTLSIsolationRPCAgent)
	}{
		{
			name: "agent capability missing",
			setup: func(_ *Panel, agent *mailTLSIsolationRPCAgent) {
				capabilities := []string{
					transport.AgentCapabilityFirewallApplyV2,
					transport.AgentCapabilityDNSZoneSyncV2,
					transport.AgentCapabilityPanelCertificateIssueV2,
				}
				agent.versionCapabilities = &capabilities
			},
		},
		{
			name: "RHEL preview remains closed",
			setup: func(panel *Panel, _ *mailTLSIsolationRPCAgent) {
				panel.pkgFamilyVal = "dnf"
				panel.hostPlatformKnown = true
				panel.hostPlatformVal = rhelPolicyTestIdentity()
			},
		},
	}
	for _, test := range tests {
		for _, action := range []string{"toggle", "domain removal"} {
			t.Run(test.name+"/"+action, func(t *testing.T) {
				panel, subscriptionID := newMailTLSIsolationFixture(t)
				domainID := addMailTLSIsolationDomain(
					t, panel, subscriptionID, "preflight.example",
					"/certs/preflight", "active", true,
				)
				agent := &mailTLSIsolationRPCAgent{
					certificates: map[string]MailTLSInspectRPCResponse{
						"/certs/preflight": validMailTLSCertificate("preflight.example"),
					},
				}
				attachMailTLSIsolationAgent(t, panel, agent)
				test.setup(panel, agent)

				switch action {
				case "toggle":
					recorder := httptest.NewRecorder()
					request := httptest.NewRequest(
						http.MethodPut, "/api/v1/domains/1/ssl/mail",
						strings.NewReader(`{"secure_mail":false}`),
					)
					panel.handleDomainSSLMail(recorder, request, domainID)
					if recorder.Code == http.StatusOK {
						t.Fatalf("rejected secure-mail toggle status=%d body=%s", recorder.Code, recorder.Body.String())
					}
				case "domain removal":
					if _, err := panel.prepareDomainMailTLSRemoval(
						context.Background(), domainID,
					); err == nil {
						t.Fatal("rejected domain removal unexpectedly passed Mail TLS preflight")
					}
				}

				var secureMail bool
				if err := panel.db.GetDB().QueryRow(`
					SELECT secure_mail FROM ssl_certificates
					WHERE domain_id = ? AND status = 'active'`, domainID,
				).Scan(&secureMail); err != nil {
					t.Fatal(err)
				}
				if !secureMail {
					t.Fatal("Mail TLS preflight failure changed secure_mail ledger")
				}
				agent.serviceOperationTestAgent.mu.Lock()
				jobs := len(agent.mutationJobs)
				events := append([]string(nil), agent.mutationEvents...)
				agent.serviceOperationTestAgent.mu.Unlock()
				agent.mu.Lock()
				inspectCalls := len(agent.inspectCalls)
				syncCalls := len(agent.syncCalls)
				agent.mu.Unlock()
				if jobs != 0 || len(events) != 0 || inspectCalls != 0 || syncCalls != 0 {
					t.Fatalf(
						"rejected preflight touched ledger lease/host: jobs=%d events=%v inspect=%d sync=%d",
						jobs, events, inspectCalls, syncCalls,
					)
				}
			})
		}
	}
}

func TestMailTLSV2PublishesCustomAndACMESnapshot(t *testing.T) {
	p, subscriptionID := newMailTLSIsolationFixture(t)
	addMailTLSIsolationDomain(
		t, p, subscriptionID, "custom.example", "/certs/custom", "active", true,
	)
	acmeDomainID := addMailTLSIsolationDomain(
		t, p, subscriptionID, "acme.example", "/certs/acme", "active", true,
	)
	if _, err := p.db.GetDB().Exec(
		`UPDATE ssl_certificates SET type = 'letsencrypt' WHERE domain_id = ?`,
		acmeDomainID,
	); err != nil {
		t.Fatalf("mark ACME certificate: %v", err)
	}
	agent := &mailTLSIsolationRPCAgent{certificates: map[string]MailTLSInspectRPCResponse{
		"/certs/custom": validMailTLSCertificate("custom.example"),
		"/certs/acme":   validMailTLSCertificate("acme.example"),
	}}
	attachMailTLSIsolationAgent(t, p, agent)

	if err := p.resyncMailTLS(context.Background()); err != nil {
		t.Fatalf("direct V2 mail TLS: %v", err)
	}
	wantPaths := []string{"/certs/acme", "/certs/custom"}
	if got := mailTLSReconcileSnapshotPaths(agent); strings.Join(got, ",") != strings.Join(wantPaths, ",") {
		t.Fatalf("bound snapshot paths = %v, want %v", got, wantPaths)
	}
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if len(agent.syncCalls) != 1 {
		t.Fatalf("SyncMailTLSV2 calls = %d, want 1", len(agent.syncCalls))
	}
	req := agent.syncCalls[0]
	if !validServiceOperationID(req.MutationRequestID) ||
		!validServiceOperationID(req.MutationOwnerID) ||
		req.ExpectedBuildCommit != strings.TrimSpace(buildCommit) ||
		req.Myhostname != "mail.panel.test" {
		t.Fatalf("bound mail TLS request metadata = %+v", req)
	}
}

func TestMailTLSV2ConcurrentCertificateChangesPublishNewestFullSnapshotLast(t *testing.T) {
	panel, subscriptionID := newMailTLSIsolationFixture(t)
	addMailTLSIsolationDomain(
		t, panel, subscriptionID, "first.example", "/certs/first", "active", true,
	)
	secondDomainID := addMailTLSIsolationDomain(
		t, panel, subscriptionID, "second.example", "/certs/second", "active", false,
	)
	release := make(chan struct{})
	agent := &mailTLSIsolationRPCAgent{
		certificates: map[string]MailTLSInspectRPCResponse{
			"/certs/first":  validMailTLSCertificate("first.example"),
			"/certs/second": validMailTLSCertificate("second.example"),
		},
		syncStarted: make(chan struct{}),
		releaseSync: release,
	}
	attachMailTLSIsolationAgent(t, panel, agent)

	firstResult := make(chan error, 1)
	go func() { firstResult <- panel.resyncMailTLS(context.Background()) }()
	select {
	case <-agent.syncStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("first Mail TLS publication did not reach the agent")
	}
	if _, err := panel.db.GetDB().Exec(`
		UPDATE ssl_certificates SET secure_mail = 1
		WHERE domain_id = ? AND status = 'active'`, secondDomainID,
	); err != nil {
		t.Fatal(err)
	}
	secondResult := make(chan error, 1)
	go func() { secondResult <- panel.resyncMailTLS(context.Background()) }()
	close(release)

	for index, result := range []<-chan error{firstResult, secondResult} {
		select {
		case err := <-result:
			if err != nil {
				t.Fatalf("concurrent Mail TLS publication %d: %v", index+1, err)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("concurrent Mail TLS publication %d deadlocked", index+1)
		}
	}

	agent.mu.Lock()
	callCount := len(agent.syncCalls)
	var last transport.SyncMailTLSV2Request
	if callCount > 0 {
		last = agent.syncCalls[callCount-1]
	}
	agent.mu.Unlock()
	if callCount != 2 || len(last.SNI) != 2 {
		t.Fatalf("serialized Mail TLS snapshots=%d last=%+v", callCount, last.SNI)
	}
	if got := mailTLSReconcileSnapshotPaths(agent); strings.Join(got, ",") != "/certs/first,/certs/second" {
		t.Fatalf("newest full snapshot paths=%v", got)
	}
}

func TestMailTLSV2DefiniteFailureCompensatesConcurrentTemporarySnapshot(t *testing.T) {
	panel, subscriptionID := newMailTLSIsolationFixture(t)
	domainID := addMailTLSIsolationDomain(
		t, panel, subscriptionID, "race.example", "/certs/race", "active", true,
	)
	agent := &mailTLSIsolationRPCAgent{
		certificates: map[string]MailTLSInspectRPCResponse{
			"/certs/race": validMailTLSCertificate("race.example"),
		},
	}
	agent.syncHook = func(
		index int,
		_ *transport.SyncMailTLSV2Request,
		resp *transport.SecureMailTLSResponse,
	) (bool, error) {
		if index == 2 {
			resp.Error = "forced definite pre-intent publication failure"
			return true, nil
		}
		return false, nil
	}
	attachMailTLSIsolationAgent(t, panel, agent)

	// Freeze the first lifecycle after it owns serviceMutationMu but before it
	// can read the snapshot. The toggle can still write secure_mail=0, then it
	// blocks behind the same outer lock. When released, the first lifecycle sees
	// and publishes that temporary new desired state.
	mailTLSSyncMu.Lock()
	mailLockHeld := true
	t.Cleanup(func() {
		if mailLockHeld {
			mailTLSSyncMu.Unlock()
		}
	})
	firstResult := make(chan error, 1)
	go func() { firstResult <- panel.resyncMailTLS(context.Background()) }()
	deadline := time.Now().Add(5 * time.Second)
	for panel.serviceMutationMu.TryLock() {
		panel.serviceMutationMu.Unlock()
		if time.Now().After(deadline) {
			t.Fatal("first lifecycle did not acquire serviceMutationMu")
		}
		time.Sleep(time.Millisecond)
	}

	recorder := httptest.NewRecorder()
	toggleDone := make(chan struct{})
	go func() {
		defer close(toggleDone)
		request := httptest.NewRequest(
			http.MethodPut, "/api/v1/domains/1/ssl/mail",
			strings.NewReader(`{"secure_mail":false}`),
		)
		panel.handleDomainSSLMail(recorder, request, domainID)
	}()
	for {
		var secureMail bool
		if err := panel.db.GetDB().QueryRow(`
			SELECT secure_mail FROM ssl_certificates
			WHERE domain_id = ? AND status = 'active'`, domainID,
		).Scan(&secureMail); err != nil {
			t.Fatal(err)
		}
		if !secureMail {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("toggle did not write temporary secure_mail=0")
		}
		time.Sleep(time.Millisecond)
	}
	mailTLSSyncMu.Unlock()
	mailLockHeld = false
	if err := <-firstResult; err != nil {
		t.Fatalf("temporary concurrent publication: %v", err)
	}
	select {
	case <-toggleDone:
	case <-time.After(5 * time.Second):
		t.Fatal("failed toggle and compensation deadlocked")
	}
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("forced toggle failure status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	var secureMail bool
	if err := panel.db.GetDB().QueryRow(`
		SELECT secure_mail FROM ssl_certificates
		WHERE domain_id = ? AND status = 'active'`, domainID,
	).Scan(&secureMail); err != nil {
		t.Fatal(err)
	}
	agent.mu.Lock()
	callCount := len(agent.syncCalls)
	last := agent.syncCalls[callCount-1]
	agent.mu.Unlock()
	if !secureMail || callCount != 3 || len(last.SNI) != 1 {
		t.Fatalf("concurrent compensation calls=%d final snapshot=%+v", callCount, last.SNI)
	}
	if got := mailTLSReconcileSnapshotPaths(agent); strings.Join(got, ",") != "/certs/race" {
		t.Fatalf("final compensating full snapshot paths=%v", got)
	}
}

func TestDomainMailTLSRemovalDefiniteFailureCompensatesConcurrentTemporarySnapshot(t *testing.T) {
	panel, subscriptionID := newMailTLSIsolationFixture(t)
	domainID := addMailTLSIsolationDomain(
		t, panel, subscriptionID, "remove-race.example",
		"/certs/remove-race", "active", true,
	)
	agent := &mailTLSIsolationRPCAgent{
		certificates: map[string]MailTLSInspectRPCResponse{
			"/certs/remove-race": validMailTLSCertificate("remove-race.example"),
		},
	}
	agent.syncHook = func(
		index int,
		_ *transport.SyncMailTLSV2Request,
		resp *transport.SecureMailTLSResponse,
	) (bool, error) {
		if index == 2 {
			resp.Error = "forced definite removal pre-intent failure"
			return true, nil
		}
		return false, nil
	}
	attachMailTLSIsolationAgent(t, panel, agent)

	mailTLSSyncMu.Lock()
	mailLockHeld := true
	t.Cleanup(func() {
		if mailLockHeld {
			mailTLSSyncMu.Unlock()
		}
	})
	firstResult := make(chan error, 1)
	go func() { firstResult <- panel.resyncMailTLS(context.Background()) }()
	deadline := time.Now().Add(5 * time.Second)
	for panel.serviceMutationMu.TryLock() {
		panel.serviceMutationMu.Unlock()
		if time.Now().After(deadline) {
			t.Fatal("first removal lifecycle did not acquire serviceMutationMu")
		}
		time.Sleep(time.Millisecond)
	}

	removalResult := make(chan error, 1)
	go func() {
		_, err := panel.prepareDomainMailTLSRemoval(context.Background(), domainID)
		removalResult <- err
	}()
	for {
		var secureMail bool
		if err := panel.db.GetDB().QueryRow(`
			SELECT secure_mail FROM ssl_certificates
			WHERE domain_id = ? AND status = 'active'`, domainID,
		).Scan(&secureMail); err != nil {
			t.Fatal(err)
		}
		if !secureMail {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("removal did not write temporary secure_mail=0")
		}
		time.Sleep(time.Millisecond)
	}
	mailTLSSyncMu.Unlock()
	mailLockHeld = false
	if err := <-firstResult; err != nil {
		t.Fatalf("temporary removal snapshot: %v", err)
	}
	select {
	case err := <-removalResult:
		if err == nil || !strings.Contains(err.Error(), "forced definite removal pre-intent failure") {
			t.Fatalf("definite removal failure=%v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("failed removal and compensation deadlocked")
	}

	var secureMail bool
	if err := panel.db.GetDB().QueryRow(`
		SELECT secure_mail FROM ssl_certificates
		WHERE domain_id = ? AND status = 'active'`, domainID,
	).Scan(&secureMail); err != nil {
		t.Fatal(err)
	}
	agent.mu.Lock()
	callCount := len(agent.syncCalls)
	last := agent.syncCalls[callCount-1]
	agent.mu.Unlock()
	if !secureMail || callCount != 3 || len(last.SNI) != 1 {
		t.Fatalf("removal compensation calls=%d secure_mail=%t final=%+v", callCount, secureMail, last.SNI)
	}
	if got := mailTLSReconcileSnapshotPaths(agent); strings.Join(got, ",") != "/certs/remove-race" {
		t.Fatalf("final removal compensation paths=%v", got)
	}
}

func TestMailTLSV2OmitsUnrelatedInvalidCertificate(t *testing.T) {
	p, subscriptionID := newMailTLSIsolationFixture(t)
	addMailTLSIsolationDomain(
		t, p, subscriptionID, "healthy.example", "/certs/healthy", "active", true,
	)
	addMailTLSIsolationDomain(
		t, p, subscriptionID, "invalid.example", "/certs/invalid", "active", true,
	)
	agent := &mailTLSIsolationRPCAgent{certificates: map[string]MailTLSInspectRPCResponse{
		"/certs/healthy": validMailTLSCertificate("healthy.example"),
		"/certs/invalid": {Error: "certificate is invalid"},
	}}
	attachMailTLSIsolationAgent(t, p, agent)

	if err := p.resyncMailTLS(context.Background()); err != nil {
		t.Fatalf("reconcile safe snapshot: %v", err)
	}
	wantPaths := []string{"/certs/healthy"}
	if got := mailTLSReconcileSnapshotPaths(agent); strings.Join(got, ",") != strings.Join(wantPaths, ",") {
		t.Fatalf("safe bound snapshot paths = %v, want %v", got, wantPaths)
	}
}

func TestMailTLSV2InspectionFailureStopsPublication(t *testing.T) {
	p, subscriptionID := newMailTLSIsolationFixture(t)
	addMailTLSIsolationDomain(
		t, p, subscriptionID, "unreadable.example", "/certs/unreadable", "active", true,
	)
	agent := &mailTLSIsolationRPCAgent{
		certificates:  map[string]MailTLSInspectRPCResponse{},
		inspectErrors: map[string]string{"/certs/unreadable": "forced inspection transport failure"},
	}
	attachMailTLSIsolationAgent(t, p, agent)

	resp := transport.SecureMailTLSResponse{}
	err := p.resyncMailTLS(context.Background())
	if err == nil || !strings.Contains(err.Error(), "forced inspection transport failure") {
		t.Fatalf("inspection failure error = %v", err)
	}
	if resp != (transport.SecureMailTLSResponse{}) {
		t.Fatalf("inspection failure response = %+v, want zero", resp)
	}
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if len(agent.syncCalls) != 0 {
		t.Fatalf("inspection failure published a snapshot: %+v", agent.syncCalls)
	}
}

func TestMailTLSV2AcceptsEmptySnapshotFallback(t *testing.T) {
	p, _ := newMailTLSIsolationFixture(t)
	agent := &mailTLSIsolationRPCAgent{reconcileResponse: &transport.SecureMailTLSResponse{
		Configured: true, SNICount: 0, DefaultCert: transport.DefaultMailTLSCertificatePath,
	}}
	attachMailTLSIsolationAgent(t, p, agent)

	resp, err := p.resyncMailTLSForTargetLocked(context.Background(), 0, "", "")
	if err != nil {
		t.Fatalf("empty snapshot fallback: %v", err)
	}
	if !resp.Configured || resp.SNICount != 0 ||
		resp.DefaultCert != transport.DefaultMailTLSCertificatePath {
		t.Fatalf("empty snapshot fallback response = %+v", resp)
	}
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if len(agent.syncCalls) != 1 || len(agent.syncCalls[0].SNI) != 0 {
		t.Fatalf("empty snapshot request = %+v", agent.syncCalls)
	}
}

func TestMailTLSV2RejectsInvalidAgentResponse(t *testing.T) {
	tests := []struct {
		name       string
		response   transport.SecureMailTLSResponse
		rpcError   string
		wantMarker string
	}{
		{
			name: "unconfigured",
			response: transport.SecureMailTLSResponse{
				Configured: false, SNICount: 0,
				DefaultCert: transport.DefaultMailTLSCertificatePath,
			},
			wantMarker: "applied 0 of 0",
		},
		{
			name: "wrong count",
			response: transport.SecureMailTLSResponse{
				Configured: true, SNICount: 1,
				DefaultCert: transport.DefaultMailTLSCertificatePath,
			},
			wantMarker: "applied 1 of 0",
		},
		{
			name:       "agent error",
			response:   transport.SecureMailTLSResponse{Configured: true, SNICount: 0, Error: "forced agent rejection"},
			wantMarker: "forced agent rejection",
		},
		{
			name:       "missing default certificate",
			response:   transport.SecureMailTLSResponse{Configured: true, SNICount: 0},
			wantMarker: "unexpected default certificate path",
		},
		{
			name: "unexpected default certificate",
			response: transport.SecureMailTLSResponse{
				Configured: true, SNICount: 0,
				DefaultCert: "/etc/ssl/certs/fallback.pem",
			},
			wantMarker: "unexpected default certificate path",
		},
		{
			name: "whitespace-padded default certificate",
			response: transport.SecureMailTLSResponse{
				Configured: true, SNICount: 0,
				DefaultCert: " " + transport.DefaultMailTLSCertificatePath + " ",
			},
			wantMarker: "unexpected default certificate path",
		},
		{
			name:       "rpc error",
			rpcError:   "forced reconcile transport failure",
			wantMarker: "forced reconcile transport failure",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, _ := newMailTLSIsolationFixture(t)
			response := tt.response
			agent := &mailTLSIsolationRPCAgent{
				reconcileResponse: &response,
				reconcileRPCError: tt.rpcError,
			}
			attachMailTLSIsolationAgent(t, p, agent)

			resp, err := p.resyncMailTLSForTargetLocked(context.Background(), 0, "", "")
			if err == nil || !strings.Contains(err.Error(), tt.wantMarker) {
				t.Fatalf("reconcile response error = %v, want marker %q", err, tt.wantMarker)
			}
			if resp != (transport.SecureMailTLSResponse{}) {
				t.Fatalf("failed reconcile response = %+v, want zero", resp)
			}
		})
	}
}

func TestMailTLSV2ExactTerminalSuccessOverridesUnconfirmedResponse(t *testing.T) {
	tests := []struct {
		name     string
		response transport.SecureMailTLSResponse
	}{
		{
			name: "stale error",
			response: transport.SecureMailTLSResponse{
				Configured: true, DefaultCert: transport.DefaultMailTLSCertificatePath,
				Error: "stale response error",
			},
		},
		{
			name: "stale configured flag",
			response: transport.SecureMailTLSResponse{
				DefaultCert: transport.DefaultMailTLSCertificatePath,
			},
		},
		{
			name: "stale SNI count",
			response: transport.SecureMailTLSResponse{
				Configured: true, DefaultCert: transport.DefaultMailTLSCertificatePath,
				SNICount: 1,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			panel, _ := newMailTLSIsolationFixture(t)
			response := test.response
			agent := &mailTLSIsolationRPCAgent{
				reconcileResponse: &response,
				terminalizeSync:   true,
			}
			attachMailTLSIsolationAgent(t, panel, agent)

			got, err := panel.resyncMailTLSForTargetLocked(
				context.Background(), 0, "", "",
			)
			if err != nil {
				t.Fatalf("authoritative terminal success: %v", err)
			}
			if !got.Configured || got.SNICount != 0 ||
				got.DefaultCert != transport.DefaultMailTLSCertificatePath ||
				!strings.Contains(got.Detail, "RPC response was lost") {
				t.Fatalf("synthesized terminal response = %+v", got)
			}
		})
	}
}

func TestMailTLSV2ResponseLossWaitsKindAwareForIntentTerminalSuccess(t *testing.T) {
	panel, _ := newMailTLSIsolationFixture(t)
	agent := &mailTLSIsolationRPCAgent{
		reconcileRPCError: "simulated SyncMailTLSV2 response loss after intent",
		leaveMailIntent:   true,
		finishMailLoss:    true,
	}
	attachMailTLSIsolationAgent(t, panel, agent)

	previousWait := waitExpectedAgentMutationTerminalFn
	waitExpectedAgentMutationTerminalFn = func(
		_ *Panel,
		ctx context.Context,
		identity agentMutationIdentity,
	) (*agentMutationJob, error) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) < 2*time.Minute {
			t.Fatalf("Mail TLS exact terminal reconcile deadline=%v ok=%t", deadline, ok)
		}
		return &agentMutationJob{
			RequestID: identity.RequestID, OwnerID: identity.OwnerID,
			Kind: identity.Kind, Target: identity.Target,
			PackageName: identity.PackageName,
			Status:      agentMutationSucceeded,
			Phase: "commit/mail-tls-sync/v1/published/" +
				identity.RequestID + "/" + identity.PackageName,
		}, nil
	}
	t.Cleanup(func() { waitExpectedAgentMutationTerminalFn = previousWait })

	response, err := panel.resyncMailTLSForTargetLocked(
		context.Background(), 0, "", "",
	)
	if err != nil {
		t.Fatalf("kind-aware Mail TLS terminal reconcile: %v", err)
	}
	if !response.Configured || response.SNICount != 0 ||
		response.DefaultCert != transport.DefaultMailTLSCertificatePath ||
		!strings.Contains(response.Detail, "RPC response was lost") {
		t.Fatalf("authoritative response after delayed terminal success = %+v", response)
	}
	agent.serviceOperationTestAgent.mu.Lock()
	defer agent.serviceOperationTestAgent.mu.Unlock()
	if len(agent.mutationJobs) != 1 {
		t.Fatalf("Mail TLS response-loss jobs=%d, want one exact intent", len(agent.mutationJobs))
	}
	for _, job := range agent.mutationJobs {
		if !strings.HasPrefix(job.Phase, "commit/mail-tls-sync/v1/intent/") {
			t.Fatalf("response loss did not occur after durable intent: %+v", job)
		}
	}
}

func TestMailTLSV2UncertainIntentRetainsRequestedDesiredState(t *testing.T) {
	for _, action := range []string{"toggle", "domain removal"} {
		t.Run(action, func(t *testing.T) {
			panel, subscriptionID := newMailTLSIsolationFixture(t)
			domainID := addMailTLSIsolationDomain(
				t, panel, subscriptionID, "uncertain.example",
				"/certs/uncertain", "active", true,
			)
			agent := &mailTLSIsolationRPCAgent{
				certificates: map[string]MailTLSInspectRPCResponse{
					"/certs/uncertain": validMailTLSCertificate("uncertain.example"),
				},
				reconcileRPCError: "simulated SyncMailTLSV2 response loss after intent",
				leaveMailIntent:   true,
				finishMailLoss:    true,
			}
			attachMailTLSIsolationAgent(t, panel, agent)

			previousWait := waitExpectedAgentMutationTerminalFn
			waitExpectedAgentMutationTerminalFn = func(
				_ *Panel,
				ctx context.Context,
				identity agentMutationIdentity,
			) (*agentMutationJob, error) {
				if identity.Kind != "mail_tls_sync" {
					t.Fatalf("uncertainty wait kind=%q", identity.Kind)
				}
				deadline, ok := ctx.Deadline()
				if !ok || time.Until(deadline) < 2*time.Minute {
					t.Fatalf("uncertainty wait deadline=%v ok=%t", deadline, ok)
				}
				return nil, context.DeadlineExceeded
			}
			t.Cleanup(func() { waitExpectedAgentMutationTerminalFn = previousWait })

			switch action {
			case "toggle":
				recorder := httptest.NewRecorder()
				request := httptest.NewRequest(
					http.MethodPut, "/api/v1/domains/1/ssl/mail",
					strings.NewReader(`{"secure_mail":false}`),
				)
				panel.handleDomainSSLMail(recorder, request, domainID)
				if recorder.Code == http.StatusOK {
					t.Fatalf("uncertain toggle status=%d body=%s", recorder.Code, recorder.Body.String())
				}
			case "domain removal":
				_, err := panel.prepareDomainMailTLSRemoval(context.Background(), domainID)
				if err == nil || !mutationTerminalUncertain(err) ||
					!strings.Contains(err.Error(), "secure_mail=0 state was retained") {
					t.Fatalf("uncertain domain removal error=%v", err)
				}
			}

			var secureMail bool
			if err := panel.db.GetDB().QueryRow(`
				SELECT secure_mail FROM ssl_certificates
				WHERE domain_id = ? AND status = 'active'`, domainID,
			).Scan(&secureMail); err != nil {
				t.Fatal(err)
			}
			if secureMail {
				t.Fatal("uncertain committed exclusion was rolled back in the desired-state ledger")
			}
			agent.mu.Lock()
			syncCalls := len(agent.syncCalls)
			agent.mu.Unlock()
			if syncCalls != 1 {
				t.Fatalf("uncertain intent triggered %d publications, want no rollback publication", syncCalls)
			}
		})
	}
}

func TestStartupTerminalizesDirectMailTLSBeforeFreshV2Publication(t *testing.T) {
	panel, _ := newMailTLSIsolationFixture(t)
	agent := &mailTLSIsolationRPCAgent{cancelMailAsSuccess: true}
	attachMailTLSIsolationAgent(t, panel, agent)

	const (
		requestID = "11111111111111111111111111111111"
		ownerID   = "22222222222222222222222222222222"
	)
	agent.serviceOperationTestAgent.mu.Lock()
	agent.mutationJobs[requestID] = &ServiceOperationMutationJob{
		RequestID: requestID,
		OwnerID:   ownerID,
		Kind:      "mail_tls_sync",
		Target:    "mail-tls",
		PackageName: "mail-tls-sync/v1:sha256:" +
			strings.Repeat("a", 64),
		Status: agentMutationRunning,
		Phase:  "commit/mail-tls-sync/v1/intent/" + requestID,
	}
	agent.mutationActive = requestID
	agent.serviceOperationTestAgent.mu.Unlock()

	if recovered, err := panel.recoverInterruptedServiceOperations(
		context.Background(),
	); err != nil || recovered != 0 {
		t.Fatalf("terminalize direct Mail TLS recovery: recovered=%d err=%v", recovered, err)
	}
	if _, err := panel.reconcileCertificateDependentsAtStartup(
		context.Background(),
	); err != nil {
		t.Fatalf("fresh startup Mail TLS V2 publication: %v", err)
	}

	agent.serviceOperationTestAgent.mu.Lock()
	events := append([]string(nil), agent.mutationEvents...)
	orphan := cloneServiceOperationMutationJob(agent.mutationJobs[requestID])
	agent.serviceOperationTestAgent.mu.Unlock()
	cancelIndex := mutationEventIndex(events, "cancel:mail_tls_sync")
	beginIndex := mutationEventIndex(events, "begin:mail_tls_sync")
	finishIndex := mutationEventIndex(events, "finish:mail_tls_sync:succeeded")
	if orphan == nil || orphan.Status != agentMutationSucceeded ||
		cancelIndex < 0 || beginIndex <= cancelIndex || finishIndex <= beginIndex {
		t.Fatalf(
			"startup Mail TLS ordering events=%v orphan=%+v",
			events, orphan,
		)
	}
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
	var domainStatus string
	if err := p.db.GetDB().QueryRow(`
		SELECT COUNT(*), COALESCE(MAX(sc.secure_mail), 0), COALESCE(MAX(d.status), '')
		FROM domains d
		LEFT JOIN ssl_certificates sc ON sc.domain_id = d.id AND sc.status = 'active'
		WHERE d.id = ?`, targetID).
		Scan(&domainCount, &secureMail, &domainStatus); err != nil {
		t.Fatalf("read aborted delete state: %v", err)
	}
	var markerCount int
	if err := p.db.GetDB().QueryRow(
		`SELECT COUNT(*) FROM domain_deletion_operations WHERE domain_id = ?`, targetID,
	).Scan(&markerCount); err != nil {
		t.Fatal(err)
	}
	if domainCount != 1 || secureMail != 1 || domainStatus != "active" || markerCount != 0 {
		t.Fatalf(
			"aborted delete state = domains:%d secure_mail:%d status:%q markers:%d",
			domainCount, secureMail, domainStatus, markerCount,
		)
	}
	// Another serialized publication may have observed the temporary ledger
	// state before this call proved its own pre-intent failure. Therefore the
	// restored ledger is always republished under a fresh exact V2 lease.
	want := []string{"/certs/keep-mail", "/certs/other-mail"}
	if got := mailTLSSnapshotPaths(agent); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("compensating full mail SNI snapshot = %v, want %v", got, want)
	}
	agent.mu.Lock()
	syncCalls := len(agent.syncCalls)
	agent.mu.Unlock()
	if syncCalls != 2 {
		t.Fatalf("pre-intent failure opened %d Mail TLS publications, want attempt plus compensation", syncCalls)
	}
}
