package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/alicelik/celikpanel/internal/transport"
)

var errDomainDeletionPending = errors.New("domain deletion is pending")

const (
	// The domains table deliberately has a small, schema-enforced lifecycle
	// vocabulary. Pending is the ledger state shared with import work; the
	// domain_deletion_operations row is the durable deletion marker that
	// distinguishes the two and preserves the previous status for rollback.
	domainDeletionLedgerStatus  = "pending"
	domainDeletionPendingStatus = "deletion_pending"
)

// markDomainDeletionPending leaves a durable retry handle before any external
// resource is removed. A crash or partial agent failure therefore cannot make
// the remaining work disappear behind an apparently active/deleted domain.
func (p *Panel) markDomainDeletionPending(ctx context.Context, domainID int) (bool, error) {
	tx, err := p.db.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin domain deletion marker: %w", err)
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `
		INSERT INTO domain_deletion_operations (domain_id, previous_status)
		SELECT id, status FROM domains WHERE id = ?
		ON CONFLICT(domain_id) DO NOTHING`, domainID)
	if err != nil {
		return false, fmt.Errorf("create domain deletion marker: %w", err)
	}
	created, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("confirm domain deletion marker creation: %w", err)
	}
	firstAttempt := created == 1
	if !firstAttempt {
		var status string
		err := tx.QueryRowContext(ctx, `
			SELECT d.status
			FROM domains d
			JOIN domain_deletion_operations op ON op.domain_id = d.id
			WHERE d.id = ?`, domainID).Scan(&status)
		if err != nil {
			return false, fmt.Errorf("read existing domain deletion marker: %w", err)
		}
		if status != domainDeletionLedgerStatus {
			return false, fmt.Errorf("existing domain deletion marker has unexpected domain status %q", status)
		}
	} else {
		result, err = tx.ExecContext(ctx, `
			UPDATE domains
			SET status = ?, updated_at = datetime('now')
			WHERE id = ?`, domainDeletionLedgerStatus, domainID)
		if err != nil {
			return false, fmt.Errorf("mark domain deletion pending: %w", err)
		}
		changed, rowsErr := result.RowsAffected()
		if rowsErr != nil {
			return false, fmt.Errorf("confirm domain deletion status: %w", rowsErr)
		}
		if changed != 1 {
			return false, fmt.Errorf("mark domain deletion pending: expected one domain, changed %d", changed)
		}
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE domain_deletion_operations
		SET updated_at = datetime('now')
		WHERE domain_id = ?`, domainID); err != nil {
		return false, fmt.Errorf("touch domain deletion marker: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit domain deletion marker: %w", err)
	}
	return firstAttempt, nil
}

func (p *Panel) restoreDomainDeletionStart(ctx context.Context, domainID int) error {
	tx, err := p.db.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin domain deletion restoration: %w", err)
	}
	defer tx.Rollback()
	var previousStatus string
	err = tx.QueryRowContext(ctx, `
		SELECT op.previous_status
		FROM domain_deletion_operations op
		JOIN domains d ON d.id = op.domain_id
		WHERE op.domain_id = ? AND d.status = ?`,
		domainID, domainDeletionLedgerStatus,
	).Scan(&previousStatus)
	if err != nil {
		return fmt.Errorf("read domain deletion restoration state: %w", err)
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE domains
		SET status = ?, updated_at = datetime('now')
		WHERE id = ? AND status = ?`,
		previousStatus, domainID, domainDeletionLedgerStatus)
	if err != nil {
		return fmt.Errorf("restore domain status: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("confirm domain status restoration: %w", err)
	}
	if changed != 1 {
		return fmt.Errorf("restore domain status: expected one pending domain, changed %d", changed)
	}
	result, err = tx.ExecContext(ctx,
		`DELETE FROM domain_deletion_operations WHERE domain_id = ?`, domainID)
	if err != nil {
		return fmt.Errorf("remove domain deletion marker: %w", err)
	}
	changed, err = result.RowsAffected()
	if err != nil {
		return fmt.Errorf("confirm domain deletion marker removal: %w", err)
	}
	if changed != 1 {
		return fmt.Errorf("remove domain deletion marker: expected one marker, changed %d", changed)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit domain deletion restoration: %w", err)
	}
	return nil
}

func (p *Panel) ensureMailDomainMutable(ctx context.Context, domainID int) error {
	var marked int
	err := p.db.GetDB().QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM domain_deletion_operations WHERE domain_id = ?
		)`, domainID).Scan(&marked)
	if err != nil {
		return fmt.Errorf("read domain deletion marker: %w", err)
	}
	if marked != 0 {
		return errDomainDeletionPending
	}
	return nil
}

func writeMailDomainMutationError(w http.ResponseWriter, err error) {
	if errors.Is(err, errDomainDeletionPending) {
		writeCodedError(
			w,
			http.StatusConflict,
			errCodeDomainDeletionPending,
			"domain deletion is pending; mail settings cannot be changed",
			"",
		)
		return
	}
	writeServerError(w, err)
}

// removeDomainMailRuntimeLocked requires p.mailMutationMu. Keeping marker
// publication, mail-TLS reconciliation and runtime cleanup under that one
// lock prevents another global forwarding snapshot from observing a marker
// that is later rolled back.
func (p *Panel) removeDomainMailRuntimeLocked(
	ctx context.Context,
	domainID int,
	domain string,
) error {
	var storedDomain string
	err := p.db.GetDB().QueryRowContext(ctx, `
		SELECT d.name
		FROM domains d
		JOIN domain_deletion_operations op ON op.domain_id = d.id
		WHERE d.id = ? AND d.status = ?`,
		domainID, domainDeletionLedgerStatus,
	).Scan(&storedDomain)
	if err != nil {
		return fmt.Errorf("verify domain deletion marker before mail cleanup: %w", err)
	}
	storedDomain = strings.ToLower(strings.TrimSpace(storedDomain))
	if !strings.EqualFold(storedDomain, strings.TrimSpace(domain)) {
		return fmt.Errorf("mail cleanup domain identity changed")
	}

	var response transport.DeleteMailDomainResponse
	err = p.callAgentContext(ctx, "Agent.DeleteMailDomain", &transport.DeleteMailDomainRequest{
		ExpectedBuildCommit: strings.TrimSpace(buildCommit),
		DomainID:            domainID,
		Domain:              storedDomain,
	}, &response)
	if err != nil {
		return fmt.Errorf("remove mail domain runtime: %w", err)
	}
	if !response.Applied {
		return fmt.Errorf("remove mail domain runtime: agent did not confirm convergence")
	}
	return nil
}

func (p *Panel) writeDomainDeletionPending(
	w http.ResponseWriter,
	r *http.Request,
	domainID int,
	domain string,
	stage string,
	cause error,
) {
	log.Printf("domain deletion pending for %s at %s: %v", domain, stage, cause)
	p.audit(r, "domain.delete.pending:"+domain+":"+stage, "domain", domainID)
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  domainDeletionPendingStatus,
		"domain":  domain,
		"stage":   stage,
		"message": "Deletion is incomplete but retryable. Retry this deletion.",
	})
}

// removeDomainDNSForDeletion reconciles the public DNS copy before the domain
// ledger row is finalized. Every step is idempotent, so a retry is safe after
// a timeout or process crash with an ambiguous remote result.
func (p *Panel) removeDomainDNSForDeletion(
	ctx context.Context,
	domain string,
	parentDomain string,
) error {
	if parentDomain != "" {
		_, err := p.removeSubdomainFromParentZone(ctx, parentDomain, domain)
		return err
	}

	var zoneID int
	err := p.db.GetDB().QueryRowContext(ctx,
		`SELECT id FROM pdns_domains WHERE name = ?`, domain,
	).Scan(&zoneID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("find DNS zone: %w", err)
	}

	// Publish the remote deletion while the local zone remains a durable retry
	// handle. If the RPC is ambiguous, the next DELETE repeats it safely.
	if err := p.syncZoneToDNS(ctx, domain, true); err != nil {
		return fmt.Errorf("publish DNS zone deletion: %w", err)
	}

	tx, err := p.db.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin DNS ledger cleanup: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM pdns_records WHERE domain_id = ?`, zoneID,
	); err != nil {
		return fmt.Errorf("remove DNS records from ledger: %w", err)
	}
	result, err := tx.ExecContext(ctx,
		`DELETE FROM pdns_domains WHERE id = ? AND name = ?`, zoneID, domain,
	)
	if err != nil {
		return fmt.Errorf("remove DNS zone from ledger: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("confirm DNS zone ledger cleanup: %w", err)
	}
	if changed != 1 {
		return fmt.Errorf("remove DNS zone from ledger: expected one zone, changed %d", changed)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit DNS ledger cleanup: %w", err)
	}
	return nil
}

func (p *Panel) removeManagedDomainCertificates(
	ctx context.Context,
	domain string,
	deleteCanonical bool,
	stagedLineages []string,
) error {
	if !deleteCanonical && len(stagedLineages) == 0 {
		return nil
	}
	var response transport.DeleteCertLineageResponse
	err := p.callAgentContext(ctx, "Agent.DeleteCertLineage", &transport.DeleteCertLineageRequest{
		Domain:              domain,
		DeleteCanonical:     deleteCanonical,
		LineageNames:        stagedLineages,
		ExpectedBuildCommit: strings.TrimSpace(buildCommit),
	}, &response)
	if err != nil {
		return fmt.Errorf("remove managed certificate lineages: %w", err)
	}
	if response.Error != "" {
		return fmt.Errorf("remove managed certificate lineages: %s", response.Error)
	}
	return nil
}

func (p *Panel) finalizeDomainDeletion(ctx context.Context, domainID int) error {
	result, err := p.db.GetDB().ExecContext(ctx, `
		DELETE FROM domains
		WHERE id = ? AND status = ?
		  AND EXISTS (
			SELECT 1
			FROM domain_deletion_operations op
			WHERE op.domain_id = domains.id
		  )`,
		domainID,
		domainDeletionLedgerStatus,
	)
	if err != nil {
		return fmt.Errorf("delete domain ledger row: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("confirm domain ledger deletion: %w", err)
	}
	if changed != 1 {
		return fmt.Errorf("delete domain ledger row: expected one pending domain, changed %d", changed)
	}
	return nil
}
