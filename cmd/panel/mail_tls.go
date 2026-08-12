package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/alicelik/celikpanel/internal/hostname"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

var mailTLSSyncMu sync.Mutex
var readMailTLSHostname = os.Hostname

const panelMailTLSManagedRoot = "/etc/ssl/celikpanel"

type mailTLSReconcileMode uint8

const (
	mailTLSReconcileNone mailTLSReconcileMode = iota
	mailTLSReconcileCleanup
	mailTLSReconcileStrict
)

// Panel side of mail TLS. The ledger is ssl_certificates.secure_mail; every
// change re-pushes the full SNI set to the agent (the same full-state-push as
// DNS zones and VPN peers). The default certificate and the daemons are the
// agent's business.
//
// Posta TLS'inin panel tarafı. Defter ssl_certificates.secure_mail'dir; her
// değişiklik tam SNI setini agent'a yeniden iter (DNS zone'ları ve VPN
// peer'larıyla aynı tam-durum-itme). Varsayılan sertifika ve daemon'lar
// agent'ın işidir.

// resyncMailTLS pushes active secure_mail certificates only for DNS names
// each installed certificate actually covers.
// resyncMailTLS, aktif secure_mail sertifikalarını yalnızca sertifikanın
// gerçekten kapsadığı DNS adları için agent'a iter.
func (p *Panel) resyncMailTLS(ctx context.Context) error {
	return p.resyncMailTLSForTarget(ctx, 0)
}

// resyncMailTLSForTarget preserves the agent's full-state SNI contract while
// making certificate validity tenant-scoped. The target domain is strict: its
// broken certificate aborts its own operation. Broken certificates belonging
// to other domains are omitted from the safe snapshot instead of blocking the
// target; omission also removes an unusable certificate from the live SNI map.
//
// resyncMailTLSForTarget, agent'ın tam-durum SNI sözleşmesini korurken
// sertifika geçerliliğini tenant kapsamına alır. Hedef domain katıdır: bozuk
// sertifikası kendi işlemini durdurur. Başka domain'lerin bozuk sertifikaları
// hedefi durdurmak yerine güvenli snapshot'tan çıkarılır; bu çıkarma,
// kullanılamaz sertifikayı canlı SNI haritasından da düşürür.
func (p *Panel) resyncMailTLSForTarget(ctx context.Context, strictDomainID int) error {
	p.serviceMutationMu.Lock()
	defer p.serviceMutationMu.Unlock()
	_, err := p.resyncMailTLSForTargetLocked(ctx, strictDomainID, "", "")
	return err
}

// resyncMailTLSForTargetLocked requires serviceMutationMu. It takes the inner
// snapshot mutex second, preserving one lock order for every lifecycle path.
func (p *Panel) resyncMailTLSForTargetLocked(
	ctx context.Context,
	strictDomainID int,
	requestID, ownerID string,
) (transport.SecureMailTLSResponse, error) {
	if _, err := panelMutationBinding(ctx); err == nil {
		return transport.SecureMailTLSResponse{},
			errors.New("direct mail TLS synchronization cannot reuse a bound mutation context")
	}
	if err := p.authorizeAgentRPCContext(ctx, "Agent.SyncMailTLSV2"); err != nil {
		return transport.SecureMailTLSResponse{}, err
	}
	if err := p.requireMailTLSSyncV2Agent(ctx); err != nil {
		return transport.SecureMailTLSResponse{}, err
	}
	// The SNI request is a full-state snapshot. Serialize snapshot + RPC so a
	// slower stale push for one domain cannot erase another domain's change.
	mailTLSSyncMu.Lock()
	defer mailTLSSyncMu.Unlock()

	host, sni, err := p.loadMailTLSSnapshotLocked(ctx, strictDomainID)
	if err != nil {
		return transport.SecureMailTLSResponse{}, err
	}
	commitment, err := mutationpayload.CanonicalMailTLSSync(
		panelMailTLSManagedRoot, host, sni,
	)
	if err != nil {
		return transport.SecureMailTLSResponse{},
			fmt.Errorf("canonicalize mail TLS snapshot: %w", err)
	}
	return p.applyCanonicalMailTLSV2Identity(ctx, commitment, requestID, ownerID)
}

type mailTLSAgentResponseError struct {
	message string
}

func (e *mailTLSAgentResponseError) Error() string { return e.message }

func (p *Panel) applyCanonicalMailTLSV2Identity(
	ctx context.Context,
	commitment mutationpayload.MailTLSSyncCommitment,
	requestID, ownerID string,
) (transport.SecureMailTLSResponse, error) {
	canonical, err := mutationpayload.CanonicalMailTLSSync(
		commitment.ManagedRoot, commitment.Myhostname, commitment.SNI,
	)
	if err != nil || canonical.Qualifier != commitment.Qualifier ||
		canonical.ManagedRoot != panelMailTLSManagedRoot {
		return transport.SecureMailTLSResponse{},
			errors.New("invalid canonical mail TLS commitment")
	}
	commitment = canonical
	var response transport.SecureMailTLSResponse
	responseConfirmed := false
	call := func(callCtx context.Context, binding agentMutationBinding) error {
		request := transport.SyncMailTLSV2Request{
			ServiceMutationBinding: binding,
			ExpectedBuildCommit:    strings.TrimSpace(buildCommit),
			Myhostname:             commitment.Myhostname,
			SNI:                    append([]transport.MailSNIEntry(nil), commitment.SNI...),
		}
		for index := range request.SNI {
			request.SNI[index].Names = append([]string(nil), request.SNI[index].Names...)
		}
		if err := p.callAgentContext(
			callCtx, "Agent.SyncMailTLSV2", &request, &response,
		); err != nil {
			return err
		}
		if response.Error != "" {
			return &mailTLSAgentResponseError{message: response.Error}
		}
		if err := validateMailTLSResponse(response, len(commitment.SNI)); err != nil {
			return err
		}
		responseConfirmed = true
		return nil
	}
	if requestID == "" && ownerID == "" {
		err = p.withStandaloneAgentMutation(
			ctx, "mail_tls_sync", "mail-tls", commitment.Qualifier, call,
		)
	} else {
		err = p.withStandaloneAgentMutationIdentity(
			ctx,
			serviceOperation{
				RequestID: requestID, Kind: "mail_tls_sync",
				ServiceID: "mail-tls", PackageName: commitment.Qualifier,
			},
			ownerID,
			call,
		)
	}
	if err == nil && !responseConfirmed {
		// Exact terminal success for this request/owner/kind/target/qualifier is
		// authoritative even when the net/rpc response was lost.
		response = transport.SecureMailTLSResponse{
			Configured: true, DefaultCert: transport.DefaultMailTLSCertificatePath,
			SNICount: len(commitment.SNI),
			Detail:   "mail TLS synchronization committed; RPC response was lost",
		}
	}
	if err != nil {
		return transport.SecureMailTLSResponse{}, err
	}
	return response, err
}

// loadMailTLSSnapshotLocked builds the safe full-state payload. Callers must hold
// mailTLSSyncMu continuously from this read through their publication RPC.
func (p *Panel) loadMailTLSSnapshotLocked(
	ctx context.Context,
	strictDomainID int,
) (string, []transport.MailSNIEntry, error) {
	rows, err := p.db.GetDB().QueryContext(ctx, `
		SELECT sc.domain_id, d.name, sc.cert_path, sc.key_path
		FROM ssl_certificates sc JOIN domains d ON d.id = sc.domain_id
		WHERE sc.status = 'active' AND sc.secure_mail = 1`)
	if err != nil {
		return "", nil, err
	}
	defer rows.Close()
	var sni []transport.MailSNIEntry
	strictTargetSeen := false
	for rows.Next() {
		var domainID int
		var name, cert, key string
		if err := rows.Scan(&domainID, &name, &cert, &key); err != nil {
			return "", nil, err
		}
		isStrictTarget := strictDomainID > 0 && domainID == strictDomainID
		if isStrictTarget {
			strictTargetSeen = true
		}
		if strings.TrimSpace(cert) == "" || strings.TrimSpace(key) == "" {
			if isStrictTarget {
				return "", nil, fmt.Errorf("mail TLS certificate for %s has an empty certificate or private-key path", name)
			}
			log.Printf("mail TLS snapshot: omit unrelated domain %s (%d): empty certificate or private-key path",
				name, domainID)
			continue
		}
		info, infoErr := p.inspectInstalledCertificate(ctx, cert, key)
		if infoErr != nil {
			// A transport failure means the panel could not safely inspect the
			// full snapshot at all. Do not publish a destructive partial map.
			return "", nil, fmt.Errorf("inspect mail TLS certificate for %s: %w", name, infoErr)
		}
		if !info.Valid {
			if isStrictTarget {
				return "", nil, fmt.Errorf("mail TLS certificate for %s is invalid: %s", name, info.Error)
			}
			log.Printf("mail TLS snapshot: omit unrelated invalid certificate for %s (%d): %s",
				name, domainID, strings.TrimSpace(info.Error))
			continue
		}
		if !info.TrustChecked || !info.Trusted {
			if isStrictTarget {
				return "", nil, fmt.Errorf("mail TLS certificate for %s is not trusted", name)
			}
			log.Printf("mail TLS snapshot: omit unrelated untrusted certificate for %s (%d)", name, domainID)
			continue
		}
		mailName := "mail." + name
		if !certificateCoversHostname(info.DNSNames, mailName) {
			if isStrictTarget {
				return "", nil, fmt.Errorf("mail TLS certificate does not cover %s", mailName)
			}
			log.Printf("mail TLS snapshot: omit unrelated certificate for %s (%d): it does not cover %s",
				name, domainID, mailName)
			continue
		}
		names := []string{mailName}
		if certificateCoversHostname(info.DNSNames, name) {
			names = append(names, name)
		}
		sni = append(sni, transport.MailSNIEntry{
			Names:    names,
			CertPath: cert,
			KeyPath:  key,
		})
	}
	if err := rows.Err(); err != nil {
		return "", nil, err
	}
	if strictDomainID > 0 && !strictTargetSeen {
		return "", nil, fmt.Errorf("target domain %d is not active in the secure-mail ledger", strictDomainID)
	}

	host, err := readMailTLSHostname()
	if err != nil {
		return "", nil, fmt.Errorf("read mail server hostname: %w", err)
	}
	canonicalHost, err := hostname.CanonicalFQDN(host)
	if err != nil {
		return "", nil, fmt.Errorf("mail server hostname is not a valid FQDN: %w", err)
	}
	return canonicalHost, sni, nil
}

func validateMailTLSResponse(resp transport.SecureMailTLSResponse, expectedSNICount int) error {
	if resp.Error != "" {
		return &backupError{resp.Error}
	}
	if !resp.Configured || resp.SNICount != expectedSNICount {
		return fmt.Errorf(
			"mail TLS agent applied %d of %d SNI entries",
			resp.SNICount, expectedSNICount,
		)
	}
	if resp.DefaultCert != transport.DefaultMailTLSCertificatePath {
		return fmt.Errorf(
			"mail TLS agent reported unexpected default certificate path %q",
			resp.DefaultCert,
		)
	}
	return nil
}

// mailTLSModeForDomain decides whether a certificate mutation can affect the
// mail SNI map. An active secure_mail certificate requires strict validation.
// A previously-secured inactive certificate requires a full-state cleanup
// after detach or replacement without mail coverage. Domains that never
// participated in secure_mail do not trigger a global mail reconcile.
func (p *Panel) mailTLSModeForDomain(ctx context.Context, domainID int) (mailTLSReconcileMode, error) {
	var activeSecure, inactiveSecure int
	if err := p.db.GetDB().QueryRowContext(ctx, `
		SELECT
			COALESCE(MAX(CASE WHEN status = 'active' AND secure_mail = 1 THEN 1 ELSE 0 END), 0),
			COALESCE(MAX(CASE WHEN status <> 'active' AND secure_mail = 1 THEN 1 ELSE 0 END), 0)
		FROM ssl_certificates
		WHERE domain_id = ?`, domainID).Scan(&activeSecure, &inactiveSecure); err != nil {
		return mailTLSReconcileNone, err
	}
	switch {
	case activeSecure == 1:
		return mailTLSReconcileStrict, nil
	case inactiveSecure == 1:
		return mailTLSReconcileCleanup, nil
	default:
		return mailTLSReconcileNone, nil
	}
}

func (p *Panel) syncCertificateDependents(ctx context.Context, domainID int) error {
	mode, err := p.mailTLSModeForDomain(ctx, domainID)
	if err != nil {
		return fmt.Errorf("read mail TLS participation: %w", err)
	}
	switch mode {
	case mailTLSReconcileStrict:
		if err := p.resyncMailTLSForTarget(ctx, domainID); err != nil {
			return fmt.Errorf("mail TLS sync: %w", err)
		}
	case mailTLSReconcileCleanup:
		if err := p.resyncMailTLS(ctx); err != nil {
			return fmt.Errorf("mail TLS cleanup: %w", err)
		}
	}
	if err := p.refreshTLSARecords(ctx, domainID); err != nil {
		return fmt.Errorf("TLSA sync: %w", err)
	}
	return nil
}

func (p *Panel) resyncMailTLSForState(ctx context.Context, domainID int, secureMail bool) error {
	if secureMail {
		return p.resyncMailTLSForTarget(ctx, domainID)
	}
	return p.resyncMailTLS(ctx)
}

type domainMailTLSRemoval struct {
	DomainID       int
	CertificateIDs []int64
}

// prepareDomainMailTLSRemoval removes a soon-to-be-deleted domain from the
// full mail SNI snapshot before its certificate rows and lineage disappear.
// If publication fails, the ledger and previous snapshot are restored.
func (p *Panel) prepareDomainMailTLSRemoval(
	ctx context.Context,
	domainID int,
) (*domainMailTLSRemoval, error) {
	rows, err := p.db.GetDB().QueryContext(ctx, `
		SELECT id FROM ssl_certificates
		WHERE domain_id = ? AND status = 'active' AND secure_mail = 1
		ORDER BY id`, domainID)
	if err != nil {
		return nil, err
	}
	var certificateIDs []int64
	for rows.Next() {
		var certificateID int64
		if err := rows.Scan(&certificateID); err != nil {
			rows.Close()
			return nil, err
		}
		certificateIDs = append(certificateIDs, certificateID)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if len(certificateIDs) == 0 {
		return &domainMailTLSRemoval{DomainID: domainID}, nil
	}
	if err := p.authorizeAgentRPCContext(ctx, "Agent.SyncMailTLSV2"); err != nil {
		return nil, fmt.Errorf("authorize Mail TLS V2 before domain removal: %w", err)
	}
	if err := p.requireMailTLSSyncV2Agent(ctx); err != nil {
		return nil, fmt.Errorf("verify Mail TLS V2 before domain removal: %w", err)
	}

	removal := &domainMailTLSRemoval{
		DomainID:       domainID,
		CertificateIDs: certificateIDs,
	}
	if _, err := p.db.GetDB().ExecContext(ctx, `
		UPDATE ssl_certificates SET secure_mail = 0, updated_at = datetime('now')
		WHERE domain_id = ? AND status = 'active' AND secure_mail = 1`,
		domainID,
	); err != nil {
		return nil, err
	}
	if err := p.resyncMailTLS(ctx); err != nil {
		if mutationTerminalUncertain(err) {
			return nil, fmt.Errorf(
				"remove domain from mail TLS: %w; the requested secure_mail=0 state was retained for exact agent recovery",
				err,
			)
		}
		rollbackCtx, cancel := sslCompensationContext()
		defer cancel()
		if rollbackErr := p.setDomainMailTLSRemovalLedger(
			rollbackCtx, removal, true,
		); rollbackErr != nil {
			return nil, fmt.Errorf("remove domain from mail TLS: %v; restore ledger: %w", err, rollbackErr)
		}
		if rollbackErr := p.resyncMailTLSForTarget(
			rollbackCtx, removal.DomainID,
		); rollbackErr != nil {
			return nil, fmt.Errorf(
				"remove domain from mail TLS: %v; publish restored snapshot: %w",
				err, rollbackErr,
			)
		}
		return nil, fmt.Errorf("remove domain from mail TLS: %w", err)
	}
	return removal, nil
}

func (p *Panel) rollbackDomainMailTLSRemoval(
	ctx context.Context,
	removal *domainMailTLSRemoval,
) error {
	if removal == nil || len(removal.CertificateIDs) == 0 {
		return nil
	}
	if err := p.setDomainMailTLSRemovalLedger(ctx, removal, true); err != nil {
		return err
	}
	if err := p.resyncMailTLSForTarget(ctx, removal.DomainID); err != nil {
		if mutationTerminalUncertain(err) {
			return fmt.Errorf("restore domain Mail TLS publication: %w; secure_mail=1 retained for exact agent recovery", err)
		}
		reinstateErr := p.setDomainMailTLSRemovalLedger(ctx, removal, false)
		var republishErr error
		if reinstateErr == nil {
			republishErr = p.resyncMailTLS(ctx)
		}
		return errors.Join(
			fmt.Errorf("restore domain Mail TLS publication: %w", err),
			func() error {
				if reinstateErr == nil {
					return nil
				}
				return fmt.Errorf("reinstate requested secure_mail=0 ledger: %w", reinstateErr)
			}(),
			func() error {
				if republishErr == nil {
					return nil
				}
				return fmt.Errorf("republish requested secure_mail=0 snapshot: %w", republishErr)
			}(),
		)
	}
	return nil
}

func (p *Panel) setDomainMailTLSRemovalLedger(
	ctx context.Context,
	removal *domainMailTLSRemoval,
	enabled bool,
) error {
	if removal == nil || len(removal.CertificateIDs) == 0 {
		return nil
	}
	tx, err := p.db.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, certificateID := range removal.CertificateIDs {
		result, err := tx.ExecContext(ctx, `
			UPDATE ssl_certificates SET secure_mail = ?, updated_at = datetime('now')
			WHERE id = ? AND domain_id = ? AND status = 'active'`,
			enabled,
			certificateID,
			removal.DomainID,
		)
		if err != nil {
			return err
		}
		if changed, _ := result.RowsAffected(); changed != 1 {
			return fmt.Errorf("active secure-mail certificate %d is no longer available", certificateID)
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}

// handleDomainSSLMail toggles "secure mail with this certificate" for a
// domain's active certificate and re-syncs the stack.
// handleDomainSSLMail, bir domain'in aktif sertifikası için "maili bu
// sertifikayla koru"yu açıp kapatır ve yığını yeniden senkronlar.
func (p *Panel) handleDomainSSLMail(w http.ResponseWriter, r *http.Request, domainID int) {
	w.Header().Set("Content-Type", "application/json")
	switch r.Method {
	case http.MethodGet:
		var enabled int
		_ = p.db.GetDB().QueryRowContext(r.Context(), `
			SELECT COALESCE(MAX(secure_mail), 0) FROM ssl_certificates
			WHERE domain_id = ? AND status = 'active'`, domainID).Scan(&enabled)
		json.NewEncoder(w).Encode(map[string]any{"secure_mail": enabled == 1})

	case http.MethodPut:
		unlock := lockDomainSSLOperation(domainID)
		defer unlock()
		var req struct {
			SecureMail bool `json:"secure_mail"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeClientError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		var (
			domainName      string
			certPath        string
			previousEnabled int
		)
		if err := p.db.GetDB().QueryRowContext(r.Context(), `
			SELECT d.name, sc.cert_path, COALESCE(sc.secure_mail, 0)
			FROM ssl_certificates sc JOIN domains d ON d.id = sc.domain_id
			WHERE sc.domain_id = ? AND sc.status = 'active'
			ORDER BY sc.created_at DESC LIMIT 1`, domainID).
			Scan(&domainName, &certPath, &previousEnabled); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeClientError(w, http.StatusConflict, "the domain has no active certificate")
			} else {
				writeServerError(w, err)
			}
			return
		}
		if req.SecureMail {
			managedNames, err := p.managedSiteHostnames(r.Context(), domainID)
			if err != nil {
				writeServerError(w, err)
				return
			}
			runtime, err := p.loadCertificateRuntimeStatus(r.Context(), domainID, managedNames)
			if err != nil {
				writeServerError(w, err)
				return
			}
			if !runtime.Usable {
				writeClientError(w, http.StatusConflict,
					"mail TLS requires an activated, trusted certificate covering every managed website hostname")
				return
			}
			mailName := "mail." + domainName
			if !certificateCoversHostname(runtime.Info.DNSNames, mailName) {
				writeClientError(w, http.StatusConflict,
					"install a certificate that covers "+mailName+" before securing mail")
				return
			}
		}
		if err := p.authorizeAgentRPCContext(r.Context(), "Agent.SyncMailTLSV2"); err != nil {
			writeAgentError(w, err, "mail TLS V2 platform preflight")
			return
		}
		if err := p.requireMailTLSSyncV2Agent(r.Context()); err != nil {
			writeAgentError(w, err, "mail TLS V2 capability preflight")
			return
		}
		val := 0
		if req.SecureMail {
			val = 1
		}
		res, err := p.db.GetDB().ExecContext(r.Context(), `
			UPDATE ssl_certificates SET secure_mail = ?
			WHERE domain_id = ? AND status = 'active'`, val, domainID)
		if err != nil {
			writeServerError(w, err)
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			writeClientError(w, http.StatusConflict, "the domain has no active certificate — install one first")
			return
		}
		if err := p.resyncMailTLSForState(r.Context(), domainID, req.SecureMail); err != nil {
			if mutationTerminalUncertain(err) {
				writeServerError(w, fmt.Errorf(
					"mail TLS terminal receipt is pending; requested secure_mail=%t was retained: %w",
					req.SecureMail, err,
				))
				return
			}
			rollbackCtx, cancel := sslCompensationContext()
			defer cancel()
			_, rollbackErr := p.db.GetDB().ExecContext(rollbackCtx, `
				UPDATE ssl_certificates SET secure_mail = ?
				WHERE domain_id = ? AND status = 'active'`, previousEnabled, domainID)
			if rollbackErr == nil {
				rollbackErr = p.resyncMailTLSForState(
					rollbackCtx, domainID, previousEnabled == 1,
				)
			}
			if rollbackErr != nil {
				writeServerError(w, fmt.Errorf("mail TLS apply failed: %v; restore snapshot failed: %w", err, rollbackErr))
				return
			}
			writeServerError(w, err)
			return
		}
		// DANE follows the toggle: publish TLSA when securing, drop when not.
		// DANE anahtarı izler: korurken TLSA yayımla, korumuyorken düşür.
		if err := p.refreshTLSARecords(r.Context(), domainID); err != nil {
			rollbackCtx, cancel := sslCompensationContext()
			defer cancel()
			_, rollbackErr := p.db.GetDB().ExecContext(rollbackCtx, `
				UPDATE ssl_certificates SET secure_mail = ?
				WHERE domain_id = ? AND status = 'active'`, previousEnabled, domainID)
			mailRollbackPublished := false
			if rollbackErr == nil {
				rollbackErr = p.resyncMailTLSForState(rollbackCtx, domainID, previousEnabled == 1)
				mailRollbackPublished = rollbackErr == nil
			}
			if rollbackErr == nil {
				rollbackErr = p.refreshTLSARecords(rollbackCtx, domainID)
			}
			if rollbackErr != nil {
				if !mailRollbackPublished && !mutationTerminalUncertain(rollbackErr) {
					requested := 0
					if req.SecureMail {
						requested = 1
					}
					_, reinstateErr := p.db.GetDB().ExecContext(rollbackCtx, `
						UPDATE ssl_certificates SET secure_mail = ?
						WHERE domain_id = ? AND status = 'active'`, requested, domainID)
					if reinstateErr != nil {
						rollbackErr = errors.Join(rollbackErr, fmt.Errorf(
							"reinstate requested secure-mail ledger: %w", reinstateErr,
						))
					}
				}
				writeServerError(w, fmt.Errorf(
					"TLSA publish failed: %v; secure-mail rollback failed: %w",
					err, rollbackErr,
				))
				return
			}
			writeServerError(w, err)
			return
		}
		action := "ssl.mail.off"
		if req.SecureMail {
			action = "ssl.mail.on"
		}
		p.audit(r, action, "domain", domainID)
		json.NewEncoder(w).Encode(map[string]any{"success": true})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
