package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/alicelik/celikpanel/internal/transport"
)

type domainDeletionStatusSnapshot struct {
	domain       string
	marked       bool
	inconsistent bool
	candidate    bool
	lease        dnsZoneEngineLease
	state        dnsZoneSyncState
	engine       dnsEngineDBState
}

func (p *Panel) readDomainDeletionStatusSnapshot(ctx context.Context, domainID int) (domainDeletionStatusSnapshot, error) {
	var snapshot domainDeletionStatusSnapshot
	tx, err := p.db.GetDB().BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return snapshot, err
	}
	defer tx.Rollback()

	var status, mode string
	var parent sql.NullInt64
	err = tx.QueryRowContext(ctx, `
        SELECT d.name, d.status, d.dns_management, d.parent_domain_id
        FROM domains d
        JOIN domain_deletion_operations op ON op.domain_id = d.id
        WHERE d.id = ?`, domainID).Scan(&snapshot.domain, &status, &mode, &parent)
	if errors.Is(err, sql.ErrNoRows) {
		return snapshot, nil
	}
	if err != nil {
		return snapshot, err
	}
	snapshot.marked = true
	if status != domainDeletionLedgerStatus {
		snapshot.inconsistent = true
		return snapshot, nil
	}
	if mode != setupDNSModeLocal || parent.Valid {
		return snapshot, nil
	}

	var markerType string
	err = tx.QueryRowContext(ctx, `SELECT zone_type FROM dns_zone_deletion_markers WHERE zone_name = ?`, snapshot.domain).Scan(&markerType)
	if errors.Is(err, sql.ErrNoRows) {
		return snapshot, nil
	}
	if err != nil {
		return snapshot, err
	}
	state, err := readDNSZoneSyncState(ctx, tx, snapshot.domain)
	if errors.Is(err, sql.ErrNoRows) {
		return snapshot, nil
	}
	if err != nil {
		return snapshot, err
	}
	lease, err := readDNSZoneEngineLease(ctx, tx, snapshot.domain)
	if errors.Is(err, sql.ErrNoRows) {
		return snapshot, nil
	}
	if err != nil {
		return snapshot, err
	}
	engineState, err := readDNSEngineDBState(ctx, tx)
	if err != nil {
		return snapshot, err
	}
	if state.ZoneName == snapshot.domain && state.Status == "pending" &&
		engineState.CurrentSwitchID == "" &&
		engineState.ActiveEngine == lease.Engine && engineState.EngineEpoch == lease.EngineEpoch &&
		state.DesiredAction == "delete" && !state.SourceDomainID.Valid &&
		state.DesiredGeneration > state.AppliedGeneration &&
		state.DesiredZoneType == markerType &&
		lease.valid() && lease.ZoneName == snapshot.domain &&
		lease.Engine == transport.DNSEngineBIND &&
		lease.DesiredAction == "delete" &&
		lease.DesiredGeneration == state.DesiredGeneration &&
		lease.DesiredZoneType == markerType {
		snapshot.candidate = true
		snapshot.lease = lease
		snapshot.state = state
		snapshot.engine = engineState
	}
	return snapshot, nil
}

func sameDomainDeletionStatusSnapshot(a, b domainDeletionStatusSnapshot) bool {
	return a.marked && b.marked && a.candidate && b.candidate &&
		a.domain == b.domain && a.lease == b.lease &&
		a.state == b.state && a.engine == b.engine
}

type domainDeletionStatusResponse struct {
	Status  string `json:"status"`
	Stage   string `json:"stage"`
	Reason  string `json:"reason,omitempty"`
	Message string `json:"message"`
}

// handleDomainDeletionStatus only observes the saved domain marker, V3 lease,
// and exact Agent job. It never reconciles a lease or starts deletion work.
func (p *Panel) handleDomainDeletionStatus(w http.ResponseWriter, r *http.Request, domainID int) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodGet {
		rejectRouteMethod(w, []string{http.MethodGet})
		return
	}
	snapshot, err := p.readDomainDeletionStatusSnapshot(r.Context(), domainID)
	if err != nil {
		log.Printf("read domain deletion status: %v", err)
		writeServerError(w, errors.New("domain deletion status could not be read"))
		return
	}
	if !snapshot.marked {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if snapshot.inconsistent {
		_ = json.NewEncoder(w).Encode(domainDeletionStatusResponse{
			Status: "unknown", Stage: "unknown",
			Message: "The saved deletion marker and domain state disagree. The server administrator must inspect the operation before retrying.",
		})
		return
	}
	result := domainDeletionStatusResponse{
		Status:  "unknown",
		Stage:   "unknown",
		Message: "Deletion is pending. The server administrator should inspect the saved operation before retrying this same deletion.",
	}
	if snapshot.candidate {
		statusCtx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		job, statusErr := p.statusAgentMutation(statusCtx, snapshot.lease.RequestID)
		cancel()
		if statusErr == nil && validateDNSZoneSyncV3PendingJob(job, snapshot.lease.identity()) == nil {
			result.Status = domainDeletionPendingStatus
			result.Stage = "dns_cleanup"
			after, rereadErr := p.readDomainDeletionStatusSnapshot(r.Context(), domainID)
			if rereadErr != nil || !sameDomainDeletionStatusSnapshot(snapshot, after) {
				result.Status = "unknown"
				result.Stage = "unknown"
				result.Message = "The saved deletion state changed during the read. The server administrator must inspect the operation before retrying."
			} else if code := dnsZoneV3PendingCodeFromJob(job); code != "" {
				result.Reason = code
				result.Message = dnsPeerPendingEnglish(code)
			}
		}
	}
	_ = json.NewEncoder(w).Encode(result)
}
