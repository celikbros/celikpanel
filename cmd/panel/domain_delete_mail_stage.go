package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/rpc"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Domain deletion, mail stage (P4-2, pair 4 t2, 2026-09-30).
//
// Whether this server holds a mail runtime for a domain is decided from the
// product's own records, never by probing the filesystem:
//
//   - the domain has mailboxes, forwardings or a catch-all in this panel's
//     ledger (email_accounts, email_forwardings, mail_catch_all), or
//   - this panel installed a mail service on this server: a succeeded
//     service_install of postfix/dovecot or mail_profile_install in the
//     append-only service_operations ledger. Such a server can still carry
//     product-written map lines and maildirs of mailboxes deleted earlier.
//
// Without either record the stage is a no-op recorded as "no mail runtime for
// this domain on this server" and the deletion proceeds to its next stage.
//
// A mail-stage error the Agent answered (rpc.ServerError, or a reply that did
// not confirm convergence) is a verified failure: its typed reason and first
// error line are kept with the deletion marker so a later read reports
// "failed", not "unknown". A transport loss is an unknown outcome and clears
// an earlier recorded failure rather than presenting it as current.

const (
	domainDeletionStageMailRuntime        = "mail_runtime_cleanup"
	domainDeletionReasonMailCleanupFailed = "mail_runtime_cleanup_failed"
	domainDeletionStatusFailed            = "failed"

	// Versioned panel_settings record, one per domain id; no schema change.
	domainDeletionFailureKeyPrefix = "domain_deletion_failure_v1:"
	domainDeletionFailureVersion   = 1
	domainDeletionErrorLineMax     = 200

	domainMailCleanupFailedEnglish = "Deletion stopped while removing this domain's mail from this server. " +
		"The DNS zone and the website are unchanged. The server owner checks, on this server, the mail " +
		"storage named in error_line (normally /var/mail/vhosts, or /var/spool/mail/vhosts where /var/mail " +
		"is the distribution's link; no other symbolic link may be in that path), then retries this same " +
		"deletion, which repeats the mail cleanup and continues with the remaining steps."
)

var errDomainMailCleanupNotConfirmed = errors.New("remove mail domain runtime: agent did not confirm convergence")

// domainDeletionStageFailure is a verified, typed failure of one deletion stage.
type domainDeletionStageFailure struct {
	Stage     string
	Reason    string
	ErrorLine string
	Cause     error
}

func (f *domainDeletionStageFailure) Error() string { return f.Cause.Error() }
func (f *domainDeletionStageFailure) Unwrap() error { return f.Cause }

type domainDeletionFailureRecord struct {
	Version    int    `json:"version"`
	DomainID   int    `json:"domain_id"`
	Domain     string `json:"domain"`
	Stage      string `json:"stage"`
	Reason     string `json:"reason"`
	ErrorLine  string `json:"error_line"`
	RecordedAt string `json:"recorded_at"`
}

func domainDeletionFailureKey(domainID int) string {
	return domainDeletionFailureKeyPrefix + strconv.Itoa(domainID)
}

// domainHasProductMailRuntime reads only the panel's own records.
func domainHasProductMailRuntime(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, domainID int) (bool, error) {
	var present int
	err := q.QueryRowContext(ctx, `
		SELECT EXISTS(SELECT 1 FROM email_accounts WHERE domain_id = ?)
		    OR EXISTS(SELECT 1 FROM email_forwardings WHERE domain_id = ?)
		    OR EXISTS(SELECT 1 FROM mail_catch_all WHERE domain_id = ?)
		    OR EXISTS(
		        SELECT 1 FROM service_operations
		        WHERE status = 'succeeded'
		          AND (kind = ? OR (kind = ? AND service_id IN ('postfix', 'dovecot')))
		    )`,
		domainID, domainID, domainID,
		serviceOperationKindMailProfileInstall, serviceOperationKindInstall,
	).Scan(&present)
	if err != nil {
		return false, fmt.Errorf("read mail runtime records: %w", err)
	}
	return present != 0, nil
}

// classifyDomainMailCleanupFailure returns a typed failure only when the Agent
// answered. Anything else (transport loss, cancellation) is not verified.
func classifyDomainMailCleanupFailure(err error) (*domainDeletionStageFailure, bool) {
	var serverErr rpc.ServerError
	if !errors.As(err, &serverErr) && !errors.Is(err, errDomainMailCleanupNotConfirmed) {
		return nil, false
	}
	return &domainDeletionStageFailure{
		Stage:     domainDeletionStageMailRuntime,
		Reason:    domainDeletionReasonMailCleanupFailed,
		ErrorLine: boundedDeletionErrorLine(err.Error()),
		Cause:     err,
	}, true
}

var deletionErrorSecretPattern = regexp.MustCompile(`\{[A-Za-z0-9._-]+\}\S*|\$[0-9a-z]{1,8}\$\S*`)

// boundedDeletionErrorLine keeps the first line, redacts password-hash shaped
// tokens, drops control characters and bounds the length.
func boundedDeletionErrorLine(text string) string {
	if index := strings.IndexAny(text, "\r\n"); index >= 0 {
		text = text[:index]
	}
	text = deletionErrorSecretPattern.ReplaceAllString(text, "[redacted]")
	text = strings.Map(func(r rune) rune {
		if r == utf8.RuneError || unicode.IsControl(r) {
			return -1
		}
		return r
	}, text)
	text = strings.TrimSpace(text)
	if utf8.RuneCountInString(text) > domainDeletionErrorLineMax {
		runes := []rune(text)
		text = strings.TrimSpace(string(runes[:domainDeletionErrorLineMax-1])) + "…"
	}
	if text == "" {
		text = "the mail cleanup reported no further detail"
	}
	return text
}

// recordDomainDeletionFailure stores the failure only while this exact
// deletion marker exists.
func (p *Panel) recordDomainDeletionFailure(
	ctx context.Context,
	domainID int,
	domain string,
	failure *domainDeletionStageFailure,
) error {
	raw, err := json.Marshal(domainDeletionFailureRecord{
		Version: domainDeletionFailureVersion, DomainID: domainID,
		Domain: strings.ToLower(strings.TrimSpace(domain)), Stage: failure.Stage,
		Reason: failure.Reason, ErrorLine: failure.ErrorLine,
		RecordedAt: time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		return err
	}
	_, err = p.db.GetDB().ExecContext(ctx, `
		INSERT INTO panel_settings (key, value, updated_at)
		SELECT ?, ?, CURRENT_TIMESTAMP
		WHERE EXISTS (SELECT 1 FROM domain_deletion_operations WHERE domain_id = ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		domainDeletionFailureKey(domainID), string(raw), domainID)
	if err != nil {
		return fmt.Errorf("record domain deletion failure: %w", err)
	}
	return nil
}

func (p *Panel) clearDomainDeletionFailure(ctx context.Context, domainID int) error {
	if _, err := p.db.GetDB().ExecContext(ctx,
		`DELETE FROM panel_settings WHERE key = ?`, domainDeletionFailureKey(domainID)); err != nil {
		return fmt.Errorf("clear domain deletion failure: %w", err)
	}
	return nil
}

// readDomainDeletionFailure is read-only. A missing, malformed, foreign or
// unreviewed record is reported as absent, so the status stays unknown.
func readDomainDeletionFailure(ctx context.Context, tx *sql.Tx, domainID int, domain string) (domainDeletionFailureRecord, bool, error) {
	var raw string
	err := tx.QueryRowContext(ctx, `SELECT value FROM panel_settings WHERE key = ?`,
		domainDeletionFailureKey(domainID)).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return domainDeletionFailureRecord{}, false, nil
	}
	if err != nil {
		return domainDeletionFailureRecord{}, false, err
	}
	var record domainDeletionFailureRecord
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&record) != nil ||
		record.Version != domainDeletionFailureVersion || record.DomainID != domainID ||
		record.Domain != strings.ToLower(strings.TrimSpace(domain)) ||
		record.Stage != domainDeletionStageMailRuntime ||
		record.Reason != domainDeletionReasonMailCleanupFailed ||
		record.ErrorLine == "" || utf8.RuneCountInString(record.ErrorLine) > domainDeletionErrorLineMax {
		return domainDeletionFailureRecord{}, false, nil
	}
	return record, true, nil
}

// runDomainDeletionMailStageLocked requires p.mailMutationMu. It returns
// ran=false when the product has no mail runtime for the domain.
func (p *Panel) runDomainDeletionMailStageLocked(
	ctx context.Context,
	domainID int,
	domain string,
) (bool, error) {
	present, err := domainHasProductMailRuntime(ctx, p.db.GetDB(), domainID)
	if err != nil {
		return false, err
	}
	if !present {
		return false, p.clearDomainDeletionFailure(ctx, domainID)
	}
	err = p.removeDomainMailRuntimeLocked(ctx, domainID, domain)
	if err == nil {
		return true, p.clearDomainDeletionFailure(ctx, domainID)
	}
	failure, verified := classifyDomainMailCleanupFailure(err)
	if !verified {
		// Unknown outcome: an older recorded failure is no longer current.
		if clearErr := p.clearDomainDeletionFailure(ctx, domainID); clearErr != nil {
			return true, errors.Join(err, clearErr)
		}
		return true, err
	}
	if recordErr := p.recordDomainDeletionFailure(ctx, domainID, domain, failure); recordErr != nil {
		failure.Cause = errors.Join(err, recordErr)
	}
	return true, failure
}
