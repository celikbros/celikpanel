package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/rpc"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/transport"
)

const (
	securityAuditTestVersion = "v0.1.0-alpha.16"
	securityAuditTestCommit  = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
)

type securityAuditTestAgent struct {
	mu         sync.Mutex
	version    transport.AgentVersionResponse
	audit      transport.SecurityAuditAgentResponse
	auditCalls int
}

func (a *securityAuditTestAgent) Version(_ *transport.Empty, response *transport.AgentVersionResponse) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	*response = a.version
	return nil
}

func (a *securityAuditTestAgent) SecurityAudit(_ *transport.Empty, response *transport.SecurityAuditAgentResponse) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.auditCalls++
	*response = a.audit
	return nil
}

func validSecurityAuditTestReply() transport.SecurityAuditAgentResponse {
	unknown := transport.SecurityAuditCheck{Status: transport.SecurityAuditStatusUnknown, Code: "platform_unsupported"}
	return transport.SecurityAuditAgentResponse{
		ContractVersion: transport.SecurityAuditContractVersion,
		Capability:      transport.AgentCapabilitySecurityAuditV1,
		BuildVersion:    securityAuditTestVersion,
		BuildCommit:     securityAuditTestCommit,
		GeneratedAt:     "2026-08-13T12:00:00Z",
		Firewall: transport.SecurityAuditFirewallResponse{
			Engine: unknown, DefaultDrop: unknown, Persistence: unknown,
			TCPAllowlist: []int{}, UDPAllowlist: []int{},
		},
		Listeners: transport.SecurityAuditListenersResponse{Check: unknown, Findings: []transport.SecurityAuditListenerFinding{}},
		SSH: transport.SecurityAuditSSHResponse{
			Check: unknown, PasswordAuthentication: "unknown",
			KeyboardInteractiveAuthentication: "unknown", PermitRootLogin: "unknown",
			PubkeyAuthentication: "unknown", HostbasedAuthentication: "unknown", GSSAPIAuthentication: "unknown",
		},
		Reboot:       transport.SecurityAuditRebootResponse{Check: unknown},
		SignedUpdate: transport.SecurityAuditSignedUpdateResponse{Check: unknown},
	}
}

func newSecurityAuditPanel(t *testing.T) (*Panel, *securityAuditTestAgent) {
	t.Helper()
	agent := &securityAuditTestAgent{
		version: transport.AgentVersionResponse{
			Version: securityAuditTestVersion, Commit: securityAuditTestCommit,
			Capabilities: []string{transport.AgentCapabilitySecurityAuditV1},
		},
		audit: validSecurityAuditTestReply(),
	}
	server := rpc.NewServer()
	if err := server.RegisterName("Agent", agent); err != nil {
		t.Fatal(err)
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
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return &Panel{agentClient: transport.NewReconnectingClientWithContextConnector(client, connector)}, agent
}

func withSecurityAuditBuild(t *testing.T) {
	t.Helper()
	previousVersion, previousCommit := buildVersion, buildCommit
	buildVersion, buildCommit = securityAuditTestVersion, securityAuditTestCommit
	t.Cleanup(func() { buildVersion, buildCommit = previousVersion, previousCommit })
}

func securityAuditHTTPRequest(method, target, role string) *http.Request {
	request := httptest.NewRequest(method, target, nil)
	return request.WithContext(context.WithValue(request.Context(), callerKey, &Caller{ID: 1, Role: role}))
}

func TestSecurityAuditGETIsAdminOnlyNoStoreAndNoInput(t *testing.T) {
	withSecurityAuditBuild(t)
	panel, agent := newSecurityAuditPanel(t)

	recorder := httptest.NewRecorder()
	panel.handleSecurityAudit(recorder, securityAuditHTTPRequest(http.MethodGet, securityAuditPath, roleAdmin))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get("Cache-Control") != "no-store, max-age=0" || recorder.Header().Get("Pragma") != "no-cache" {
		t.Fatalf("cache headers = %#v", recorder.Header())
	}
	var response transport.SecurityAuditHTTPResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.ContractVersion != transport.SecurityAuditContractVersion || response.Agent.Capability != transport.AgentCapabilitySecurityAuditV1 {
		t.Fatalf("response = %#v", response)
	}

	for _, test := range []struct {
		name   string
		method string
		target string
		role   string
		body   string
		want   int
	}{
		{name: "reseller", method: http.MethodGet, target: securityAuditPath, role: roleReseller, want: http.StatusForbidden},
		{name: "mutation method", method: http.MethodPost, target: securityAuditPath, role: roleAdmin, want: http.StatusMethodNotAllowed},
		{name: "query input", method: http.MethodGet, target: securityAuditPath + "?path=/etc/shadow", role: roleAdmin, want: http.StatusBadRequest},
		{name: "body input", method: http.MethodGet, target: securityAuditPath, role: roleAdmin, body: `{}`, want: http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.target, strings.NewReader(test.body))
			request = request.WithContext(context.WithValue(request.Context(), callerKey, &Caller{ID: 1, Role: test.role}))
			recorder := httptest.NewRecorder()
			panel.handleSecurityAudit(recorder, request)
			if recorder.Code != test.want {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
		})
	}
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if agent.auditCalls != 1 {
		t.Fatalf("SecurityAudit calls=%d, want exactly one accepted GET", agent.auditCalls)
	}
}

func TestSecurityAuditBuildCapabilityAndReplyIdentityFailClosed(t *testing.T) {
	withSecurityAuditBuild(t)
	panel, agent := newSecurityAuditPanel(t)

	agent.mu.Lock()
	agent.version.Capabilities = nil
	agent.mu.Unlock()
	recorder := httptest.NewRecorder()
	panel.handleSecurityAudit(recorder, securityAuditHTTPRequest(http.MethodGet, securityAuditPath, roleAdmin))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing capability status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	agent.mu.Lock()
	agent.version.Capabilities = []string{transport.AgentCapabilitySecurityAuditV1}
	agent.audit.BuildCommit = strings.Repeat("f", 40)
	agent.mu.Unlock()
	recorder = httptest.NewRecorder()
	panel.handleSecurityAudit(recorder, securityAuditHTTPRequest(http.MethodGet, securityAuditPath, roleAdmin))
	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("changed reply identity status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestSecurityAuditRouteUsesCentralAdminGuard(t *testing.T) {
	mainSource, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(mainSource), "http.HandleFunc(securityAuditPath, panel.handleSecurityAudit)") {
		t.Fatal("security audit route is not registered")
	}
	if !isAdminOnlyPath(securityAuditPath) {
		t.Fatal("security audit route is not covered by the central admin guard")
	}
}

func securityAuditTestTLSMaterial(t *testing.T, notBefore, notAfter time.Time) ([]byte, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "panel.example.test"},
		NotBefore: notBefore, NotAfter: notAfter,
		KeyUsage:    x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
}

func TestPanelTLSSecurityAuditReportsSelfSignedExpiryAndKeyMatch(t *testing.T) {
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	certificate, key := securityAuditTestTLSMaterial(t, now.Add(-time.Hour), now.Add(10*24*time.Hour))
	result := inspectPanelTLSMaterial(certificate, key, now)
	if result.Certificate.Status != transport.SecurityAuditStatusPass ||
		result.SelfSigned.Status != transport.SecurityAuditStatusWarning || !result.IsSelfSigned ||
		result.Expiry.Status != transport.SecurityAuditStatusWarning ||
		result.KeyMatch.Status != transport.SecurityAuditStatusPass {
		t.Fatalf("TLS audit = %#v", result)
	}
	if err := transport.ValidateSecurityAuditTLSResponse(result); err != nil {
		t.Fatalf("TLS audit contract rejected: %v", err)
	}

	_, otherKey := securityAuditTestTLSMaterial(t, now.Add(-time.Hour), now.Add(365*24*time.Hour))
	mismatch := inspectPanelTLSMaterial(certificate, otherKey, now)
	if mismatch.KeyMatch.Status != transport.SecurityAuditStatusFail || mismatch.KeyMatch.Code != "panel_tls_key_mismatch" {
		t.Fatalf("mismatched key audit = %#v", mismatch)
	}

	expiredCertificate, expiredKey := securityAuditTestTLSMaterial(t, now.Add(-48*time.Hour), now.Add(-time.Hour))
	expired := inspectPanelTLSMaterial(expiredCertificate, expiredKey, now)
	if expired.Expiry.Status != transport.SecurityAuditStatusFail || expired.Expiry.Code != "panel_tls_expired" {
		t.Fatalf("expired TLS audit = %#v", expired)
	}
}

func TestActivePanelTLSPathsFollowRuntimeConfigurationWithoutMutation(t *testing.T) {
	t.Run("TLS disabled ignores stale managed material", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "panel.crt"), []byte("stale"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "panel.key"), []byte("stale"), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("CELIKPANEL_TLS", "0")
		t.Setenv("CELIKPANEL_TLS_CERT", "")
		t.Setenv("CELIKPANEL_TLS_KEY", "")
		t.Setenv("CELIKPANEL_TLS_DIR", dir)
		_, _, state := activePanelTLSPaths()
		if state != fixedPanelTLSMissing {
			t.Fatalf("disabled TLS state = %v", state)
		}
	})

	t.Run("explicit pair wins over stale managed directory", func(t *testing.T) {
		certPath := filepath.Join(t.TempDir(), "active.crt")
		keyPath := filepath.Join(t.TempDir(), "active.key")
		t.Setenv("CELIKPANEL_TLS", "1")
		t.Setenv("CELIKPANEL_TLS_CERT", certPath)
		t.Setenv("CELIKPANEL_TLS_KEY", keyPath)
		t.Setenv("CELIKPANEL_TLS_DIR", t.TempDir())
		cert, key, state := activePanelTLSPaths()
		if state != fixedPanelTLSReady || cert != certPath || key != keyPath {
			t.Fatalf("explicit TLS paths = %q %q %v", cert, key, state)
		}
	})

	t.Run("incomplete explicit pair fails closed", func(t *testing.T) {
		t.Setenv("CELIKPANEL_TLS", "1")
		t.Setenv("CELIKPANEL_TLS_CERT", "/explicit/panel.crt")
		t.Setenv("CELIKPANEL_TLS_KEY", "")
		_, _, state := activePanelTLSPaths()
		if state != fixedPanelTLSIncomplete {
			t.Fatalf("incomplete TLS state = %v", state)
		}
	})
}

func TestNonSelfSignedCertificateDoesNotClaimChainTrust(t *testing.T) {
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	rootKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	rootTemplate := x509.Certificate{
		SerialNumber: big.NewInt(41), Subject: pkix.Name{CommonName: "test root"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(365 * 24 * time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	rootDER, err := x509.CreateCertificate(rand.Reader, &rootTemplate, &rootTemplate, &rootKey.PublicKey, rootKey)
	if err != nil {
		t.Fatal(err)
	}
	root, err := x509.ParseCertificate(rootDER)
	if err != nil {
		t.Fatal(err)
	}
	leafTemplate := x509.Certificate{
		SerialNumber: big.NewInt(42), Subject: pkix.Name{CommonName: "panel.example.test"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(90 * 24 * time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, &leafTemplate, root, &leafKey.PublicKey, rootKey)
	if err != nil {
		t.Fatal(err)
	}
	leafKeyDER, err := x509.MarshalECPrivateKey(leafKey)
	if err != nil {
		t.Fatal(err)
	}
	result := inspectPanelTLSMaterial(
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: leafKeyDER}),
		now,
	)
	if result.SelfSigned.Status != transport.SecurityAuditStatusUnknown || result.SelfSigned.Code != "panel_tls_chain_unverified" {
		t.Fatalf("non-self-signed chain result = %#v", result.SelfSigned)
	}
	if err := transport.ValidateSecurityAuditTLSResponse(result); err != nil {
		t.Fatalf("TLS contract rejected unverified chain: %v", err)
	}
}

func TestLivePanelTLSProofMatchesTheServedLeaf(t *testing.T) {
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	certificatePEM, keyPEM := securityAuditTestTLSMaterial(t, now.Add(-time.Hour), now.Add(24*time.Hour))
	pair, err := tls.X509KeyPair(certificatePEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{pair},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	serveOnce := func() <-chan error {
		result := make(chan error, 1)
		go func() {
			connection, acceptErr := listener.Accept()
			if acceptErr != nil {
				result <- acceptErr
				return
			}
			defer connection.Close()
			if tlsConnection, ok := connection.(*tls.Conn); ok {
				acceptErr = tlsConnection.Handshake()
			}
			result <- acceptErr
		}()
		return result
	}
	t.Setenv("CELIKPANEL_LISTEN", listener.Addr().String())
	block, _ := pem.Decode(certificatePEM)
	if block == nil {
		t.Fatal("test certificate did not decode")
	}

	serverResult := serveOnce()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	matches, err := livePanelTLSMatches(ctx, block.Bytes)
	if err != nil || !matches {
		t.Fatalf("live TLS match = %v, %v", matches, err)
	}
	if err := <-serverResult; err != nil {
		t.Fatalf("TLS server handshake: %v", err)
	}

	serverResult = serveOnce()
	matches, err = livePanelTLSMatches(ctx, append([]byte(nil), block.Bytes[:len(block.Bytes)-1]...))
	if err != nil || matches {
		t.Fatalf("changed live TLS leaf match = %v, %v", matches, err)
	}
	if err := <-serverResult; err != nil {
		t.Fatalf("TLS server handshake: %v", err)
	}
}

func TestLivePanelTLSProofRefusesNonLoopbackDial(t *testing.T) {
	t.Setenv("CELIKPANEL_LISTEN", "192.0.2.10:2083")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := livePanelTLSMatches(ctx, []byte("leaf")); err == nil {
		t.Fatal("live TLS proof dialed a non-loopback address")
	}
}
