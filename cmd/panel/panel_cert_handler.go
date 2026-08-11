package main

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/hostname"
	"github.com/alicelik/celikpanel/internal/transport"
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

type panelCertificateMutationStage string

const (
	panelCertificateStagePreflight panelCertificateMutationStage = "firewall_preflight"
	panelCertificateStageIssueRPC  panelCertificateMutationStage = "certificate_issue"
	panelCertificateStageRejected  panelCertificateMutationStage = "certificate_rejected"
	panelCertificateStageFirewall  panelCertificateMutationStage = "firewall_finalize"
)

type panelCertificateMutationResult struct {
	Response           transport.IssuePanelCertificateResponse
	Stage              panelCertificateMutationStage
	Err                error
	CompensationErr    error
	LeaseUnavailable   bool
	FinalizationFailed bool
}

func (p *Panel) issuePanelCertificateDurably(
	ctx context.Context,
	domain, email string,
) (result panelCertificateMutationResult) {
	var callbackErr error
	callbackStarted := false
	compensatedInLease := false

	result.Err = p.withStandaloneAgentMutation(
		ctx,
		"panel_certificate_issue",
		domain,
		"certbot",
		func(boundCtx context.Context, binding agentMutationBinding) error {
			callbackStarted = true

			// Keep compensation inside the same durable lease whenever that
			// lease is still usable. syncFirewall reuses the binding carried by
			// boundCtx, so it cannot open a second mutation window here.
			failWithFirewallReconciliation := func(
				stage panelCertificateMutationStage,
				cause error,
			) error {
				result.Stage = stage
				callbackErr = cause
				if reconcileErr := p.syncFirewall(boundCtx); reconcileErr != nil {
					callbackErr = errors.Join(
						callbackErr,
						fmt.Errorf("reconcile firewall inside certificate mutation: %w", reconcileErr),
					)
					return callbackErr
				}
				compensatedInLease = true
				return callbackErr
			}

			// A fresh installation still serves a self-signed certificate, so
			// the derived policy does not contain HTTP-01 yet. Open :80 before
			// certbot while retaining the same mutation identity end to end.
			if err := p.syncFirewallWithExtraTCP(boundCtx, 80); err != nil {
				return failWithFirewallReconciliation(
					panelCertificateStagePreflight,
					fmt.Errorf("prepare firewall for ACME HTTP-01: %w", err),
				)
			}

			err := p.callAgentContext(
				boundCtx,
				"Agent.IssuePanelCertificate",
				&transport.IssuePanelCertificateRequest{
					MutationRequestID:   binding.MutationRequestID,
					MutationOwnerID:     binding.MutationOwnerID,
					ExpectedBuildCommit: strings.TrimSpace(buildCommit),
					Domain:              domain,
					Email:               email,
					TLSDir:              tlsDir(),
				},
				&result.Response,
			)
			if err != nil {
				return failWithFirewallReconciliation(
					panelCertificateStageIssueRPC,
					fmt.Errorf("issue panel certificate through agent: %w", err),
				)
			}
			if result.Response.Error != "" {
				return failWithFirewallReconciliation(
					panelCertificateStageRejected,
					fmt.Errorf("agent rejected panel certificate issuance: %s", result.Response.Error),
				)
			}

			// Publication changes the desired firewall set: a real certificate
			// needs :80 for later HTTP-01 renewals. A failed synchronization is
			// retried once as in-lease reconciliation before the mutation ends.
			if err := p.syncFirewall(boundCtx); err != nil {
				return failWithFirewallReconciliation(
					panelCertificateStageFirewall,
					fmt.Errorf("synchronize firewall after certificate publication: %w", err),
				)
			}
			return nil
		},
	)
	if result.Err == nil {
		return result
	}

	if !callbackStarted {
		// BeginServiceMutation refused before this request changed anything.
		// Starting a compensating mutation here would be both unnecessary and
		// liable to obscure the real host-lease contention.
		result.LeaseUnavailable = true
		return result
	}

	result.FinalizationFailed = callbackErr == nil || !errors.Is(result.Err, callbackErr)
	if compensatedInLease && !result.FinalizationFailed {
		return result
	}

	// A lost heartbeat or failed Finish may invalidate boundCtx. Make one
	// clean, bounded reconciliation attempt only after the outer wrapper has
	// tried to finalize its lease. This creates a new durable mutation rather
	// than smuggling cleanup through a cancelled/stale binding.
	compensationCtx, compensationCancel := sslCompensationContext()
	defer compensationCancel()
	if err := p.syncFirewall(compensationCtx); err != nil {
		result.CompensationErr = err
		result.Err = errors.Join(
			result.Err,
			fmt.Errorf("standalone certificate firewall compensation: %w", err),
		)
	}
	return result
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
		var req struct {
			Domain string `json:"domain"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Domain == "" {
			writeClientError(w, http.StatusBadRequest, "domain is required")
			return
		}
		canonicalDomain, err := hostname.CanonicalFQDN(req.Domain)
		if err != nil {
			writeClientError(w, http.StatusBadRequest, err.Error())
			return
		}
		if code, detail, blocked := panelCertificateManagementBlocker(); blocked {
			writeCodedError(
				w,
				http.StatusConflict,
				code,
				detail,
				"/settings?section=panel",
			)
			return
		}

		// Let's Encrypt wants a contact email; the admin's account email is
		// the honest default.
		// Let's Encrypt bir iletişim e-postası ister; yöneticinin hesap
		// e-postası dürüst varsayılandır.
		email := ""
		if err := p.db.GetDB().QueryRowContext(r.Context(),
			`SELECT email FROM users WHERE id = ?`, currentCaller(r).ID).Scan(&email); err != nil {
			writeServerError(w, fmt.Errorf("load certificate contact email: %w", err))
			return
		}
		email = strings.TrimSpace(email)
		parsedEmail, err := mail.ParseAddress(email)
		if err != nil || parsedEmail.Address != email {
			writeCodedError(
				w,
				http.StatusConflict,
				"panel_certificate_contact_email_invalid",
				"set a valid administrator email address before requesting a panel certificate",
				"/settings?section=account",
			)
			return
		}

		issueCtx, issueCancel := sslDurableContext(r.Context())
		defer issueCancel()
		result := p.issuePanelCertificateDurably(issueCtx, canonicalDomain, email)
		if result.Err != nil {
			log.Printf("panel certificate mutation failed for %s: %v", canonicalDomain, result.Err)
			auditCtx, auditCancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
			auditReq := r.Clone(auditCtx)
			action := "panel.certificate.failed:" + canonicalDomain + " — " + auditReason(result.Err.Error())
			switch {
			case result.CompensationErr != nil:
				action = "panel.certificate.compensation_failed:" + canonicalDomain + " — " + auditReason(result.Err.Error())
			case result.FinalizationFailed:
				action = "panel.certificate.finalization_failed:" + canonicalDomain + " — " + auditReason(result.Err.Error())
			case result.LeaseUnavailable:
				action = "panel.certificate.lease_unavailable:" + canonicalDomain + " — " + auditReason(result.Err.Error())
			}
			p.audit(auditReq, action, "panel", 0)
			auditCancel()

			switch {
			case result.CompensationErr != nil:
				writeCodedErrorDetails(
					w,
					http.StatusInternalServerError,
					"panel_certificate_compensation_failed",
					"the certificate operation failed and the active firewall policy could not be reconciled",
					"/services",
					[]string{
						"the durable certificate mutation did not complete cleanly",
						"the bounded standalone firewall reconciliation also failed",
					},
				)
			case result.FinalizationFailed:
				writeCodedError(
					w,
					http.StatusInternalServerError,
					"panel_certificate_mutation_finalize_failed",
					"the certificate operation could not be durably finalized; the firewall was reconciled, but certificate state must be checked before retrying",
					"/settings?section=panel",
				)
			case result.LeaseUnavailable:
				writeCodedError(
					w,
					http.StatusConflict,
					"panel_certificate_mutation_unavailable",
					"another privileged host operation currently owns the durable mutation lease",
					"/services",
				)
			case result.Stage == panelCertificateStagePreflight:
				writeCodedError(
					w,
					http.StatusInternalServerError,
					"firewall_acme_preflight_failed",
					"the active firewall policy could not be prepared for the HTTP-01 certificate challenge",
					"/services",
				)
			case result.Stage == panelCertificateStageRejected:
				if result.Response.ErrorCode == transport.IssuePanelCertificateErrorActivationPending {
					writeCodedError(
						w,
						http.StatusConflict,
						transport.IssuePanelCertificateErrorActivationPending,
						"a previous panel certificate activation is still being finalized; wait briefly, then retry",
						"/settings?section=panel",
					)
				} else {
					writeClientError(w, http.StatusConflict, result.Response.Error)
				}
			case result.Stage == panelCertificateStageFirewall:
				writeCodedError(
					w,
					http.StatusInternalServerError,
					"firewall_sync_failed",
					"the certificate was issued, but the active firewall policy could not be synchronized",
					"/services",
				)
			default:
				writeAgentError(w, result.Err, "panel certificate")
			}
			return
		}

		p.audit(r, "panel.certificate:"+canonicalDomain, "panel", 0)
		json.NewEncoder(w).Encode(map[string]any{
			"issued":     true,
			"expires_at": result.Response.ExpiresAt,
			"detail":     result.Response.Detail,
			"restarting": true,
		})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
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
