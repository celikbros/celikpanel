package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/transport"
)

const securityAuditPath = "/api/v1/security/audit"

var errPanelTLSMetadataUnsafe = errors.New("panel TLS path metadata is unsafe")

func (p *Panel) requireSecurityAuditAgent(ctx context.Context) (transport.AgentVersionResponse, error) {
	panelVersion := strings.TrimSpace(buildVersion)
	panelCommit := strings.TrimSpace(buildCommit)
	if !validPanelUpdateVersion(panelVersion) || !panelUpdateCommitPattern.MatchString(panelCommit) {
		return transport.AgentVersionResponse{}, errors.New("panel build identity is unavailable")
	}
	var agent transport.AgentVersionResponse
	if err := p.callAgentContext(ctx, "Agent.Version", &transport.Empty{}, &agent); err != nil {
		return transport.AgentVersionResponse{}, fmt.Errorf("verify security audit agent identity: %w", err)
	}
	if strings.TrimSpace(agent.Version) != panelVersion || strings.TrimSpace(agent.Commit) != panelCommit {
		return transport.AgentVersionResponse{}, errors.New("panel and security audit agent build identities do not match")
	}
	if err := requireKnownAgentCapabilities(agent.Capabilities, transport.AgentCapabilitySecurityAuditV1); err != nil {
		return transport.AgentVersionResponse{}, fmt.Errorf("verify security audit agent capability: %w", err)
	}
	return agent, nil
}

// handleSecurityAudit exposes one administrator-only, no-input GET. It never
// writes host state and does not offer an auto-fix or arbitrary probe surface.
func (p *Panel) handleSecurityAudit(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Content-Type", "application/json")
	caller := currentCaller(r)
	if caller == nil || !caller.hasAccountRole(roleAdmin) {
		writeCodedError(w, http.StatusForbidden, "ADMIN_ONLY", "administrator access is required", "")
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeClientError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if r.URL.RawQuery != "" || r.ContentLength != 0 {
		writeClientError(w, http.StatusBadRequest, "security audit accepts no input")
		return
	}
	identity, err := p.requireSecurityAuditAgent(r.Context())
	if err != nil {
		writeCodedError(w, http.StatusServiceUnavailable, "security_audit_agent_incompatible", "security audit requires the matching panel and agent release", "")
		return
	}
	var agent transport.SecurityAuditAgentResponse
	if err := p.callAgentContext(r.Context(), "Agent.SecurityAudit", &transport.Empty{}, &agent); err != nil {
		writeServerError(w, fmt.Errorf("run security audit: %w", err))
		return
	}
	if err := transport.ValidateSecurityAuditAgentResponse(agent); err != nil ||
		agent.BuildVersion != identity.Version || agent.BuildCommit != identity.Commit {
		writeCodedError(w, http.StatusBadGateway, "security_audit_response_invalid", "the security audit response could not be verified", "")
		return
	}
	// net/rpc's gob transport does not preserve the distinction between a nil
	// slice and an empty slice. Canonicalize the browser-facing JSON boundary
	// after validation so bounded empty collections are always encoded as []
	// rather than null without changing the internal agent RPC contract.
	agent.Firewall.TCPAllowlist = append([]int{}, agent.Firewall.TCPAllowlist...)
	agent.Firewall.UDPAllowlist = append([]int{}, agent.Firewall.UDPAllowlist...)
	agent.Listeners.Findings = append([]transport.SecurityAuditListenerFinding{}, agent.Listeners.Findings...)
	tlsResult := inspectActivePanelTLS(r.Context(), time.Now().UTC())
	if err := transport.ValidateSecurityAuditTLSResponse(tlsResult); err != nil {
		writeCodedError(w, http.StatusInternalServerError, "security_audit_tls_invalid", "the local panel TLS audit could not be verified", "")
		return
	}
	response := transport.SecurityAuditHTTPResponse{
		ContractVersion: transport.SecurityAuditContractVersion,
		GeneratedAt:     agent.GeneratedAt,
		Agent:           agent,
		TLS:             tlsResult,
	}
	if err := json.NewEncoder(w).Encode(response); err != nil {
		return
	}
}

type fixedPanelTLSPathState uint8

const (
	fixedPanelTLSReady fixedPanelTLSPathState = iota
	fixedPanelTLSMissing
	fixedPanelTLSIncomplete
	fixedPanelTLSUnreadable
)

func inspectActivePanelTLS(ctx context.Context, now time.Time) transport.SecurityAuditTLSResponse {
	unknown := transport.SecurityAuditCheck{Status: transport.SecurityAuditStatusUnknown, Code: "panel_tls_unknown"}
	response := transport.SecurityAuditTLSResponse{
		Certificate: transport.SecurityAuditCheck{Status: transport.SecurityAuditStatusUnknown, Code: "panel_tls_not_managed"},
		SelfSigned:  unknown, Expiry: unknown, KeyMatch: unknown,
	}
	certPath, keyPath, state := activePanelTLSPaths()
	switch state {
	case fixedPanelTLSMissing:
		return response
	case fixedPanelTLSIncomplete:
		response.Certificate = transport.SecurityAuditCheck{Status: transport.SecurityAuditStatusFail, Code: "panel_tls_incomplete"}
		return response
	case fixedPanelTLSUnreadable:
		response.Certificate = transport.SecurityAuditCheck{Status: transport.SecurityAuditStatusUnknown, Code: "panel_tls_unreadable"}
		return response
	}
	certPEM, keyPEM, err := readPinnedPanelTLSFiles(certPath, keyPath, 1<<20, 64<<10)
	if err != nil {
		response.Certificate = panelTLSReadFailure(err)
		return response
	}
	result := inspectPanelTLSMaterial(certPEM, keyPEM, now)
	if result.Certificate.Status != transport.SecurityAuditStatusPass {
		return result
	}
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return unknownPanelTLSResponse("panel_tls_live_unverified")
	}
	liveMatches, err := livePanelTLSMatches(ctx, block.Bytes)
	if err != nil {
		return unknownPanelTLSResponse("panel_tls_live_unverified")
	}
	if !liveMatches {
		return nonAuthoritativePanelTLSResponse(transport.SecurityAuditStatusFail, "panel_tls_live_mismatch")
	}
	return result
}

func panelTLSReadFailure(err error) transport.SecurityAuditCheck {
	if errors.Is(err, errPanelTLSMetadataUnsafe) {
		return transport.SecurityAuditCheck{Status: transport.SecurityAuditStatusFail, Code: "panel_tls_metadata_unsafe"}
	}
	return transport.SecurityAuditCheck{Status: transport.SecurityAuditStatusUnknown, Code: "panel_tls_unreadable"}
}

func unknownPanelTLSResponse(code string) transport.SecurityAuditTLSResponse {
	return nonAuthoritativePanelTLSResponse(transport.SecurityAuditStatusUnknown, code)
}

func nonAuthoritativePanelTLSResponse(status, code string) transport.SecurityAuditTLSResponse {
	unknown := transport.SecurityAuditCheck{Status: transport.SecurityAuditStatusUnknown, Code: "panel_tls_unknown"}
	return transport.SecurityAuditTLSResponse{
		Certificate: transport.SecurityAuditCheck{Status: status, Code: code},
		SelfSigned:  unknown, Expiry: unknown, KeyMatch: unknown,
	}
}

func livePanelTLSMatches(ctx context.Context, expectedLeaf []byte) (bool, error) {
	host, portText, err := net.SplitHostPort(listenAddr())
	if err != nil {
		return false, err
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return false, errors.New("panel TLS listen port is invalid")
	}
	switch host {
	case "", "0.0.0.0":
		host = "127.0.0.1"
	case "::", "[::]":
		host = "::1"
	default:
		address := net.ParseIP(host)
		if address == nil || !address.IsLoopback() {
			return false, errors.New("panel TLS listener is not safe to self-dial")
		}
	}
	dialer := &tls.Dialer{
		NetDialer: &net.Dialer{Timeout: 2 * time.Second},
		Config: &tls.Config{
			MinVersion: tls.VersionTLS12,
			// This connection proves byte identity with the separately parsed
			// configured leaf; CA and hostname trust are deliberately not inferred.
			InsecureSkipVerify: true, //nolint:gosec
		},
	}
	connection, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, portText))
	if err != nil {
		return false, err
	}
	defer connection.Close()
	tlsConnection, ok := connection.(*tls.Conn)
	if !ok {
		return false, errors.New("panel TLS self-dial did not negotiate TLS")
	}
	peerCertificates := tlsConnection.ConnectionState().PeerCertificates
	if len(peerCertificates) == 0 {
		return false, errors.New("panel TLS self-dial returned no certificate")
	}
	return bytes.Equal(peerCertificates[0].Raw, expectedLeaf), nil
}

func inspectPanelTLSMaterial(certPEM, keyPEM []byte, now time.Time) transport.SecurityAuditTLSResponse {
	unknown := transport.SecurityAuditCheck{Status: transport.SecurityAuditStatusUnknown, Code: "panel_tls_unknown"}
	response := transport.SecurityAuditTLSResponse{
		Certificate: transport.SecurityAuditCheck{Status: transport.SecurityAuditStatusUnknown, Code: "panel_tls_invalid"},
		SelfSigned:  unknown, Expiry: unknown, KeyMatch: unknown,
	}
	block, _ := pem.Decode(certPEM)
	if block == nil || block.Type != "CERTIFICATE" {
		response.Certificate = transport.SecurityAuditCheck{Status: transport.SecurityAuditStatusFail, Code: "panel_tls_invalid"}
		return response
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		response.Certificate = transport.SecurityAuditCheck{Status: transport.SecurityAuditStatusFail, Code: "panel_tls_invalid"}
		return response
	}
	response.Certificate = transport.SecurityAuditCheck{Status: transport.SecurityAuditStatusPass, Code: "panel_tls_certificate_valid"}
	response.ExpiresAt = certificate.NotAfter.UTC().Truncate(time.Second).Format(time.RFC3339)

	sameIdentity := bytes.Equal(certificate.RawIssuer, certificate.RawSubject)
	isSelfSigned := sameIdentity && certificate.CheckSignature(
		certificate.SignatureAlgorithm, certificate.RawTBSCertificate, certificate.Signature,
	) == nil
	if isSelfSigned {
		response.IsSelfSigned = true
		response.SelfSigned = transport.SecurityAuditCheck{Status: transport.SecurityAuditStatusWarning, Code: "panel_tls_self_signed"}
	} else {
		response.SelfSigned = transport.SecurityAuditCheck{Status: transport.SecurityAuditStatusUnknown, Code: "panel_tls_chain_unverified"}
	}

	switch {
	case now.Before(certificate.NotBefore):
		response.Expiry = transport.SecurityAuditCheck{Status: transport.SecurityAuditStatusFail, Code: "panel_tls_not_yet_valid"}
	case !now.Before(certificate.NotAfter):
		response.Expiry = transport.SecurityAuditCheck{Status: transport.SecurityAuditStatusFail, Code: "panel_tls_expired"}
	case certificate.NotAfter.Sub(now) <= 30*24*time.Hour:
		response.Expiry = transport.SecurityAuditCheck{Status: transport.SecurityAuditStatusWarning, Code: "panel_tls_expiring"}
	default:
		response.Expiry = transport.SecurityAuditCheck{Status: transport.SecurityAuditStatusPass, Code: "panel_tls_valid"}
	}
	if _, err := tls.X509KeyPair(certPEM, keyPEM); err != nil {
		response.KeyMatch = transport.SecurityAuditCheck{Status: transport.SecurityAuditStatusFail, Code: "panel_tls_key_mismatch"}
	} else {
		response.KeyMatch = transport.SecurityAuditCheck{Status: transport.SecurityAuditStatusPass, Code: "panel_tls_key_match"}
	}
	return response
}

// activePanelTLSPaths mirrors tlsSettings without generating or changing
// certificate material. The audit must inspect the pair selected by the
// running configuration, including explicit and custom-directory overrides.
func activePanelTLSPaths() (string, string, fixedPanelTLSPathState) {
	certPath := os.Getenv("CELIKPANEL_TLS_CERT")
	keyPath := os.Getenv("CELIKPANEL_TLS_KEY")
	if certPath != "" || keyPath != "" {
		if certPath == "" || keyPath == "" {
			return "", "", fixedPanelTLSIncomplete
		}
		return certPath, keyPath, fixedPanelTLSReady
	}
	if os.Getenv("CELIKPANEL_TLS") != "1" {
		return "", "", fixedPanelTLSMissing
	}

	dir := tlsDir()
	certPath, keyPath, found, err := managedPanelCertificatePaths(dir)
	if err != nil {
		return "", "", fixedPanelTLSUnreadable
	}
	if found {
		return certPath, keyPath, fixedPanelTLSReady
	}
	certPath = filepath.Join(dir, "panel.crt")
	keyPath = filepath.Join(dir, "panel.key")
	certInfo, certErr := os.Lstat(certPath)
	keyInfo, keyErr := os.Lstat(keyPath)
	certMissing := errors.Is(certErr, os.ErrNotExist)
	keyMissing := errors.Is(keyErr, os.ErrNotExist)
	if certMissing && keyMissing {
		return "", "", fixedPanelTLSMissing
	}
	if certMissing != keyMissing {
		return "", "", fixedPanelTLSIncomplete
	}
	if certErr != nil || keyErr != nil {
		return "", "", fixedPanelTLSUnreadable
	}
	if !certInfo.Mode().IsRegular() || certInfo.Mode()&os.ModeSymlink != 0 ||
		!keyInfo.Mode().IsRegular() || keyInfo.Mode()&os.ModeSymlink != 0 {
		return "", "", fixedPanelTLSIncomplete
	}
	return certPath, keyPath, fixedPanelTLSReady
}
