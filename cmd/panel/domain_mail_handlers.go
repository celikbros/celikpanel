package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/transport"
)

type EmailAccount struct {
	ID      int    `json:"id"`
	Address string `json:"address"`
	QuotaMB int    `json:"quota_mb"`
}

type EmailForwarding struct {
	ID          int    `json:"id"`
	Source      string `json:"source"`
	Destination string `json:"destination"`
}

func (p *Panel) handleDomainMail(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 5 {
		http.NotFound(w, r)
		return
	}
	domainID, err := strconv.Atoi(parts[4])
	if err != nil || domainID <= 0 {
		http.NotFound(w, r)
		return
	}

	switch {
	case strings.Contains(r.URL.Path, "/mail/auth"):
		p.handleMailAuth(w, r, domainID)
	case strings.HasSuffix(r.URL.Path, "/mail/rbl"):
		p.handleMailRBL(w, r)
	case strings.HasSuffix(r.URL.Path, "/mail/setup"):
		domain, err := p.domainNameByIDStrict(r.Context(), domainID)
		if err != nil {
			writeServerError(w, err)
			return
		}
		p.handleMailClientSetup(w, r, domain)
	case strings.HasSuffix(r.URL.Path, "/mail/catch-all"):
		p.handleMailCatchAll(w, r, domainID)
	case strings.HasSuffix(r.URL.Path, "/quota"):
		p.handleMailQuotaStatus(w, r, domainID)
	case strings.HasSuffix(r.URL.Path, "/mail/accounts/password"):
		p.handleUpdateEmailPassword(w, r, domainID)
	case strings.HasSuffix(r.URL.Path, "/accounts"):
		switch r.Method {
		case http.MethodGet:
			p.handleListEmailAccounts(w, r, domainID)
		case http.MethodPost:
			p.handleAddEmailAccount(w, r, domainID)
		case http.MethodPut:
			p.handleUpdateEmailQuota(w, r, domainID)
		case http.MethodDelete:
			p.handleDeleteEmailAccount(w, r, domainID)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	case strings.HasSuffix(r.URL.Path, "/forwardings"):
		switch r.Method {
		case http.MethodGet:
			p.handleListEmailForwardings(w, r, domainID)
		case http.MethodPost:
			p.handleAddEmailForwarding(w, r, domainID)
		case http.MethodDelete:
			p.handleDeleteEmailForwarding(w, r, domainID)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	default:
		http.NotFound(w, r)
	}
}

func (p *Panel) domainNameByIDStrict(ctx context.Context, domainID int) (string, error) {
	var domain string
	if err := p.db.GetDB().QueryRowContext(ctx, "SELECT name FROM domains WHERE id = ?", domainID).Scan(&domain); err != nil {
		return "", err
	}
	if _, err := transport.CanonicalMailAddress("postmaster@" + domain); err != nil {
		return "", fmt.Errorf("stored domain is invalid: %w", err)
	}
	return strings.ToLower(domain), nil
}

func (p *Panel) handleListEmailAccounts(w http.ResponseWriter, r *http.Request, domainID int) {
	rows, err := p.db.GetDB().QueryContext(r.Context(),
		"SELECT id, address, quota_mb FROM email_accounts WHERE domain_id = ? ORDER BY id", domainID)
	if err != nil {
		writeServerError(w, err)
		return
	}
	defer rows.Close()
	accounts := make([]EmailAccount, 0)
	for rows.Next() {
		var account EmailAccount
		if err := rows.Scan(&account.ID, &account.Address, &account.QuotaMB); err != nil {
			writeServerError(w, err)
			return
		}
		accounts = append(accounts, account)
	}
	if err := rows.Err(); err != nil {
		writeServerError(w, err)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"accounts": accounts})
}

func (p *Panel) handleListEmailForwardings(w http.ResponseWriter, r *http.Request, domainID int) {
	rows, err := p.db.GetDB().QueryContext(r.Context(),
		"SELECT id, source, destination FROM email_forwardings WHERE domain_id = ? ORDER BY id", domainID)
	if err != nil {
		writeServerError(w, err)
		return
	}
	defer rows.Close()
	forwardings := make([]EmailForwarding, 0)
	for rows.Next() {
		var forwarding EmailForwarding
		if err := rows.Scan(&forwarding.ID, &forwarding.Source, &forwarding.Destination); err != nil {
			writeServerError(w, err)
			return
		}
		forwardings = append(forwardings, forwarding)
	}
	if err := rows.Err(); err != nil {
		writeServerError(w, err)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"forwardings": forwardings})
}

func (p *Panel) callMailMutation(ctx context.Context, method string, request any) error {
	var response transport.MailMutationResponse
	if err := p.callAgentContext(ctx, method, request, &response); err != nil {
		return err
	}
	if !response.Applied {
		return fmt.Errorf("%s did not confirm a complete mutation", method)
	}
	return nil
}

func rollbackMailTx(tx *sql.Tx, cause error) error {
	if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
		return errors.Join(cause, fmt.Errorf("database rollback failed: %w", err))
	}
	return cause
}

const mailCompensationTimeout = 30 * time.Second

func mailCompensationContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(parent), mailCompensationTimeout)
}

func (p *Panel) handleAddEmailAccount(w http.ResponseWriter, r *http.Request, domainID int) {
	var request struct {
		Address  string `json:"address"`
		Password string `json:"password"`
		QuotaMB  int    `json:"quota_mb"`
	}
	if err := decodeStrictJSON(w, r, &request); err != nil {
		writeClientError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	domain, err := p.domainNameByIDStrict(r.Context(), domainID)
	if err != nil {
		writeServerError(w, err)
		return
	}
	address, err := transport.CanonicalMailboxForDomain(request.Address, domain)
	if err != nil {
		writeClientError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(request.Password) < transport.MinMailboxPasswordBytes || len(request.Password) > transport.MaxMailboxPasswordBytes {
		writeClientError(w, http.StatusBadRequest, "password length is outside the allowed range")
		return
	}
	if request.QuotaMB <= 0 || request.QuotaMB > transport.MaxMailboxQuotaMB {
		writeClientError(w, http.StatusBadRequest, "quota is outside the allowed range")
		return
	}
	if !p.enforceDomainQuota(w, r, domainID, quotaMail) {
		return
	}

	p.mailMutationMu.Lock()
	defer p.mailMutationMu.Unlock()
	if err := p.ensureMailDomainMutable(r.Context(), domainID); err != nil {
		writeMailDomainMutationError(w, err)
		return
	}
	tx, err := p.db.GetDB().BeginTx(r.Context(), nil)
	if err != nil {
		writeServerError(w, err)
		return
	}
	result, err := tx.ExecContext(r.Context(),
		"INSERT INTO email_accounts (domain_id, address, password_hash, quota_mb) VALUES (?, ?, ?, ?)",
		domainID, address, "managed-by-agent", request.QuotaMB)
	if err != nil {
		writeServerError(w, rollbackMailTx(tx, err))
		return
	}
	newID, err := result.LastInsertId()
	if err != nil {
		writeServerError(w, rollbackMailTx(tx, err))
		return
	}
	if err := p.callMailMutation(r.Context(), "Agent.AddMailAccount", &transport.MailAccount{
		Email: address, Password: request.Password, QuotaMB: request.QuotaMB,
	}); err != nil {
		writeServerError(w, rollbackMailTx(tx, err))
		return
	}
	if err := tx.Commit(); err != nil {
		compensationCtx, cancel := mailCompensationContext(r.Context())
		compensation := p.callMailMutation(compensationCtx, "Agent.DeleteMailAccount",
			&transport.DeleteMailAccountRequest{Email: address})
		cancel()
		if compensation != nil {
			err = errors.Join(err, fmt.Errorf("agent compensation failed: %w", compensation))
		}
		writeServerError(w, err)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "id": newID})
}

func (p *Panel) handleUpdateEmailPassword(w http.ResponseWriter, r *http.Request, domainID int) {
	w.Header().Set("Cache-Control", "no-store")
	var request struct {
		ID          int    `json:"id"`
		NewPassword string `json:"new_password"`
	}
	if err := decodeStrictJSON(w, r, &request); err != nil || request.ID <= 0 {
		writeClientError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := transport.ValidateMailboxPassword(request.NewPassword); err != nil {
		writeClientError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	p.mailMutationMu.Lock()
	defer p.mailMutationMu.Unlock()
	if err := p.ensureMailDomainMutable(r.Context(), domainID); err != nil {
		writeMailDomainMutationError(w, err)
		return
	}

	var storedAddress, domain string
	err := p.db.GetDB().QueryRowContext(r.Context(), `
		SELECT account.address, domain.name
		FROM email_accounts account
		JOIN domains domain ON domain.id = account.domain_id
		WHERE account.id = ? AND account.domain_id = ?`,
		request.ID, domainID,
	).Scan(&storedAddress, &domain)
	if errors.Is(err, sql.ErrNoRows) {
		writeClientError(w, http.StatusNotFound, "account not found")
		return
	}
	if err != nil {
		writeServerError(w, err)
		return
	}
	address, err := transport.CanonicalMailboxForDomain(storedAddress, domain)
	if err != nil {
		writeServerError(w, errors.New("stored mailbox identity is invalid"))
		return
	}

	var response transport.MailMutationResponse
	if err := p.callAgentContext(r.Context(), "Agent.UpdateMailPassword", &transport.UpdateMailPasswordRequest{
		ExpectedBuildCommit: strings.TrimSpace(buildCommit),
		Email:               address,
		NewPassword:         request.NewPassword,
	}, &response); err != nil {
		// Do not pass a remote error through the logger: an untrusted agent
		// message must not be able to echo the mailbox address or password.
		writeServerError(w, errors.New("mail password rotation RPC failed"))
		return
	}
	if !response.Applied {
		writeServerError(w, errors.New("mail password rotation was not confirmed"))
		return
	}

	p.audit(r, "mail.account.password.rotate", "email_account", request.ID)
	_ = json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

func (p *Panel) handleDeleteEmailAccount(w http.ResponseWriter, r *http.Request, domainID int) {
	id, err := strconv.Atoi(r.URL.Query().Get("id"))
	if err != nil || id <= 0 {
		writeClientError(w, http.StatusBadRequest, "invalid account id")
		return
	}
	p.mailMutationMu.Lock()
	defer p.mailMutationMu.Unlock()
	if err := p.ensureMailDomainMutable(r.Context(), domainID); err != nil {
		writeMailDomainMutationError(w, err)
		return
	}
	var address, passwordHash string
	var quotaMB int
	err = p.db.GetDB().QueryRowContext(r.Context(),
		"SELECT address, password_hash, quota_mb FROM email_accounts WHERE id = ? AND domain_id = ?",
		id, domainID).Scan(&address, &passwordHash, &quotaMB)
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "account not found", http.StatusNotFound)
		return
	}
	if err != nil {
		writeServerError(w, err)
		return
	}
	tx, err := p.db.GetDB().BeginTx(r.Context(), nil)
	if err != nil {
		writeServerError(w, err)
		return
	}
	if _, err := tx.ExecContext(r.Context(), "DELETE FROM email_accounts WHERE id = ? AND domain_id = ?", id, domainID); err != nil {
		writeServerError(w, rollbackMailTx(tx, err))
		return
	}
	if err := tx.Commit(); err != nil {
		writeServerError(w, err)
		return
	}
	if err := p.callMailMutation(r.Context(), "Agent.DeleteMailAccount",
		&transport.DeleteMailAccountRequest{Email: address}); err != nil {
		compensationCtx, cancel := mailCompensationContext(r.Context())
		_, restoreErr := p.db.GetDB().ExecContext(compensationCtx,
			"INSERT INTO email_accounts (id, domain_id, address, password_hash, quota_mb) VALUES (?, ?, ?, ?, ?)",
			id, domainID, address, passwordHash, quotaMB)
		cancel()
		if restoreErr != nil {
			err = errors.Join(err, fmt.Errorf("database compensation failed: %w", restoreErr))
		}
		writeServerError(w, err)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

func (p *Panel) handleUpdateEmailQuota(w http.ResponseWriter, r *http.Request, domainID int) {
	var request struct {
		ID      int `json:"id"`
		QuotaMB int `json:"quota_mb"`
	}
	if err := decodeStrictJSON(w, r, &request); err != nil ||
		request.ID <= 0 || request.QuotaMB <= 0 || request.QuotaMB > transport.MaxMailboxQuotaMB {
		writeClientError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	p.mailMutationMu.Lock()
	defer p.mailMutationMu.Unlock()
	if err := p.ensureMailDomainMutable(r.Context(), domainID); err != nil {
		writeMailDomainMutationError(w, err)
		return
	}
	tx, err := p.db.GetDB().BeginTx(r.Context(), nil)
	if err != nil {
		writeServerError(w, err)
		return
	}
	var address string
	var oldQuota int
	err = tx.QueryRowContext(r.Context(),
		"SELECT address, quota_mb FROM email_accounts WHERE id = ? AND domain_id = ?",
		request.ID, domainID).Scan(&address, &oldQuota)
	if errors.Is(err, sql.ErrNoRows) {
		_ = tx.Rollback()
		http.Error(w, "account not found", http.StatusNotFound)
		return
	}
	if err != nil {
		writeServerError(w, rollbackMailTx(tx, err))
		return
	}
	if _, err := tx.ExecContext(r.Context(),
		"UPDATE email_accounts SET quota_mb = ?, updated_at = datetime('now') WHERE id = ? AND domain_id = ?",
		request.QuotaMB, request.ID, domainID); err != nil {
		writeServerError(w, rollbackMailTx(tx, err))
		return
	}
	if err := p.callMailMutation(r.Context(), "Agent.UpdateMailQuota",
		&transport.UpdateMailQuotaRequest{Email: address, QuotaMB: request.QuotaMB}); err != nil {
		writeServerError(w, rollbackMailTx(tx, err))
		return
	}
	if err := tx.Commit(); err != nil {
		compensationCtx, cancel := mailCompensationContext(r.Context())
		compensation := p.callMailMutation(compensationCtx, "Agent.UpdateMailQuota",
			&transport.UpdateMailQuotaRequest{Email: address, QuotaMB: oldQuota})
		cancel()
		if compensation != nil {
			err = errors.Join(err, fmt.Errorf("agent compensation failed: %w", compensation))
		}
		writeServerError(w, err)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

func (p *Panel) handleMailQuotaStatus(w http.ResponseWriter, r *http.Request, domainID int) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	rows, err := p.db.GetDB().QueryContext(r.Context(),
		"SELECT address FROM email_accounts WHERE domain_id = ? ORDER BY id", domainID)
	if err != nil {
		writeServerError(w, err)
		return
	}
	defer rows.Close()
	emails := make([]string, 0)
	for rows.Next() {
		var address string
		if err := rows.Scan(&address); err != nil {
			writeServerError(w, err)
			return
		}
		emails = append(emails, address)
	}
	if err := rows.Err(); err != nil {
		writeServerError(w, err)
		return
	}
	var response transport.MailQuotaStatusResponse
	if err := p.callAgentContext(r.Context(), "Agent.GetMailQuotaStatus",
		&transport.MailQuotaStatusRequest{Emails: emails}, &response); err != nil {
		writeServerError(w, err)
		return
	}
	if response.Usages == nil {
		response.Usages = []transport.MailQuotaUsage{}
	}
	_ = json.NewEncoder(w).Encode(response)
}

type mailForwardingQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func loadAllForwardings(ctx context.Context, queryer mailForwardingQueryer) ([]transport.MailForwarding, error) {
	all := make([]transport.MailForwarding, 0)
	rows, err := queryer.QueryContext(ctx, `
		SELECT f.source, f.destination
		FROM email_forwardings f
		LEFT JOIN domain_deletion_operations op ON op.domain_id = f.domain_id
		WHERE op.domain_id IS NULL
		ORDER BY f.id`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var forwarding transport.MailForwarding
		if err := rows.Scan(&forwarding.Source, &forwarding.Destination); err != nil {
			_ = rows.Close()
			return nil, err
		}
		source, err := transport.CanonicalForwardSource(forwarding.Source)
		if err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("invalid stored forwarding source: %w", err)
		}
		destination, err := transport.CanonicalMailAddress(forwarding.Destination)
		if err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("invalid stored forwarding destination: %w", err)
		}
		all = append(all, transport.MailForwarding{Source: source, Destination: destination})
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	catchAllRows, err := queryer.QueryContext(ctx, `
		SELECT '@' || d.name, c.destination
		FROM mail_catch_all c
		JOIN domains d ON d.id = c.domain_id
		LEFT JOIN domain_deletion_operations op ON op.domain_id = c.domain_id
		WHERE op.domain_id IS NULL
		ORDER BY c.domain_id`)
	if err != nil {
		return nil, err
	}
	defer catchAllRows.Close()
	for catchAllRows.Next() {
		var forwarding transport.MailForwarding
		if err := catchAllRows.Scan(&forwarding.Source, &forwarding.Destination); err != nil {
			return nil, err
		}
		source, err := transport.CanonicalForwardSource(forwarding.Source)
		if err != nil {
			return nil, fmt.Errorf("invalid stored catch-all source: %w", err)
		}
		destination, err := transport.CanonicalMailAddress(forwarding.Destination)
		if err != nil {
			return nil, fmt.Errorf("invalid stored catch-all destination: %w", err)
		}
		all = append(all, transport.MailForwarding{Source: source, Destination: destination})
	}
	if err := catchAllRows.Err(); err != nil {
		return nil, err
	}
	return all, nil
}

func (p *Panel) applyForwardingState(ctx context.Context, forwardings []transport.MailForwarding) error {
	return p.callMailMutation(ctx, "Agent.UpdateMailForwarding",
		&transport.UpdateMailForwardingRequest{Forwardings: forwardings})
}

func (p *Panel) mutateForwardings(
	ctx context.Context,
	domainID int,
	mutation func(*sql.Tx) error,
) error {
	if err := p.ensureMailDomainMutable(ctx, domainID); err != nil {
		return err
	}
	previous, err := loadAllForwardings(ctx, p.db.GetDB())
	if err != nil {
		return err
	}
	tx, err := p.db.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := mutation(tx); err != nil {
		return rollbackMailTx(tx, err)
	}
	next, err := loadAllForwardings(ctx, tx)
	if err != nil {
		return rollbackMailTx(tx, err)
	}
	if err := p.applyForwardingState(ctx, next); err != nil {
		return rollbackMailTx(tx, err)
	}
	if err := tx.Commit(); err != nil {
		compensationCtx, cancel := mailCompensationContext(ctx)
		compensation := p.applyForwardingState(compensationCtx, previous)
		cancel()
		if compensation != nil {
			err = errors.Join(err, fmt.Errorf("agent compensation failed: %w", compensation))
		}
		return err
	}
	return nil
}

func (p *Panel) handleAddEmailForwarding(w http.ResponseWriter, r *http.Request, domainID int) {
	var request struct {
		Source      string `json:"source"`
		Destination string `json:"destination"`
	}
	if err := decodeStrictJSON(w, r, &request); err != nil {
		writeClientError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	domain, err := p.domainNameByIDStrict(r.Context(), domainID)
	if err != nil {
		writeServerError(w, err)
		return
	}
	source, err := transport.CanonicalMailboxForDomain(request.Source, domain)
	if err != nil {
		writeClientError(w, http.StatusBadRequest, err.Error())
		return
	}
	destination, err := transport.CanonicalMailAddress(request.Destination)
	if err != nil {
		writeClientError(w, http.StatusBadRequest, err.Error())
		return
	}
	p.mailMutationMu.Lock()
	defer p.mailMutationMu.Unlock()
	var newID int64
	err = p.mutateForwardings(r.Context(), domainID, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(r.Context(),
			"INSERT INTO email_forwardings (domain_id, source, destination) VALUES (?, ?, ?)",
			domainID, source, destination)
		if err != nil {
			return err
		}
		newID, err = result.LastInsertId()
		return err
	})
	if err != nil {
		writeMailDomainMutationError(w, err)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "id": newID})
}

func (p *Panel) handleDeleteEmailForwarding(w http.ResponseWriter, r *http.Request, domainID int) {
	id, err := strconv.Atoi(r.URL.Query().Get("id"))
	if err != nil || id <= 0 {
		writeClientError(w, http.StatusBadRequest, "invalid forwarding id")
		return
	}
	p.mailMutationMu.Lock()
	defer p.mailMutationMu.Unlock()
	err = p.mutateForwardings(r.Context(), domainID, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(r.Context(),
			"DELETE FROM email_forwardings WHERE id = ? AND domain_id = ?", id, domainID)
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count != 1 {
			return sql.ErrNoRows
		}
		return nil
	})
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "forwarding not found", http.StatusNotFound)
		return
	}
	if err != nil {
		writeMailDomainMutationError(w, err)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

func (p *Panel) pushForwardingsToAgent(ctx context.Context) error {
	all, err := loadAllForwardings(ctx, p.db.GetDB())
	if err != nil {
		return err
	}
	return p.applyForwardingState(ctx, all)
}
