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
	Version      int                 `json:"version"`
	RequestID    string              `json:"request_id"`
	SwitchID     string              `json:"switch_id"`
	SourceEngine transport.DNSEngine `json:"source_engine,omitempty"`
	TargetEngine transport.DNSEngine `json:"target_engine"`
	Action       string              `json:"action"`
	Phase        string              `json:"phase"`
}

func validDNSEngineSwitchAction(action string) bool {
	return action == "install" || action == "adopt" ||
		action == "switch" || action == "reconfigure"
}

func dnsEngineMutationMode(action string) string {
	if action == "adopt" {
		return transport.DNSEngineSwitchModeAdopt
	}
	return transport.DNSEngineSwitchModeSwitch
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
	} else if marker.Action == "reconfigure" &&
		(marker.SourceEngine != "" ||
			marker.TargetEngine != transport.DNSEnginePowerDNS) {
		return errors.New("DNS engine reconfigure marker has invalid identity")
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
	FirewallErr error
	ScanErr     error
}

func (result dnsEnginePostCommitResult) failed() bool {
	return result.FirewallErr != nil || result.ScanErr != nil
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
	// panel-managed PowerDNS authority. It must not alter host firewall state.
	if persisted.Action != "adopt" {
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
	if result.FirewallErr != nil {
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
	action := dnsEngineAuditAction(outcome, persisted)
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
