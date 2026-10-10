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
//
// The result names only the one step that may be retried later in this
// process: the mail dependents, when the host refused them as busy (a Panel
// started inside an update or rollback). Every other outcome returns the zero
// value and keeps the next-start behaviour.
func (p *Panel) reconcileCertificateRuntimeAtStartup() startupCertificateDeferral {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if err := p.requireMatchingAgentBuild(ctx); err != nil {
		log.Printf("certificate startup reconcile: verify panel-agent build pair: %v", err)
		return startupCertificateDeferral{}
	}

	referencedLineages, err := referencedStagedCertificateLineages(
		ctx,
		p.db.GetDB(),
	)
	if err != nil {
		log.Printf("certificate startup reconcile: list referenced lineages: %v", err)
		return startupCertificateDeferral{}
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

	hostedVhosts, err := p.reconcileHostedVhostsAtStartupDetailed(ctx, maxStartupHostedVhosts)
	logHostedVhostStartup(hostedVhosts)
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
		return startupCertificateDeferral{}
	}
	if len(hostedVhosts.notApplied) > 0 {
		// D-031: a pending activation whose site file was kept, missing or not
		// written is not complete; only those return to pending, the other
		// sites' activations go on.
		pending = p.withdrawStartupActivations(pending, hostedVhosts.notApplied)
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
		// upd11 F2: inside an update or rollback the host refuses the mail
		// publication as busy, and nothing else repeats it in this process.
		// Say so in the same line and hand the step to the deferred retry.
		deferral := startupCertificateDeferral{}
		retryNote := ""
		if startupWorkDeferredForBusyHost(dependentErr) {
			deferral = startupCertificateDeferral{
				deferred:       true,
				pendingDomains: append([]int(nil), pending.eligible...),
			}
			retryNote = "; " + startupDeferredRetryNote
		}
		log.Printf(
			"certificate startup reconcile: certificate dependents: %v; preserve pending outbox: %v%s",
			dependentErr,
			promoteErr,
			retryNote,
		)
		return deferral
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
		return startupCertificateDeferral{}
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
	return startupCertificateDeferral{}
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

func (p *Panel) withdrawStartupActivations(
	pending startupPendingCertificateState,
	notApplied map[int]bool,
) startupPendingCertificateState {
	eligible := make([]int, 0, len(pending.eligible))
	for _, domainID := range pending.eligible {
		if !notApplied[domainID] {
			eligible = append(eligible, domainID)
		}
	}
	activated := make([]int, 0, len(pending.activated))
	var rollback []int
	for _, domainID := range pending.activated {
		if notApplied[domainID] {
			rollback = append(rollback, domainID)
			continue
		}
		activated = append(activated, domainID)
	}
	if withdrawn := len(pending.eligible) - len(eligible); withdrawn > 0 {
		rollbackCtx, rollbackCancel := sslCompensationContext()
		err := p.rollbackStartupCertificateActivations(rollbackCtx, rollback)
		rollbackCancel()
		log.Printf(
			"certificate startup reconcile: %d pending certificate activations stay pending because their site configuration file was not written; rollback: %v",
			withdrawn, err,
		)
	}
	pending.eligible = eligible
	pending.activated = activated
	return pending
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
	result, err := p.reconcileHostedVhostsAtStartupDetailed(ctx, limit)
	if err != nil {
		return 0, err
	}
	return result.applied, nil
}

// hostedVhostStartupResult is the start-up batch per site (D-031): which
// domains did not end with the Panel's text on disk (kept, missing, refused,
// failed), and the counts of the start line.
type hostedVhostStartupResult struct {
	applied     int
	notApplied  map[int]bool
	counts      transport.SiteFileCounts
	agentLegacy bool
}

// reconcileHostedVhostsAtStartupDetailed renders every hosted site and sends
// one batch. A site whose render input cannot be prepared is that site's
// failure; the others are sent (D-031 point 5). The Agent classifies each file
// and writes only a managed and unchanged one whose text differs: nothing is
// written and nginx is not reloaded when every file already is the Panel's
// text. A removed file is not recreated at start.
func (p *Panel) reconcileHostedVhostsAtStartupDetailed(
	ctx context.Context,
	limit int,
) (hostedVhostStartupResult, error) {
	result := hostedVhostStartupResult{notApplied: map[int]bool{}}
	if limit <= 0 {
		return result, errors.New("hosted vhost reconciliation limit must be positive")
	}

	rows, err := p.db.GetDB().QueryContext(ctx, `
		SELECT d.id
		FROM domains d
		JOIN sites s ON s.domain_id = d.id
		WHERE COALESCE(s.project_type, 'php') <> 'dnsonly'
		ORDER BY d.id
		LIMIT ?`, limit+1)
	if err != nil {
		return result, fmt.Errorf("list hosted vhosts: %w", err)
	}
	var domainIDs []int
	for rows.Next() {
		var domainID int
		if err := rows.Scan(&domainID); err != nil {
			rows.Close()
			return result, fmt.Errorf("scan hosted vhost: %w", err)
		}
		domainIDs = append(domainIDs, domainID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return result, fmt.Errorf("hosted vhost rows: %w", err)
	}
	if err := rows.Close(); err != nil {
		return result, fmt.Errorf("hosted vhost rows: %w", err)
	}
	if len(domainIDs) > limit {
		return result, fmt.Errorf(
			"hosted vhost count exceeds safe startup limit %d; nginx was left unchanged",
			limit,
		)
	}
	if len(domainIDs) == 0 {
		return result, nil
	}

	if err := p.requireMatchingAgentBuild(ctx); err != nil {
		return result, fmt.Errorf("verify startup vhost batch capability: %w", err)
	}

	requests := make([]applyVhostRPCRequest, 0, len(domainIDs))
	for _, domainID := range domainIDs {
		request, err := p.buildVhostRequest(ctx, domainID, nil)
		if err != nil {
			// One site's input (an unreadable certificate) is that site's
			// failure, named; the other sites are still rendered.
			result.notApplied[domainID] = true
			result.counts.Failed++
			if siteID, domain, idErr := p.siteIDForDomain(ctx, domainID); idErr == nil {
				log.Printf("site configuration %s (startup): not rendered: %v", domain, err)
				p.recordSiteFile(ctx, siteID, domainID, domain, transport.SiteFileTriggerStartup, nil, siteConfigReasonRenderInput)
			} else {
				log.Printf("site configuration of domain %d (startup): not rendered: %v", domainID, err)
			}
			continue
		}
		request.FileTrigger = transport.SiteFileTriggerStartup
		request.RecordedSHA256 = p.recordedVhostSHA256(ctx, request.SiteID)
		requests = append(requests, request)
	}
	if len(requests) == 0 {
		return result, nil
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
		return result, fmt.Errorf("apply hosted vhost batch: %w", err)
	}

	if len(resp.Items) == 0 {
		// An Agent that predates D-031: its old answer, and every file's
		// state is unknown.
		if resp.Error != "" {
			return result, errors.New(resp.Error)
		}
		if resp.Applied != len(requests) {
			return result, fmt.Errorf(
				"agent reported %d applied startup vhosts, want %d",
				resp.Applied,
				len(requests),
			)
		}
		result.agentLegacy = true
		for _, request := range requests {
			p.recordSiteFile(ctx, request.SiteID, request.DomainID, request.Domain,
				transport.SiteFileTriggerStartup, nil, siteConfigReasonAgentDoesNotReport)
		}
		result.applied = resp.Applied
		return result, nil
	}

	byDomain := make(map[int]applyVhostRPCRequest, len(requests))
	for _, request := range requests {
		byDomain[request.DomainID] = request
	}
	for _, item := range resp.Items {
		request, ok := byDomain[item.DomainID]
		if !ok {
			continue
		}
		file := item.File
		if item.Error != "" {
			log.Printf("site configuration %s (startup): not rendered: %s", request.Domain, boundedAgentDiagnostic(item.Error))
		}
		if file.Path == "" {
			p.recordSiteFile(ctx, request.SiteID, request.DomainID, request.Domain,
				transport.SiteFileTriggerStartup, nil, siteConfigReasonRenderInput)
		} else {
			p.recordSiteFile(ctx, request.SiteID, request.DomainID, request.Domain,
				transport.SiteFileTriggerStartup, &file, "")
		}
		if !siteFileApplied(file.Outcome) {
			result.notApplied[item.DomainID] = true
		}
	}
	if resp.Counts != nil {
		counts := *resp.Counts
		counts.Failed += result.counts.Failed
		result.counts = counts
	}
	result.applied = resp.Applied
	if resp.Error != "" {
		return result, errors.New(resp.Error)
	}
	return result, nil
}

// logHostedVhostStartup is the start line (D-031 point 5): the counts, and
// the first state after an update from a release without the header.
func logHostedVhostStartup(result hostedVhostStartupResult) {
	if result.agentLegacy {
		log.Printf(
			"certificate startup reconcile: restored %d hosted vhosts; the Agent did not report what it found in each file, so their state is unknown",
			result.applied,
		)
		return
	}
	counts := result.counts
	total := counts.Written + counts.Unchanged + counts.Kept + counts.Foreign + counts.Unknown +
		counts.Unreadable + counts.Missing + counts.Failed
	if total == 0 {
		return
	}
	log.Printf(
		"site configuration files at start: %d written, %d unchanged, %d kept (owner-edited), %d kept (foreign), %d kept (unknown origin), %d unreadable or unwritable, %d missing (not recreated), %d failed; %d adopted from an earlier release",
		counts.Written, counts.Unchanged, counts.Kept, counts.Foreign, counts.Unknown,
		counts.Unreadable, counts.Missing, counts.Failed, counts.Adopted,
	)
	if counts.Adopted > 0 || counts.Unknown > 0 {
		log.Printf(
			"site configuration files at start: %d adopted, %d left alone because they differ from every known CelikPanel text",
			counts.Adopted, counts.Unknown,
		)
	}
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
