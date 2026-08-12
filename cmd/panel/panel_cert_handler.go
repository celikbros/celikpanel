package main

import (
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/hostname"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
)

// The panel's own certificate, panel side. GET reports what the panel is
// serving right now (self-signed or real, expiry); POST asks the agent to
// issue a Let's Encrypt certificate for the given domain and install it where
// tlsSettings() loads from. The agent durably schedules a lock-aware restart
// before reporting success, so publication cannot be overtaken by restart.
//
// Panelin kendi sertifikası, panel tarafı. GET panelin şu an ne sunduğunu
// bildirir (kendinden imzalı mı gerçek mi, bitiş); POST agent'tan verilen
// alan adı için Let's Encrypt sertifikası alıp tlsSettings()'in yüklediği
// yere kurmasını ister. Yeni sertifika ancak yeniden başlatmada etkinleşir;
// bu yüzden panel cevap verdikten sonra agent üzerinden kendi yeniden
// başlatmasını zamanlar — tarayıcı "yeniden başlıyor" gösterir ve yenilenir.

type panelCertInfo struct {
	HTTPSEnabled bool      `json:"https_enabled"`
	SelfSigned   bool      `json:"self_signed"`
	Subject      string    `json:"subject,omitempty"`
	Issuer       string    `json:"issuer,omitempty"`
	DNSNames     []string  `json:"dns_names,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
}

const panelManagedTLSDirectory = "/var/lib/celikpanel/tls"

type panelCertificateIssueRequest struct {
	Domain    string `json:"domain"`
	RequestID string `json:"request_id"`
}

func (p *Panel) handlePanelCertificate(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if c := currentCaller(r); c == nil || c.Role != roleAdmin {
		writeClientError(w, http.StatusForbidden, "admin only")
		return
	}

	switch r.Method {
	case http.MethodGet:
		json.NewEncoder(w).Encode(currentPanelCert())

	case http.MethodPost:
		p.handlePanelCertificateSagaPost(w, r)
		return
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (p *Panel) handlePanelCertificateSagaPost(w http.ResponseWriter, r *http.Request) {
	var req panelCertificateIssueRequest
	if err := decodeServiceOperationJSON(w, r, &req); err != nil {
		writeClientError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !validServiceOperationID(req.RequestID) {
		writeClientError(w, http.StatusBadRequest, "invalid request_id")
		return
	}
	canonicalDomain, err := hostname.CanonicalFQDN(req.Domain)
	if err != nil {
		writeClientError(w, http.StatusBadRequest, err.Error())
		return
	}
	if code, detail, blocked := panelCertificateManagementBlocker(); blocked {
		writeCodedError(
			w, http.StatusConflict, code, detail, "/settings?section=panel",
		)
		return
	}
	email := ""
	if err := p.db.GetDB().QueryRowContext(
		r.Context(), `SELECT email FROM users WHERE id = ?`, currentCaller(r).ID,
	).Scan(&email); err != nil {
		writeServerError(w, fmt.Errorf("load certificate contact email: %w", err))
		return
	}
	email = strings.TrimSpace(email)
	parsedEmail, err := mail.ParseAddress(email)
	if err != nil || parsedEmail.Name != "" || parsedEmail.Address != email {
		writeCodedError(
			w,
			http.StatusConflict,
			"panel_certificate_contact_email_invalid",
			"set a valid administrator email address before requesting a panel certificate",
			"/settings?section=account",
		)
		return
	}
	commitment, err := mutationpayload.CanonicalPanelCertificateIssue(
		canonicalDomain, email, tlsDir(), strings.TrimSpace(buildCommit),
	)
	if err != nil {
		writeClientError(w, http.StatusBadRequest, err.Error())
		return
	}
	existing, found, err := p.idempotentServiceOperation(
		r.Context(), req.RequestID, serviceOperationKindPanelCertificate,
		commitment.Domain, commitment.Qualifier,
	)
	if err != nil {
		if errors.Is(err, errServiceOperationRequestConflict) {
			writeServiceOperationRequestConflict(w)
			return
		}
		writeServerError(w, err)
		return
	}
	if found {
		writeAcceptedServiceOperation(w, existing)
		return
	}
	// Fail before the durable row exists when this panel/agent pair cannot
	// execute every privileged V2 child the saga may require. Once the row is
	// persisted, startup recovery must never discover a permanently missing
	// method or a platform policy that could have been rejected up front.
	if err := p.requireMatchingAgentBuild(r.Context()); err != nil {
		writeServerError(w, err)
		return
	}
	if err := p.requirePanelCertificateSagaAgentCapabilities(r.Context()); err != nil {
		writeServerError(w, err)
		return
	}
	for _, method := range []string{
		"Agent.ApplyFirewallV2",
		"Agent.IssuePanelCertificateV2",
	} {
		if err := p.authorizeAgentRPCContext(r.Context(), method); err != nil {
			writeServerError(w, err)
			return
		}
	}
	releaseMutation, blocked := p.beginServiceMutation(w, r)
	if blocked {
		return
	}
	releaseInHandler := true
	defer func() {
		if releaseInHandler {
			releaseMutation()
		}
	}()
	actor := captureServiceOperationActor(r)
	data := newPanelCertificateSagaData(commitment)
	identity := serviceOperation{
		RequestID:   req.RequestID,
		Kind:        serviceOperationKindPanelCertificate,
		ServiceID:   commitment.Domain,
		PackageName: commitment.Qualifier,
		Status:      serviceOperationQueued,
		Phase:       panelCertificatePhaseQueued,
	}
	operationData, err := canonicalPanelCertificateSagaData(identity, data)
	if err != nil {
		writeServerError(w, err)
		return
	}
	op, err := p.createServiceOperationRequestWithState(
		r.Context(), serviceOperationKindPanelCertificate,
		commitment.Domain, commitment.Qualifier, req.RequestID, actor,
		panelCertificatePhaseQueued, operationData,
	)
	switch {
	case errors.Is(err, errServiceOperationBusy):
		writeServiceOperationBusy(w)
		return
	case errors.Is(err, errServiceOperationReplay):
		writeAcceptedServiceOperation(w, op)
		return
	case errors.Is(err, errServiceOperationRequestConflict):
		writeServiceOperationRequestConflict(w)
		return
	case err != nil:
		writeServerError(w, err)
		return
	}
	p.launchPanelCertificateSaga(op, actor, releaseMutation)
	releaseInHandler = false
	writeAcceptedServiceOperation(w, op)
}

// currentPanelCert parses the certificate the panel serves (tls dir pair).
// currentPanelCert, panelin sunduğu sertifikayı (tls dizini çifti) çözümler.
func currentPanelCert() panelCertInfo {
	info := panelCertInfo{}
	var data []byte
	var err error
	explicitCertPath := os.Getenv("CELIKPANEL_TLS_CERT")
	if explicitCertPath != "" {
		data, err = os.ReadFile(explicitCertPath)
		if err != nil {
			return info
		}
	}
	if explicitCertPath == "" {
		activeCert, _, found, resolveErr := managedPanelCertificatePaths(tlsDir())
		switch {
		case resolveErr != nil:
			err = resolveErr
		case found:
			data, err = os.ReadFile(activeCert)
		default:
			data, err = os.ReadFile(filepath.Join(tlsDir(), "panel.crt"))
		}
	}
	if err != nil {
		return info
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return info
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return info
	}
	info.HTTPSEnabled = true
	info.SelfSigned = cert.Issuer.String() == cert.Subject.String()
	info.Subject = cert.Subject.CommonName
	info.Issuer = cert.Issuer.CommonName
	info.DNSNames = cert.DNSNames
	info.ExpiresAt = cert.NotAfter
	return info
}

func panelCertificateManagementBlocker() (code, detail string, blocked bool) {
	certPath := strings.TrimSpace(os.Getenv("CELIKPANEL_TLS_CERT"))
	keyPath := strings.TrimSpace(os.Getenv("CELIKPANEL_TLS_KEY"))
	if certPath != "" || keyPath != "" {
		return "panel_certificate_externally_managed",
			"the panel is serving an explicitly configured certificate; remove the external CELIKPANEL_TLS_CERT/KEY override before using managed certificate issuance",
			true
	}
	if tlsDir() != panelManagedTLSDirectory {
		return "panel_certificate_directory_unmanaged",
			"managed certificate issuance requires CELIKPANEL_TLS_DIR=/var/lib/celikpanel/tls; migrate the custom TLS directory before requesting a certificate",
			true
	}
	return "", "", false
}
