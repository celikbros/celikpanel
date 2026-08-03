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

const (
	// The domains table deliberately has a small, schema-enforced lifecycle
	// vocabulary. Reuse its existing pending state as the durable deletion
	// marker; the API uses the more explicit value below so callers can
	// distinguish deletion work from other pending states.
	domainDeletionLedgerStatus  = "pending"
	domainDeletionPendingStatus = "deletion_pending"
)

// markDomainDeletionPending leaves a durable retry handle before any external
// resource is removed. A crash or partial agent failure therefore cannot make
// the remaining work disappear behind an apparently active/deleted domain.
func (p *Panel) markDomainDeletionPending(ctx context.Context, domainID int) error {
	result, err := p.db.GetDB().ExecContext(ctx, `
		UPDATE domains
		SET status = ?, updated_at = datetime('now')
		WHERE id = ?`,
		domainDeletionLedgerStatus,
		domainID,
	)
	if err != nil {
		return fmt.Errorf("mark domain deletion pending: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("confirm domain deletion marker: %w", err)
	}
	if changed != 1 {
		return fmt.Errorf("mark domain deletion pending: expected one domain, changed %d", changed)
	}
	return nil
}

func (p *Panel) restoreDomainStatus(
	ctx context.Context,
	domainID int,
	previousStatus string,
) error {
	result, err := p.db.GetDB().ExecContext(ctx, `
		UPDATE domains
		SET status = ?, updated_at = datetime('now')
		WHERE id = ? AND status = ?`,
		previousStatus,
		domainID,
		domainDeletionLedgerStatus,
	)
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
		WHERE id = ? AND status = ?`,
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
