package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/core"
)

const (
	serviceOperationKindInstall        = "service_install"
	serviceOperationKindRuntimeInstall = "runtime_install"

	serviceOperationQueued    = "queued"
	serviceOperationRunning   = "running"
	serviceOperationSucceeded = "succeeded"
	serviceOperationFailed    = "failed"

	errCodeServiceOperationBusy = "service_operation_busy"

	maxServiceOperationBody = 64 << 10
)

var errServiceOperationBusy = errors.New("service operation busy")

type serviceInstallRequest struct {
	ServiceID string `json:"service_id"`
	Package   string `json:"package,omitempty"`
}

type serviceOperationResult map[string]any

// serviceOperationFailure keeps the raw cause in memory for server logs only.
// Code and Message are stable, sanitized values safe to persist and return.
type serviceOperationFailure struct {
	Code    string
	Message string
	Cause   error
}

func operationFailure(code, message string, cause error) *serviceOperationFailure {
	return &serviceOperationFailure{Code: code, Message: message, Cause: cause}
}

type serviceOperationError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type serviceOperation struct {
	ID          string                 `json:"id"`
	Kind        string                 `json:"kind"`
	ServiceID   string                 `json:"service_id"`
	PackageName string                 `json:"package_name,omitempty"`
	Status      string                 `json:"status"`
	Phase       string                 `json:"phase"`
	StartedAt   string                 `json:"started_at"`
	FinishedAt  string                 `json:"finished_at,omitempty"`
	Result      json.RawMessage        `json:"result,omitempty"`
	Error       *serviceOperationError `json:"error,omitempty"`
}

type serviceOperationActor struct {
	UserID    int
	IP        string
	UserAgent string
}

type serviceOperationRunner func(context.Context, func(string) error) (serviceOperationResult, *serviceOperationFailure)

func captureServiceOperationActor(r *http.Request) serviceOperationActor {
	actor := serviceOperationActor{
		UserID:    currentUserID(r),
		IP:        clientIP(r),
		UserAgent: r.UserAgent(),
	}
	if len(actor.UserAgent) > 300 {
		actor.UserAgent = actor.UserAgent[:300]
	}
	return actor
}

func decodeServiceOperationJSON(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxServiceOperationBody)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

// handleServiceInstall enqueues the machine mutation and returns immediately.
// The package manager, service configuration, scan and firewall work runs on a
// background context that is deliberately independent from r.Context().
func (p *Panel) handleServiceInstall(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req serviceInstallRequest
	if err := decodeServiceOperationJSON(w, r, &req); err != nil {
		writeClientError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.ServiceID = strings.TrimSpace(req.ServiceID)
	if req.ServiceID == "" {
		writeClientError(w, http.StatusBadRequest, "service_id is required")
		return
	}
	if core.GetManagedServiceByID(req.ServiceID) == nil {
		writeClientError(w, http.StatusBadRequest, "unknown managed service")
		return
	}
	release, busy := p.beginServiceMutation(w, r)
	if busy {
		return
	}
	releaseInHandler := true
	defer func() {
		if releaseInHandler {
			release()
		}
	}()

	actor := captureServiceOperationActor(r)
	op, err := p.createServiceOperation(
		r.Context(), serviceOperationKindInstall, req.ServiceID, req.Package, actor,
	)
	if errors.Is(err, errServiceOperationBusy) {
		writeServiceOperationBusy(w)
		return
	}
	if err != nil {
		writeServerError(w, err)
		return
	}

	p.launchServiceOperation(
		op, actor, "installing",
		"service.install:"+req.ServiceID,
		"service.install.failed:"+req.ServiceID,
		release,
		func(ctx context.Context, advance func(string) error) (serviceOperationResult, *serviceOperationFailure) {
			return p.runServiceInstall(ctx, req, advance)
		},
	)
	releaseInHandler = false
	writeAcceptedServiceOperation(w, op)
}

func writeAcceptedServiceOperation(w http.ResponseWriter, op serviceOperation) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]any{"operation": op})
}

func writeServiceOperationBusy(w http.ResponseWriter) {
	writeCodedError(
		w,
		http.StatusConflict,
		errCodeServiceOperationBusy,
		"another package operation is already in progress",
		"/api/v1/service/operation",
	)
}

// rejectIfServiceOperationBusy gates synchronous package mutations that have
// not yet moved to the durable job runner.
func (p *Panel) beginServiceMutation(w http.ResponseWriter, r *http.Request) (func(), bool) {
	if !p.serviceMutationMu.TryLock() {
		writeServiceOperationBusy(w)
		return nil, true
	}
	release := p.serviceMutationMu.Unlock
	op, err := p.activeServiceOperation(r.Context())
	if err != nil {
		release()
		writeServerError(w, err)
		return nil, true
	}
	if op != nil {
		release()
		writeServiceOperationBusy(w)
		return nil, true
	}
	return release, false
}

func newServiceOperationID() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

func nullablePositiveInt(value int) any {
	if value > 0 {
		return value
	}
	return nil
}

func nullableNonEmpty(value string) any {
	if value != "" {
		return value
	}
	return nil
}

func (p *Panel) createServiceOperation(
	ctx context.Context,
	kind, serviceID, packageName string,
	actor serviceOperationActor,
) (serviceOperation, error) {
	id, err := newServiceOperationID()
	if err != nil {
		return serviceOperation{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	op := serviceOperation{
		ID: id, Kind: kind, ServiceID: serviceID, PackageName: packageName,
		Status: serviceOperationQueued, Phase: "queued", StartedAt: now,
	}
	_, err = p.db.GetDB().ExecContext(ctx, `
		INSERT INTO service_operations (
			id, kind, service_id, package_name, status, phase,
			requested_by, request_ip, user_agent,
			started_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, kind, serviceID, nullableNonEmpty(packageName),
		serviceOperationQueued, "queued",
		nullablePositiveInt(actor.UserID), nullableNonEmpty(actor.IP), nullableNonEmpty(actor.UserAgent),
		now, now, now,
	)
	if err == nil {
		return op, nil
	}
	active, activeErr := p.activeServiceOperation(ctx)
	if activeErr == nil && active != nil {
		return serviceOperation{}, errServiceOperationBusy
	}
	return serviceOperation{}, err
}

func (p *Panel) launchServiceOperation(
	op serviceOperation,
	actor serviceOperationActor,
	initialPhase, successAudit, failureAudit string,
	releaseMutation func(),
	runner serviceOperationRunner,
) {
	go func() {
		defer releaseMutation()
		ctx := context.Background()
		if err := p.markServiceOperationRunning(ctx, op.ID, initialPhase); err != nil {
			log.Printf("service operation %s could not start: %v", op.ID, err)
			failure := operationStartFailure(err)
			if fallbackErr := p.forceFailActiveServiceOperation(ctx, op.ID, "start_failed", failure); fallbackErr != nil {
				log.Printf("service operation %s start failure could not be persisted: %v", op.ID, fallbackErr)
				return
			}
			p.auditServiceOperation(ctx, actor, failureAudit+" — "+failure.Code)
			return
		}
		phase := initialPhase
		advance := func(next string) error {
			if err := p.updateServiceOperationPhase(ctx, op.ID, next); err != nil {
				return err
			}
			phase = next
			return nil
		}

		result, failure := runner(ctx, advance)
		if failure != nil {
			if failure.Cause != nil {
				log.Printf("service operation %s (%s) failed in %s: %v", op.ID, op.ServiceID, phase, failure.Cause)
			}
			if err := p.finishServiceOperationFailed(ctx, op.ID, phase, result, failure); err != nil {
				log.Printf("service operation %s failure could not be persisted: %v", op.ID, err)
				fallback := operationAdvanceFailure(err)
				if fallbackErr := p.forceFailActiveServiceOperation(ctx, op.ID, phase, fallback); fallbackErr != nil {
					log.Printf("service operation %s failure fallback could not be persisted: %v", op.ID, fallbackErr)
					return
				}
			}
			p.auditServiceOperation(ctx, actor, failureAudit+" — "+failure.Code)
			return
		}
		if err := p.finishServiceOperationSucceeded(ctx, op.ID, result); err != nil {
			log.Printf("service operation %s success could not be persisted: %v", op.ID, err)
			fallback := operationAdvanceFailure(err)
			if fallbackErr := p.forceFailActiveServiceOperation(ctx, op.ID, phase, fallback); fallbackErr != nil {
				log.Printf("service operation %s success fallback could not be persisted: %v", op.ID, fallbackErr)
				return
			}
			p.auditServiceOperation(ctx, actor, failureAudit+" — "+fallback.Code)
			return
		}
		p.auditServiceOperation(ctx, actor, successAudit)
	}()
}

func (p *Panel) forceFailActiveServiceOperation(
	ctx context.Context,
	id, phase string,
	failure *serviceOperationFailure,
) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	update, err := p.db.GetDB().ExecContext(ctx, `
		UPDATE service_operations
		SET status=?, phase=?, result_json='{"success":false}',
		    error_code=?, error_message=?, finished_at=?, updated_at=?
		WHERE id=? AND status IN (?, ?)`,
		serviceOperationFailed, phase, failure.Code, failure.Message,
		now, now, id, serviceOperationQueued, serviceOperationRunning,
	)
	if err != nil {
		return err
	}
	affected, err := update.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return errors.New("active operation fallback lost its mutable state")
	}
	return nil
}

func (p *Panel) markServiceOperationRunning(ctx context.Context, id, phase string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := p.db.GetDB().ExecContext(ctx, `
		UPDATE service_operations
		SET status=?, phase=?, updated_at=?
		WHERE id=? AND status=?`,
		serviceOperationRunning, phase, now, id, serviceOperationQueued,
	)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return errors.New("operation is not queued")
	}
	return nil
}

func (p *Panel) updateServiceOperationPhase(ctx context.Context, id, phase string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := p.db.GetDB().ExecContext(ctx, `
		UPDATE service_operations SET phase=?, updated_at=?
		WHERE id=? AND status=?`,
		phase, now, id, serviceOperationRunning,
	)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return errors.New("operation is not running")
	}
	return nil
}

func marshalServiceOperationResult(result serviceOperationResult) (string, error) {
	if result == nil {
		result = serviceOperationResult{"success": false}
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func (p *Panel) finishServiceOperationSucceeded(
	ctx context.Context,
	id string,
	result serviceOperationResult,
) error {
	resultJSON, err := marshalServiceOperationResult(result)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	update, err := p.db.GetDB().ExecContext(ctx, `
		UPDATE service_operations
		SET status=?, phase='completed', result_json=?,
		    error_code=NULL, error_message=NULL,
		    finished_at=?, updated_at=?
		WHERE id=? AND status=?`,
		serviceOperationSucceeded, resultJSON, now, now, id, serviceOperationRunning,
	)
	if err != nil {
		return err
	}
	affected, err := update.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return errors.New("operation completion lost its running state")
	}
	return nil
}

func (p *Panel) finishServiceOperationFailed(
	ctx context.Context,
	id, phase string,
	result serviceOperationResult,
	failure *serviceOperationFailure,
) error {
	resultJSON, err := marshalServiceOperationResult(result)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	update, err := p.db.GetDB().ExecContext(ctx, `
		UPDATE service_operations
		SET status=?, phase=?, result_json=?, error_code=?, error_message=?,
		    finished_at=?, updated_at=?
		WHERE id=? AND status=?`,
		serviceOperationFailed, phase, resultJSON, failure.Code, failure.Message,
		now, now, id, serviceOperationRunning,
	)
	if err != nil {
		return err
	}
	affected, err := update.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return errors.New("operation failure lost its running state")
	}
	return nil
}

type serviceOperationScanner interface {
	Scan(...any) error
}

func scanServiceOperation(scanner serviceOperationScanner) (serviceOperation, error) {
	var op serviceOperation
	var packageName, finishedAt, resultJSON, errorCode, errorMessage sql.NullString
	err := scanner.Scan(
		&op.ID, &op.Kind, &op.ServiceID, &packageName, &op.Status, &op.Phase, &op.StartedAt,
		&finishedAt, &resultJSON, &errorCode, &errorMessage,
	)
	if err != nil {
		return serviceOperation{}, err
	}
	if finishedAt.Valid {
		op.FinishedAt = finishedAt.String
	}
	if packageName.Valid {
		op.PackageName = packageName.String
	}
	if resultJSON.Valid && json.Valid([]byte(resultJSON.String)) {
		op.Result = json.RawMessage(resultJSON.String)
	}
	if errorCode.Valid || errorMessage.Valid {
		op.Error = &serviceOperationError{Code: errorCode.String, Message: errorMessage.String}
	}
	return op, nil
}

const serviceOperationSelect = `
	SELECT id, kind, service_id, package_name, status, phase, started_at,
	       finished_at, result_json, error_code, error_message
	FROM service_operations`

func (p *Panel) serviceOperationByID(ctx context.Context, id string) (serviceOperation, error) {
	return scanServiceOperation(p.db.GetDB().QueryRowContext(
		ctx, serviceOperationSelect+` WHERE id=?`, id,
	))
}

func (p *Panel) activeServiceOperation(ctx context.Context) (*serviceOperation, error) {
	op, err := scanServiceOperation(p.db.GetDB().QueryRowContext(
		ctx,
		serviceOperationSelect+`
		WHERE status IN (?, ?)
		ORDER BY started_at DESC LIMIT 1`,
		serviceOperationQueued, serviceOperationRunning,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &op, nil
}

func (p *Panel) latestServiceOperation(ctx context.Context) (*serviceOperation, error) {
	if active, err := p.activeServiceOperation(ctx); err != nil || active != nil {
		return active, err
	}
	op, err := scanServiceOperation(p.db.GetDB().QueryRowContext(
		ctx, serviceOperationSelect+` ORDER BY started_at DESC LIMIT 1`,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &op, nil
}

func validServiceOperationID(id string) bool {
	if len(id) != 32 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}

func (p *Panel) handleServiceOperation(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id != "" {
		if !validServiceOperationID(id) {
			writeClientError(w, http.StatusNotFound, "service operation not found")
			return
		}
		op, err := p.serviceOperationByID(r.Context(), id)
		if errors.Is(err, sql.ErrNoRows) {
			writeClientError(w, http.StatusNotFound, "service operation not found")
			return
		}
		if err != nil {
			writeServerError(w, err)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"operation": op})
		return
	}
	op, err := p.latestServiceOperation(r.Context())
	if err != nil {
		writeServerError(w, err)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"operation": op})
}

// recoverInterruptedServiceOperations runs once before HTTP routes start.
// A package command may have completed, failed, or still been running when
// the panel process disappeared; without end-state verification success is
// unknowable, so the only honest durable state is failed.
func (p *Panel) recoverInterruptedServiceOperations(ctx context.Context) (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := p.db.GetDB().ExecContext(ctx, `
		UPDATE service_operations
		SET status=?, phase='interrupted',
		    result_json='{"success":false}',
		    error_code='panel_restarted_before_verification',
		    error_message='Panel restarted before the package operation could be verified.',
		    finished_at=?, updated_at=?
		WHERE status IN (?, ?)`,
		serviceOperationFailed, now, now, serviceOperationQueued, serviceOperationRunning,
	)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (p *Panel) auditServiceOperation(ctx context.Context, actor serviceOperationActor, action string) {
	if _, err := p.db.GetDB().ExecContext(ctx, `
		INSERT INTO audit_logs (
			user_id, action, resource_type, resource_id, ip_address, user_agent
		) VALUES (?, ?, 'service', NULL, ?, ?)`,
		nullablePositiveInt(actor.UserID), action,
		nullableNonEmpty(actor.IP), nullableNonEmpty(actor.UserAgent),
	); err != nil {
		log.Printf("audit write failed (%s): %v", action, err)
	}
}

func operationAdvanceFailure(err error) *serviceOperationFailure {
	return operationFailure(
		"operation_state_persist_failed",
		"The package operation state could not be persisted.",
		err,
	)
}

func operationStartFailure(err error) *serviceOperationFailure {
	return operationFailure(
		"operation_start_failed",
		"The package operation could not be started.",
		err,
	)
}

func serviceInstallFailure(cause error) *serviceOperationFailure {
	return operationFailure(
		"service_install_failed",
		"The service could not be installed and verified.",
		cause,
	)
}

func nodeInstallFailure(cause error) *serviceOperationFailure {
	return operationFailure(
		"node_runtime_install_failed",
		"The Node.js runtime could not be installed and verified.",
		cause,
	)
}

func verifyManagedServiceInstalled(services []ManagedServiceResponse, serviceID string) bool {
	for _, service := range services {
		if service.ID == serviceID {
			return service.IsInstalled
		}
	}
	return false
}

func verifyManagedServiceReady(services []ManagedServiceResponse, serviceID string) bool {
	managed := core.GetManagedServiceByID(serviceID)
	if managed == nil {
		return false
	}
	for _, service := range services {
		if service.ID != serviceID || !service.IsInstalled {
			continue
		}
		if managed.Kind != core.KindService {
			return true
		}
		return strings.HasPrefix(strings.ToLower(service.Status), "active")
	}
	return false
}

func verifyNodeVersionInstalled(services []ManagedServiceResponse, version string) bool {
	for _, service := range services {
		if service.ID == "node" {
			return contains(service.Versions, version)
		}
	}
	return false
}

func (p *Panel) runNodeInstall(
	ctx context.Context,
	version string,
	advance func(string) error,
) (serviceOperationResult, *serviceOperationFailure) {
	result := serviceOperationResult{"success": false, "installed": false, "version": version}
	if err := p.preflightManagedServiceInstall(ctx, "node"); err != nil {
		return result, nodeInstallFailure(err)
	}
	var response struct {
		Installed bool   `json:"installed"`
		Error     string `json:"error,omitempty"`
	}
	if err := p.agentClient.CallContext(ctx, "Agent.InstallNodeVersion", &struct {
		Version string `json:"version"`
	}{Version: version}, &response); err != nil {
		return result, nodeInstallFailure(err)
	}
	if response.Error != "" {
		return result, nodeInstallFailure(fmt.Errorf("agent refused Node.js install: %s", response.Error))
	}
	result["installed"] = response.Installed
	if err := advance("scanning"); err != nil {
		return result, operationAdvanceFailure(err)
	}
	services, err := p.scanManagedServices(ctx)
	if err != nil {
		return result, nodeInstallFailure(fmt.Errorf("post-install scan: %w", err))
	}
	if !verifyNodeVersionInstalled(services, version) {
		return result, nodeInstallFailure(errors.New("post-install scan did not find the requested Node.js version"))
	}
	result["success"], result["installed"] = true, true
	return result, nil
}
