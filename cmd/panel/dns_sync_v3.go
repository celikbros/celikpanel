package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/hostname"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

// dnsZoneEngineLease is the durable authority for one engine-bound V3
// publication. It deliberately does not reuse migration 032's PowerDNS-only
// lease columns: a V2 receipt can never finalize a BIND generation.
type dnsZoneEngineLease struct {
	ZoneName          string
	Engine            transport.DNSEngine
	EngineEpoch       int64
	RequestID         string
	OwnerID           string
	DesiredGeneration int64
	DesiredAction     string
	DesiredZoneType   string
	Qualifier         string
	ExpiresAt         string
}

func (lease dnsZoneEngineLease) valid() bool {
	return lease.ZoneName != "" && transport.ValidDNSEngine(lease.Engine) &&
		lease.EngineEpoch >= 1 && validServiceOperationID(lease.RequestID) &&
		validServiceOperationID(lease.OwnerID) &&
		lease.DesiredGeneration >= 0 &&
		(lease.DesiredAction == "sync" || lease.DesiredAction == "delete") &&
		(lease.DesiredZoneType == "NATIVE" || lease.DesiredZoneType == "MASTER") &&
		mutationpayload.ValidDNSZoneSyncV3Qualifier(lease.Qualifier) &&
		strings.TrimSpace(lease.ExpiresAt) != ""
}

func (lease dnsZoneEngineLease) identity() agentMutationIdentity {
	return agentMutationIdentity{
		RequestID: lease.RequestID, OwnerID: lease.OwnerID,
		Kind: "dns_zone_sync", Target: lease.ZoneName,
		PackageName: lease.Qualifier,
	}
}

type dnsZoneSyncV3Plan struct {
	State      dnsZoneSyncState
	Lease      dnsZoneEngineLease
	Commitment mutationpayload.DNSZoneSyncV3Commitment
}

type dnsZoneExistingEngineLeaseError struct {
	Lease dnsZoneEngineLease
}

func (err *dnsZoneExistingEngineLeaseError) Error() string {
	return fmt.Sprintf("DNS zone %s already has a durable engine-bound publication lease", err.Lease.ZoneName)
}

type dnsZoneExistingLegacyLeaseError struct {
	State dnsZoneSyncState
}

type dnsZoneV3PropagationPendingError struct{}

func (*dnsZoneV3PropagationPendingError) Error() string {
	return "DNS zone publication is waiting for exact paired propagation recovery"
}

var errDNSZoneV3PropagationDeferred = errors.New(
	"DNS zone V3 paired propagation recovery is deferred",
)

func dnsZoneSyncV3PendingPhase(identity agentMutationIdentity) (string, error) {
	return dnsZoneSyncV3CommitPhase(identity, "propagation-pending")
}

func dnsZoneSyncV3RecoveringPhase(identity agentMutationIdentity) (string, error) {
	return dnsZoneSyncV3CommitPhase(identity, "recovering")
}

func dnsZoneSyncV3CommitPhase(
	identity agentMutationIdentity,
	state string,
) (string, error) {
	canonical, err := hostname.CanonicalFQDN(identity.Target)
	if err != nil || canonical != identity.Target ||
		!validServiceOperationID(identity.RequestID) ||
		!validServiceOperationID(identity.OwnerID) ||
		identity.Kind != "dns_zone_sync" ||
		!mutationpayload.ValidDNSZoneSyncV3Qualifier(identity.PackageName) ||
		(state != "propagation-pending" && state != "recovering") {
		return "", errors.New("invalid DNS zone V3 commit identity")
	}
	return "commit/dns-zone-sync/v3/" + state + "/" +
		identity.RequestID + "/" + identity.Target + "/" +
		identity.PackageName, nil
}

func validateDNSZoneSyncV3PendingJob(
	job *agentMutationJob,
	identity agentMutationIdentity,
) error {
	if err := validateAgentMutationIdentity(job, identity); err != nil {
		return err
	}
	want, err := dnsZoneSyncV3PendingPhase(identity)
	if err != nil || job.Status != agentMutationPending || job.Phase != want {
		return errors.New("agent DNS zone V3 pending receipt is not exact")
	}
	return nil
}

func validateDNSZoneSyncV3RecoveringJob(
	job *agentMutationJob,
	identity agentMutationIdentity,
) error {
	if err := validateAgentMutationIdentity(job, identity); err != nil {
		return err
	}
	want, err := dnsZoneSyncV3RecoveringPhase(identity)
	if err != nil || !agentMutationActive(job.Status) || job.Phase != want {
		return errors.New("agent DNS zone V3 recovering receipt is not exact")
	}
	return nil
}

func (err *dnsZoneExistingLegacyLeaseError) Error() string {
	return fmt.Sprintf("DNS zone %s retains a legacy PowerDNS publication lease", err.State.ZoneName)
}

type dnsZoneEngineLeaseQuery interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func readDNSZoneEngineLease(
	ctx context.Context,
	query dnsZoneEngineLeaseQuery,
	zone string,
) (dnsZoneEngineLease, error) {
	var lease dnsZoneEngineLease
	err := query.QueryRowContext(ctx, `
		SELECT zone_name, engine, engine_epoch, request_id, owner_id,
		       desired_generation, desired_action, desired_zone_type,
		       qualifier, expires_at
		FROM dns_zone_engine_leases WHERE zone_name = ?`, zone).Scan(
		&lease.ZoneName, &lease.Engine, &lease.EngineEpoch,
		&lease.RequestID, &lease.OwnerID, &lease.DesiredGeneration,
		&lease.DesiredAction, &lease.DesiredZoneType,
		&lease.Qualifier, &lease.ExpiresAt,
	)
	return lease, err
}

func dnsZoneEngineLeaseWhere(lease dnsZoneEngineLease) []any {
	return []any{
		lease.ZoneName, lease.Engine, lease.EngineEpoch,
		lease.RequestID, lease.OwnerID, lease.DesiredGeneration,
		lease.DesiredAction, lease.DesiredZoneType,
		lease.Qualifier, lease.ExpiresAt,
	}
}

// syncZoneToDNSV3Locked publishes through the exact active engine+epoch. The
// caller holds serviceMutationMu followed by dnsPublicationMu.
func (p *Panel) syncZoneToDNSV3Locked(
	ctx context.Context,
	domain string,
	deleted bool,
	publisher dnsPublisherIdentity,
) error {
	if !transport.ValidDNSEngine(publisher.Engine) || publisher.Epoch < 1 {
		return &dnsAgentPublicationError{Err: errors.New("DNS publication authority is not exact")}
	}
	if err := p.requireDNSZoneSyncV3Agent(ctx); err != nil {
		return &dnsAgentPublicationError{Err: err}
	}
	for attempt := 0; attempt < dnsZoneSyncMaxAttempts; attempt++ {
		plan, err := p.prepareDNSZoneSyncV3Plan(ctx, domain, deleted, publisher)
		if errors.Is(err, errDNSZoneAlreadyAbsent) && deleted {
			return nil
		}
		var legacy *dnsZoneExistingLegacyLeaseError
		if errors.As(err, &legacy) {
			done, reconcileErr := p.reconcileDNSZoneLease(ctx, legacy.State, false)
			if reconcileErr != nil {
				return reconcileErr
			}
			if done {
				return nil
			}
			continue
		}
		var existing *dnsZoneExistingEngineLeaseError
		if errors.As(err, &existing) {
			done, reconcileErr := p.reconcileDNSZoneEngineLease(ctx, existing.Lease, false)
			if reconcileErr != nil {
				return reconcileErr
			}
			if done {
				return nil
			}
			continue
		}
		if err != nil {
			return err
		}

		req := transport.SyncDNSZoneV3Request{
			Engine: publisher.Engine, EngineEpoch: publisher.Epoch,
			DesiredGeneration: plan.Commitment.DesiredGeneration,
			Domain:            plan.Commitment.Domain, Delete: plan.Commitment.Delete,
			ZoneType: plan.Commitment.ZoneType,
			Records:  append([]zoneRecord(nil), plan.Commitment.Records...),
		}
		var resp transport.SyncDNSZoneV3Response
		op := serviceOperation{
			RequestID: plan.Lease.RequestID, Kind: "dns_zone_sync",
			ServiceID:   plan.Commitment.Domain,
			PackageName: plan.Commitment.Qualifier,
		}
		callErr := p.withStandaloneAgentMutationIdentity(
			ctx, op, plan.Lease.OwnerID,
			func(callCtx context.Context, binding agentMutationBinding) error {
				req.ServiceMutationBinding = binding
				return p.callSyncDNSZoneV3(callCtx, &req, &resp)
			},
		)
		if callErr != nil {
			done, retry, settleErr := p.settleDNSZoneSyncV3CallError(
				ctx, plan.Lease, callErr,
			)
			if settleErr != nil {
				return settleErr
			}
			if done {
				return nil
			}
			if retry {
				continue
			}
			log.Printf("dns V3 sync %s generation %d: %v",
				domain, plan.Commitment.DesiredGeneration, callErr)
			return &dnsAgentPublicationError{Err: callErr}
		}
		finalizeCtx, cancel := dnsZoneFinalizeContext(ctx)
		exact, finalizeErr := p.recordDNSZoneSyncV3Success(finalizeCtx, plan.Lease)
		cancel()
		if finalizeErr != nil {
			return fmt.Errorf("record engine-bound DNS publication success: %w", finalizeErr)
		}
		if exact {
			return nil
		}
	}
	return &dnsAgentPublicationError{Err: errors.New(
		"DNS zone changed during every bounded engine-bound publication attempt",
	)}
}

// prepareDNSZoneSyncV3PlanReconciledLocked creates the crash bridge used by
// compound host mutations such as DNSSEC signing. Both legacy and
// engine-bound predecessors are reconciled without ever dropping an
// unqueryable or active exact identity.
func (p *Panel) prepareDNSZoneSyncV3PlanReconciledLocked(
	ctx context.Context,
	domain string,
	deleted bool,
	publisher dnsPublisherIdentity,
) (dnsZoneSyncV3Plan, error) {
	for attempt := 0; attempt < dnsZoneSyncMaxAttempts; attempt++ {
		plan, err := p.prepareDNSZoneSyncV3Plan(
			ctx, domain, deleted, publisher,
		)
		var legacy *dnsZoneExistingLegacyLeaseError
		if errors.As(err, &legacy) {
			if _, reconcileErr := p.reconcileDNSZoneLease(
				ctx, legacy.State, false,
			); reconcileErr != nil {
				return dnsZoneSyncV3Plan{}, reconcileErr
			}
			continue
		}
		var existing *dnsZoneExistingEngineLeaseError
		if errors.As(err, &existing) {
			if _, reconcileErr := p.reconcileDNSZoneEngineLease(
				ctx, existing.Lease, false,
			); reconcileErr != nil {
				return dnsZoneSyncV3Plan{}, reconcileErr
			}
			continue
		}
		return plan, err
	}
	return dnsZoneSyncV3Plan{}, errors.New(
		"DNS V3 publication lease changed during every bounded preparation attempt",
	)
}

// publishPreparedDNSZoneSyncV3PlanLocked consumes an exact, already-persisted
// engine+epoch lease. The caller owns the global host mutation and DNS
// publication locks, so a crash before this call leaves startup recovery the
// same durable authority.
func (p *Panel) publishPreparedDNSZoneSyncV3PlanLocked(
	ctx context.Context,
	plan dnsZoneSyncV3Plan,
) (bool, error) {
	if !plan.Lease.valid() ||
		plan.Lease.Qualifier != plan.Commitment.Qualifier ||
		string(plan.Lease.Engine) != plan.Commitment.Engine ||
		plan.Lease.EngineEpoch != plan.Commitment.EngineEpoch {
		return false, errors.New("prepared DNS V3 publication identity is invalid")
	}
	req := transport.SyncDNSZoneV3Request{
		Engine: plan.Lease.Engine, EngineEpoch: plan.Lease.EngineEpoch,
		DesiredGeneration: plan.Commitment.DesiredGeneration,
		Domain:            plan.Commitment.Domain,
		Delete:            plan.Commitment.Delete,
		ZoneType:          plan.Commitment.ZoneType,
		Records: append(
			[]zoneRecord(nil), plan.Commitment.Records...,
		),
	}
	var resp transport.SyncDNSZoneV3Response
	op := serviceOperation{
		RequestID: plan.Lease.RequestID, Kind: "dns_zone_sync",
		ServiceID: plan.Commitment.Domain, PackageName: plan.Lease.Qualifier,
	}
	callErr := p.withStandaloneAgentMutationIdentity(
		ctx, op, plan.Lease.OwnerID,
		func(callCtx context.Context, binding agentMutationBinding) error {
			req.ServiceMutationBinding = binding
			return p.callSyncDNSZoneV3(callCtx, &req, &resp)
		},
	)
	if callErr != nil {
		done, retry, settleErr := p.settleDNSZoneSyncV3CallError(
			ctx, plan.Lease, callErr,
		)
		if settleErr != nil {
			return false, settleErr
		}
		if done {
			return true, nil
		}
		if retry {
			return false, errors.New(
				"DNS zone changed while its prepared V3 publication was in flight",
			)
		}
		return false, &dnsAgentPublicationError{Err: callErr}
	}
	finalizeCtx, cancel := dnsZoneFinalizeContext(ctx)
	exact, err := p.recordDNSZoneSyncV3Success(finalizeCtx, plan.Lease)
	cancel()
	if err != nil {
		return false, err
	}
	if !exact {
		return false, errors.New(
			"DNS zone changed while its prepared V3 publication was in flight",
		)
	}
	return true, nil
}

func (p *Panel) prepareDNSZoneSyncV3Plan(
	ctx context.Context,
	domain string,
	deleted bool,
	publisher dnsPublisherIdentity,
) (dnsZoneSyncV3Plan, error) {
	requestID, err := newServiceOperationID()
	if err != nil {
		return dnsZoneSyncV3Plan{}, fmt.Errorf("create DNS V3 publication request identity: %w", err)
	}
	ownerID, err := newServiceOperationID()
	if err != nil {
		return dnsZoneSyncV3Plan{}, fmt.Errorf("create DNS V3 publication owner identity: %w", err)
	}
	zoneType := ""
	if !deleted {
		zoneType = p.dnsZoneType(ctx)
	}

	tx, err := p.db.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return dnsZoneSyncV3Plan{}, fmt.Errorf("begin DNS V3 publication snapshot: %w", err)
	}
	defer tx.Rollback()
	engineState, err := readDNSEngineDBState(ctx, tx)
	if err != nil {
		return dnsZoneSyncV3Plan{}, fmt.Errorf("read DNS engine authority: %w", err)
	}
	if engineState.CurrentSwitchID != "" ||
		engineState.ActiveEngine != publisher.Engine ||
		engineState.EngineEpoch != publisher.Epoch {
		return dnsZoneSyncV3Plan{}, errors.New("DNS engine authority changed before publication snapshot")
	}

	state, err := readDNSZoneSyncState(ctx, tx, domain)
	if errors.Is(err, sql.ErrNoRows) && deleted {
		return dnsZoneSyncV3Plan{}, errDNSZoneAlreadyAbsent
	}
	if err != nil {
		return dnsZoneSyncV3Plan{}, fmt.Errorf("read DNS V3 publication state: %w", err)
	}
	if state.hasLease() {
		return dnsZoneSyncV3Plan{}, &dnsZoneExistingLegacyLeaseError{State: state}
	}
	if lease, leaseErr := readDNSZoneEngineLease(ctx, tx, domain); leaseErr == nil {
		return dnsZoneSyncV3Plan{}, &dnsZoneExistingEngineLeaseError{Lease: lease}
	} else if !errors.Is(leaseErr, sql.ErrNoRows) {
		return dnsZoneSyncV3Plan{}, fmt.Errorf("read existing DNS V3 lease: %w", leaseErr)
	}

	var records []zoneRecord
	if deleted {
		if state.DesiredAction != "delete" || state.SourceDomainID.Valid {
			return dnsZoneSyncV3Plan{}, errors.New("DNS deletion does not match the durable desired action")
		}
	} else {
		if state.DesiredAction != "sync" || !state.SourceDomainID.Valid {
			return dnsZoneSyncV3Plan{}, errors.New("DNS publication does not match the durable desired action")
		}
		var zoneID, soaRecordID int64
		var soa string
		if err := tx.QueryRowContext(ctx, `
			SELECT d.id, r.id, r.content
			FROM pdns_domains d
			JOIN pdns_records r
			  ON r.domain_id = d.id AND r.type = 'SOA' AND r.name = d.name
			WHERE d.name = ? LIMIT 1`, domain).Scan(
			&zoneID, &soaRecordID, &soa,
		); err != nil {
			return dnsZoneSyncV3Plan{}, fmt.Errorf("zone %s has no valid SOA: %w", domain, err)
		}
		next, err := nextSOASerial(soa, time.Now())
		if err != nil {
			return dnsZoneSyncV3Plan{}, fmt.Errorf("zone %s: %w", domain, err)
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE pdns_records SET content = ? WHERE id = ?`, next, soaRecordID,
		); err != nil {
			return dnsZoneSyncV3Plan{}, fmt.Errorf("advance zone SOA: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE pdns_domains SET type = ? WHERE id = ? AND type IS NOT ?`,
			zoneType, zoneID, zoneType,
		); err != nil {
			return dnsZoneSyncV3Plan{}, fmt.Errorf("align zone type: %w", err)
		}
		state, err = readDNSZoneSyncState(ctx, tx, domain)
		if err != nil {
			return dnsZoneSyncV3Plan{}, fmt.Errorf("reload DNS V3 publication state: %w", err)
		}
		rows, err := tx.QueryContext(ctx, `
			SELECT r.name, r.type, r.content,
			       COALESCE(r.ttl, 3600), COALESCE(r.prio, 0), r.disabled
			FROM pdns_records r
			JOIN pdns_domains d ON d.id = r.domain_id
			WHERE d.name = ?`, domain)
		if err != nil {
			return dnsZoneSyncV3Plan{}, fmt.Errorf("read DNS V3 zone snapshot: %w", err)
		}
		for rows.Next() {
			var record zoneRecord
			if err := rows.Scan(
				&record.Name, &record.Type, &record.Content,
				&record.TTL, &record.Prio, &record.Disabled,
			); err != nil {
				rows.Close()
				return dnsZoneSyncV3Plan{}, fmt.Errorf("scan DNS zone snapshot: %w", err)
			}
			records = append(records, record)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return dnsZoneSyncV3Plan{}, fmt.Errorf("read DNS V3 zone snapshot rows: %w", err)
		}
		if err := rows.Close(); err != nil {
			return dnsZoneSyncV3Plan{}, fmt.Errorf("close DNS V3 zone snapshot: %w", err)
		}
	}

	commitment, err := mutationpayload.CanonicalDNSZoneSyncV3(
		publisher.Engine, publisher.Epoch,
		state.DesiredGeneration, state.ZoneName,
		state.DesiredAction == "delete", state.DesiredZoneType, records,
	)
	if err != nil {
		return dnsZoneSyncV3Plan{}, fmt.Errorf("canonicalize DNS V3 publication snapshot: %w", err)
	}
	expiresAt := time.Now().UTC().Add(dnsZoneSyncLeaseTimeout).Format(time.RFC3339Nano)
	result, err := tx.ExecContext(ctx, `
		INSERT INTO dns_zone_engine_leases (
		  zone_name, engine, engine_epoch, request_id, owner_id,
		  desired_generation, desired_action, desired_zone_type,
		  qualifier, expires_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		state.ZoneName, publisher.Engine, publisher.Epoch,
		requestID, ownerID, commitment.DesiredGeneration,
		state.DesiredAction, commitment.ZoneType,
		commitment.Qualifier, expiresAt,
	)
	if err != nil {
		return dnsZoneSyncV3Plan{}, fmt.Errorf("persist DNS V3 publication lease: %w", err)
	}
	if err := requireExactRows(
		result, 1, "DNS V3 publication lease was not persisted exactly once",
	); err != nil {
		return dnsZoneSyncV3Plan{}, err
	}
	if err := tx.Commit(); err != nil {
		return dnsZoneSyncV3Plan{}, fmt.Errorf("commit DNS V3 publication lease: %w", err)
	}
	lease := dnsZoneEngineLease{
		ZoneName: state.ZoneName, Engine: publisher.Engine,
		EngineEpoch: publisher.Epoch, RequestID: requestID, OwnerID: ownerID,
		DesiredGeneration: commitment.DesiredGeneration,
		DesiredAction:     state.DesiredAction, DesiredZoneType: commitment.ZoneType,
		Qualifier: commitment.Qualifier, ExpiresAt: expiresAt,
	}
	return dnsZoneSyncV3Plan{State: state, Lease: lease, Commitment: commitment}, nil
}

func (p *Panel) recordDNSZoneSyncV3Success(
	ctx context.Context,
	lease dnsZoneEngineLease,
) (bool, error) {
	if !lease.valid() {
		return false, errors.New("DNS V3 publication success has no exact lease")
	}
	tx, err := p.db.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	engineState, err := readDNSEngineDBState(ctx, tx)
	if err != nil {
		return false, err
	}
	if engineState.CurrentSwitchID != "" ||
		engineState.ActiveEngine != lease.Engine ||
		engineState.EngineEpoch != lease.EngineEpoch {
		return false, errors.New("DNS V3 publication authority changed before finalization")
	}
	application, err := tx.ExecContext(ctx, `
		INSERT INTO dns_zone_engine_applications (
		  zone_name, engine, engine_epoch, applied_generation,
		  applied_action, applied_zone_type, qualifier,
		  mutation_request_id, mutation_owner_id, switch_id, revision
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, 1)
		ON CONFLICT(zone_name, engine) DO UPDATE SET
		  engine_epoch = excluded.engine_epoch,
		  applied_generation = excluded.applied_generation,
		  applied_action = excluded.applied_action,
		  applied_zone_type = excluded.applied_zone_type,
		  qualifier = excluded.qualifier,
		  mutation_request_id = excluded.mutation_request_id,
		  mutation_owner_id = excluded.mutation_owner_id,
		  switch_id = NULL,
		  revision = dns_zone_engine_applications.revision + 1,
		  applied_at = datetime('now'), updated_at = datetime('now')
		WHERE dns_zone_engine_applications.engine_epoch <= excluded.engine_epoch
		  AND dns_zone_engine_applications.applied_generation <= excluded.applied_generation`,
		lease.ZoneName, lease.Engine, lease.EngineEpoch,
		lease.DesiredGeneration, lease.DesiredAction, lease.DesiredZoneType,
		lease.Qualifier, lease.RequestID, lease.OwnerID,
	)
	if err != nil {
		return false, err
	}
	if err := requireExactRows(
		application, 1, "DNS V3 application finalization lost monotonic CAS",
	); err != nil {
		return false, err
	}
	stateResult, err := tx.ExecContext(ctx, `
		UPDATE dns_zone_sync_state
		SET applied_generation = max(applied_generation, ?),
		    status = CASE
		      WHEN desired_generation = ? AND desired_action = ?
		       AND desired_zone_type = ? THEN 'applied'
		      ELSE 'pending'
		    END,
		    last_error = NULL, updated_at = datetime('now')
		WHERE zone_name = ?`,
		lease.DesiredGeneration, lease.DesiredGeneration,
		lease.DesiredAction, lease.DesiredZoneType, lease.ZoneName,
	)
	if err != nil {
		return false, err
	}
	if err := requireExactRows(
		stateResult, 1, "DNS V3 publication state finalization was not exact",
	); err != nil {
		return false, err
	}
	removed, err := tx.ExecContext(ctx, `
		DELETE FROM dns_zone_engine_leases
		WHERE zone_name = ? AND engine = ? AND engine_epoch = ?
		  AND request_id = ? AND owner_id = ? AND desired_generation = ?
		  AND desired_action = ? AND desired_zone_type = ?
		  AND qualifier = ? AND expires_at = ?`, dnsZoneEngineLeaseWhere(lease)...)
	if err != nil {
		return false, err
	}
	if err := requireExactRows(
		removed, 1, "DNS V3 publication success lost its exact lease CAS",
	); err != nil {
		return false, err
	}
	state, err := readDNSZoneSyncState(ctx, tx, lease.ZoneName)
	if err != nil {
		return false, err
	}
	exact := state.Status == "applied" &&
		state.DesiredGeneration == lease.DesiredGeneration &&
		state.DesiredAction == lease.DesiredAction &&
		state.DesiredZoneType == lease.DesiredZoneType
	if exact && lease.DesiredAction == "delete" {
		retired, err := tx.ExecContext(ctx,
			`DELETE FROM dns_zone_deletion_markers WHERE zone_name = ?`,
			lease.ZoneName,
		)
		if err != nil {
			return false, fmt.Errorf("retire applied DNS V3 deletion marker: %w", err)
		}
		if err := requireExactRows(
			retired, 1, "applied DNS V3 deletion marker was not retired exactly once",
		); err != nil {
			return false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return exact, nil
}

func (p *Panel) recordDNSZoneSyncV3Failure(
	ctx context.Context,
	lease dnsZoneEngineLease,
	failure error,
) error {
	if !lease.valid() {
		return errors.New("DNS V3 publication failure has no exact lease")
	}
	tx, err := p.db.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	removed, err := tx.ExecContext(ctx, `
		DELETE FROM dns_zone_engine_leases
		WHERE zone_name = ? AND engine = ? AND engine_epoch = ?
		  AND request_id = ? AND owner_id = ? AND desired_generation = ?
		  AND desired_action = ? AND desired_zone_type = ?
		  AND qualifier = ? AND expires_at = ?`, dnsZoneEngineLeaseWhere(lease)...)
	if err != nil {
		return err
	}
	if err := requireExactRows(
		removed, 1, "DNS V3 publication failure lost its exact lease CAS",
	); err != nil {
		return err
	}
	message := boundedDNSZoneError(failure)
	stateResult, err := tx.ExecContext(ctx, `
		UPDATE dns_zone_sync_state
		SET status = CASE
		      WHEN desired_generation = ? AND desired_action = ?
		       AND desired_zone_type = ? THEN 'error'
		      ELSE 'pending'
		    END,
		    last_error = CASE
		      WHEN desired_generation = ? AND desired_action = ?
		       AND desired_zone_type = ? THEN ?
		      ELSE NULL
		    END,
		    updated_at = datetime('now')
		WHERE zone_name = ?`,
		lease.DesiredGeneration, lease.DesiredAction, lease.DesiredZoneType,
		lease.DesiredGeneration, lease.DesiredAction, lease.DesiredZoneType,
		message, lease.ZoneName,
	)
	if err != nil {
		return err
	}
	if err := requireExactRows(
		stateResult, 1, "DNS V3 failure state was not updated exactly once",
	); err != nil {
		return err
	}
	return tx.Commit()
}

func (p *Panel) releaseDNSZoneEngineLease(
	ctx context.Context,
	lease dnsZoneEngineLease,
) error {
	if !lease.valid() {
		return errors.New("DNS V3 publication release has no exact lease")
	}
	tx, err := p.db.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	removed, err := tx.ExecContext(ctx, `
		DELETE FROM dns_zone_engine_leases
		WHERE zone_name = ? AND engine = ? AND engine_epoch = ?
		  AND request_id = ? AND owner_id = ? AND desired_generation = ?
		  AND desired_action = ? AND desired_zone_type = ?
		  AND qualifier = ? AND expires_at = ?`, dnsZoneEngineLeaseWhere(lease)...)
	if err != nil {
		return err
	}
	if err := requireExactRows(
		removed, 1, "DNS V3 publication release lost its exact lease CAS",
	); err != nil {
		return err
	}
	stateResult, err := tx.ExecContext(ctx, `
		UPDATE dns_zone_sync_state
		SET status = 'pending', last_error = NULL, updated_at = datetime('now')
		WHERE zone_name = ?`, lease.ZoneName)
	if err != nil {
		return err
	}
	if err := requireExactRows(
		stateResult, 1, "DNS V3 release state was not updated exactly once",
	); err != nil {
		return err
	}
	return tx.Commit()
}

func (p *Panel) recoverPendingDNSZoneV3(
	ctx context.Context,
	lease dnsZoneEngineLease,
) (bool, error) {
	if !lease.valid() {
		return false, errors.New("DNS V3 recovery has no exact durable lease")
	}
	if err := p.requireDNSZoneSyncV3Agent(ctx); err != nil {
		return false, &dnsAgentPublicationError{Err: fmt.Errorf(
			"verify DNS V3 recovery capability: %w", err,
		)}
	}
	op := serviceOperation{
		RequestID:   lease.RequestID,
		Kind:        "dns_zone_sync",
		ServiceID:   lease.ZoneName,
		PackageName: lease.Qualifier,
	}
	var response transport.RecoverDNSZoneV3Response
	attemptCtx, cancelAttempt := context.WithTimeout(
		ctx, panelMutationRecoveryTimeout,
	)
	callErr := p.withResumedStandaloneAgentMutationIdentity(
		attemptCtx,
		op,
		lease.OwnerID,
		func(callCtx context.Context, binding agentMutationBinding) error {
			return p.callRecoverDNSZoneV3(
				callCtx, lease, binding, &response,
			)
		},
	)
	cancelAttempt()

	statusCtx, cancelStatus := dnsZoneFinalizeContext(ctx)
	job, statusErr := p.statusAgentMutation(statusCtx, lease.RequestID)
	cancelStatus()
	if statusErr != nil {
		return false, &dnsAgentPublicationError{Err: errors.Join(
			callErr,
			fmt.Errorf("read exact DNS V3 recovery status: %w", statusErr),
		)}
	}
	identity := lease.identity()
	if job == nil {
		return false, &dnsAgentPublicationError{Err: errors.Join(
			callErr,
			errors.New("exact DNS V3 recovery job disappeared"),
		)}
	}
	if err := validateAgentMutationIdentity(job, identity); err != nil {
		return false, err
	}
	if agentMutationActive(job.Status) {
		if err := validateDNSZoneSyncV3RecoveringJob(job, identity); err != nil {
			return false, err
		}
		waitCtx, cancelWait := context.WithTimeout(
			ctx, panelMutationRecoveryTimeout,
		)
		waited, waitErr := p.waitExpectedAgentMutationTerminal(
			waitCtx, identity,
		)
		cancelWait()
		if waited != nil {
			job = waited
		}
		if waitErr != nil {
			if job != nil &&
				validateDNSZoneSyncV3RecoveringJob(job, identity) == nil {
				return false, &dnsAgentPublicationError{Err: fmt.Errorf(
					"%w: exact recovering attempt remains active: %v",
					errDNSZoneV3PropagationDeferred,
					errors.Join(callErr, waitErr),
				)}
			}
			return false, &dnsAgentPublicationError{Err: errors.Join(
				callErr,
				fmt.Errorf("wait for exact DNS V3 recovery: %w", waitErr),
			)}
		}
		if job == nil {
			return false, errors.New("exact DNS V3 recovery disappeared while waiting")
		}
		if err := validateAgentMutationIdentity(job, identity); err != nil {
			return false, err
		}
	}
	if job.Status == agentMutationSucceeded {
		if err := validateAgentMutationSucceededReceipt(job, identity); err != nil {
			return false, err
		}
		finalizeCtx, cancelFinalize := dnsZoneFinalizeContext(ctx)
		exact, err := p.recordDNSZoneSyncV3Success(finalizeCtx, lease)
		cancelFinalize()
		return exact, err
	}
	if job.Status == agentMutationPending {
		if err := validateDNSZoneSyncV3PendingJob(job, identity); err != nil {
			return false, err
		}
		return false, &dnsAgentPublicationError{Err: fmt.Errorf(
			"%w: %v",
			errDNSZoneV3PropagationDeferred,
			errors.Join(callErr, &dnsZoneV3PropagationPendingError{}),
		)}
	}
	if agentMutationActive(job.Status) {
		return false, &dnsAgentPublicationError{Err: errors.Join(
			callErr,
			errors.New("exact DNS V3 recovery remains active"),
		)}
	}
	return false, &dnsAgentPublicationError{Err: errors.Join(
		callErr,
		fmt.Errorf(
			"exact committed DNS V3 recovery ended with unsafe status %s",
			job.Status,
		),
	)}
}

func (p *Panel) settleDNSZoneSyncV3CallError(
	ctx context.Context,
	lease dnsZoneEngineLease,
	callErr error,
) (done bool, retry bool, err error) {
	statusCtx, statusCancel := dnsZoneFinalizeContext(ctx)
	job, statusErr := p.statusAgentMutation(statusCtx, lease.RequestID)
	statusCancel()
	if statusErr != nil {
		return false, false, &dnsAgentPublicationError{Err: errors.Join(
			callErr,
			fmt.Errorf("DNS V3 publication terminal status is ambiguous: %w", statusErr),
		)}
	}
	if job == nil {
		finalizeCtx, cancel := dnsZoneFinalizeContext(ctx)
		finalizeErr := p.recordDNSZoneSyncV3Failure(finalizeCtx, lease, callErr)
		cancel()
		if finalizeErr != nil {
			return false, false, fmt.Errorf("record pre-Begin DNS V3 publication failure: %w", finalizeErr)
		}
		return false, false, nil
	}
	if identityErr := validateAgentMutationIdentity(job, lease.identity()); identityErr != nil {
		return false, false, identityErr
	}
	if agentMutationActive(job.Status) {
		return false, false, &dnsAgentPublicationError{Err: errors.Join(
			callErr,
			errors.New("exact DNS V3 publication remains active for startup recovery"),
		)}
	}
	if job.Status == agentMutationPending {
		if err := validateDNSZoneSyncV3PendingJob(job, lease.identity()); err != nil {
			return false, false, err
		}
		exact, recoverErr := p.recoverPendingDNSZoneV3(ctx, lease)
		if recoverErr != nil {
			return false, false, &dnsAgentPublicationError{Err: errors.Join(
				callErr, recoverErr,
			)}
		}
		return exact, !exact, nil
	}
	finalizeCtx, cancel := dnsZoneFinalizeContext(ctx)
	defer cancel()
	if job.Status == agentMutationSucceeded {
		if err := validateAgentMutationSucceededReceipt(job, lease.identity()); err != nil {
			return false, false, err
		}
		exact, finalizeErr := p.recordDNSZoneSyncV3Success(finalizeCtx, lease)
		return exact, !exact, finalizeErr
	}
	if finalizeErr := p.recordDNSZoneSyncV3Failure(finalizeCtx, lease, callErr); finalizeErr != nil {
		return false, false, fmt.Errorf("record terminal DNS V3 publication failure: %w", finalizeErr)
	}
	return false, false, nil
}

func (p *Panel) reconcileDNSZoneEngineLease(
	ctx context.Context,
	lease dnsZoneEngineLease,
	waitActive bool,
) (bool, error) {
	if !lease.valid() {
		return false, errors.New("DNS V3 publication state contains an invalid durable lease")
	}
	identity := lease.identity()
	job, err := p.statusAgentMutation(ctx, identity.RequestID)
	if err != nil {
		return false, &dnsAgentPublicationError{Err: fmt.Errorf("read durable DNS V3 publication status: %w", err)}
	}
	if job == nil {
		finalizeCtx, cancel := dnsZoneFinalizeContext(ctx)
		releaseErr := p.releaseDNSZoneEngineLease(finalizeCtx, lease)
		cancel()
		return false, releaseErr
	}
	if err := validateAgentMutationIdentity(job, identity); err != nil {
		return false, err
	}
	if agentMutationActive(job.Status) {
		if !waitActive {
			return false, &dnsAgentPublicationError{Err: errors.New("DNS V3 publication is already in progress")}
		}
		job, err = p.waitExpectedAgentMutationTerminal(ctx, identity)
		if err != nil && (job == nil || agentMutationActive(job.Status)) {
			return false, &dnsAgentPublicationError{Err: fmt.Errorf("wait for durable DNS V3 publication: %w", err)}
		}
		if job == nil {
			return false, errors.New("durable DNS V3 publication disappeared during recovery")
		}
	}
	if job.Status == agentMutationPending {
		if err := validateDNSZoneSyncV3PendingJob(job, identity); err != nil {
			return false, err
		}
		return p.recoverPendingDNSZoneV3(ctx, lease)
	}
	finalizeCtx, cancel := dnsZoneFinalizeContext(ctx)
	defer cancel()
	if job.Status == agentMutationSucceeded {
		if err := validateAgentMutationSucceededReceipt(job, identity); err != nil {
			return false, err
		}
		return p.recordDNSZoneSyncV3Success(finalizeCtx, lease)
	}
	failure := errors.New(strings.TrimSpace(job.ErrorMessage))
	if strings.TrimSpace(job.ErrorMessage) == "" {
		failure = fmt.Errorf("durable DNS V3 publication ended with status %s", job.Status)
	}
	if err := p.recordDNSZoneSyncV3Failure(finalizeCtx, lease, failure); err != nil {
		return false, err
	}
	return false, nil
}

func validDirectDNSZoneSyncV3(job *agentMutationJob) bool {
	if job == nil || !agentMutationActive(job.Status) ||
		!validServiceOperationID(job.RequestID) ||
		!validServiceOperationID(job.OwnerID) ||
		job.Kind != "dns_zone_sync" ||
		!mutationpayload.ValidDNSZoneSyncV3Qualifier(job.PackageName) {
		return false
	}
	canonical, err := hostname.CanonicalFQDN(job.Target)
	return err == nil && canonical == job.Target
}

func (p *Panel) exactDNSZoneEngineLeaseForJob(
	ctx context.Context,
	job *agentMutationJob,
) (dnsZoneEngineLease, error) {
	if !validDirectDNSZoneSyncV3(job) {
		return dnsZoneEngineLease{}, errors.New("direct DNS V3 publication has an invalid durable identity")
	}
	lease, err := readDNSZoneEngineLease(ctx, p.db.GetDB(), job.Target)
	if err != nil {
		return dnsZoneEngineLease{}, fmt.Errorf("read direct DNS V3 publication lease: %w", err)
	}
	if !lease.valid() || !lease.identity().matches(job) {
		return dnsZoneEngineLease{}, errors.New("direct DNS V3 publication does not match the exact persisted zone lease")
	}
	return lease, nil
}

// recoverDirectDNSZoneSyncV3Locked requires serviceMutationMu and consumes
// only a global active V3 job whose complete identity matches migration 033.
func (p *Panel) recoverDirectDNSZoneSyncV3Locked(
	ctx context.Context,
	job *agentMutationJob,
) error {
	batchCtx, cancelBatch := dnsZoneBatchContext(ctx)
	defer cancelBatch()
	dnsPublicationMu.Lock()
	defer dnsPublicationMu.Unlock()
	lease, err := p.exactDNSZoneEngineLeaseForJob(batchCtx, job)
	if err != nil {
		return err
	}
	done, err := p.reconcileDNSZoneEngineLease(batchCtx, lease, true)
	if err != nil {
		if errors.Is(err, errDNSZoneV3PropagationDeferred) {
			log.Printf(
				"deferring exact DNS V3 publication %s until paired propagation is ready: %v",
				lease.ZoneName, err,
			)
			return nil
		}
		return err
	}
	if !done {
		if err := p.syncZoneToDNSLocked(
			batchCtx, lease.ZoneName, lease.DesiredAction == "delete",
		); err != nil {
			return err
		}
	}
	return p.recoverDNSZoneSyncStateAlreadyLocked(batchCtx)
}
