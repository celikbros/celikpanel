package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/transport"
)

const (
	startupCertificateDependentTimeout     = panelMailTLSSyncTimeout
	maxStartupSecureMailCertificateDomains = 4096
	maxStartupHostedVhosts                 = 4096
)

var validReferencedStagedLineage = regexp.MustCompile(
	`^cp-site-[1-9][0-9]*-[a-f0-9]{24}$`,
)

type startupPendingCertificateState struct {
	eligible  []int
	activated []int
	skipped   int
}

// referencedStagedCertificateLineages returns every agent-generated lineage
// that remains in the durable certificate ledger, regardless of certificate
// status. Revoked rows are deliberately retained: they are still valid
// rollback targets until their ledger rows are explicitly removed.
func referencedStagedCertificateLineages(
	ctx context.Context,
	database *sql.DB,
) ([]string, error) {
	rows, err := database.QueryContext(ctx, `
		SELECT lineage_name
		FROM ssl_certificates
		WHERE lineage_name IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	referenced := make(map[string]struct{})
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		lineage := strings.ToLower(strings.TrimSpace(raw))
		if validReferencedStagedLineage.MatchString(lineage) {
			referenced[lineage] = struct{}{}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	lineages := make([]string, 0, len(referenced))
	for lineage := range referenced {
		lineages = append(lineages, lineage)
	}
	sort.Strings(lineages)
	return lineages, nil
}

// reconcileCertificateRuntimeAtStartup closes three crash windows using only
// the durable database ledger: orphaned agent-generated certbot lineages are
// removed, and every hosted vhost is regenerated without transient validation
// names. It also republishes the full mail SNI snapshot and reconciles the
// TLSA dependent for each active secure-mail certificate. Failures are visible
// in logs but do not make an otherwise usable panel unavailable; the same
// derived-state reconciliation runs next start. No user setting is changed.
func (p *Panel) reconcileCertificateRuntimeAtStartup() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if err := p.requireMatchingAgentBuild(ctx); err != nil {
		log.Printf("certificate startup reconcile: verify panel-agent build pair: %v", err)
		return
	}

	referencedLineages, err := referencedStagedCertificateLineages(
		ctx,
		p.db.GetDB(),
	)
	if err != nil {
		log.Printf("certificate startup reconcile: list referenced lineages: %v", err)
		return
	}

	var lineageResp transport.ReconcileSiteCertLineagesResponse
	err = p.callAgentContext(ctx, "Agent.ReconcileSiteCertLineages", &transport.ReconcileSiteCertLineagesRequest{
		ExpectedBuildCommit: strings.TrimSpace(buildCommit),
		ReferencedLineages:  referencedLineages,
		// Keep the former field populated during rolling upgrades. Old agents
		// interpret it as the complete retain set, which is exactly what this
		// expanded ledger query now provides.
		ActiveLineages: referencedLineages,
	}, &lineageResp)
	if err != nil || lineageResp.Error != "" {
		log.Printf(
			"certificate startup reconcile: staged lineages: %v %s",
			err, lineageResp.Error,
		)
	} else if lineageResp.Deleted > 0 {
		log.Printf(
			"certificate startup reconcile: removed %d orphaned staged lineages",
			lineageResp.Deleted,
		)
	}

	pending, pendingErr := p.preparePendingCertificatesAtStartup(
		ctx,
		maxStartupHostedVhosts,
	)
	if pendingErr != nil {
		rollbackCtx, rollbackCancel := sslCompensationContext()
		rollbackErr := p.rollbackStartupCertificateActivations(
			rollbackCtx,
			pending.activated,
		)
		rollbackCancel()
		log.Printf(
			"certificate startup reconcile: prepare pending certificate outbox: %v; rollback: %v",
			pendingErr,
			rollbackErr,
		)
		pending = startupPendingCertificateState{}
	} else if pending.skipped > 0 {
		log.Printf(
			"certificate startup reconcile: left %d invalid pending certificates disabled for manual review",
			pending.skipped,
		)
	}

	hostedVhosts, err := p.reconcileHostedVhostsAtStartup(ctx)
	if err != nil {
		rollbackCtx, rollbackCancel := sslCompensationContext()
		rollbackErr := p.rollbackStartupCertificateActivations(
			rollbackCtx,
			pending.activated,
		)
		rollbackCancel()
		log.Printf(
			"certificate startup reconcile: restore hosted vhost batch: %v; pending activation rollback: %v",
			err,
			rollbackErr,
		)
		return
	} else if hostedVhosts > 0 {
		log.Printf(
			"certificate startup reconcile: restored %d hosted vhosts with one nginx validation and reload",
			hostedVhosts,
		)
	}

	dependentCtx, dependentCancel := context.WithTimeout(
		ctx,
		startupCertificateDependentTimeout,
	)
	secureMailDomains, dependentErr := p.reconcileCertificateDependentsAtStartup(dependentCtx)
	dependentCancel()
	if dependentErr != nil {
		promoteCtx, promoteCancel := sslCompensationContext()
		promoteErr := p.promoteStartupCertificateDependents(
			promoteCtx,
			pending.eligible,
		)
		promoteCancel()
		log.Printf(
			"certificate startup reconcile: certificate dependents: %v; preserve pending outbox: %v",
			dependentErr,
			promoteErr,
		)
		return
	}

	clearCtx, clearCancel := sslCompensationContext()
	clearErr := p.clearStartupCertificatePending(
		clearCtx,
		pending.eligible,
	)
	clearCancel()
	if clearErr != nil {
		log.Printf(
			"certificate startup reconcile: clear completed pending certificate outbox: %v",
			clearErr,
		)
		return
	}

	if daneState := currentDANEAutomationState(); !daneState.Enabled {
		log.Printf(
			"certificate startup reconcile: mail SNI reconciled from %d active secure-mail certificates; TLSA left unchanged: %s",
			secureMailDomains,
			daneState.Reason,
		)
	} else {
		log.Printf(
			"certificate startup reconcile: mail SNI and TLSA dependents reconciled from %d active secure-mail certificates",
			secureMailDomains,
		)
	}
	if len(pending.eligible) > 0 {
		log.Printf(
			"certificate startup reconcile: completed %d pending certificate activations",
			len(pending.eligible),
		)
	}
}

func (p *Panel) preparePendingCertificatesAtStartup(
	ctx context.Context,
	limit int,
) (startupPendingCertificateState, error) {
	var state startupPendingCertificateState
	if limit <= 0 {
		return state, errors.New("pending certificate reconciliation limit must be positive")
	}

	rows, err := p.db.GetDB().QueryContext(ctx, `
		SELECT sc.domain_id, COALESCE(sc.renewal_status, '')
		FROM ssl_certificates sc
		JOIN sites s ON s.domain_id = sc.domain_id
		WHERE sc.status = 'active'
		  AND sc.renewal_status IN (?, ?)
		  AND COALESCE(s.project_type, 'php') <> 'dnsonly'
		ORDER BY sc.domain_id
		LIMIT ?`,
		sslPendingActivation,
		sslPendingDependents,
		limit+1,
	)
	if err != nil {
		return state, fmt.Errorf("list pending certificates: %w", err)
	}
	type pendingRow struct {
		domainID int
		pending  string
	}
	var pendingRows []pendingRow
	for rows.Next() {
		var row pendingRow
		if err := rows.Scan(&row.domainID, &row.pending); err != nil {
			rows.Close()
			return state, fmt.Errorf("scan pending certificate: %w", err)
		}
		pendingRows = append(pendingRows, row)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return state, fmt.Errorf("pending certificate rows: %w", err)
	}
	if err := rows.Close(); err != nil {
		return state, fmt.Errorf("pending certificate rows: %w", err)
	}
	if len(pendingRows) > limit {
		return state, fmt.Errorf(
			"pending certificate count exceeds safe startup limit %d",
			limit,
		)
	}

	for _, row := range pendingRows {
		managedNames, err := p.managedSiteHostnames(ctx, row.domainID)
		if err != nil {
			return state, fmt.Errorf(
				"read managed names for pending domain %d: %w",
				row.domainID,
				err,
			)
		}
		runtime, err := p.loadCertificateRuntimeStatus(
			ctx,
			row.domainID,
			managedNames,
		)
		if err != nil {
			return state, fmt.Errorf(
				"read pending certificate for domain %d: %w",
				row.domainID,
				err,
			)
		}
		if !runtime.Info.Valid ||
			!runtime.Info.TrustChecked ||
			!runtime.Info.Trusted ||
			!runtime.CoversManaged {
			if err := p.markCertificatePending(
				ctx,
				row.domainID,
				sslPendingActivation,
				true,
			); err != nil {
				return state, fmt.Errorf(
					"disable invalid pending certificate for domain %d: %w",
					row.domainID,
					err,
				)
			}
			state.skipped++
			continue
		}

		needsActivation := row.pending == sslPendingActivation ||
			!runtime.Activated ||
			!runtime.SitePathsMatch
		if needsActivation {
			if err := p.enableActiveCertificateForRetry(ctx, row.domainID); err != nil {
				return state, fmt.Errorf(
					"prepare pending certificate for domain %d: %w",
					row.domainID,
					err,
				)
			}
			state.activated = append(state.activated, row.domainID)
		}
		state.eligible = append(state.eligible, row.domainID)
	}
	return state, nil
}

func (p *Panel) rollbackStartupCertificateActivations(
	ctx context.Context,
	domainIDs []int,
) error {
	var rollbackErrors []error
	for _, domainID := range domainIDs {
		if err := p.markCertificatePending(
			ctx,
			domainID,
			sslPendingActivation,
			true,
		); err != nil {
			rollbackErrors = append(
				rollbackErrors,
				fmt.Errorf("domain %d: %w", domainID, err),
			)
		}
	}
	return errors.Join(rollbackErrors...)
}

func (p *Panel) promoteStartupCertificateDependents(
	ctx context.Context,
	domainIDs []int,
) error {
	var promoteErrors []error
	for _, domainID := range domainIDs {
		if err := p.markCertificatePending(
			ctx,
			domainID,
			sslPendingDependents,
			false,
		); err != nil {
			promoteErrors = append(
				promoteErrors,
				fmt.Errorf("domain %d: %w", domainID, err),
			)
		}
	}
	return errors.Join(promoteErrors...)
}

func (p *Panel) clearStartupCertificatePending(
	ctx context.Context,
	domainIDs []int,
) error {
	var clearErrors []error
	for _, domainID := range domainIDs {
		if err := p.clearCertificatePending(ctx, domainID); err != nil {
			clearErrors = append(
				clearErrors,
				fmt.Errorf("domain %d: %w", domainID, err),
			)
		}
	}
	return errors.Join(clearErrors...)
}

func (p *Panel) reconcileHostedVhostsAtStartup(
	ctx context.Context,
) (int, error) {
	return p.reconcileHostedVhostsAtStartupWithLimit(
		ctx,
		maxStartupHostedVhosts,
	)
}

func (p *Panel) reconcileHostedVhostsAtStartupWithLimit(
	ctx context.Context,
	limit int,
) (int, error) {
	if limit <= 0 {
		return 0, errors.New("hosted vhost reconciliation limit must be positive")
	}

	rows, err := p.db.GetDB().QueryContext(ctx, `
		SELECT d.id
		FROM domains d
		JOIN sites s ON s.domain_id = d.id
		WHERE COALESCE(s.project_type, 'php') <> 'dnsonly'
		ORDER BY d.id
		LIMIT ?`, limit+1)
	if err != nil {
		return 0, fmt.Errorf("list hosted vhosts: %w", err)
	}
	var domainIDs []int
	for rows.Next() {
		var domainID int
		if err := rows.Scan(&domainID); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan hosted vhost: %w", err)
		}
		domainIDs = append(domainIDs, domainID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, fmt.Errorf("hosted vhost rows: %w", err)
	}
	if err := rows.Close(); err != nil {
		return 0, fmt.Errorf("hosted vhost rows: %w", err)
	}
	if len(domainIDs) > limit {
		return 0, fmt.Errorf(
			"hosted vhost count exceeds safe startup limit %d; nginx was left unchanged",
			limit,
		)
	}
	if len(domainIDs) == 0 {
		return 0, nil
	}

	if err := p.requireMatchingAgentBuild(ctx); err != nil {
		return 0, fmt.Errorf("verify startup vhost batch capability: %w", err)
	}

	requests := make([]applyVhostRPCRequest, 0, len(domainIDs))
	for _, domainID := range domainIDs {
		request, err := p.buildVhostRequest(ctx, domainID, nil)
		if err != nil {
			return 0, fmt.Errorf(
				"prepare hosted vhost for domain %d: %w",
				domainID,
				err,
			)
		}
		requests = append(requests, request)
	}

	var resp transport.ApplyVhostsResponse
	err = p.callAgentContext(
		ctx,
		"Agent.ApplyVhosts",
		&transport.ApplyVhostsRequest{
			ExpectedBuildCommit: strings.TrimSpace(buildCommit),
			Vhosts:              requests,
		},
		&resp,
	)
	if err != nil {
		return 0, fmt.Errorf("apply hosted vhost batch: %w", err)
	}
	if resp.Error != "" {
		return 0, errors.New(resp.Error)
	}
	if resp.Applied != len(requests) {
		return 0, fmt.Errorf(
			"agent reported %d applied startup vhosts, want %d",
			resp.Applied,
			len(requests),
		)
	}
	return resp.Applied, nil
}

// reconcileCertificateDependentsAtStartup republishes derived runtime state
// from the durable certificate ledger. The mail RPC is a single full-state
// push (including an empty snapshot, which removes stale SNI entries). TLSA is
// reconciled once per active secure-mail domain; while the DANE safety gate is
// disabled refreshTLSARecords is deliberately a no-op.
//
// The hard row cap prevents a corrupt or unexpectedly large ledger from
// causing unbounded certificate-inspection RPCs during boot. If the cap is
// exceeded nothing is published, because a partial SNI snapshot would
// destructively erase omitted tenants.
func (p *Panel) reconcileCertificateDependentsAtStartup(
	ctx context.Context,
) (int, error) {
	return p.reconcileCertificateDependentsAtStartupWithLimit(
		ctx,
		maxStartupSecureMailCertificateDomains,
	)
}

func (p *Panel) reconcileCertificateDependentsAtStartupWithLimit(
	ctx context.Context,
	limit int,
) (int, error) {
	if limit <= 0 {
		return 0, errors.New("secure-mail reconciliation limit must be positive")
	}

	rows, err := p.db.GetDB().QueryContext(ctx, `
		SELECT domain_id
		FROM ssl_certificates
		WHERE status = 'active' AND secure_mail = 1
		ORDER BY domain_id
		LIMIT ?`, limit+1)
	if err != nil {
		return 0, fmt.Errorf("list active secure-mail certificates: %w", err)
	}
	var domainIDs []int
	for rows.Next() {
		var domainID int
		if err := rows.Scan(&domainID); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan active secure-mail certificate: %w", err)
		}
		domainIDs = append(domainIDs, domainID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, fmt.Errorf("active secure-mail certificate rows: %w", err)
	}
	if err := rows.Close(); err != nil {
		return 0, fmt.Errorf("active secure-mail certificate rows: %w", err)
	}
	if len(domainIDs) > limit {
		return 0, fmt.Errorf(
			"active secure-mail certificate count exceeds safe startup limit %d; runtime state was left unchanged",
			limit,
		)
	}

	var reconcileErrors []error
	if err := p.resyncMailTLS(ctx); err != nil {
		reconcileErrors = append(
			reconcileErrors,
			fmt.Errorf("publish full mail SNI snapshot: %w", err),
		)
	}
	for _, domainID := range domainIDs {
		if err := p.refreshTLSARecords(ctx, domainID); err != nil {
			reconcileErrors = append(
				reconcileErrors,
				fmt.Errorf("reconcile TLSA for domain %d: %w", domainID, err),
			)
		}
	}
	return len(domainIDs), errors.Join(reconcileErrors...)
}
