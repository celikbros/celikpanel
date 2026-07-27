package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/rpc"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/transport"
)

type ValidationRestoreApplyRequest struct {
	ServerNames        []string
	ACMEChallengeNames []string
	SSLCert            string
}

type ValidationRestoreApplyResponse struct {
	Config string
	Error  string
}

type ValidationRestoreIssueRequest struct {
	Domain             string
	Aliases            []string
	DomainID           int
	ForceRenewal       bool
	StageLineage       bool
	FreshLineage       bool
	CurrentCertPath    string
	CurrentLineageName string
}

type ValidationRestoreIssueResponse struct {
	Success     bool
	CertPath    string
	KeyPath     string
	ChainPath   string
	ExpiresAt   time.Time
	DNSNames    []string
	LineageName string
	Error       string
}

type ValidationRestoreRenewRequest struct {
	Domain          string
	CurrentCertPath string
	LineageName     string
	SubscriptionID  int
	DomainID        int
}

type ValidationRestoreRenewResponse struct {
	Success     bool
	CertPath    string
	KeyPath     string
	ChainPath   string
	ExpiresAt   time.Time
	DNSNames    []string
	LineageName string
	Error       string
}

type ValidationRestoreInspectRequest struct {
	CertPath string
	KeyPath  string
}

type ValidationRestoreInspectResponse struct {
	Valid        bool
	Trusted      bool
	TrustChecked bool
	Issuer       string
	Subject      string
	IssuedAt     time.Time
	ExpiresAt    time.Time
	DNSNames     []string
	Error        string
}

type ValidationRestoreDeleteRequest struct {
	Domain          string
	DeleteCanonical bool
	LineageNames    []string
	SnapshotPath    string
}

type ValidationRestoreDeleteResponse struct {
	Deleted bool
	Error   string
}

type validationRestoreAgent struct {
	mu sync.Mutex

	domain    string
	issueMode string
	renewMode string

	applyCalls  []ValidationRestoreApplyRequest
	deleteCalls []ValidationRestoreDeleteRequest
	issueCalls  []ValidationRestoreIssueRequest
}

func (a *validationRestoreAgent) ApplyVhost(
	req *ValidationRestoreApplyRequest,
	resp *ValidationRestoreApplyResponse,
) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.applyCalls = append(a.applyCalls, ValidationRestoreApplyRequest{
		ServerNames:        append([]string(nil), req.ServerNames...),
		ACMEChallengeNames: append([]string(nil), req.ACMEChallengeNames...),
		SSLCert:            req.SSLCert,
	})
	resp.Config = "ok"
	return nil
}

func (a *validationRestoreAgent) IssueLetsEncryptCertificate(
	req *ValidationRestoreIssueRequest,
	resp *ValidationRestoreIssueResponse,
) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.issueCalls = append(a.issueCalls, *req)

	if a.issueMode == "rejected" {
		resp.LineageName = a.domain
		resp.Error = "forced issuance failure"
		return nil
	}
	lineageName := a.domain
	if req.StageLineage {
		lineageName = fmt.Sprintf("cp-site-%d-%024x", req.DomainID, 1)
	}
	*resp = ValidationRestoreIssueResponse{
		Success:     true,
		CertPath:    "/certs/staged/fullchain.pem",
		KeyPath:     "/certs/staged/privkey.pem",
		ChainPath:   "/certs/staged/chain.pem",
		ExpiresAt:   time.Date(2026, time.October, 25, 0, 0, 0, 0, time.UTC),
		DNSNames:    []string{a.domain}, // Deliberately missing www and mail.
		LineageName: lineageName,
	}
	return nil
}

func TestIssueLetsEncryptReplacesCustomCertificateWithFreshLineage(t *testing.T) {
	const domain = "custom-to-acme.example"
	p, domainID := newIncludeMailFixture(t, domain)
	if _, err := p.db.GetDB().Exec(`
		INSERT INTO ssl_certificates (
			domain_id, type, cert_path, key_path, issuer, subject,
			issued_at, expires_at, auto_renew, secure_mail, status
		) VALUES (?, 'custom', '/certs/custom/fullchain.pem',
		          '/certs/custom/privkey.pem', 'Private CA', ?,
		          '2026-07-01T00:00:00Z', '2027-07-01T00:00:00Z',
		          0, 0, 'active')`,
		domainID, domain,
	); err != nil {
		t.Fatalf("insert active custom certificate: %v", err)
	}
	if _, err := p.db.GetDB().Exec(`
		UPDATE sites
		SET ssl_enabled = 1, ssl_type = 'custom',
		    ssl_cert_path = '/certs/custom/fullchain.pem',
		    ssl_key_path = '/certs/custom/privkey.pem'
		WHERE domain_id = ?`, domainID,
	); err != nil {
		t.Fatalf("enable custom certificate site state: %v", err)
	}

	agent := &validationRestoreAgent{domain: domain}
	attachValidationRestoreAgent(t, p, agent)
	request := httptest.NewRequest(
		http.MethodPost,
		fmt.Sprintf("/api/v1/domains/%d/ssl/letsencrypt", domainID),
		strings.NewReader(
			`{"email":"admin@custom-to-acme.example","auto_renew":true,"reissue":true}`,
		),
	)
	recorder := httptest.NewRecorder()
	p.handleIssueLetsEncrypt(recorder, request)
	if recorder.Code < 200 || recorder.Code >= 300 {
		t.Fatalf(
			"custom to ACME replacement failed: status=%d body=%s",
			recorder.Code, recorder.Body.String(),
		)
	}

	agent.mu.Lock()
	if len(agent.issueCalls) != 1 {
		agent.mu.Unlock()
		t.Fatalf("issue calls = %d, want 1", len(agent.issueCalls))
	}
	issue := agent.issueCalls[0]
	agent.mu.Unlock()
	if !issue.ForceRenewal || !issue.StageLineage || !issue.FreshLineage {
		t.Fatalf("custom replacement did not request a fresh staged lineage: %#v", issue)
	}
	if issue.CurrentCertPath != "/certs/custom/fullchain.pem" {
		t.Fatalf("current custom certificate path = %q", issue.CurrentCertPath)
	}

	var (
		certType      string
		lineage       string
		renewalStatus string
	)
	if err := p.db.GetDB().QueryRow(`
		SELECT type, lineage_name, COALESCE(renewal_status, '')
		FROM ssl_certificates
		WHERE domain_id = ? AND status = 'active'`,
		domainID,
	).Scan(&certType, &lineage, &renewalStatus); err != nil {
		t.Fatalf("read replacement certificate: %v", err)
	}
	if certType != "letsencrypt" ||
		lineage != fmt.Sprintf("cp-site-%d-%024x", domainID, 1) ||
		renewalStatus != "" {
		t.Fatalf(
			"replacement state type=%q lineage=%q renewal=%q",
			certType, lineage, renewalStatus,
		)
	}
}

func (a *validationRestoreAgent) RenewLetsEncryptCertificate(
	req *ValidationRestoreRenewRequest,
	resp *ValidationRestoreRenewResponse,
) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.renewMode == "rejected" {
		resp.Error = "forced renewal failure"
		return nil
	}
	certPath := "/certs/renewed/fullchain.pem"
	keyPath := "/certs/renewed/privkey.pem"
	chainPath := "/certs/renewed/chain.pem"
	if a.renewMode == "current" {
		certPath = req.CurrentCertPath
		keyPath = "/certs/old/privkey.pem"
		chainPath = "/certs/old/chain.pem"
	}
	*resp = ValidationRestoreRenewResponse{
		Success:     true,
		CertPath:    certPath,
		KeyPath:     keyPath,
		ChainPath:   chainPath,
		ExpiresAt:   time.Date(2027, time.January, 1, 0, 0, 0, 0, time.UTC),
		LineageName: req.LineageName,
	}
	return nil
}

func (a *validationRestoreAgent) InspectInstalledCertificate(
	req *ValidationRestoreInspectRequest,
	resp *ValidationRestoreInspectResponse,
) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	names := []string{a.domain, "www." + a.domain}
	if req.CertPath == "/certs/renewed/fullchain.pem" {
		names = []string{a.domain} // Deliberately missing www.
	}
	*resp = ValidationRestoreInspectResponse{
		Valid:        true,
		Trusted:      true,
		TrustChecked: true,
		Issuer:       "R13",
		Subject:      a.domain,
		IssuedAt:     time.Date(2026, time.July, 27, 0, 0, 0, 0, time.UTC),
		ExpiresAt:    time.Date(2027, time.January, 1, 0, 0, 0, 0, time.UTC),
		DNSNames:     names,
	}
	return nil
}

func (a *validationRestoreAgent) DeleteCertLineage(
	req *ValidationRestoreDeleteRequest,
	resp *ValidationRestoreDeleteResponse,
) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.deleteCalls = append(a.deleteCalls, ValidationRestoreDeleteRequest{
		Domain:          req.Domain,
		DeleteCanonical: req.DeleteCanonical,
		LineageNames:    append([]string(nil), req.LineageNames...),
		SnapshotPath:    req.SnapshotPath,
	})
	resp.Deleted = true
	return nil
}

func attachValidationRestoreAgent(
	t *testing.T,
	p *Panel,
	agent *validationRestoreAgent,
) {
	t.Helper()
	server := rpc.NewServer()
	if err := server.RegisterName("Agent", agent); err != nil {
		t.Fatalf("register validation-restore agent: %v", err)
	}
	connector := func(ctx context.Context) (*rpc.Client, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		serverConn, clientConn := net.Pipe()
		go server.ServeConn(serverConn)
		return rpc.NewClient(clientConn), nil
	}
	client, err := connector(context.Background())
	if err != nil {
		t.Fatalf("connect validation-restore agent: %v", err)
	}
	p.agentClient = transport.NewReconnectingClientWithContextConnector(client, connector)
	t.Cleanup(func() {
		_ = client.Close()
	})
}

func validationRestoreApplyCalls(
	agent *validationRestoreAgent,
) []ValidationRestoreApplyRequest {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	calls := make([]ValidationRestoreApplyRequest, len(agent.applyCalls))
	copy(calls, agent.applyCalls)
	return calls
}

func assertNormalVhostWasRestored(
	t *testing.T,
	agent *validationRestoreAgent,
	domain string,
	wantValidationNames []string,
) {
	t.Helper()
	calls := validationRestoreApplyCalls(agent)
	if len(calls) != 2 {
		t.Fatalf("apply-vhost calls = %d, want validation + immediate restore", len(calls))
	}
	if !equalStringSlices(calls[0].ACMEChallengeNames, wantValidationNames) {
		t.Fatalf(
			"validation challenge names = %#v, want %#v",
			calls[0].ACMEChallengeNames,
			wantValidationNames,
		)
	}
	if len(calls[1].ACMEChallengeNames) != 0 {
		t.Fatalf(
			"restored vhost retained validation-only names: %#v",
			calls[1].ACMEChallengeNames,
		)
	}
	wantServerNames := []string{domain, "www." + domain}
	if !equalStringSlices(calls[1].ServerNames, wantServerNames) {
		t.Fatalf(
			"restored server names = %#v, want %#v",
			calls[1].ServerNames,
			wantServerNames,
		)
	}
	for _, name := range calls[1].ServerNames {
		if strings.EqualFold(name, "mail."+domain) {
			t.Fatalf("restored website vhost leaked validation-only mail name")
		}
	}
}

func TestIssueLetsEncryptRestoresNormalVhostBeforeActivationOnFailure(t *testing.T) {
	for _, testCase := range []struct {
		name              string
		issueMode         string
		wantCleanupCalls  int
		wantActiveRecords int
	}{
		{
			name:              "agent rejects issuance",
			issueMode:         "rejected",
			wantCleanupCalls:  1,
			wantActiveRecords: 0,
		},
		{
			name:              "issued SAN set is not exact",
			issueMode:         "san-mismatch",
			wantCleanupCalls:  1,
			wantActiveRecords: 0,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			const domain = "restore-issue.example"
			p, domainID := newIncludeMailFixture(t, domain)
			agent := &validationRestoreAgent{
				domain:    domain,
				issueMode: testCase.issueMode,
			}
			attachValidationRestoreAgent(t, p, agent)

			request := httptest.NewRequest(
				http.MethodPost,
				fmt.Sprintf("/api/v1/domains/%d/ssl/letsencrypt", domainID),
				strings.NewReader(
					`{"email":"admin@restore-issue.example","auto_renew":true,"include_mail":true}`,
				),
			)
			recorder := httptest.NewRecorder()
			p.handleIssueLetsEncrypt(recorder, request)
			if recorder.Code >= 200 && recorder.Code < 300 {
				t.Fatalf(
					"issuance unexpectedly succeeded: status=%d body=%s",
					recorder.Code,
					recorder.Body.String(),
				)
			}

			assertNormalVhostWasRestored(
				t,
				agent,
				domain,
				[]string{"mail." + domain},
			)
			var activeRecords int
			if err := p.db.GetDB().QueryRow(`
				SELECT COUNT(*) FROM ssl_certificates
				WHERE domain_id = ? AND status = 'active'`, domainID).
				Scan(&activeRecords); err != nil {
				t.Fatalf("count active certificates: %v", err)
			}
			if activeRecords != testCase.wantActiveRecords {
				t.Fatalf(
					"active certificate count = %d, want %d",
					activeRecords,
					testCase.wantActiveRecords,
				)
			}
			var sslEnabled bool
			if err := p.db.GetDB().QueryRow(
				`SELECT ssl_enabled FROM sites WHERE domain_id = ?`,
				domainID,
			).Scan(&sslEnabled); err != nil {
				t.Fatalf("read site SSL state: %v", err)
			}
			if sslEnabled {
				t.Fatal("pre-activation failure enabled SSL in the site ledger")
			}
			agent.mu.Lock()
			deleteCallCount := len(agent.deleteCalls)
			agent.mu.Unlock()
			if deleteCallCount != testCase.wantCleanupCalls {
				t.Fatalf(
					"staged-lineage cleanup calls = %d, want %d",
					deleteCallCount,
					testCase.wantCleanupCalls,
				)
			}
			if testCase.wantCleanupCalls > 0 {
				agent.mu.Lock()
				deleteCall := agent.deleteCalls[0]
				agent.mu.Unlock()
				if deleteCall.Domain != domain || !deleteCall.DeleteCanonical {
					t.Fatalf(
						"initial cleanup authority = %#v, want exact canonical domain",
						deleteCall,
					)
				}
				wantSnapshot := ""
				if testCase.issueMode == "san-mismatch" {
					wantSnapshot = "/certs/staged/fullchain.pem"
				}
				if deleteCall.SnapshotPath != wantSnapshot {
					t.Fatalf(
						"cleanup snapshot = %q, want %q",
						deleteCall.SnapshotPath,
						wantSnapshot,
					)
				}
			}
		})
	}
}

func TestCleanupUncommittedCertificateProtectsEveryLedgerStatus(t *testing.T) {
	const domain = "cleanup-ledger.example"
	p, domainID := newIncludeMailFixture(t, domain)
	agent := &validationRestoreAgent{domain: domain}
	attachValidationRestoreAgent(t, p, agent)

	const (
		retainedCert  = "/certs/retained/fullchain.pem"
		retainedKey   = "/certs/retained/privkey.pem"
		retainedChain = "/certs/retained/chain.pem"
		lineage       = "cp-site-42-00112233445566778899aabb"
	)
	if _, err := p.db.GetDB().Exec(`
		INSERT INTO ssl_certificates (
			domain_id, type, cert_path, key_path, chain_path,
			issuer, subject, issued_at, expires_at, auto_renew,
			secure_mail, status
		) VALUES (?, 'letsencrypt', ?, ?, ?, 'R13', ?,
		          '2026-07-01T00:00:00Z', '2026-10-01T00:00:00Z',
		          1, 0, 'revoked')`,
		domainID, retainedCert, retainedKey, retainedChain, domain,
	); err != nil {
		t.Fatalf("insert retained certificate ledger row: %v", err)
	}

	p.cleanupUncommittedCertificate(
		context.Background(),
		certificateCleanupTarget{
			Domain:      domain,
			LineageName: lineage,
			CertPath:    retainedCert,
			KeyPath:     retainedKey,
			ChainPath:   retainedChain,
		},
	)
	p.cleanupUncommittedCertificate(
		context.Background(),
		certificateCleanupTarget{
			Domain:      domain,
			LineageName: lineage,
			CertPath:    "/certs/uncommitted/fullchain.pem",
			KeyPath:     "/certs/uncommitted/privkey.pem",
			ChainPath:   "/certs/uncommitted/chain.pem",
		},
	)

	agent.mu.Lock()
	defer agent.mu.Unlock()
	if len(agent.deleteCalls) != 2 {
		t.Fatalf("cleanup calls = %d, want 2", len(agent.deleteCalls))
	}
	if agent.deleteCalls[0].SnapshotPath != "" {
		t.Fatalf(
			"ledger-referenced snapshot was authorized for deletion: %#v",
			agent.deleteCalls[0],
		)
	}
	if agent.deleteCalls[1].SnapshotPath !=
		"/certs/uncommitted/fullchain.pem" {
		t.Fatalf(
			"uncommitted exact snapshot was not sent for cleanup: %#v",
			agent.deleteCalls[1],
		)
	}
}

func newValidationRestoreRenewalFixture(
	t *testing.T,
	mode string,
) (*Panel, int, int, *validationRestoreAgent) {
	t.Helper()
	p, domainID, certificateID64 := newSSLStateFixture(t)
	certificateID := int(certificateID64)
	if _, err := p.db.GetDB().Exec(`
		UPDATE ssl_certificates
		SET type = 'letsencrypt',
		    lineage_name = 'ssl-state.example',
		    acme_provider_id = 'letsencrypt',
		    issuer = 'Let''s Encrypt',
		    auto_renew = true,
		    secure_mail = false
		WHERE id = ?`, certificateID); err != nil {
		t.Fatalf("prepare renewal certificate: %v", err)
	}
	if _, err := p.db.GetDB().Exec(`
		UPDATE sites
		SET ssl_type = 'letsencrypt',
		    force_https = false,
		    hsts_enabled = false,
		    hsts_retire_after = NULL
		WHERE domain_id = ?`, domainID); err != nil {
		t.Fatalf("prepare renewal site: %v", err)
	}
	agent := &validationRestoreAgent{
		domain:    "ssl-state.example",
		renewMode: mode,
	}
	attachValidationRestoreAgent(t, p, agent)
	return p, domainID, certificateID, agent
}

func TestScheduledRenewalRestoresNormalVhostBeforeActivation(t *testing.T) {
	for _, testCase := range []struct {
		name              string
		mode              string
		wantRenewalStatus string
	}{
		{
			name:              "agent rejects renewal",
			mode:              "rejected",
			wantRenewalStatus: "failed",
		},
		{
			name:              "renewed SAN set is not exact",
			mode:              "san-mismatch",
			wantRenewalStatus: "failed",
		},
		{
			name:              "certificate is still current",
			mode:              "current",
			wantRenewalStatus: "current",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			p, domainID, certificateID, agent :=
				newValidationRestoreRenewalFixture(t, testCase.mode)

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			p.renewLetsEncrypt(
				ctx,
				certificateID,
				domainID,
				"ssl-state.example",
			)

			assertNormalVhostWasRestored(
				t,
				agent,
				"ssl-state.example",
				nil,
			)
			var (
				activeID     int
				activeCount  int
				renewalState string
			)
			if err := p.db.GetDB().QueryRow(`
				SELECT id, COALESCE(renewal_status, '')
				FROM ssl_certificates
				WHERE domain_id = ? AND status = 'active'`,
				domainID,
			).Scan(&activeID, &renewalState); err != nil {
				t.Fatalf("read active certificate: %v", err)
			}
			if activeID != certificateID {
				t.Fatalf(
					"active certificate ID = %d, want unchanged ID %d",
					activeID,
					certificateID,
				)
			}
			if renewalState != testCase.wantRenewalStatus {
				t.Fatalf(
					"renewal status = %q, want %q",
					renewalState,
					testCase.wantRenewalStatus,
				)
			}
			if err := p.db.GetDB().QueryRow(`
				SELECT COUNT(*) FROM ssl_certificates
				WHERE domain_id = ? AND status = 'active'`,
				domainID,
			).Scan(&activeCount); err != nil {
				t.Fatalf("count active certificates: %v", err)
			}
			if activeCount != 1 {
				t.Fatalf("active certificate count = %d, want 1", activeCount)
			}
		})
	}
}
