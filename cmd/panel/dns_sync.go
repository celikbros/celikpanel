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
	"sync"
	"time"

	"github.com/alicelik/celikpanel/internal/hostname"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

// The ledger↔pdns bridge. Zone records live in the panel's own database
// (the ledger, ownership-filtered, edited via the UI); PowerDNS serves from
// its own separate sqlite file. Every zone mutation ends with a push of the
// full zone through the agent — full-zone rewrite is idempotent, so a missed
// push is repaired by the next one. Best-effort by design: DNS serving being
// down must not block panel edits; failures land in the log.
//
// Defter↔pdns köprüsü. Zone kayıtları panelin kendi veritabanında yaşar
// (defter, sahiplik-süzgeçli, arayüzden düzenlenir); PowerDNS kendi ayrı
// sqlite dosyasından sunar. Her zone değişikliği, tam zone'un agent
// üzerinden itilmesiyle biter — tam-zone yazımı idempotenttir; kaçan bir
// itiş sonrakiyle onarılır. Bilerek en-iyi-çaba: DNS sunumunun kapalı olması
// panel düzenlemelerini engellememeli; hatalar günlüğe düşer.

type zoneRecord = transport.ZoneRecord

const (
	dnsZoneSyncMaxAttempts  = 4
	dnsZoneSyncLeaseTimeout = 2 * time.Minute
	dnsZoneSyncBatchTimeout = 45 * time.Minute
)

// dnsPublicationMu is always taken after serviceMutationMu. The database
// lease is the durable cross-restart authority; this process lock prevents
// two local publishers from racing while either one is constructing the
// exact full-zone snapshot that will be committed before BeginServiceMutation.
var dnsPublicationMu sync.Mutex

type dnsZoneSyncState struct {
	ZoneName          string
	SourceDomainID    sql.NullInt64
	DesiredGeneration int64
	AppliedGeneration int64
	DesiredAction     string
	DesiredZoneType   string
	Status            string
	LastError         sql.NullString
	LeaseRequestID    sql.NullString
	LeaseOwnerID      sql.NullString
	LeaseGeneration   sql.NullInt64
	LeaseAction       sql.NullString
	LeaseZoneType     sql.NullString
	LeaseQualifier    sql.NullString
	LeaseExpiresAt    sql.NullString
}

func (state dnsZoneSyncState) hasLease() bool {
	return state.LeaseRequestID.Valid && state.LeaseOwnerID.Valid &&
		state.LeaseGeneration.Valid && state.LeaseAction.Valid &&
		state.LeaseZoneType.Valid && state.LeaseQualifier.Valid &&
		state.LeaseExpiresAt.Valid
}

func (state dnsZoneSyncState) leaseIdentity() agentMutationIdentity {
	return agentMutationIdentity{
		RequestID: state.LeaseRequestID.String,
		OwnerID:   state.LeaseOwnerID.String,
		Kind:      "dns_zone_sync", Target: state.ZoneName,
		PackageName: state.LeaseQualifier.String,
	}
}

type dnsZoneSyncPlan struct {
	State      dnsZoneSyncState
	Commitment mutationpayload.DNSZoneSyncCommitment
	RequestID  string
	OwnerID    string
}

type dnsZoneExistingLeaseError struct {
	State dnsZoneSyncState
}

func (e *dnsZoneExistingLeaseError) Error() string {
	return fmt.Sprintf("DNS zone %s already has a durable publication lease", e.State.ZoneName)
}

var errDNSZoneAlreadyAbsent = errors.New("DNS zone and deletion marker are already absent")

type dnsZoneSyncFailure struct {
	Zone string
	Err  error
}

// dnsSyncAllResult records the whole publication attempt. Settings changes use
// Failures as a hard error; ordinary record edits call syncZoneToDNS directly
// and return an explicit saved-but-not-published conflict when it fails.
type dnsSyncAllResult struct {
	Attempted int
	Synced    int
	Failures  []dnsZoneSyncFailure
}

type dnsPublicationError struct {
	Result dnsSyncAllResult
}

// dnsAgentPublicationError marks failures that happened at the publication
// boundary: the agent RPC failed, rejected the zone, or did not confirm it.
// Only these failures are safe to present as a retryable HTTP 409. Ledger
// preparation/read failures remain internal HTTP 500 errors.
type dnsAgentPublicationError struct {
	Err error
}

func (e *dnsAgentPublicationError) Error() string {
	return e.Err.Error()
}

func (e *dnsAgentPublicationError) Unwrap() error {
	return e.Err
}

type dnsSyncInternalError struct {
	Result dnsSyncAllResult
}

func formatDNSFailures(prefix string, result dnsSyncAllResult) string {
	details := make([]string, 0, len(result.Failures))
	for _, failure := range result.Failures {
		details = append(details, fmt.Sprintf("%s: %v", failure.Zone, failure.Err))
	}
	return fmt.Sprintf("%s for %d of %d zones: %s",
		prefix, len(result.Failures), result.Attempted, strings.Join(details, "; "))
}

func (e *dnsPublicationError) Error() string {
	return formatDNSFailures("DNS publication failed", e.Result)
}

func (e *dnsSyncInternalError) Error() string {
	return formatDNSFailures("DNS synchronization failed", e.Result)
}

func (r dnsSyncAllResult) err() error {
	if len(r.Failures) == 0 {
		return nil
	}
	for _, failure := range r.Failures {
		var publicationErr *dnsAgentPublicationError
		if !errors.As(failure.Err, &publicationErr) {
			return &dnsSyncInternalError{Result: r}
		}
	}
	return &dnsPublicationError{Result: r}
}

// writeDNSPublicationConflict exposes an operational, retryable publication
// failure without leaking agent output. It returns false for database and
// other internal errors so callers can keep treating those as HTTP 500.
func writeDNSPublicationConflict(w http.ResponseWriter, err error, safeMessage string) bool {
	var publicationErr *dnsPublicationError
	if !errors.As(err, &publicationErr) {
		return false
	}
	log.Printf("[409][dns] %v", err)
	writeCodedError(w, http.StatusConflict, errCodeDNSPublicationFailed, safeMessage, "")
	return true
}

// syncZoneToDNS publishes one exact desired generation. The in-process lock
// order is serviceMutationMu -> dnsPublicationMu; the durable per-zone lease
// is committed before the agent mutation is begun.
func (p *Panel) syncZoneToDNS(ctx context.Context, domain string, deleted bool) error {
	p.serviceMutationMu.Lock()
	defer p.serviceMutationMu.Unlock()
	dnsPublicationMu.Lock()
	defer dnsPublicationMu.Unlock()
	return p.syncZoneToDNSLocked(ctx, domain, deleted)
}

func (p *Panel) syncZoneToDNSLocked(ctx context.Context, domain string, deleted bool) error {
	if err := p.requireNoPendingDNSClusterSaga(ctx); err != nil {
		return &dnsAgentPublicationError{Err: err}
	}
	canonicalDomain, err := hostname.CanonicalFQDN(domain)
	if err != nil {
		return fmt.Errorf("canonicalize DNS zone: %w", err)
	}
	publisher, ready, err := p.activeDNSPublisher(ctx)
	if err != nil {
		return &dnsAgentPublicationError{Err: fmt.Errorf("verify active DNS publisher: %w", err)}
	}
	if !ready {
		return &dnsAgentPublicationError{Err: errors.New("no proven active DNS publisher is ready")}
	}
	if !transport.ValidDNSEngine(publisher.Engine) || publisher.Epoch < 1 {
		return &dnsAgentPublicationError{Err: errors.New("active DNS publisher identity is invalid")}
	}
	return p.syncZoneToDNSV3Locked(ctx, canonicalDomain, deleted, publisher)
}

// prepareDNSZoneSyncPlanReconciledLocked prepares a fresh exact publication
// lease while preserving the authority of any earlier lease. Callers hold
// serviceMutationMu followed by dnsPublicationMu. An active or unqueryable
// earlier child is returned as a retryable publication error and remains
// byte-for-byte persisted; terminal and proven-never-begun children are
// reconciled before a fresh snapshot is prepared.
func (p *Panel) prepareDNSZoneSyncPlanReconciledLocked(
	ctx context.Context,
	domain string,
	deleted bool,
) (dnsZoneSyncPlan, error) {
	for attempt := 0; attempt < dnsZoneSyncMaxAttempts; attempt++ {
		plan, err := p.prepareDNSZoneSyncPlan(ctx, domain, deleted)
		var existing *dnsZoneExistingLeaseError
		if !errors.As(err, &existing) {
			return plan, err
		}
		if _, reconcileErr := p.reconcileDNSZoneLease(
			ctx, existing.State, false,
		); reconcileErr != nil {
			return dnsZoneSyncPlan{}, reconcileErr
		}
	}
	return dnsZoneSyncPlan{}, errors.New(
		"DNS publication lease changed during every bounded preparation attempt",
	)
}

// publishPreparedDNSZoneSyncPlanLocked consumes the exact already-persisted
// panel lease without preparing (or briefly releasing) a replacement. DNSSEC
// uses this after signing so a process crash at every boundary leaves startup
// with the authoritative publication lease created before the host mutation.
func (p *Panel) publishPreparedDNSZoneSyncPlanLocked(
	ctx context.Context,
	plan dnsZoneSyncPlan,
) (bool, error) {
	req := transport.SyncDNSZoneV2Request{
		DesiredGeneration: plan.Commitment.DesiredGeneration,
		Domain:            plan.Commitment.Domain,
		Delete:            plan.Commitment.Delete,
		ZoneType:          plan.Commitment.ZoneType,
		Records:           append([]zoneRecord(nil), plan.Commitment.Records...),
	}
	var resp transport.SyncDNSZoneV2Response
	op := serviceOperation{
		RequestID:   plan.RequestID,
		Kind:        "dns_zone_sync",
		ServiceID:   plan.Commitment.Domain,
		PackageName: plan.Commitment.Qualifier,
	}
	callErr := p.withStandaloneAgentMutationIdentity(
		ctx,
		op,
		plan.OwnerID,
		func(callCtx context.Context, binding agentMutationBinding) error {
			req.ServiceMutationBinding = binding
			if err := p.callAgentContext(
				callCtx, "Agent.SyncDNSZoneV2", &req, &resp,
			); err != nil {
				return err
			}
			if resp.Error != "" {
				return errors.New(resp.Error)
			}
			if !resp.Synced {
				return errors.New("agent did not confirm DNS publication")
			}
			if resp.AppliedGeneration != plan.Commitment.DesiredGeneration {
				return errors.New("agent confirmed a different DNS generation")
			}
			return nil
		},
	)
	if callErr != nil {
		done, retry, settleErr := p.settleDNSZoneSyncCallError(
			ctx, plan.State, callErr,
		)
		if settleErr != nil {
			return false, settleErr
		}
		if done {
			return true, nil
		}
		if retry {
			return false, errors.New(
				"DNS zone changed while its prepared publication was in flight",
			)
		}
		return false, &dnsAgentPublicationError{Err: callErr}
	}
	finalizeCtx, cancel := dnsZoneFinalizeContext(ctx)
	exact, err := p.recordDNSZoneSyncSuccess(finalizeCtx, plan.State)
	cancel()
	if err != nil {
		return false, fmt.Errorf("record prepared DNS publication success: %w", err)
	}
	if !exact {
		return false, errors.New(
			"DNS zone changed while its prepared publication was in flight",
		)
	}
	return true, nil
}

// settleDNSZoneSyncCallError separates a proven host failure from an
// ambiguous transport/finish failure. An active or unqueryable exact job keeps
// its DB lease for startup recovery; only a proven terminal result or proven
// pre-Begin no-job outcome may clear it.
func (p *Panel) settleDNSZoneSyncCallError(
	ctx context.Context,
	state dnsZoneSyncState,
	callErr error,
) (done bool, retry bool, err error) {
	statusCtx, statusCancel := dnsZoneFinalizeContext(ctx)
	job, statusErr := p.statusAgentMutation(statusCtx, state.LeaseRequestID.String)
	statusCancel()
	if statusErr != nil {
		return false, false, &dnsAgentPublicationError{Err: errors.Join(
			callErr,
			fmt.Errorf("DNS publication terminal status is ambiguous: %w", statusErr),
		)}
	}
	if job == nil {
		// A successful exact Status lookup returning nil proves this durable
		// request identity has no agent job, regardless of whether Begin failed
		// locally, by response, or by transport. Clear only the exact DB CAS.
		finalizeCtx, cancel := dnsZoneFinalizeContext(ctx)
		finalizeErr := p.recordDNSZoneSyncFailure(finalizeCtx, state, callErr)
		cancel()
		if finalizeErr != nil {
			return false, false, fmt.Errorf("record pre-Begin DNS publication failure: %w", finalizeErr)
		}
		return false, false, nil
	}
	if identityErr := validateAgentMutationIdentity(job, state.leaseIdentity()); identityErr != nil {
		return false, false, identityErr
	}
	if agentMutationActive(job.Status) {
		return false, false, &dnsAgentPublicationError{Err: errors.Join(
			callErr,
			errors.New("exact DNS publication remains active for startup recovery"),
		)}
	}
	finalizeCtx, cancel := dnsZoneFinalizeContext(ctx)
	defer cancel()
	if job.Status == agentMutationSucceeded {
		if err := validateAgentMutationSucceededReceipt(job, state.leaseIdentity()); err != nil {
			return false, false, err
		}
		exact, finalizeErr := p.recordDNSZoneSyncSuccess(finalizeCtx, state)
		return exact, !exact, finalizeErr
	}
	if finalizeErr := p.recordDNSZoneSyncFailure(finalizeCtx, state, callErr); finalizeErr != nil {
		return false, false, fmt.Errorf("record terminal DNS publication failure: %w", finalizeErr)
	}
	return false, false, nil
}

func (p *Panel) prepareDNSZoneSyncPlan(
	ctx context.Context,
	domain string,
	deleted bool,
) (dnsZoneSyncPlan, error) {
	requestID, err := newServiceOperationID()
	if err != nil {
		return dnsZoneSyncPlan{}, fmt.Errorf("create DNS publication request identity: %w", err)
	}
	ownerID, err := newServiceOperationID()
	if err != nil {
		return dnsZoneSyncPlan{}, fmt.Errorf("create DNS publication owner identity: %w", err)
	}
	zoneType := ""
	if !deleted {
		zoneType = p.dnsZoneType(ctx)
	}

	tx, err := p.db.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return dnsZoneSyncPlan{}, fmt.Errorf("begin DNS publication snapshot: %w", err)
	}
	defer tx.Rollback()

	state, err := readDNSZoneSyncState(ctx, tx, domain)
	if errors.Is(err, sql.ErrNoRows) && deleted {
		return dnsZoneSyncPlan{}, errDNSZoneAlreadyAbsent
	}
	if err != nil {
		return dnsZoneSyncPlan{}, fmt.Errorf("read DNS publication state: %w", err)
	}
	if state.hasLease() {
		return dnsZoneSyncPlan{}, &dnsZoneExistingLeaseError{State: state}
	}

	var records []zoneRecord
	if deleted {
		if state.DesiredAction != "delete" || state.SourceDomainID.Valid {
			return dnsZoneSyncPlan{}, errors.New("DNS deletion does not match the durable desired action")
		}
	} else {
		if state.DesiredAction != "sync" || !state.SourceDomainID.Valid {
			return dnsZoneSyncPlan{}, errors.New("DNS publication does not match the durable desired action")
		}
		var zoneID, soaRecordID int64
		var soa string
		if err := tx.QueryRowContext(ctx, `
			SELECT d.id, r.id, r.content
			FROM pdns_domains d
			JOIN pdns_records r
			  ON r.domain_id = d.id AND r.type = 'SOA' AND r.name = d.name
			WHERE d.name = ?
			LIMIT 1`, domain).Scan(&zoneID, &soaRecordID, &soa); err != nil {
			return dnsZoneSyncPlan{}, fmt.Errorf("zone %s has no valid SOA: %w", domain, err)
		}
		next, err := nextSOASerial(soa, time.Now())
		if err != nil {
			return dnsZoneSyncPlan{}, fmt.Errorf("zone %s: %w", domain, err)
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE pdns_records SET content = ? WHERE id = ?`,
			next, soaRecordID,
		); err != nil {
			return dnsZoneSyncPlan{}, fmt.Errorf("advance zone SOA: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE pdns_domains SET type = ? WHERE id = ? AND type IS NOT ?`,
			zoneType, zoneID, zoneType,
		); err != nil {
			return dnsZoneSyncPlan{}, fmt.Errorf("align zone type: %w", err)
		}
		state, err = readDNSZoneSyncState(ctx, tx, domain)
		if err != nil {
			return dnsZoneSyncPlan{}, fmt.Errorf("reload DNS publication state: %w", err)
		}
		rows, err := tx.QueryContext(ctx, `
			SELECT r.name, r.type, r.content,
			       COALESCE(r.ttl, 3600), COALESCE(r.prio, 0), r.disabled
			FROM pdns_records r
			JOIN pdns_domains d ON d.id = r.domain_id
			WHERE d.name = ?`, domain)
		if err != nil {
			return dnsZoneSyncPlan{}, fmt.Errorf("read DNS zone snapshot: %w", err)
		}
		for rows.Next() {
			var record zoneRecord
			if err := rows.Scan(
				&record.Name, &record.Type, &record.Content,
				&record.TTL, &record.Prio, &record.Disabled,
			); err != nil {
				rows.Close()
				return dnsZoneSyncPlan{}, fmt.Errorf("scan DNS zone snapshot: %w", err)
			}
			records = append(records, record)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return dnsZoneSyncPlan{}, fmt.Errorf("read DNS zone snapshot rows: %w", err)
		}
		if err := rows.Close(); err != nil {
			return dnsZoneSyncPlan{}, fmt.Errorf("close DNS zone snapshot: %w", err)
		}
	}

	commitment, err := mutationpayload.CanonicalDNSZoneSync(
		state.DesiredGeneration,
		state.ZoneName,
		state.DesiredAction == "delete",
		state.DesiredZoneType,
		records,
	)
	if err != nil {
		return dnsZoneSyncPlan{}, fmt.Errorf("canonicalize DNS publication snapshot: %w", err)
	}
	expiresAt := time.Now().UTC().Add(dnsZoneSyncLeaseTimeout).Format(time.RFC3339Nano)
	result, err := tx.ExecContext(ctx, `
		UPDATE dns_zone_sync_state
		SET status = 'pending', last_error = NULL,
		    lease_request_id = ?, lease_owner_id = ?,
		    lease_generation = ?, lease_action = ?, lease_zone_type = ?,
		    lease_qualifier = ?, lease_expires_at = ?, updated_at = datetime('now')
		WHERE zone_name = ?
		  AND desired_generation = ?
		  AND desired_action = ?
		  AND desired_zone_type = ?
		  AND lease_request_id IS NULL`,
		requestID, ownerID,
		commitment.DesiredGeneration,
		state.DesiredAction,
		commitment.ZoneType,
		commitment.Qualifier,
		expiresAt,
		state.ZoneName,
		state.DesiredGeneration,
		state.DesiredAction,
		state.DesiredZoneType,
	)
	if err != nil {
		return dnsZoneSyncPlan{}, fmt.Errorf("persist DNS publication lease: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil || changed != 1 {
		return dnsZoneSyncPlan{}, errors.New("DNS publication state changed before its lease was persisted")
	}
	if err := tx.Commit(); err != nil {
		return dnsZoneSyncPlan{}, fmt.Errorf("commit DNS publication lease: %w", err)
	}
	state.LeaseRequestID = sql.NullString{String: requestID, Valid: true}
	state.LeaseOwnerID = sql.NullString{String: ownerID, Valid: true}
	state.LeaseGeneration = sql.NullInt64{Int64: commitment.DesiredGeneration, Valid: true}
	state.LeaseAction = sql.NullString{String: state.DesiredAction, Valid: true}
	state.LeaseZoneType = sql.NullString{String: commitment.ZoneType, Valid: true}
	state.LeaseQualifier = sql.NullString{String: commitment.Qualifier, Valid: true}
	state.LeaseExpiresAt = sql.NullString{String: expiresAt, Valid: true}
	return dnsZoneSyncPlan{
		State: state, Commitment: commitment,
		RequestID: requestID, OwnerID: ownerID,
	}, nil
}

type dnsZoneStateQuery interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func readDNSZoneSyncState(
	ctx context.Context,
	query dnsZoneStateQuery,
	zone string,
) (dnsZoneSyncState, error) {
	var state dnsZoneSyncState
	err := query.QueryRowContext(ctx, `
		SELECT zone_name, source_domain_id,
		       desired_generation, applied_generation,
		       desired_action, desired_zone_type, status, last_error,
		       lease_request_id, lease_owner_id, lease_generation,
		       lease_action, lease_zone_type, lease_qualifier, lease_expires_at
		FROM dns_zone_sync_state
		WHERE zone_name = ?`, zone).Scan(
		&state.ZoneName, &state.SourceDomainID,
		&state.DesiredGeneration, &state.AppliedGeneration,
		&state.DesiredAction, &state.DesiredZoneType,
		&state.Status, &state.LastError,
		&state.LeaseRequestID, &state.LeaseOwnerID, &state.LeaseGeneration,
		&state.LeaseAction, &state.LeaseZoneType,
		&state.LeaseQualifier, &state.LeaseExpiresAt,
	)
	return state, err
}

func dnsZoneFinalizeContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), panelMutationFinishTimeout)
}

func dnsZoneBatchContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), dnsZoneSyncBatchTimeout)
}

func dnsZoneLeaseWhere(state dnsZoneSyncState) []any {
	return []any{
		state.ZoneName,
		state.LeaseRequestID.String,
		state.LeaseOwnerID.String,
		state.LeaseGeneration.Int64,
		state.LeaseAction.String,
		state.LeaseZoneType.String,
		state.LeaseQualifier.String,
		state.LeaseExpiresAt.String,
	}
}

func (p *Panel) recordDNSZoneSyncSuccess(
	ctx context.Context,
	lease dnsZoneSyncState,
) (bool, error) {
	if !lease.hasLease() {
		return false, errors.New("DNS publication success has no exact lease")
	}
	tx, err := p.db.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	engineState, err := readDNSEngineDBState(ctx, tx)
	if err != nil {
		return false, fmt.Errorf("read DNS engine authority for legacy receipt: %w", err)
	}
	// V2 has no engine or epoch in either its request or receipt. It is safe to
	// consume only while the host still has no durable publisher identity. Once
	// an engine has been adopted, a delayed V2 success could otherwise cross a
	// PDNS -> BIND -> PDNS epoch and falsely mark the new authority as applied.
	// Retire that exact legacy lease as pending; the active engine is republished
	// through V3 by the caller/startup recovery.
	if engineState.ActiveEngine != "" || engineState.EngineEpoch != 0 ||
		engineState.CurrentSwitchID != "" {
		result, releaseErr := tx.ExecContext(ctx, `
			UPDATE dns_zone_sync_state
			SET status = 'pending', last_error = NULL,
			    lease_request_id = NULL, lease_owner_id = NULL,
			    lease_generation = NULL, lease_action = NULL,
			    lease_zone_type = NULL, lease_qualifier = NULL,
			    lease_expires_at = NULL, updated_at = datetime('now')
			WHERE zone_name = ?
			  AND lease_request_id = ? AND lease_owner_id = ?
			  AND lease_generation = ? AND lease_action = ?
			  AND lease_zone_type = ? AND lease_qualifier = ?
			  AND lease_expires_at = ?`, dnsZoneLeaseWhere(lease)...)
		if releaseErr != nil {
			return false, releaseErr
		}
		if err := requireExactRows(
			result, 1, "legacy DNS receipt lost its exact lease CAS",
		); err != nil {
			return false, err
		}
		if err := tx.Commit(); err != nil {
			return false, err
		}
		return false, nil
	}
	args := []any{
		lease.LeaseGeneration.Int64,
		lease.LeaseGeneration.Int64,
		lease.LeaseAction.String,
		lease.LeaseZoneType.String,
	}
	args = append(args, dnsZoneLeaseWhere(lease)...)
	result, err := tx.ExecContext(ctx, `
		UPDATE dns_zone_sync_state
		SET applied_generation = max(applied_generation, ?),
		    status = CASE
		      WHEN desired_generation = ?
		       AND desired_action = ?
		       AND desired_zone_type = ? THEN 'applied'
		      ELSE 'pending'
		    END,
		    last_error = NULL,
		    lease_request_id = NULL, lease_owner_id = NULL,
		    lease_generation = NULL, lease_action = NULL,
		    lease_zone_type = NULL, lease_qualifier = NULL,
		    lease_expires_at = NULL, updated_at = datetime('now')
		WHERE zone_name = ?
		  AND lease_request_id = ? AND lease_owner_id = ?
		  AND lease_generation = ? AND lease_action = ?
		  AND lease_zone_type = ? AND lease_qualifier = ?
		  AND lease_expires_at = ?`, args...)
	if err != nil {
		return false, err
	}
	changed, err := result.RowsAffected()
	if err != nil || changed != 1 {
		return false, errors.New("DNS publication success lost its exact lease CAS")
	}
	state, err := readDNSZoneSyncState(ctx, tx, lease.ZoneName)
	if err != nil {
		return false, err
	}
	exact := state.Status == "applied" &&
		state.DesiredGeneration == lease.LeaseGeneration.Int64 &&
		state.DesiredAction == lease.LeaseAction.String &&
		state.DesiredZoneType == lease.LeaseZoneType.String
	if exact && state.DesiredAction == "delete" {
		result, err := tx.ExecContext(ctx,
			`DELETE FROM dns_zone_deletion_markers WHERE zone_name = ?`,
			lease.ZoneName,
		)
		if err != nil {
			return false, fmt.Errorf("retire applied DNS deletion marker: %w", err)
		}
		retired, err := result.RowsAffected()
		if err != nil || retired != 1 {
			return false, errors.New("applied DNS deletion marker was not retired exactly once")
		}
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return exact, nil
}

func (p *Panel) recordDNSZoneSyncFailure(
	ctx context.Context,
	lease dnsZoneSyncState,
	failure error,
) error {
	if !lease.hasLease() {
		return errors.New("DNS publication failure has no exact lease")
	}
	message := boundedDNSZoneError(failure)
	where := dnsZoneLeaseWhere(lease)
	args := []any{
		lease.LeaseGeneration.Int64,
		lease.LeaseAction.String,
		lease.LeaseZoneType.String,
		lease.LeaseGeneration.Int64,
		lease.LeaseAction.String,
		lease.LeaseZoneType.String,
		message,
	}
	args = append(args, where...)
	result, err := p.db.GetDB().ExecContext(ctx, `
		UPDATE dns_zone_sync_state
		SET status = CASE
		      WHEN desired_generation = ?
		       AND desired_action = ?
		       AND desired_zone_type = ? THEN 'error'
		      ELSE 'pending'
		    END,
		    last_error = CASE
		      WHEN desired_generation = ?
		       AND desired_action = ?
		       AND desired_zone_type = ? THEN ?
		      ELSE NULL
		    END,
		    lease_request_id = NULL, lease_owner_id = NULL,
		    lease_generation = NULL, lease_action = NULL,
		    lease_zone_type = NULL, lease_qualifier = NULL,
		    lease_expires_at = NULL, updated_at = datetime('now')
		WHERE zone_name = ?
		  AND lease_request_id = ? AND lease_owner_id = ?
		  AND lease_generation = ? AND lease_action = ?
		  AND lease_zone_type = ? AND lease_qualifier = ?
		  AND lease_expires_at = ?`, args...)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil || changed != 1 {
		return errors.New("DNS publication failure lost its exact lease CAS")
	}
	return nil
}

func boundedDNSZoneError(err error) string {
	message := strings.TrimSpace(err.Error())
	if message == "" {
		message = "DNS publication failed"
	}
	runes := []rune(message)
	if len(runes) > 2048 {
		message = string(runes[:2048])
	}
	return message
}

func (p *Panel) releaseDNSZoneLease(ctx context.Context, lease dnsZoneSyncState) error {
	if !lease.hasLease() {
		return errors.New("DNS publication release has no exact lease")
	}
	result, err := p.db.GetDB().ExecContext(ctx, `
		UPDATE dns_zone_sync_state
		SET status = 'pending', last_error = NULL,
		    lease_request_id = NULL, lease_owner_id = NULL,
		    lease_generation = NULL, lease_action = NULL,
		    lease_zone_type = NULL, lease_qualifier = NULL,
		    lease_expires_at = NULL, updated_at = datetime('now')
		WHERE zone_name = ?
		  AND lease_request_id = ? AND lease_owner_id = ?
		  AND lease_generation = ? AND lease_action = ?
		  AND lease_zone_type = ? AND lease_qualifier = ?
		  AND lease_expires_at = ?`, dnsZoneLeaseWhere(lease)...)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil || changed != 1 {
		return errors.New("DNS publication release lost its exact lease CAS")
	}
	return nil
}

func (p *Panel) reconcileDNSZoneLease(
	ctx context.Context,
	state dnsZoneSyncState,
	waitActive bool,
) (bool, error) {
	if !state.hasLease() || !mutationpayload.ValidDNSZoneSyncQualifier(state.LeaseQualifier.String) {
		return false, errors.New("DNS publication state contains an invalid durable lease")
	}
	identity := state.leaseIdentity()
	job, err := p.statusAgentMutation(ctx, identity.RequestID)
	if err != nil {
		return false, &dnsAgentPublicationError{Err: fmt.Errorf("read durable DNS publication status: %w", err)}
	}
	if job == nil {
		// A successful exact Status lookup returning no job proves Begin never
		// durably created this identity. Release immediately even when the DB
		// expiry is still in the future; retaining it would wedge startup.
		finalizeCtx, cancel := dnsZoneFinalizeContext(ctx)
		releaseErr := p.releaseDNSZoneLease(finalizeCtx, state)
		cancel()
		return false, releaseErr
	}
	if err := validateAgentMutationIdentity(job, identity); err != nil {
		return false, err
	}
	if agentMutationActive(job.Status) {
		if !waitActive {
			return false, &dnsAgentPublicationError{Err: errors.New("DNS publication is already in progress")}
		}
		job, err = p.waitExpectedAgentMutationTerminal(ctx, identity)
		if err != nil && (job == nil || agentMutationActive(job.Status)) {
			return false, &dnsAgentPublicationError{Err: fmt.Errorf("wait for durable DNS publication: %w", err)}
		}
		if job == nil {
			return false, errors.New("durable DNS publication disappeared during recovery")
		}
	}
	finalizeCtx, cancel := dnsZoneFinalizeContext(ctx)
	defer cancel()
	if job.Status == agentMutationSucceeded {
		if err := validateAgentMutationSucceededReceipt(job, identity); err != nil {
			return false, err
		}
		return p.recordDNSZoneSyncSuccess(finalizeCtx, state)
	}
	failure := errors.New(strings.TrimSpace(job.ErrorMessage))
	if strings.TrimSpace(job.ErrorMessage) == "" {
		failure = fmt.Errorf("durable DNS publication ended with status %s", job.Status)
	}
	if err := p.recordDNSZoneSyncFailure(finalizeCtx, state, failure); err != nil {
		return false, err
	}
	return false, nil
}

func validDirectDNSZoneSyncV2(job *agentMutationJob) bool {
	if job == nil || !agentMutationActive(job.Status) ||
		!validServiceOperationID(job.RequestID) ||
		!validServiceOperationID(job.OwnerID) ||
		job.Kind != "dns_zone_sync" ||
		!mutationpayload.ValidDNSZoneSyncQualifier(job.PackageName) {
		return false
	}
	canonical, err := hostname.CanonicalFQDN(job.Target)
	return err == nil && canonical == job.Target
}

func validDirectDNSZoneSync(job *agentMutationJob) bool {
	return validDirectDNSZoneSyncV2(job) || validDirectDNSZoneSyncV3(job)
}

func validDirectDNSSECSecure(job *agentMutationJob) bool {
	if job == nil || !agentMutationActive(job.Status) ||
		!validServiceOperationID(job.RequestID) ||
		!validServiceOperationID(job.OwnerID) ||
		job.Kind != "dnssec_secure" || job.PackageName != "" {
		return false
	}
	canonical, err := hostname.CanonicalFQDN(job.Target)
	return err == nil && canonical == job.Target
}

func (p *Panel) terminalizeInterruptedDNSSECSecure(
	ctx context.Context,
	job *agentMutationJob,
) (*agentMutationJob, error) {
	identity := agentMutationJobIdentity(job)
	if !validDirectDNSSECSecure(job) || !identity.matches(job) {
		return job, errors.New("interrupted DNSSEC signing has an invalid durable identity")
	}
	current := job
	if current.Status == agentMutationRunning {
		cancelErr := p.cancelAgentMutation(
			ctx,
			current,
			"panel_restarted_during_dnssec_signing",
			"The panel restarted before DNSSEC signing was reconciled.",
		)
		if cancelErr != nil {
			observed, statusErr := p.statusAgentMutation(ctx, identity.RequestID)
			if statusErr != nil {
				return observed, errors.Join(cancelErr, statusErr)
			}
			if err := validateAgentMutationIdentity(observed, identity); err != nil {
				return observed, err
			}
			current = observed
			if current.Status == agentMutationRunning {
				return current, cancelErr
			}
		}
	}
	if agentMutationActive(current.Status) {
		terminal, err := p.waitExpectedAgentMutationTerminal(ctx, identity)
		if err != nil {
			return terminal, fmt.Errorf("wait for interrupted DNSSEC signing: %w", err)
		}
		if err := validateAgentMutationIdentity(terminal, identity); err != nil {
			return terminal, err
		}
		current = terminal
	}
	if current == nil || agentMutationActive(current.Status) {
		return current, errors.New("interrupted DNSSEC signing did not reach a terminal state")
	}
	return current, nil
}

// recoverDirectDNSSECSecureLocked requires serviceMutationMu. The pre-sign
// publication lease is verified before the orphan is touched, then signing is
// idempotently retried under a fresh durable identity and that exact snapshot
// is published. Any mismatch or terminal ambiguity retains the lease and
// blocks startup instead of fabricating recovery authority.
func (p *Panel) recoverDirectDNSSECSecureLocked(
	ctx context.Context,
	job *agentMutationJob,
) error {
	batchCtx, cancelBatch := dnsZoneBatchContext(ctx)
	defer cancelBatch()
	dnsPublicationMu.Lock()
	defer dnsPublicationMu.Unlock()
	if !validDirectDNSSECSecure(job) {
		return errors.New("direct DNSSEC signing has an invalid durable identity")
	}
	state, err := readDNSZoneSyncState(batchCtx, p.db.GetDB(), job.Target)
	if err != nil {
		return fmt.Errorf("read interrupted DNSSEC publication bridge: %w", err)
	}
	var engineLease dnsZoneEngineLease
	legacyBridge := state.hasLease()
	if !legacyBridge {
		engineLease, err = readDNSZoneEngineLease(
			batchCtx, p.db.GetDB(), job.Target,
		)
	}
	validLegacy := legacyBridge &&
		state.LeaseAction.String == "sync" &&
		mutationpayload.ValidDNSZoneSyncQualifier(state.LeaseQualifier.String) &&
		(state.LeaseZoneType.String == "NATIVE" ||
			state.LeaseZoneType.String == "MASTER")
	validV3 := !legacyBridge && err == nil && engineLease.valid() &&
		engineLease.ZoneName == job.Target &&
		engineLease.DesiredAction == "sync"
	if !state.SourceDomainID.Valid || (!validLegacy && !validV3) {
		return errors.New(
			"interrupted DNSSEC signing has no valid same-domain publication bridge",
		)
	}
	if _, err := p.terminalizeInterruptedDNSSECSecure(batchCtx, job); err != nil {
		return err
	}
	if err := p.requireDNSSECSecureV2Agent(batchCtx); err != nil {
		return fmt.Errorf("verify DNSSEC recovery capability: %w", err)
	}
	var response transport.SecureDNSZoneV2Response
	request := transport.SecureDNSZoneV2Request{Zone: job.Target}
	requestID, err := newServiceOperationID()
	if err != nil {
		return fmt.Errorf("create DNSSEC recovery request identity: %w", err)
	}
	ownerID, err := newServiceOperationID()
	if err != nil {
		return fmt.Errorf("create DNSSEC recovery owner identity: %w", err)
	}
	op := serviceOperation{
		RequestID: requestID, Kind: "dnssec_secure", ServiceID: job.Target,
	}
	identity := agentMutationIdentityForOperation(op, ownerID)
	secureErr := p.withStandaloneAgentMutationIdentity(
		batchCtx, op, ownerID,
		func(callCtx context.Context, binding agentMutationBinding) error {
			request.ServiceMutationBinding = binding
			if err := p.callAgentContext(
				callCtx, "Agent.SecureDNSZoneV2", &request, &response,
			); err != nil {
				return err
			}
			if problem := dnssecResultError(response, true); problem != "" {
				return errors.New(problem)
			}
			return nil
		},
	)
	_, settleErr := p.dnssecSecureTerminalKnown(batchCtx, identity, secureErr)
	if settleErr != nil {
		// An exact active or unqueryable retry may already be changing the
		// signed database. Preserve the bridge until a later recovery can
		// prove a terminal result.
		return fmt.Errorf("retry interrupted DNSSEC signing: %w", settleErr)
	}
	// No DNS child can coexist with the global direct DNSSEC job. Reconcile
	// the pre-sign lease as proven-never-begun, then publish the current ledger
	// generation. This also handles a record edit committed while signing held
	// the global host lock: current desired state remains recovery authority.
	if legacyBridge {
		if _, err := p.reconcileDNSZoneLease(batchCtx, state, false); err != nil {
			return fmt.Errorf("reconcile legacy DNSSEC publication bridge: %w", err)
		}
	} else {
		if _, err := p.reconcileDNSZoneEngineLease(
			batchCtx, engineLease, false,
		); err != nil {
			return fmt.Errorf("reconcile DNSSEC V3 publication bridge: %w", err)
		}
	}
	if err := p.syncZoneToDNSLocked(batchCtx, job.Target, false); err != nil {
		return fmt.Errorf("publish current DNSSEC recovery snapshot: %w", err)
	}
	if err := p.recoverDNSZoneSyncStateAlreadyLocked(batchCtx); err != nil {
		return err
	}
	if secureErr != nil {
		// A terminal signing failure may follow partial key creation. The
		// current ledger is now published, so startup can safely remain live
		// and let the operator retry DNSSEC explicitly.
		log.Printf("dnssec recovery for %s reached a known terminal failure after publication: %v",
			job.Target, secureErr)
	}
	return nil
}

func (p *Panel) exactDNSZoneLeaseForJob(
	ctx context.Context,
	job *agentMutationJob,
) (dnsZoneSyncState, error) {
	if !validDirectDNSZoneSyncV2(job) {
		return dnsZoneSyncState{}, errors.New("direct DNS publication has an invalid durable identity")
	}
	state, err := readDNSZoneSyncState(ctx, p.db.GetDB(), job.Target)
	if err != nil {
		return dnsZoneSyncState{}, fmt.Errorf("read direct DNS publication lease: %w", err)
	}
	if !state.hasLease() || !state.leaseIdentity().matches(job) {
		return dnsZoneSyncState{}, errors.New("direct DNS publication does not match the exact persisted zone lease")
	}
	return state, nil
}

// recoverDirectDNSZoneSyncLocked requires serviceMutationMu and consumes only
// a global active job whose complete identity matches migration 032 state.
func (p *Panel) recoverDirectDNSZoneSyncLocked(
	ctx context.Context,
	job *agentMutationJob,
) error {
	if validDirectDNSZoneSyncV3(job) {
		return p.recoverDirectDNSZoneSyncV3Locked(ctx, job)
	}
	batchCtx, cancelBatch := dnsZoneBatchContext(ctx)
	defer cancelBatch()
	dnsPublicationMu.Lock()
	defer dnsPublicationMu.Unlock()
	state, err := p.exactDNSZoneLeaseForJob(batchCtx, job)
	if err != nil {
		return err
	}
	done, err := p.reconcileDNSZoneLease(batchCtx, state, true)
	if err != nil {
		return err
	}
	if !done {
		if err := p.syncZoneToDNSLocked(batchCtx, state.ZoneName, state.DesiredAction == "delete"); err != nil {
			return err
		}
	}
	return p.recoverDNSZoneSyncStateAlreadyLocked(batchCtx)
}

// recoverDNSZoneSyncStateLocked requires serviceMutationMu. It repairs every
// pending/error generation and reconciles any exact stale/crash lease before
// the startup HTTP gate is opened.
func (p *Panel) recoverDNSZoneSyncStateLocked(ctx context.Context) error {
	batchCtx, cancelBatch := dnsZoneBatchContext(ctx)
	defer cancelBatch()
	dnsPublicationMu.Lock()
	defer dnsPublicationMu.Unlock()
	return p.recoverDNSZoneSyncStateAlreadyLocked(batchCtx)
}

func (p *Panel) recoverDNSZoneSyncStateAlreadyLocked(ctx context.Context) error {
	rows, err := p.db.GetDB().QueryContext(ctx, `
		SELECT zone_name
		FROM dns_zone_sync_state
		WHERE status IN ('pending', 'error') OR lease_request_id IS NOT NULL
		   OR EXISTS (
		     SELECT 1 FROM dns_zone_engine_leases AS engine_lease
		     WHERE engine_lease.zone_name = dns_zone_sync_state.zone_name
		   )
		ORDER BY zone_name`)
	if err != nil {
		return fmt.Errorf("list recoverable DNS publications: %w", err)
	}
	var zones []string
	for rows.Next() {
		var zone string
		if err := rows.Scan(&zone); err != nil {
			rows.Close()
			return fmt.Errorf("scan recoverable DNS publication: %w", err)
		}
		zones = append(zones, zone)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("list recoverable DNS publication rows: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close recoverable DNS publication list: %w", err)
	}
	// Reconcile every exact durable lease before consulting host readiness.
	// A committed/crashed child is authority and must never be hidden by a
	// currently missing PowerDNS binary or a permanent platform denial.
	for _, zone := range zones {
		state, err := readDNSZoneSyncState(ctx, p.db.GetDB(), zone)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return fmt.Errorf("read recoverable DNS publication %s: %w", zone, err)
		}
		if state.hasLease() {
			done, err := p.reconcileDNSZoneLease(ctx, state, true)
			if err != nil {
				return fmt.Errorf("reconcile DNS publication %s: %w", zone, err)
			}
			if done {
				continue
			}
		}
		engineLease, leaseErr := readDNSZoneEngineLease(ctx, p.db.GetDB(), zone)
		if leaseErr == nil {
			done, err := p.reconcileDNSZoneEngineLease(ctx, engineLease, true)
			if err != nil {
				return fmt.Errorf("reconcile DNS V3 publication %s: %w", zone, err)
			}
			if done {
				continue
			}
		} else if !errors.Is(leaseErr, sql.ErrNoRows) {
			return fmt.Errorf("read recoverable DNS V3 publication %s: %w", zone, leaseErr)
		}
	}

	var pending []dnsZoneSyncState
	for _, zone := range zones {
		state, err := readDNSZoneSyncState(ctx, p.db.GetDB(), zone)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return fmt.Errorf("reload recoverable DNS publication %s: %w", zone, err)
		}
		if state.hasLease() {
			return fmt.Errorf(
				"DNS publication %s retained a lease after exact reconciliation",
				zone,
			)
		}
		if _, leaseErr := readDNSZoneEngineLease(ctx, p.db.GetDB(), zone); leaseErr == nil {
			return fmt.Errorf(
				"DNS V3 publication %s retained a lease after exact reconciliation",
				zone,
			)
		} else if !errors.Is(leaseErr, sql.ErrNoRows) {
			return fmt.Errorf("reload recoverable DNS V3 lease %s: %w", zone, leaseErr)
		}
		if state.Status == "pending" || state.Status == "error" {
			pending = append(pending, state)
		}
	}
	if len(pending) == 0 {
		return nil
	}

	// Migration 032 intentionally seeds old zones as pending. An unconfigured
	// host must still boot so the operator can choose an engine; no pending row
	// is published until durable identity and runtime readiness agree exactly.
	publisher, ready, publisherErr := p.activeDNSPublisher(ctx)
	if publisherErr != nil || !ready {
		log.Printf(
			"deferring %d pending DNS publication(s) until an active publisher is ready: %v",
			len(pending), publisherErr,
		)
		return nil
	}
	if !transport.ValidDNSEngine(publisher.Engine) || publisher.Epoch < 1 {
		return errors.New("active DNS publisher has an invalid durable identity")
	}
	if err := p.requireDNSZoneSyncV3Agent(ctx); err != nil {
		if errors.Is(err, errDNSZoneSyncV3AgentIncompatible) ||
			errors.Is(err, errAgentRPCPlatformCapabilityDenied) {
			log.Printf(
				"deferring %d pending DNS publication(s) until V3 is available: %v",
				len(pending), err,
			)
			return nil
		}
		return fmt.Errorf("verify pending DNS V3 publication capability: %w", err)
	}
	if publisher.Engine == transport.DNSEnginePowerDNS {
		var readiness dnsClusterReadinessResponse
		if err := p.callAgentContext(
			ctx, "Agent.DNSClusterReadiness", &transport.Empty{}, &readiness,
		); err != nil {
			return fmt.Errorf("read PowerDNS startup readiness: %w", err)
		}
		if !readiness.Ready {
			log.Printf(
				"deferring %d pending DNS publication(s): %s",
				len(pending), strings.TrimSpace(readiness.Detail),
			)
			return nil
		}
	}
	for _, state := range pending {
		if err := p.syncZoneToDNSLocked(
			ctx, state.ZoneName, state.DesiredAction == "delete",
		); err != nil {
			return fmt.Errorf(
				"republish recoverable DNS zone %s: %w",
				state.ZoneName, err,
			)
		}
	}
	return nil
}

// handlePDNSEnable configures PowerDNS with our dedicated sqlite backend and
// pushes every ledger zone into it — the one-shot "start serving DNS"
// action. Admin-only via the /api/v1/pdns/ prefix.
// handlePDNSEnable, PowerDNS'i bize ayrılmış sqlite backend'iyle yapılandırır
// ve defterdeki her zone'u içine iter — tek seferlik "DNS sunmaya başla"
// eylemi. /api/v1/pdns/ öneki üzerinden yalnız admin.
func (p *Panel) handlePDNSEnable(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := p.requireActivePowerDNSPublisher(r.Context()); err != nil {
		writeDNSEngineWorkflowRequired(w)
		return
	}
	if err := p.requireNoPendingDNSClusterSaga(r.Context()); err != nil {
		writeClientError(w, http.StatusConflict,
			"DNS cluster topology is pending recovery; PowerDNS configuration is blocked")
		return
	}
	if err := p.requireDNSZoneSyncV2Agent(r.Context()); err != nil {
		writeServerError(w, err)
		return
	}
	p.serviceMutationMu.Lock()
	defer p.serviceMutationMu.Unlock()
	if err := p.requireNoPendingDNSClusterSaga(r.Context()); err != nil {
		writeClientError(w, http.StatusConflict,
			"DNS cluster topology is pending recovery; PowerDNS configuration is blocked")
		return
	}
	if err := p.requireActivePowerDNSPublisher(r.Context()); err != nil {
		writeDNSEngineWorkflowRequired(w)
		return
	}

	var resp transport.SyncDNSZoneResponse
	err := p.withStandaloneAgentMutation(r.Context(), "pdns_configure", "pdns", "", func(callCtx context.Context, binding agentMutationBinding) error {
		req := transport.ServiceMutationRequest{ServiceMutationBinding: binding}
		if err := p.callAgentContext(callCtx, "Agent.ConfigurePowerDNSSQLite", &req, &resp); err != nil {
			return err
		}
		if resp.Error != "" {
			return errors.New(resp.Error)
		}
		if !resp.Synced {
			return errors.New("agent did not confirm PowerDNS configuration")
		}
		return nil
	})
	if err != nil {
		if resp.Error != "" {
			log.Printf("PowerDNS repair agent detail: %s",
				boundedAgentDiagnostic(resp.Error))
			writeCodedError(
				w, http.StatusConflict, errCodeDNSPublicationFailed,
				"PowerDNS configuration could not be repaired; check the DNS service and retry",
				"",
			)
			return
		}
		writeServerError(w, err)
		return
	}
	if resp.Error != "" {
		log.Printf("PowerDNS repair agent detail: %s",
			boundedAgentDiagnostic(resp.Error))
		writeCodedError(
			w, http.StatusConflict, errCodeDNSPublicationFailed,
			"PowerDNS configuration could not be repaired; check the DNS service and retry",
			"",
		)
		return
	}

	dnsPublicationMu.Lock()
	result, err := p.syncAllZonesLocked(r.Context())
	dnsPublicationMu.Unlock()
	if err != nil {
		err = fmt.Errorf("publish PowerDNS zones: %w", err)
		if writeDNSPublicationConflict(w, err,
			"PowerDNS was configured, but one or more DNS zones could not be published; check the DNS service and retry") {
			return
		}
		writeServerError(w, err)
		return
	}
	json.NewEncoder(w).Encode(map[string]any{"success": true, "zones_synced": result.Synced})
}

// syncAllZonesResult pushes every ledger zone and retains every failure. The
// callback keeps aggregation testable without changing Panel's RPC client.
func (p *Panel) syncAllZonesResult(
	ctx context.Context,
	syncZone func(context.Context, string, bool) error,
) (dnsSyncAllResult, error) {
	rows, err := p.db.GetDB().QueryContext(ctx, `SELECT name FROM pdns_domains ORDER BY name`)
	if err != nil {
		return dnsSyncAllResult{}, fmt.Errorf("list DNS zones: %w", err)
	}
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			rows.Close()
			return dnsSyncAllResult{}, fmt.Errorf("scan DNS zone: %w", err)
		}
		names = append(names, n)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return dnsSyncAllResult{}, fmt.Errorf("list DNS zones: %w", err)
	}
	if err := rows.Close(); err != nil {
		return dnsSyncAllResult{}, fmt.Errorf("close DNS zone list: %w", err)
	}

	result := dnsSyncAllResult{Attempted: len(names)}
	for _, n := range names {
		if err := syncZone(ctx, n, false); err != nil {
			result.Failures = append(result.Failures, dnsZoneSyncFailure{Zone: n, Err: err})
			continue
		}
		result.Synced++
	}
	return result, nil
}

// syncAllZonesStrict is for settings operations whose success promises that
// the new topology is already published. It still attempts every zone so one
// bad zone does not prevent healthy zones from receiving the update.
func (p *Panel) syncAllZonesStrict(ctx context.Context) (dnsSyncAllResult, error) {
	p.serviceMutationMu.Lock()
	defer p.serviceMutationMu.Unlock()
	dnsPublicationMu.Lock()
	defer dnsPublicationMu.Unlock()
	return p.syncAllZonesLocked(ctx)
}

// syncAllZonesLocked requires serviceMutationMu and dnsPublicationMu. It
// includes durable deletion tombstones as well as live zones, so repair and
// post-install runs cannot silently strand a pending remote deletion.
func (p *Panel) syncAllZonesLocked(ctx context.Context) (dnsSyncAllResult, error) {
	if err := p.requireNoPendingDNSClusterSaga(ctx); err != nil {
		return dnsSyncAllResult{}, err
	}
	rows, err := p.db.GetDB().QueryContext(ctx, `
		SELECT zone_name, desired_action
		FROM dns_zone_sync_state
		ORDER BY zone_name`)
	if err != nil {
		return dnsSyncAllResult{}, fmt.Errorf("list desired DNS publications: %w", err)
	}
	type desiredZone struct {
		name   string
		delete bool
	}
	var zones []desiredZone
	for rows.Next() {
		var zone desiredZone
		var action string
		if err := rows.Scan(&zone.name, &action); err != nil {
			rows.Close()
			return dnsSyncAllResult{}, fmt.Errorf("scan desired DNS publication: %w", err)
		}
		if action != "sync" && action != "delete" {
			rows.Close()
			return dnsSyncAllResult{}, errors.New("invalid durable DNS desired action")
		}
		zone.delete = action == "delete"
		zones = append(zones, zone)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return dnsSyncAllResult{}, fmt.Errorf("list desired DNS publication rows: %w", err)
	}
	if err := rows.Close(); err != nil {
		return dnsSyncAllResult{}, fmt.Errorf("close desired DNS publication list: %w", err)
	}

	result := dnsSyncAllResult{Attempted: len(zones)}
	for _, zone := range zones {
		if err := p.syncZoneToDNSLocked(ctx, zone.name, zone.delete); err != nil {
			result.Failures = append(result.Failures, dnsZoneSyncFailure{Zone: zone.name, Err: err})
			continue
		}
		result.Synced++
	}
	return result, result.err()
}

// syncAllZones is the legacy best-effort wrapper used by repair/install
// flows. It logs partial failure and returns the number actually published.
func (p *Panel) syncAllZones(ctx context.Context) int {
	result, err := p.syncAllZonesStrict(ctx)
	if err != nil {
		log.Printf("dns sync all: %v", err)
		return result.Synced
	}
	return result.Synced
}
