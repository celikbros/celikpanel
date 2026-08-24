package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/rpc"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/transport"
)

type AliasReissueApplyVhostRequest struct {
	ServerNames        []string
	ACMEChallengeNames []string
	SSLCert            string
}

type AliasReissueApplyVhostResponse struct {
	Config string
	Error  string
}

type AliasReissueIssueRequest struct {
	Domain             string
	Aliases            []string
	SubscriptionID     int
	DomainID           int
	AutoRenew          bool
	ForceRenewal       bool
	StageLineage       bool
	CurrentCertPath    string
	CurrentLineageName string
	ACMEServer         string
}

type AliasReissueIssueResponse struct {
	Success     bool
	CertPath    string
	KeyPath     string
	ChainPath   string
	ExpiresAt   time.Time
	DNSNames    []string
	LineageName string
	Error       string
}

type AliasReissueInspectRequest struct {
	CertPath string
	KeyPath  string
}

type AliasReissueInspectResponse struct {
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

type aliasReissueAgent struct {
	verifiedAPTAgentRPCFixture

	mu sync.Mutex

	oldNames []string

	applyCalls []AliasReissueApplyVhostRequest
	issueCalls []AliasReissueIssueRequest
	issueNames []string

	failApplyCall int
}

func (a *aliasReissueAgent) ApplyVhost(
	req *AliasReissueApplyVhostRequest,
	resp *AliasReissueApplyVhostResponse,
) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.applyCalls = append(a.applyCalls, AliasReissueApplyVhostRequest{
		ServerNames:        append([]string(nil), req.ServerNames...),
		ACMEChallengeNames: append([]string(nil), req.ACMEChallengeNames...),
		SSLCert:            req.SSLCert,
	})
	if a.failApplyCall > 0 && len(a.applyCalls) == a.failApplyCall {
		resp.Error = "forced final vhost failure"
		return nil
	}
	resp.Config = "ok"
	return nil
}

func (a *aliasReissueAgent) IssueLetsEncryptCertificate(
	req *AliasReissueIssueRequest,
	resp *AliasReissueIssueResponse,
) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.issueCalls = append(a.issueCalls, AliasReissueIssueRequest{
		Domain:             req.Domain,
		Aliases:            append([]string(nil), req.Aliases...),
		SubscriptionID:     req.SubscriptionID,
		DomainID:           req.DomainID,
		AutoRenew:          req.AutoRenew,
		ForceRenewal:       req.ForceRenewal,
		StageLineage:       req.StageLineage,
		CurrentCertPath:    req.CurrentCertPath,
		CurrentLineageName: req.CurrentLineageName,
		ACMEServer:         req.ACMEServer,
	})
	a.issueNames = append([]string{req.Domain}, req.Aliases...)
	*resp = AliasReissueIssueResponse{
		Success:   true,
		CertPath:  "/certs/staged-alias/fullchain.pem",
		KeyPath:   "/certs/staged-alias/privkey.pem",
		ChainPath: "/certs/staged-alias/chain.pem",
		ExpiresAt: time.Date(2026, time.October, 25, 12, 0, 0, 0, time.UTC),
		DNSNames:  append([]string(nil), a.issueNames...),
		LineageName: fmt.Sprintf(
			"cp-site-%d-a1b2c3d4a1b2c3d4a1b2c3d4",
			req.DomainID,
		),
	}
	return nil
}

func (a *aliasReissueAgent) InspectInstalledCertificate(
	req *AliasReissueInspectRequest,
	resp *AliasReissueInspectResponse,
) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	names := a.oldNames
	if req.CertPath == "/certs/staged-alias/fullchain.pem" {
		names = a.issueNames
	}
	*resp = AliasReissueInspectResponse{
		Valid:        true,
		Trusted:      true,
		TrustChecked: true,
		Issuer:       "R13",
		Subject:      "ssl-state.example",
		IssuedAt:     time.Date(2026, time.July, 27, 12, 0, 0, 0, time.UTC),
		ExpiresAt:    time.Date(2026, time.October, 25, 12, 0, 0, 0, time.UTC),
		DNSNames:     append([]string(nil), names...),
	}
	return nil
}

func attachAliasReissueAgent(
	t *testing.T,
	p *Panel,
	agent *aliasReissueAgent,
) {
	t.Helper()
	p.pkgFamilyVal = "apt"

	server := rpc.NewServer()
	if err := server.RegisterName("Agent", agent); err != nil {
		t.Fatalf("register alias reissue agent: %v", err)
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
		t.Fatalf("connect alias reissue agent: %v", err)
	}
	p.agentClient = transport.NewReconnectingClientWithContextConnector(client, connector)
	t.Cleanup(func() {
		_ = client.Close()
	})
}

func newAliasReissueHandlerFixture(
	t *testing.T,
	existingAlias string,
	failApplyCall int,
) (*Panel, int, int64, *aliasReissueAgent) {
	t.Helper()

	p, domainID, certificateID := newSSLStateFixture(t)
	db := p.db.GetDB()
	if _, err := db.Exec(`
		UPDATE sites
		SET ssl_type = 'letsencrypt',
		    force_https = false,
		    hsts_enabled = false,
		    hsts_retire_after = NULL
		WHERE domain_id = ?`, domainID); err != nil {
		t.Fatalf("prepare alias reissue site: %v", err)
	}
	if _, err := db.Exec(`
		UPDATE ssl_certificates
		SET type = 'letsencrypt',
		    lineage_name = 'ssl-state.example',
		    acme_provider_id = 'letsencrypt',
		    issuer = 'Let''s Encrypt',
		    auto_renew = true,
		    secure_mail = false
		WHERE id = ?`, certificateID); err != nil {
		t.Fatalf("prepare alias reissue certificate: %v", err)
	}

	oldNames := []string{"ssl-state.example", "www.ssl-state.example"}
	if existingAlias != "" {
		if _, err := db.Exec(`
			INSERT INTO domain_aliases (domain_id, alias)
			VALUES (?, ?)`, domainID, existingAlias); err != nil {
			t.Fatalf("insert alias reissue fixture: %v", err)
		}
		oldNames = append(oldNames, existingAlias)
	}
	agent := &aliasReissueAgent{
		oldNames:      oldNames,
		failApplyCall: failApplyCall,
	}
	attachAliasReissueAgent(t, p, agent)
	return p, domainID, certificateID, agent
}

func aliasReissueErrorCode(t *testing.T, recorder *httptest.ResponseRecorder) string {
	t.Helper()
	var response struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode coded alias response: %v; body=%s", err, recorder.Body.String())
	}
	return response.Code
}

func aliasReissueCertificateCount(t *testing.T, p *Panel, domainID int) int {
	t.Helper()
	var count int
	if err := p.db.GetDB().QueryRow(
		`SELECT COUNT(*) FROM ssl_certificates WHERE domain_id = ?`,
		domainID,
	).Scan(&count); err != nil {
		t.Fatalf("count alias reissue certificates: %v", err)
	}
	return count
}

func TestAliasReissueHandlersRequireConfirmationWithoutMutation(t *testing.T) {
	const alias = "alias.ssl-state.example"
	for _, testCase := range []struct {
		name           string
		existingAlias  string
		method         string
		path           func(int) string
		body           string
		wantAliasCount int
	}{
		{
			name:   "add",
			method: http.MethodPost,
			path: func(domainID int) string {
				return fmt.Sprintf("/api/v1/domains/%d/aliases", domainID)
			},
			body:           `{"alias":"alias.ssl-state.example"}`,
			wantAliasCount: 0,
		},
		{
			name:          "delete",
			existingAlias: alias,
			method:        http.MethodDelete,
			path: func(domainID int) string {
				return fmt.Sprintf(
					"/api/v1/domains/%d/aliases/%s",
					domainID,
					alias,
				)
			},
			wantAliasCount: 1,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			p, domainID, certificateID, agent := newAliasReissueHandlerFixture(
				t,
				testCase.existingAlias,
				0,
			)
			request := httptest.NewRequest(
				testCase.method,
				testCase.path(domainID),
				strings.NewReader(testCase.body),
			)
			recorder := httptest.NewRecorder()

			p.handleDomainAliases(recorder, request)

			if recorder.Code != http.StatusConflict {
				t.Fatalf(
					"status = %d, want %d; body=%s",
					recorder.Code,
					http.StatusConflict,
					recorder.Body.String(),
				)
			}
			if got := aliasReissueErrorCode(t, recorder); got != errCodeAliasCertificateReissueRequired {
				t.Fatalf(
					"error code = %q, want %q",
					got,
					errCodeAliasCertificateReissueRequired,
				)
			}
			if got := aliasCount(t, p, domainID, alias); got != testCase.wantAliasCount {
				t.Fatalf("alias count = %d, want %d", got, testCase.wantAliasCount)
			}
			if got := aliasReissueCertificateCount(t, p, domainID); got != 1 {
				t.Fatalf("certificate count = %d, want 1", got)
			}
			assertCertificateStatus(t, p, certificateID, "active")

			agent.mu.Lock()
			issueCalls := len(agent.issueCalls)
			applyCalls := len(agent.applyCalls)
			agent.mu.Unlock()
			if issueCalls != 0 || applyCalls != 0 {
				t.Fatalf(
					"unconfirmed request leaked agent mutations: issue=%d apply=%d",
					issueCalls,
					applyCalls,
				)
			}
		})
	}
}

func TestAliasReissueHandlersStageAndCommitConfirmedMutation(t *testing.T) {
	const alias = "alias.ssl-state.example"
	for _, testCase := range []struct {
		name           string
		existingAlias  string
		method         string
		path           func(int) string
		body           string
		wantStatus     int
		wantAliasCount int
		wantIssueNames []string
	}{
		{
			name:   "add",
			method: http.MethodPost,
			path: func(domainID int) string {
				return fmt.Sprintf("/api/v1/domains/%d/aliases", domainID)
			},
			body: `{
				"alias":"alias.ssl-state.example",
				"confirm_certificate_reissue":true
			}`,
			wantStatus:     http.StatusCreated,
			wantAliasCount: 1,
			wantIssueNames: []string{
				"ssl-state.example",
				"alias.ssl-state.example",
				"www.ssl-state.example",
			},
		},
		{
			name:          "delete",
			existingAlias: alias,
			method:        http.MethodDelete,
			path: func(domainID int) string {
				return fmt.Sprintf(
					"/api/v1/domains/%d/aliases/%s?confirm_certificate_reissue=true",
					domainID,
					alias,
				)
			},
			wantStatus:     http.StatusOK,
			wantAliasCount: 0,
			wantIssueNames: []string{
				"ssl-state.example",
				"www.ssl-state.example",
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			p, domainID, oldCertificateID, agent := newAliasReissueHandlerFixture(
				t,
				testCase.existingAlias,
				0,
			)
			request := httptest.NewRequest(
				testCase.method,
				testCase.path(domainID),
				strings.NewReader(testCase.body),
			)
			recorder := httptest.NewRecorder()

			p.handleDomainAliases(recorder, request)

			if recorder.Code != testCase.wantStatus {
				t.Fatalf(
					"status = %d, want %d; body=%s",
					recorder.Code,
					testCase.wantStatus,
					recorder.Body.String(),
				)
			}
			if got := aliasCount(t, p, domainID, alias); got != testCase.wantAliasCount {
				t.Fatalf("alias count = %d, want %d", got, testCase.wantAliasCount)
			}
			assertCertificateStatus(t, p, oldCertificateID, "revoked")

			agent.mu.Lock()
			issueCalls := append([]AliasReissueIssueRequest(nil), agent.issueCalls...)
			applyCalls := append([]AliasReissueApplyVhostRequest(nil), agent.applyCalls...)
			agent.mu.Unlock()
			if len(issueCalls) != 1 {
				t.Fatalf("issue calls = %d, want 1", len(issueCalls))
			}
			issue := issueCalls[0]
			if !issue.StageLineage || !issue.ForceRenewal {
				t.Fatalf(
					"issue staging flags = stage:%t force:%t, want true/true",
					issue.StageLineage,
					issue.ForceRenewal,
				)
			}
			if issue.CurrentCertPath != "/certs/old/fullchain.pem" {
				t.Fatalf(
					"current cert path = %q, want old immutable certificate",
					issue.CurrentCertPath,
				)
			}
			if issue.CurrentLineageName != "ssl-state.example" {
				t.Fatalf(
					"current lineage = %q, want ssl-state.example",
					issue.CurrentLineageName,
				)
			}
			gotIssueNames := append([]string{issue.Domain}, issue.Aliases...)
			if !reflect.DeepEqual(gotIssueNames, testCase.wantIssueNames) {
				t.Fatalf(
					"issued names = %#v, want %#v",
					gotIssueNames,
					testCase.wantIssueNames,
				)
			}
			if len(applyCalls) != 3 {
				t.Fatalf("vhost apply calls = %d, want validation, restore and activation", len(applyCalls))
			}
			if testCase.name == "add" {
				if !reflect.DeepEqual(
					applyCalls[0].ACMEChallengeNames,
					[]string{alias},
				) {
					t.Fatalf(
						"validation-only names = %#v, want [%s]",
						applyCalls[0].ACMEChallengeNames,
						alias,
					)
				}
				if containsFold(applyCalls[0].ServerNames, alias) {
					t.Fatalf(
						"uncommitted alias leaked into website server names: %#v",
						applyCalls[0].ServerNames,
					)
				}
			}

			var (
				activeType     string
				activePath     string
				activeLineage  string
				activeProvider string
				renewalStatus  string
				activeCount    int
			)
			wantLineage := fmt.Sprintf(
				"cp-site-%d-a1b2c3d4a1b2c3d4a1b2c3d4",
				domainID,
			)
			if err := p.db.GetDB().QueryRow(`
				SELECT type, cert_path, COALESCE(lineage_name, ''),
				       COALESCE(acme_provider_id, ''),
				       COALESCE(renewal_status, ''),
				       COUNT(*) OVER ()
				FROM ssl_certificates
				WHERE domain_id = ? AND status = 'active'`,
				domainID,
			).Scan(
				&activeType,
				&activePath,
				&activeLineage,
				&activeProvider,
				&renewalStatus,
				&activeCount,
			); err != nil {
				t.Fatalf("read committed certificate ledger: %v", err)
			}
			if activeType != "letsencrypt" ||
				activePath != "/certs/staged-alias/fullchain.pem" ||
				activeLineage != wantLineage ||
				activeProvider != "letsencrypt" ||
				renewalStatus != "" ||
				activeCount != 1 {
				t.Fatalf(
					"active ledger = type:%q path:%q lineage:%q provider:%q pending:%q count:%d",
					activeType,
					activePath,
					activeLineage,
					activeProvider,
					renewalStatus,
					activeCount,
				)
			}
		})
	}
}

func TestAliasReissueFinalVhostFailureKeepsCommittedPendingLedger(t *testing.T) {
	const alias = "pending.ssl-state.example"
	p, domainID, oldCertificateID, agent := newAliasReissueHandlerFixture(t, "", 3)
	request := httptest.NewRequest(
		http.MethodPost,
		fmt.Sprintf("/api/v1/domains/%d/aliases", domainID),
		strings.NewReader(`{
			"alias":"pending.ssl-state.example",
			"confirm_certificate_reissue":true
		}`),
	)
	recorder := httptest.NewRecorder()

	p.handleDomainAliases(recorder, request)

	if recorder.Code != http.StatusConflict {
		t.Fatalf(
			"status = %d, want %d; body=%s",
			recorder.Code,
			http.StatusConflict,
			recorder.Body.String(),
		)
	}
	if got := aliasReissueErrorCode(t, recorder); got != errCodeAliasCertificatePending {
		t.Fatalf("error code = %q, want %q", got, errCodeAliasCertificatePending)
	}
	if got := aliasCount(t, p, domainID, alias); got != 1 {
		t.Fatalf("committed alias count = %d, want 1", got)
	}
	assertCertificateStatus(t, p, oldCertificateID, "revoked")

	var (
		lineage       string
		provider      string
		renewalStatus string
		siteEnabled   bool
	)
	wantLineage := fmt.Sprintf(
		"cp-site-%d-a1b2c3d4a1b2c3d4a1b2c3d4",
		domainID,
	)
	if err := p.db.GetDB().QueryRow(`
		SELECT COALESCE(sc.lineage_name, ''),
		       COALESCE(sc.acme_provider_id, ''),
		       COALESCE(sc.renewal_status, ''),
		       COALESCE(s.ssl_enabled, false)
		FROM ssl_certificates sc
		JOIN sites s ON s.domain_id = sc.domain_id
		WHERE sc.domain_id = ? AND sc.status = 'active'`,
		domainID,
	).Scan(&lineage, &provider, &renewalStatus, &siteEnabled); err != nil {
		t.Fatalf("read pending alias certificate ledger: %v", err)
	}
	if lineage != wantLineage ||
		provider != "letsencrypt" ||
		renewalStatus != sslPendingActivation ||
		siteEnabled {
		t.Fatalf(
			"pending ledger = lineage:%q provider:%q status:%q site-enabled:%t",
			lineage,
			provider,
			renewalStatus,
			siteEnabled,
		)
	}

	agent.mu.Lock()
	issueCalls := len(agent.issueCalls)
	applyCalls := len(agent.applyCalls)
	agent.mu.Unlock()
	if issueCalls != 1 || applyCalls != 3 {
		t.Fatalf(
			"pending flow calls = issue:%d apply:%d, want issue:1 apply:3",
			issueCalls,
			applyCalls,
		)
	}
}

func containsFold(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(value, target) {
			return true
		}
	}
	return false
}
