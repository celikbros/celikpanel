package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/transport"
)

const (
	dnsEngineOperationSetting       = "dns_engine_operation_v1"
	dnsEngineOperationVersion       = 1
	dnsEngineOperationAccepted      = "accepted"
	dnsEngineOperationPostCommit    = "post_commit"
	dnsEnginePostCommitAuditTimeout = 5 * time.Second
)

type dnsEngineOperationMarker struct {
	Version                int                 `json:"version"`
	RequestID              string              `json:"request_id"`
	SwitchID               string              `json:"switch_id"`
	SourceEngine           transport.DNSEngine `json:"source_engine,omitempty"`
	TargetEngine           transport.DNSEngine `json:"target_engine"`
	Action                 string              `json:"action"`
	Phase                  string              `json:"phase"`
	ConfigurePDNSRequestID string              `json:"configure_pdns_request_id,omitempty"`
	ConfigurePDNSOwnerID   string              `json:"configure_pdns_owner_id,omitempty"`
	ConfigurePDNSComplete  bool                `json:"configure_pdns_complete,omitempty"`
}

func validDNSEngineSwitchAction(action string) bool {
	return action == "install" || action == "adopt" ||
		action == "switch" || action == "reconfigure" ||
		action == dnsEngineActionAdoptUnmanaged
}

func dnsEngineMutationMode(action string) string {
	switch action {
	case "adopt":
		return transport.DNSEngineSwitchModeAdopt
	case dnsEngineActionReinstall:
		return transport.DNSEngineSwitchModeReinstall
	default:
		return transport.DNSEngineSwitchModeSwitch
	}
}

func validateDNSEngineOperationMarker(marker dnsEngineOperationMarker) error {
	if marker.Version != dnsEngineOperationVersion ||
		!validServiceOperationID(marker.RequestID) ||
		!validServiceOperationID(marker.SwitchID) ||
		(marker.SourceEngine != "" &&
			!transport.ValidDNSEngine(marker.SourceEngine)) ||
		!transport.ValidDNSEngine(marker.TargetEngine) ||
		!validDNSEngineSwitchAction(marker.Action) ||
		(marker.Phase != dnsEngineOperationAccepted &&
			marker.Phase != dnsEngineOperationPostCommit) {
		return errors.New("DNS engine operation marker is invalid")
	}
	if marker.Action == "switch" {
		if marker.SourceEngine == "" ||
			marker.SourceEngine == marker.TargetEngine {
			return errors.New("DNS engine switch marker has invalid source")
		}
	} else if marker.Action == "adopt" && marker.SourceEngine != "" {
		return errors.New("DNS engine adopt marker has a source")
	} else if marker.Action == dnsEngineActionAdoptUnmanaged &&
		(marker.SourceEngine != "" ||
			marker.TargetEngine != transport.DNSEngineBIND) {
		return errors.New("DNS engine takeover marker has invalid identity")
	} else if marker.Action == "reconfigure" &&
		(marker.SourceEngine != "" ||
			marker.TargetEngine != transport.DNSEnginePowerDNS) {
		return errors.New("DNS engine reconfigure marker has invalid identity")
	}
	hasConfigureRequest := marker.ConfigurePDNSRequestID != ""
	hasConfigureOwner := marker.ConfigurePDNSOwnerID != ""
	if hasConfigureRequest != hasConfigureOwner {
		return errors.New("DNS engine operation marker has incomplete configure identity")
	}
	if hasConfigureRequest &&
		(!validServiceOperationID(marker.ConfigurePDNSRequestID) ||
			!validServiceOperationID(marker.ConfigurePDNSOwnerID) ||
			marker.Action != "adopt" ||
			marker.TargetEngine != transport.DNSEnginePowerDNS ||
			marker.Phase != dnsEngineOperationPostCommit) {
		return errors.New("DNS engine operation marker has invalid configure identity")
	}
	if marker.ConfigurePDNSComplete && !hasConfigureRequest {
		return errors.New("DNS engine operation marker completed an absent configure child")
	}
	return nil
}

func encodeDNSEngineOperationMarker(
	marker dnsEngineOperationMarker,
) (string, error) {
	if err := validateDNSEngineOperationMarker(marker); err != nil {
		return "", err
	}
	encoded, err := json.Marshal(marker)
	return string(encoded), err
}

func decodeDNSEngineOperationMarker(
	raw string,
) (*dnsEngineOperationMarker, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	if len(raw) > 4096 {
		return nil, errors.New("DNS engine operation marker is too large")
	}
	var marker dnsEngineOperationMarker
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&marker); err != nil {
		return nil, fmt.Errorf("decode DNS engine operation marker: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, errors.New("DNS engine operation marker contains trailing data")
	}
	canonical, err := encodeDNSEngineOperationMarker(marker)
	if err != nil || !bytes.Equal([]byte(raw), []byte(canonical)) {
		return nil, errors.New("DNS engine operation marker is not canonical")
	}
	return &marker, nil
}

func readDNSEngineOperationMarker(
	ctx context.Context,
	query dnsZoneStateQuery,
) (*dnsEngineOperationMarker, error) {
	var raw string
	err := query.QueryRowContext(
		ctx, `SELECT value FROM panel_settings WHERE key = ?`,
		dnsEngineOperationSetting,
	).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return decodeDNSEngineOperationMarker(raw)
}

func persistDNSEngineOperationMarkerTx(
	ctx context.Context,
	tx *sql.Tx,
	marker dnsEngineOperationMarker,
) error {
	current, err := settingTx(ctx, tx, dnsEngineOperationSetting)
	if err != nil {
		return err
	}
	if strings.TrimSpace(current) != "" {
		return errors.New("another DNS engine post-commit operation is pending")
	}
	raw, err := encodeDNSEngineOperationMarker(marker)
	if err != nil {
		return err
	}
	return setSettingTx(ctx, tx, dnsEngineOperationSetting, raw)
}

func advanceDNSEngineOperationMarkerTx(
	ctx context.Context,
	tx *sql.Tx,
	persisted persistedDNSEngineSwitch,
	from, to string,
) error {
	marker, err := readDNSEngineOperationMarker(ctx, tx)
	if err != nil {
		return err
	}
	if marker == nil || marker.RequestID != persisted.RequestID ||
		marker.SwitchID != persisted.SwitchID ||
		marker.SourceEngine != persisted.SourceEngine ||
		marker.TargetEngine != persisted.TargetEngine ||
		marker.Action != persisted.Action || marker.Phase != from {
		return errors.New("DNS engine operation marker lost its exact CAS")
	}
	before, err := encodeDNSEngineOperationMarker(*marker)
	if err != nil {
		return err
	}
	marker.Phase = to
	after, err := encodeDNSEngineOperationMarker(*marker)
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE panel_settings
		SET value = ?, updated_at = CURRENT_TIMESTAMP
		WHERE key = ? AND value = ?`,
		after, dnsEngineOperationSetting, before,
	)
	if err != nil {
		return err
	}
	return requireExactRows(
		result, 1, "DNS engine operation marker advance was not exact",
	)
}

func clearDNSEngineOperationMarkerTx(
	ctx context.Context,
	tx *sql.Tx,
	persisted persistedDNSEngineSwitch,
	phase string,
) error {
	marker, err := readDNSEngineOperationMarker(ctx, tx)
	if err != nil {
		return err
	}
	if marker == nil || marker.RequestID != persisted.RequestID ||
		marker.SwitchID != persisted.SwitchID ||
		marker.SourceEngine != persisted.SourceEngine ||
		marker.TargetEngine != persisted.TargetEngine ||
		marker.Action != persisted.Action || marker.Phase != phase {
		return errors.New("DNS engine operation marker lost its exact clear CAS")
	}
	raw, err := encodeDNSEngineOperationMarker(*marker)
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE panel_settings
		SET value = '', updated_at = CURRENT_TIMESTAMP
		WHERE key = ? AND value = ?`,
		dnsEngineOperationSetting, raw,
	)
	if err != nil {
		return err
	}
	return requireExactRows(
		result, 1, "DNS engine operation marker clear was not exact",
	)
}

func (p *Panel) clearDNSEnginePostCommitMarker(
	ctx context.Context,
	persisted persistedDNSEngineSwitch,
) error {
	tx, err := p.db.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := clearDNSEngineOperationMarkerTx(
		ctx, tx, persisted, dnsEngineOperationPostCommit,
	); err != nil {
		return err
	}
	return tx.Commit()
}

func exactDNSEnginePostCommitMarker(
	marker *dnsEngineOperationMarker,
	persisted persistedDNSEngineSwitch,
) bool {
	return marker != nil &&
		marker.RequestID == persisted.RequestID &&
		marker.SwitchID == persisted.SwitchID &&
		marker.SourceEngine == persisted.SourceEngine &&
		marker.TargetEngine == persisted.TargetEngine &&
		marker.Action == persisted.Action &&
		marker.Phase == dnsEngineOperationPostCommit
}

func (p *Panel) updateDNSEnginePostCommitMarker(
	ctx context.Context,
	persisted persistedDNSEngineSwitch,
	mutate func(*dnsEngineOperationMarker) error,
) (dnsEngineOperationMarker, error) {
	tx, err := p.db.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return dnsEngineOperationMarker{}, err
	}
	defer tx.Rollback()
	marker, err := readDNSEngineOperationMarker(ctx, tx)
	if err != nil {
		return dnsEngineOperationMarker{}, err
	}
	if !exactDNSEnginePostCommitMarker(marker, persisted) {
		return dnsEngineOperationMarker{}, errors.New(
			"DNS engine post-commit marker lost its exact identity",
		)
	}
	before, err := encodeDNSEngineOperationMarker(*marker)
	if err != nil {
		return dnsEngineOperationMarker{}, err
	}
	if err := mutate(marker); err != nil {
		return dnsEngineOperationMarker{}, err
	}
	after, err := encodeDNSEngineOperationMarker(*marker)
	if err != nil {
		return dnsEngineOperationMarker{}, err
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE panel_settings
		SET value = ?, updated_at = CURRENT_TIMESTAMP
		WHERE key = ? AND value = ?`,
		after, dnsEngineOperationSetting, before,
	)
	if err != nil {
		return dnsEngineOperationMarker{}, err
	}
	if err := requireExactRows(
		result, 1, "DNS engine post-commit marker update lost its exact CAS",
	); err != nil {
		return dnsEngineOperationMarker{}, err
	}
	if err := tx.Commit(); err != nil {
		return dnsEngineOperationMarker{}, err
	}
	return *marker, nil
}

func (p *Panel) ensureAdoptionConfigureIdentity(
	ctx context.Context,
	persisted persistedDNSEngineSwitch,
) (dnsEngineOperationMarker, error) {
	marker, err := readDNSEngineOperationMarker(ctx, p.db.GetDB())
	if err != nil {
		return dnsEngineOperationMarker{}, err
	}
	if !exactDNSEnginePostCommitMarker(marker, persisted) {
		return dnsEngineOperationMarker{}, errors.New(
			"adopted PowerDNS has no exact post-commit marker",
		)
	}
	if marker.ConfigurePDNSRequestID != "" {
		return *marker, nil
	}
	requestID, err := newServiceOperationID()
	if err != nil {
		return dnsEngineOperationMarker{}, err
	}
	ownerID, err := newServiceOperationID()
	if err != nil {
		return dnsEngineOperationMarker{}, err
	}
	return p.updateDNSEnginePostCommitMarker(
		ctx, persisted, func(current *dnsEngineOperationMarker) error {
			if current.ConfigurePDNSRequestID != "" ||
				current.ConfigurePDNSOwnerID != "" ||
				current.ConfigurePDNSComplete {
				return errors.New("PowerDNS configure child identity changed before persistence")
			}
			current.ConfigurePDNSRequestID = requestID
			current.ConfigurePDNSOwnerID = ownerID
			return nil
		},
	)
}

func (p *Panel) markAdoptionConfigureComplete(
	ctx context.Context,
	persisted persistedDNSEngineSwitch,
	requestID, ownerID string,
) error {
	_, err := p.updateDNSEnginePostCommitMarker(
		ctx, persisted, func(marker *dnsEngineOperationMarker) error {
			if marker.ConfigurePDNSRequestID != requestID ||
				marker.ConfigurePDNSOwnerID != ownerID {
				return errors.New("PowerDNS configure completion lost its exact child identity")
			}
			marker.ConfigurePDNSComplete = true
			return nil
		},
	)
	return err
}

func attachDNSEngineOperationAction(
	ctx context.Context,
	query dnsZoneStateQuery,
	persisted *persistedDNSEngineSwitch,
) error {
	marker, err := readDNSEngineOperationMarker(ctx, query)
	if err != nil {
		return err
	}
	if marker == nil || persisted == nil ||
		marker.RequestID != persisted.RequestID ||
		marker.SwitchID != persisted.SwitchID ||
		marker.SourceEngine != persisted.SourceEngine ||
		marker.TargetEngine != persisted.TargetEngine {
		return errors.New("DNS engine switch has no exact operation marker")
	}
	if persisted.Mode != dnsEngineMutationMode(marker.Action) {
		return errors.New("DNS engine action does not match its persisted mode")
	}
	persisted.Action = marker.Action
	return nil
}

type dnsEnginePostCommitResult struct {
	NormalizationErr error
	FirewallErr      error
	ScanErr          error
}

func (result dnsEnginePostCommitResult) failed() bool {
	return result.NormalizationErr != nil ||
		result.FirewallErr != nil || result.ScanErr != nil
}

func (p *Panel) configureAdoptedPowerDNSLocked(
	ctx context.Context,
	persisted persistedDNSEngineSwitch,
) error {
	marker, err := p.ensureAdoptionConfigureIdentity(ctx, persisted)
	if err != nil {
		return err
	}
	if marker.ConfigurePDNSComplete {
		return nil
	}
	op := serviceOperation{
		RequestID: marker.ConfigurePDNSRequestID,
		Kind:      "pdns_configure",
		ServiceID: "pdns",
	}
	identity := agentMutationIdentityForOperation(
		op, marker.ConfigurePDNSOwnerID,
	)
	job, err := p.statusAgentMutation(ctx, op.RequestID)
	if err != nil {
		return fmt.Errorf("read adopted PowerDNS configure child: %w", err)
	}
	if job != nil {
		if err := validateAgentMutationIdentity(job, identity); err != nil {
			return err
		}
		if agentMutationActive(job.Status) {
			job, err = p.waitExpectedAgentMutationTerminal(ctx, identity)
			if err != nil && (job == nil || agentMutationActive(job.Status)) {
				return fmt.Errorf("wait for adopted PowerDNS configure child: %w", err)
			}
		}
		if job != nil && job.Status == agentMutationSucceeded {
			if err := validateAgentMutationSucceededReceipt(job, identity); err != nil {
				return err
			}
			return p.markAdoptionConfigureComplete(
				ctx, persisted, op.RequestID, marker.ConfigurePDNSOwnerID,
			)
		}
	}

	var response transport.SyncDNSZoneResponse
	call := func(
		callCtx context.Context,
		binding agentMutationBinding,
	) error {
		request := transport.ServiceMutationRequest{
			ServiceMutationBinding: binding,
		}
		if err := p.callAgentContext(
			callCtx, "Agent.ConfigurePowerDNSSQLite", &request, &response,
		); err != nil {
			return err
		}
		if response.Error != "" {
			log.Printf(
				"PowerDNS adoption configure agent detail: %s",
				boundedAgentDiagnostic(response.Error),
			)
			return errors.New("agent did not confirm PowerDNS configuration")
		}
		if !response.Synced {
			return errors.New("agent did not confirm PowerDNS configuration")
		}
		return nil
	}
	if job != nil {
		err = p.withResumedStandaloneAgentMutationIdentity(
			ctx, op, marker.ConfigurePDNSOwnerID, call,
		)
	} else {
		err = p.withStandaloneAgentMutationIdentity(
			ctx, op, marker.ConfigurePDNSOwnerID, call,
		)
	}
	if err != nil {
		return fmt.Errorf("configure adopted PowerDNS: %w", err)
	}
	return p.markAdoptionConfigureComplete(
		ctx, persisted, op.RequestID, marker.ConfigurePDNSOwnerID,
	)
}

// normalizeAdoptedPowerDNSLocked requires serviceMutationMu, dnsTopologyMu,
// and dnsPublicationMu in that order. Configure's child identity is committed
// to the post-commit marker before BeginServiceMutation; every V3 zone child
// is independently committed in dns_zone_engine_leases before its begin.
func (p *Panel) normalizeAdoptedPowerDNSLocked(
	ctx context.Context,
	persisted persistedDNSEngineSwitch,
) error {
	if legacyNonDirectionalPairedAdoption(persisted) {
		return errors.New(
			"legacy paired PowerDNS adoption has no directional catalog authority",
		)
	}
	if persisted.TargetEngine != transport.DNSEnginePowerDNS {
		return errors.New("adoption normalization target is not PowerDNS")
	}
	if err := p.requireDNSZoneSyncV3Agent(ctx); err != nil {
		return fmt.Errorf("verify adopted PowerDNS normalization capability: %w", err)
	}
	if err := p.requireActivePowerDNSPublisher(ctx); err != nil {
		return err
	}
	if err := p.configureAdoptedPowerDNSLocked(ctx, persisted); err != nil {
		return err
	}
	result, err := p.syncAllZonesLocked(ctx)
	if err != nil {
		return fmt.Errorf(
			"publish all adopted PowerDNS zones (%d/%d): %w",
			result.Synced, result.Attempted, err,
		)
	}
	return nil
}

func legacyNonDirectionalPairedAdoption(
	persisted persistedDNSEngineSwitch,
) bool {
	return persisted.Topology == transport.DNSTopologyPaired &&
		persisted.PairRole == ""
}

// reconcileDNSEnginePostCommitLocked performs independent, idempotent
// follow-ups only after the engine state is committed. Its caller owns
// serviceMutationMu. Neither failure is allowed to roll the DNS engine back.
func (p *Panel) reconcileDNSEnginePostCommitLocked(
	ctx context.Context,
	persisted persistedDNSEngineSwitch,
) dnsEnginePostCommitResult {
	result := dnsEnginePostCommitResult{}
	// Registration-only adoption proves and records an already-running
	// panel-managed PowerDNS authority. Normalize that external database via
	// the reviewed agent APIs, but do not alter host firewall state.
	if persisted.Action == "adopt" {
		result.NormalizationErr = p.normalizeAdoptedPowerDNSLocked(ctx, persisted)
	} else {
		result.FirewallErr = p.syncFirewallLocked(ctx)
	}
	if _, err := p.scanManagedServices(ctx); err != nil {
		result.ScanErr = err
	}
	if result.failed() {
		return result
	}
	if err := p.clearDNSEnginePostCommitMarker(ctx, persisted); err != nil {
		result.ScanErr = fmt.Errorf(
			"finalize DNS engine post-commit recovery: %w", err,
		)
	}
	return result
}

func writeDNSEnginePostCommitFailed(
	w http.ResponseWriter,
	result dnsEnginePostCommitResult,
) {
	code := errCodeServiceStateRefreshFailed
	message := "DNS engine change completed, but component state could not be refreshed"
	if result.NormalizationErr != nil {
		code = errCodeDNSPublicationFailed
		message = "PowerDNS adoption completed, but its external zone database could not be normalized"
		if result.ScanErr != nil {
			message = "PowerDNS adoption completed, but DNS normalization and component state follow-up need attention"
		}
	} else if result.FirewallErr != nil {
		code = errCodeFirewallSyncFailed
		message = "DNS engine change completed, but the active firewall policy could not be synchronized"
		if result.ScanErr != nil {
			message = "DNS engine change completed, but firewall and component state follow-up need attention"
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadGateway)
	_ = json.NewEncoder(w).Encode(apiErrorBody{
		Error: message, Code: code, Action: "/services",
		PartialSuccess: true, MutationApplied: true,
	})
}

type dnsEngineAuditActor struct {
	UserID    int
	IP        string
	UserAgent string
}

func dnsEngineActorFromRequest(r *http.Request) dnsEngineAuditActor {
	actor := dnsEngineAuditActor{}
	if caller := currentCaller(r); caller != nil {
		actor.UserID = caller.ID
	}
	actor.IP = clientIP(r)
	actor.UserAgent = r.UserAgent()
	if len(actor.UserAgent) > 300 {
		actor.UserAgent = actor.UserAgent[:300]
	}
	return actor
}

func dnsEngineAuditAction(
	outcome string,
	persisted persistedDNSEngineSwitch,
) string {
	source := string(persisted.SourceEngine)
	if source == "" {
		source = "none"
	}
	return fmt.Sprintf(
		"dns.engine.switch.%s request=%s switch=%s source=%s target=%s action=%s mode=%s",
		outcome, persisted.RequestID, persisted.SwitchID,
		source, persisted.TargetEngine, persisted.Action, persisted.Mode,
	)
}

func (p *Panel) auditDNSEngine(
	ctx context.Context,
	actor dnsEngineAuditActor,
	outcome string,
	persisted persistedDNSEngineSwitch,
) {
	p.auditDNSEngineAction(ctx, actor, dnsEngineAuditAction(outcome, persisted))
}

func (p *Panel) auditDNSEngineAction(
	ctx context.Context,
	actor dnsEngineAuditActor,
	action string,
) {
	if _, err := p.db.GetDB().ExecContext(ctx, `
		INSERT INTO audit_logs (
		  user_id, action, resource_type, resource_id, ip_address, user_agent
		)
		SELECT ?, ?, 'dns_engine', NULL, ?, ?
		WHERE NOT EXISTS (
		  SELECT 1 FROM audit_logs
		  WHERE action = ? AND resource_type = 'dns_engine'
		)`,
		nullablePositiveInt(actor.UserID), action,
		nullableNonEmpty(actor.IP), nullableNonEmpty(actor.UserAgent), action,
	); err != nil {
		log.Printf("audit write failed (%s): %v", action, err)
	}
}

func (p *Panel) auditDNSEngineBounded(
	actor dnsEngineAuditActor,
	outcome string,
	persisted persistedDNSEngineSwitch,
) {
	ctx, cancel := context.WithTimeout(
		context.Background(), dnsEnginePostCommitAuditTimeout,
	)
	defer cancel()
	p.auditDNSEngine(ctx, actor, outcome, persisted)
}

func (p *Panel) auditDNSEngineSystem(
	ctx context.Context,
	outcome string,
	persisted persistedDNSEngineSwitch,
) {
	p.auditDNSEngine(ctx, dnsEngineAuditActor{}, outcome, persisted)
}
