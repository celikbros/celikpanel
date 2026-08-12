package main

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"mime/multipart"
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

type CustomGuardValidateRequest struct {
	Domain string
}

type CustomGuardValidateResponse struct {
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

type CustomGuardInstallRequest struct {
	Domain string
}

type CustomGuardInstallResponse struct {
	Success   bool
	CertPath  string
	KeyPath   string
	ChainPath string
	Error     string
}

type CustomGuardInspectRequest struct {
	Domain    string
	CertPath  string
	KeyPath   string
	ChainPath string
}

type CustomGuardInspectResponse struct {
	Valid        bool
	Trusted      bool
	TrustChecked bool
	Issuer       string
	Subject      string
	IssuedAt     time.Time
	ExpiresAt    time.Time
	DNSNames     []string
}

type CustomGuardApplyVhostRequest struct{}

type CustomGuardApplyVhostResponse struct {
	Error string
}

type CustomGuardMailSNI struct {
	Names    []string
	CertPath string
	KeyPath  string
}

type CustomGuardSecureMailRequest struct {
	Myhostname string
	SNI        []CustomGuardMailSNI
}

type CustomGuardSecureMailResponse struct {
	Configured  bool
	DefaultCert string
	SNICount    int
	Error       string
}

const customGuardCertificateVersionRoot = "/etc/ssl/celikpanel/ssl-state.example/sha256-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

type customCertificateGuardAgent struct {
	*serviceOperationTestAgent

	mu sync.Mutex

	trustChecked bool
	trusted      bool
	dnsNames     []string

	validateCalls int
	installCalls  int
	inspectCalls  int
	applyCalls    int
	mailCalls     int

	applyVhostError string
	secureMailError string
	omitChainPath   bool
}

func (a *customCertificateGuardAgent) ValidateCertificate(
	req *CustomGuardValidateRequest,
	resp *CustomGuardValidateResponse,
) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.validateCalls++
	*resp = CustomGuardValidateResponse{
		Valid:        true,
		Trusted:      a.trusted,
		TrustChecked: a.trustChecked,
		Issuer:       "Replacement CA",
		Subject:      req.Domain,
		IssuedAt:     time.Now().UTC().Add(-time.Hour),
		ExpiresAt:    time.Now().UTC().Add(90 * 24 * time.Hour),
		DNSNames:     append([]string(nil), a.dnsNames...),
	}
	return nil
}

func (a *customCertificateGuardAgent) InstallCustomCertificate(
	_ *CustomGuardInstallRequest,
	resp *CustomGuardInstallResponse,
) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.installCalls++
	chainPath := "/certs/replacement/chain.pem"
	if a.omitChainPath {
		chainPath = ""
	}
	*resp = CustomGuardInstallResponse{
		Success:   true,
		CertPath:  customGuardCertificateVersionRoot + "/fullchain.pem",
		KeyPath:   customGuardCertificateVersionRoot + "/privkey.pem",
		ChainPath: chainPath,
	}
	return nil
}

func (a *customCertificateGuardAgent) InspectInstalledCertificate(
	req *CustomGuardInspectRequest,
	resp *CustomGuardInspectResponse,
) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.inspectCalls++
	*resp = CustomGuardInspectResponse{
		Valid:        true,
		Trusted:      true,
		TrustChecked: true,
		Issuer:       "Installed Replacement CA",
		Subject:      req.Domain,
		IssuedAt:     time.Now().UTC().Add(-time.Hour),
		ExpiresAt:    time.Now().UTC().Add(90 * 24 * time.Hour),
		DNSNames:     append([]string(nil), a.dnsNames...),
	}
	return nil
}

func (a *customCertificateGuardAgent) ApplyVhost(
	_ *CustomGuardApplyVhostRequest,
	resp *CustomGuardApplyVhostResponse,
) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.applyCalls++
	if a.applyVhostError != "" {
		resp.Error = a.applyVhostError
	}
	return nil
}

func (a *customCertificateGuardAgent) SecureMailTLS(
	req *CustomGuardSecureMailRequest,
	resp *CustomGuardSecureMailResponse,
) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.mailCalls++
	if a.secureMailError != "" {
		resp.Error = a.secureMailError
		return nil
	}
	resp.Configured = true
	resp.DefaultCert = transport.DefaultMailTLSCertificatePath
	resp.SNICount = len(req.SNI)
	return nil
}

func (a *customCertificateGuardAgent) SyncMailTLSV2(
	req *transport.SyncMailTLSV2Request,
	resp *transport.SecureMailTLSResponse,
) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.mailCalls++
	if a.secureMailError != "" {
		resp.Error = a.secureMailError
		return nil
	}
	resp.Configured = true
	resp.DefaultCert = transport.DefaultMailTLSCertificatePath
	resp.SNICount = len(req.SNI)
	return nil
}

func attachCustomCertificateGuardAgent(
	t *testing.T,
	p *Panel,
	agent *customCertificateGuardAgent,
) {
	t.Helper()
	if agent.serviceOperationTestAgent == nil {
		agent.serviceOperationTestAgent = newServiceOperationTestAgent()
	}
	p.pkgFamilyVal = "apt"
	previousHostname := readMailTLSHostname
	readMailTLSHostname = func() (string, error) { return "guard.panel.test", nil }
	server := rpc.NewServer()
	if err := server.RegisterName("Agent", agent); err != nil {
		t.Fatalf("register custom-certificate guard agent: %v", err)
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
		t.Fatalf("connect custom-certificate guard agent: %v", err)
	}
	p.agentClient = transport.NewReconnectingClientWithContextConnector(client, connector)
	t.Cleanup(func() {
		readMailTLSHostname = previousHostname
		_ = client.Close()
	})
}

func customCertificateUploadRequest(t *testing.T, domainID int) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for name, content := range map[string]string{
		"certificate": "replacement certificate",
		"private_key": "replacement private key",
		"chain":       "replacement chain",
	} {
		part, err := writer.CreateFormFile(name, name+".pem")
		if err != nil {
			t.Fatalf("create %s upload part: %v", name, err)
		}
		if _, err := part.Write([]byte(content)); err != nil {
			t.Fatalf("write %s upload part: %v", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	request := httptest.NewRequest(
		http.MethodPost,
		fmt.Sprintf("/api/v1/domains/%d/ssl/upload", domainID),
		&body,
	)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	return request
}

type customCertificateDBSnapshot struct {
	certificateCount int
	activeID         int64
	activePath       string
	activeSecureMail bool
	siteCertPath     sql.NullString
	siteKeyPath      sql.NullString
}

func readCustomCertificateDBSnapshot(
	t *testing.T,
	p *Panel,
	domainID int,
) customCertificateDBSnapshot {
	t.Helper()
	db := p.db.GetDB()
	var snapshot customCertificateDBSnapshot
	if err := db.QueryRow(`
		SELECT COUNT(*) FROM ssl_certificates WHERE domain_id = ?`,
		domainID,
	).Scan(&snapshot.certificateCount); err != nil {
		t.Fatalf("count certificates: %v", err)
	}
	if err := db.QueryRow(`
		SELECT id, cert_path, COALESCE(secure_mail, false)
		FROM ssl_certificates
		WHERE domain_id = ? AND status = 'active'`,
		domainID,
	).Scan(
		&snapshot.activeID,
		&snapshot.activePath,
		&snapshot.activeSecureMail,
	); err != nil {
		t.Fatalf("read active certificate: %v", err)
	}
	if err := db.QueryRow(`
		SELECT ssl_cert_path, ssl_key_path FROM sites WHERE domain_id = ?`,
		domainID,
	).Scan(&snapshot.siteCertPath, &snapshot.siteKeyPath); err != nil {
		t.Fatalf("read site certificate paths: %v", err)
	}
	return snapshot
}

func TestCustomCertificateReplacementStrictGuard(t *testing.T) {
	futureRetirement := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	for _, testCase := range []struct {
		name           string
		forceHTTPS     bool
		hstsEnabled    bool
		retireAfter    any
		secureMail     bool
		trustChecked   bool
		trusted        bool
		dnsNames       []string
		wantStatus     int
		wantMessage    string
		wantInstalled  bool
		omitChainPath  bool
		wantInspectMin int
		wantMailCalls  int
	}{
		{
			name:         "untrusted force HTTPS",
			forceHTTPS:   true,
			dnsNames:     []string{"ssl-state.example", "www.ssl-state.example"},
			wantStatus:   http.StatusConflict,
			wantMessage:  "not trusted",
			trustChecked: true,
		},
		{
			name:         "trust unchecked HSTS",
			hstsEnabled:  true,
			trusted:      true,
			dnsNames:     []string{"ssl-state.example", "www.ssl-state.example"},
			wantStatus:   http.StatusConflict,
			wantMessage:  "not trusted",
			trustChecked: false,
		},
		{
			name:         "untrusted future HSTS retirement",
			retireAfter:  futureRetirement,
			dnsNames:     []string{"ssl-state.example", "www.ssl-state.example"},
			wantStatus:   http.StatusConflict,
			wantMessage:  "not trusted",
			trustChecked: true,
		},
		{
			name:         "untrusted secure mail",
			secureMail:   true,
			dnsNames:     []string{"ssl-state.example", "www.ssl-state.example", "mail.ssl-state.example"},
			wantStatus:   http.StatusConflict,
			wantMessage:  "not trusted",
			trustChecked: true,
		},
		{
			name:         "trusted certificate missing mail SAN",
			secureMail:   true,
			trustChecked: true,
			trusted:      true,
			dnsNames:     []string{"ssl-state.example", "www.ssl-state.example"},
			wantStatus:   http.StatusConflict,
			wantMessage:  "does not cover mail.ssl-state.example",
		},
		{
			name:         "malformed HSTS retirement fails closed",
			retireAfter:  "not-a-time",
			trustChecked: true,
			trusted:      true,
			dnsNames:     []string{"ssl-state.example", "www.ssl-state.example"},
			wantStatus:   http.StatusInternalServerError,
		},
		{
			name:           "trusted replacement preserves protected services",
			forceHTTPS:     true,
			hstsEnabled:    true,
			retireAfter:    futureRetirement,
			secureMail:     true,
			trustChecked:   true,
			trusted:        true,
			dnsNames:       []string{"ssl-state.example", "www.ssl-state.example", "mail.ssl-state.example"},
			wantStatus:     http.StatusOK,
			wantInstalled:  true,
			wantInspectMin: 2,
			wantMailCalls:  1,
		},
		{
			name:           "trusted leaf-only certificate keeps optional chain empty",
			trustChecked:   true,
			trusted:        true,
			dnsNames:       []string{"ssl-state.example", "www.ssl-state.example"},
			wantStatus:     http.StatusOK,
			wantInstalled:  true,
			omitChainPath:  true,
			wantInspectMin: 1,
			wantMailCalls:  0,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			p, domainID, oldCertificateID := newSSLStateFixture(t)
			db := p.db.GetDB()
			if _, err := db.Exec(`
				UPDATE sites
				SET force_https = ?, hsts_enabled = ?, hsts_retire_after = ?
				WHERE domain_id = ?`,
				testCase.forceHTTPS,
				testCase.hstsEnabled,
				testCase.retireAfter,
				domainID,
			); err != nil {
				t.Fatalf("prepare site state: %v", err)
			}
			if _, err := db.Exec(`
				UPDATE ssl_certificates SET secure_mail = ?
				WHERE id = ?`,
				testCase.secureMail,
				oldCertificateID,
			); err != nil {
				t.Fatalf("prepare mail state: %v", err)
			}

			agent := &customCertificateGuardAgent{
				trustChecked:  testCase.trustChecked,
				trusted:       testCase.trusted,
				dnsNames:      append([]string(nil), testCase.dnsNames...),
				omitChainPath: testCase.omitChainPath,
			}
			attachCustomCertificateGuardAgent(t, p, agent)
			before := readCustomCertificateDBSnapshot(t, p, domainID)

			recorder := httptest.NewRecorder()
			p.handleUploadCertificate(
				recorder,
				customCertificateUploadRequest(t, domainID),
			)
			if recorder.Code != testCase.wantStatus {
				t.Fatalf(
					"upload status = %d, want %d; body=%s",
					recorder.Code,
					testCase.wantStatus,
					recorder.Body.String(),
				)
			}
			if testCase.wantMessage != "" &&
				!strings.Contains(recorder.Body.String(), testCase.wantMessage) {
				t.Fatalf(
					"upload body %q does not contain %q",
					recorder.Body.String(),
					testCase.wantMessage,
				)
			}

			agent.mu.Lock()
			validateCalls := agent.validateCalls
			installCalls := agent.installCalls
			inspectCalls := agent.inspectCalls
			applyCalls := agent.applyCalls
			mailCalls := agent.mailCalls
			agent.mu.Unlock()
			if validateCalls != 1 {
				t.Fatalf("validation calls = %d, want 1", validateCalls)
			}

			if !testCase.wantInstalled {
				if installCalls != 0 || inspectCalls != 0 ||
					applyCalls != 0 || mailCalls != 0 {
					t.Fatalf(
						"guarded request leaked side effects: install=%d inspect=%d apply=%d mail=%d",
						installCalls,
						inspectCalls,
						applyCalls,
						mailCalls,
					)
				}
				after := readCustomCertificateDBSnapshot(t, p, domainID)
				if after != before {
					t.Fatalf("guarded request changed certificate state: before=%+v after=%+v", before, after)
				}
				return
			}

			if installCalls != 1 ||
				applyCalls != 1 ||
				inspectCalls < testCase.wantInspectMin ||
				mailCalls != testCase.wantMailCalls {
				t.Fatalf(
					"trusted replacement calls: install=%d inspect=%d apply=%d mail=%d",
					installCalls,
					inspectCalls,
					applyCalls,
					mailCalls,
				)
			}
			after := readCustomCertificateDBSnapshot(t, p, domainID)
			if after.certificateCount != before.certificateCount+1 {
				t.Fatalf(
					"certificate count = %d, want %d",
					after.certificateCount,
					before.certificateCount+1,
				)
			}
			if after.activeID == before.activeID ||
				after.activePath != customGuardCertificateVersionRoot+"/fullchain.pem" ||
				after.activeSecureMail != testCase.secureMail {
				t.Fatalf("trusted replacement active state = %+v", after)
			}
			assertCertificateStatus(t, p, oldCertificateID, "revoked")
		})
	}
}

func TestCustomCertificateActivationFailureKeepsDurablePendingSnapshot(
	t *testing.T,
) {
	for _, testCase := range []struct {
		name            string
		applyVhostError string
		secureMailError string
		wantPending     string
		wantSiteEnabled bool
		wantApplyCalls  int
		wantMailCalls   int
	}{
		{
			name:            "vhost failure",
			applyVhostError: "forced vhost failure",
			wantPending:     sslPendingActivation,
			wantSiteEnabled: false,
			wantApplyCalls:  1,
		},
		{
			name:            "dependent failure",
			secureMailError: "forced mail TLS failure",
			wantPending:     sslPendingDependents,
			wantSiteEnabled: true,
			wantApplyCalls:  1,
			wantMailCalls:   1,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			p, domainID, oldCertificateID := newSSLStateFixture(t)
			agent := &customCertificateGuardAgent{
				trustChecked: true,
				trusted:      true,
				dnsNames: []string{
					"ssl-state.example",
					"www.ssl-state.example",
					"mail.ssl-state.example",
				},
				applyVhostError: testCase.applyVhostError,
				secureMailError: testCase.secureMailError,
			}
			attachCustomCertificateGuardAgent(t, p, agent)

			recorder := httptest.NewRecorder()
			p.handleUploadCertificate(
				recorder,
				customCertificateUploadRequest(t, domainID),
			)
			if recorder.Code != http.StatusConflict {
				t.Fatalf(
					"upload status = %d, want %d; body=%s",
					recorder.Code,
					http.StatusConflict,
					recorder.Body.String(),
				)
			}
			assertCertificateStatus(t, p, oldCertificateID, "revoked")

			var (
				activeID    int64
				activePath  string
				pending     string
				siteEnabled bool
				activeCount int
			)
			if err := p.db.GetDB().QueryRow(`
				SELECT sc.id, sc.cert_path,
				       COALESCE(sc.renewal_status, ''),
				       COALESCE(s.ssl_enabled, false),
				       COUNT(*) OVER ()
				FROM ssl_certificates sc
				JOIN sites s ON s.domain_id = sc.domain_id
				WHERE sc.domain_id = ? AND sc.status = 'active'`,
				domainID,
			).Scan(
				&activeID,
				&activePath,
				&pending,
				&siteEnabled,
				&activeCount,
			); err != nil {
				t.Fatalf("read pending custom certificate state: %v", err)
			}
			if activeID == oldCertificateID ||
				activePath != customGuardCertificateVersionRoot+"/fullchain.pem" ||
				pending != testCase.wantPending ||
				siteEnabled != testCase.wantSiteEnabled ||
				activeCount != 1 {
				t.Fatalf(
					"pending custom state = id:%d path:%q pending:%q site-enabled:%t active-count:%d",
					activeID,
					activePath,
					pending,
					siteEnabled,
					activeCount,
				)
			}

			agent.mu.Lock()
			applyCalls := agent.applyCalls
			mailCalls := agent.mailCalls
			agent.mu.Unlock()
			if applyCalls != testCase.wantApplyCalls ||
				mailCalls != testCase.wantMailCalls {
				t.Fatalf(
					"external calls = apply:%d mail:%d, want apply:%d mail:%d",
					applyCalls,
					mailCalls,
					testCase.wantApplyCalls,
					testCase.wantMailCalls,
				)
			}
		})
	}
}
