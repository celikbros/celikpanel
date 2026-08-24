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

	"github.com/alicelik/celikpanel/internal/hostingpath"
	"github.com/alicelik/celikpanel/internal/transport"
)

type IncludeMailApplyVhostRequest struct {
	ServerNames        []string
	ACMEChallengeNames []string
}

type IncludeMailApplyVhostResponse struct {
	Error string
}

type IncludeMailIssueRequest struct {
	Domain  string
	Aliases []string
}

type IncludeMailIssueResponse struct {
	Success     bool
	CertPath    string
	KeyPath     string
	ChainPath   string
	ExpiresAt   time.Time
	DNSNames    []string
	LineageName string
	Error       string
}

type IncludeMailInspectRequest struct {
	CertPath string
	KeyPath  string
}

type IncludeMailInspectResponse struct {
	Valid        bool
	Trusted      bool
	TrustChecked bool
	DNSNames     []string
}

type includeMailRPCAgent struct {
	verifiedAPTAgentRPCFixture

	mu          sync.Mutex
	applyCalls  []IncludeMailApplyVhostRequest
	issueCalls  []IncludeMailIssueRequest
	issuedNames []string
}

func (a *includeMailRPCAgent) ApplyVhost(
	req *IncludeMailApplyVhostRequest,
	resp *IncludeMailApplyVhostResponse,
) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.applyCalls = append(a.applyCalls, IncludeMailApplyVhostRequest{
		ServerNames:        append([]string(nil), req.ServerNames...),
		ACMEChallengeNames: append([]string(nil), req.ACMEChallengeNames...),
	})
	return nil
}

func (a *includeMailRPCAgent) IssueLetsEncryptCertificate(
	req *IncludeMailIssueRequest,
	resp *IncludeMailIssueResponse,
) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.issueCalls = append(a.issueCalls, IncludeMailIssueRequest{
		Domain:  req.Domain,
		Aliases: append([]string(nil), req.Aliases...),
	})
	a.issuedNames = append([]string{req.Domain}, req.Aliases...)
	*resp = IncludeMailIssueResponse{
		Success:     true,
		CertPath:    "/certificates/example/fullchain.pem",
		KeyPath:     "/certificates/example/privkey.pem",
		ChainPath:   "/certificates/example/chain.pem",
		ExpiresAt:   time.Now().UTC().Add(90 * 24 * time.Hour),
		DNSNames:    append([]string(nil), a.issuedNames...),
		LineageName: req.Domain,
	}
	return nil
}

func (a *includeMailRPCAgent) InspectInstalledCertificate(
	_ *IncludeMailInspectRequest,
	resp *IncludeMailInspectResponse,
) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	*resp = IncludeMailInspectResponse{
		Valid:        true,
		Trusted:      true,
		TrustChecked: true,
		DNSNames:     append([]string(nil), a.issuedNames...),
	}
	return nil
}

func attachIncludeMailAgent(t *testing.T, p *Panel, agent *includeMailRPCAgent) {
	t.Helper()
	p.pkgFamilyVal = "apt"
	server := rpc.NewServer()
	if err := server.RegisterName("Agent", agent); err != nil {
		t.Fatalf("register include-mail agent: %v", err)
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
		t.Fatalf("connect include-mail agent: %v", err)
	}
	p.agentClient = transport.NewReconnectingClientWithContextConnector(client, connector)
	t.Cleanup(func() {
		_ = client.Close()
	})
}

func newIncludeMailFixture(t *testing.T, domainName string) (*Panel, int) {
	t.Helper()
	p := newDNSPanelForTest(t)
	db := p.db.GetDB()
	userResult, err := db.Exec(`
		INSERT INTO users (username, password_hash, email, role)
		VALUES (?, 'hash', ?, 'customer')`,
		"include-mail-"+strings.ReplaceAll(domainName, ".", "-"),
		"owner@"+domainName,
	)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	userID, err := userResult.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	subscriptionResult, err := db.Exec(`
		INSERT INTO subscriptions (owner_id, name)
		VALUES (?, 'Include mail')`, userID)
	if err != nil {
		t.Fatalf("insert subscription: %v", err)
	}
	subscriptionID, err := subscriptionResult.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	domainResult, err := db.Exec(`
		INSERT INTO domains (subscription_id, name, status)
		VALUES (?, ?, 'active')`, subscriptionID, domainName)
	if err != nil {
		t.Fatalf("insert domain: %v", err)
	}
	domainID64, err := domainResult.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	domainID := int(domainID64)
	documentRoot, err := hostingpath.DocumentRoot(int(subscriptionID), domainID)
	if err != nil {
		t.Fatalf("derive document root: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO sites (domain_id, document_root, project_type, ssl_enabled, ssl_type)
		VALUES (?, ?, 'static', false, 'none')`,
		domainID,
		documentRoot,
	); err != nil {
		t.Fatalf("insert site: %v", err)
	}
	return p, domainID
}

func TestIssueCertificateIncludeMailIsDerivedAndChallengeIsIsolated(t *testing.T) {
	for _, testCase := range []struct {
		name          string
		includeMail   bool
		wantAliases   []string
		wantChallenge []string
	}{
		{
			name:          "included",
			includeMail:   true,
			wantAliases:   []string{"www.example.test", "mail.example.test"},
			wantChallenge: []string{"mail.example.test"},
		},
		{
			name:          "not included",
			includeMail:   false,
			wantAliases:   []string{"www.example.test"},
			wantChallenge: nil,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			p, domainID := newIncludeMailFixture(t, "example.test")
			agent := &includeMailRPCAgent{}
			attachIncludeMailAgent(t, p, agent)

			body := fmt.Sprintf(
				`{"email":"admin@example.test","auto_renew":true,"include_mail":%t,"mail_hostname":"attacker.test"}`,
				testCase.includeMail,
			)
			request := httptest.NewRequest(
				http.MethodPost,
				fmt.Sprintf("/api/v1/domains/%d/ssl/letsencrypt", domainID),
				strings.NewReader(body),
			)
			recorder := httptest.NewRecorder()
			p.handleIssueLetsEncrypt(recorder, request)
			if recorder.Code != http.StatusOK {
				t.Fatalf("issue status = %d, body=%s", recorder.Code, recorder.Body.String())
			}

			agent.mu.Lock()
			defer agent.mu.Unlock()
			if len(agent.issueCalls) != 1 {
				t.Fatalf("issue calls = %d, want 1", len(agent.issueCalls))
			}
			if got := agent.issueCalls[0].Aliases; !equalStringSlices(got, testCase.wantAliases) {
				t.Fatalf("certificate aliases = %#v, want %#v", got, testCase.wantAliases)
			}
			for _, alias := range agent.issueCalls[0].Aliases {
				if alias == "attacker.test" {
					t.Fatal("request-supplied mail hostname reached the certificate agent")
				}
			}
			if len(agent.applyCalls) != 2 {
				t.Fatalf("apply vhost calls = %d, want validation + activation", len(agent.applyCalls))
			}
			for index, call := range agent.applyCalls {
				if got := call.ACMEChallengeNames; !equalStringSlices(got, testCase.wantChallenge) {
					t.Fatalf("apply call %d challenge names = %#v, want %#v", index, got, testCase.wantChallenge)
				}
				for _, serverName := range call.ServerNames {
					if serverName == "mail.example.test" {
						t.Fatalf("apply call %d leaked mail identity into website server names", index)
					}
				}
			}
		})
	}
}

func TestCreateDomainRejectsPrimaryWhoseDerivedMailNameIsTooLong(t *testing.T) {
	primary := strings.Join([]string{
		strings.Repeat("a", 63),
		strings.Repeat("b", 63),
		strings.Repeat("c", 63),
		strings.Repeat("d", 61),
	}, ".")
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/domains",
		strings.NewReader(fmt.Sprintf(`{"domain":%q,"project_type":"static"}`, primary)),
	)
	recorder := httptest.NewRecorder()
	(&Panel{}).handleCreateDomain(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("create status = %d, want 400; body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(strings.ToLower(recorder.Body.String()), "derived mail hostname") {
		t.Fatalf("create error is not actionable: %s", recorder.Body.String())
	}
}

func equalStringSlices(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}
