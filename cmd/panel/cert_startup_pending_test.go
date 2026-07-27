package main

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

type StartupPendingInspectRequest struct {
	Domain    string
	CertPath  string
	KeyPath   string
	ChainPath string
}

type StartupPendingInspectResponse struct {
	Valid        bool
	Trusted      bool
	TrustChecked bool
	DNSNames     []string
}

type StartupPendingSecureMailRequest struct {
	SNI []struct{}
}

type StartupPendingSecureMailResponse struct {
	Configured bool
	SNICount   int
	Error      string
}

type StartupPendingReconcileRequest struct {
	ReferencedLineages []string
	ActiveLineages     []string
}

type StartupPendingReconcileResponse struct {
	Deleted int
	Error   string
}

type startupPendingAgent struct {
	mu sync.Mutex

	certificates map[string]StartupPendingInspectResponse
	applyError   string
	mailError    string

	batches     [][]applyVhostRPCRequest
	secureCalls int
}

func (a *startupPendingAgent) ReconcileSiteCertLineages(
	_ *StartupPendingReconcileRequest,
	resp *StartupPendingReconcileResponse,
) error {
	resp.Deleted = 0
	return nil
}

func (a *startupPendingAgent) InspectInstalledCertificate(
	req *StartupPendingInspectRequest,
	resp *StartupPendingInspectResponse,
) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	*resp = a.certificates[req.CertPath]
	return nil
}

func (a *startupPendingAgent) ApplyVhosts(
	req *StartupVhostBatchRequest,
	resp *StartupVhostBatchResponse,
) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.batches = append(
		a.batches,
		append([]applyVhostRPCRequest(nil), req.Vhosts...),
	)
	if a.applyError != "" {
		resp.Error = a.applyError
		return nil
	}
	resp.Applied = len(req.Vhosts)
	return nil
}

func (a *startupPendingAgent) SecureMailTLS(
	req *StartupPendingSecureMailRequest,
	resp *StartupPendingSecureMailResponse,
) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.secureCalls++
	if a.mailError != "" {
		resp.Error = a.mailError
		return nil
	}
	resp.Configured = true
	resp.SNICount = len(req.SNI)
	return nil
}

func TestStartupPendingCertificateOutboxIsAcknowledgedOnlyAfterRuntimeSuccess(
	t *testing.T,
) {
	for _, testCase := range []struct {
		name            string
		applyError      string
		mailError       string
		wantPending     string
		wantSiteEnabled bool
		wantMailCalls   int
	}{
		{
			name:            "full success clears pending",
			wantPending:     "",
			wantSiteEnabled: true,
			wantMailCalls:   1,
		},
		{
			name:            "vhost failure rolls activation back",
			applyError:      "forced startup vhost failure",
			wantPending:     sslPendingActivation,
			wantSiteEnabled: false,
			wantMailCalls:   0,
		},
		{
			name:            "dependent failure remains retryable",
			mailError:       "forced startup mail failure",
			wantPending:     sslPendingDependents,
			wantSiteEnabled: true,
			wantMailCalls:   1,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			panel, subscriptionID := newStartupVhostBatchFixture(t)
			const domainName = "pending-startup.example"
			domainID := addStartupHostedDomain(
				t,
				panel,
				subscriptionID,
				domainName,
				"static",
			)
			certPath := fmt.Sprintf("/certs/pending-startup/%d/fullchain.pem", domainID)
			keyPath := fmt.Sprintf("/certs/pending-startup/%d/privkey.pem", domainID)
			if _, err := panel.db.GetDB().Exec(`
				UPDATE sites
				SET ssl_enabled = false,
				    ssl_type = 'custom',
				    ssl_cert_path = ?,
				    ssl_key_path = ?
				WHERE domain_id = ?`,
				certPath,
				keyPath,
				domainID,
			); err != nil {
				t.Fatalf("prepare pending startup site: %v", err)
			}
			if _, err := panel.db.GetDB().Exec(`
				INSERT INTO ssl_certificates (
					domain_id, type, cert_path, key_path, chain_path,
					issuer, subject, issued_at, expires_at,
					auto_renew, secure_mail, renewal_status, status
				) VALUES (?, 'custom', ?, ?, '', 'Test CA', ?,
				          ?, ?, false, false, ?, 'active')`,
				domainID,
				certPath,
				keyPath,
				domainName,
				time.Now().UTC().Add(-time.Hour).Format(time.RFC3339),
				time.Now().UTC().Add(90*24*time.Hour).Format(time.RFC3339),
				sslPendingActivation,
			); err != nil {
				t.Fatalf("insert pending startup certificate: %v", err)
			}

			agent := &startupPendingAgent{
				certificates: map[string]StartupPendingInspectResponse{
					certPath: {
						Valid:        true,
						Trusted:      true,
						TrustChecked: true,
						DNSNames: []string{
							domainName,
							"www." + domainName,
						},
					},
				},
				applyError: testCase.applyError,
				mailError:  testCase.mailError,
			}
			attachStartupVhostBatchAgent(t, panel, agent)

			panel.reconcileCertificateRuntimeAtStartup()

			var (
				pending     string
				siteEnabled bool
			)
			if err := panel.db.GetDB().QueryRow(`
				SELECT COALESCE(sc.renewal_status, ''),
				       COALESCE(s.ssl_enabled, false)
				FROM ssl_certificates sc
				JOIN sites s ON s.domain_id = sc.domain_id
				WHERE sc.domain_id = ? AND sc.status = 'active'`,
				domainID,
			).Scan(&pending, &siteEnabled); err != nil {
				t.Fatalf("read reconciled startup certificate: %v", err)
			}
			if pending != testCase.wantPending ||
				siteEnabled != testCase.wantSiteEnabled {
				t.Fatalf(
					"startup pending state = %q enabled=%t, want %q enabled=%t",
					pending,
					siteEnabled,
					testCase.wantPending,
					testCase.wantSiteEnabled,
				)
			}

			agent.mu.Lock()
			defer agent.mu.Unlock()
			if len(agent.batches) != 1 || len(agent.batches[0]) != 1 {
				t.Fatalf("startup vhost batches = %#v, want one domain in one batch", agent.batches)
			}
			if testCase.applyError == "" &&
				agent.batches[0][0].SSLCert != certPath {
				t.Fatalf(
					"startup activation batch cert = %q, want %q",
					agent.batches[0][0].SSLCert,
					certPath,
				)
			}
			if agent.secureCalls != testCase.wantMailCalls {
				t.Fatalf(
					"startup dependent calls = %d, want %d",
					agent.secureCalls,
					testCase.wantMailCalls,
				)
			}
		})
	}
}

func TestStartupInvalidPendingCertificateStaysDisabledAndPending(t *testing.T) {
	panel, subscriptionID := newStartupVhostBatchFixture(t)
	const domainName = "invalid-pending-startup.example"
	domainID := addStartupHostedDomain(
		t,
		panel,
		subscriptionID,
		domainName,
		"static",
	)
	const certPath = "/certs/invalid-pending/fullchain.pem"
	const keyPath = "/certs/invalid-pending/privkey.pem"
	if _, err := panel.db.GetDB().Exec(`
		UPDATE sites
		SET ssl_enabled = false, ssl_type = 'custom',
		    ssl_cert_path = ?, ssl_key_path = ?
		WHERE domain_id = ?`,
		certPath,
		keyPath,
		domainID,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := panel.db.GetDB().Exec(`
		INSERT INTO ssl_certificates (
			domain_id, type, cert_path, key_path, expires_at,
			renewal_status, status
		) VALUES (?, 'custom', ?, ?, ?, ?, 'active')`,
		domainID,
		certPath,
		keyPath,
		time.Now().UTC().Add(90*24*time.Hour).Format(time.RFC3339),
		sslPendingActivation,
	); err != nil {
		t.Fatal(err)
	}

	agent := &startupPendingAgent{
		certificates: map[string]StartupPendingInspectResponse{
			certPath: {
				Valid:        true,
				Trusted:      false,
				TrustChecked: true,
				DNSNames:     []string{domainName, "www." + domainName},
			},
		},
	}
	attachStartupVhostBatchAgent(t, panel, agent)

	state, err := panel.preparePendingCertificatesAtStartup(
		context.Background(),
		10,
	)
	if err != nil {
		t.Fatalf("prepare invalid pending certificate: %v", err)
	}
	if state.skipped != 1 || len(state.eligible) != 0 ||
		len(state.activated) != 0 {
		t.Fatalf("invalid pending preparation = %+v", state)
	}

	var (
		pending     string
		siteEnabled bool
	)
	if err := panel.db.GetDB().QueryRow(`
		SELECT COALESCE(sc.renewal_status, ''),
		       COALESCE(s.ssl_enabled, false)
		FROM ssl_certificates sc
		JOIN sites s ON s.domain_id = sc.domain_id
		WHERE sc.domain_id = ? AND sc.status = 'active'`,
		domainID,
	).Scan(&pending, &siteEnabled); err != nil {
		t.Fatal(err)
	}
	if pending != sslPendingActivation || siteEnabled {
		t.Fatalf(
			"invalid pending certificate became active: pending=%q enabled=%t",
			pending,
			siteEnabled,
		)
	}
}
