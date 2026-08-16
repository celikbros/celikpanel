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
	"sync"
	"time"

	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

const (
	serviceOperationKindInstall        = "service_install"
	serviceOperationKindRuntimeInstall = "runtime_install"

	serviceOperationQueued    = "queued"
	serviceOperationRunning   = "running"
	serviceOperationSucceeded = "succeeded"
	serviceOperationFailed    = "failed"

	errCodeServiceOperationBusy            = "service_operation_busy"
	errCodeServiceOperationRequestConflict = "service_operation_request_conflict"
	errCodeServiceOperationRunnerPanicked  = "service_operation_runner_panicked"
	errCodeServiceOperationLeaseLost       = "service_operation_lease_lost"

	maxServiceOperationBody = 64 << 10
)

var (
	errServiceOperationBusy            = errors.New("service operation busy")
	errServiceOperationRequestConflict = errors.New("service operation request id conflict")
	errServiceOperationReplay          = errors.New("service operation request replay")
)

type serviceInstallRequest struct {
	ServiceID string `json:"service_id"`
	Package   string `json:"package,omitempty"`
	RequestID string `json:"request_id"`
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
	ID            string                 `json:"id"`
	RequestID     string                 `json:"request_id,omitempty"`
	Kind          string                 `json:"kind"`
	ServiceID     string                 `json:"service_id"`
	PackageName   string                 `json:"package_name,omitempty"`
	Status        string                 `json:"status"`
	Phase         string                 `json:"phase"`
	StartedAt     string                 `json:"started_at"`
	FinishedAt    string                 `json:"finished_at,omitempty"`
	Result        json.RawMessage        `json:"result,omitempty"`
	Error         *serviceOperationError `json:"error,omitempty"`
	OperationData string                 `json:"-"`
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
	if !validServiceOperationID(req.RequestID) {
		writeClientError(w, http.StatusBadRequest, "invalid request_id")
		return
	}
	req.ServiceID = strings.TrimSpace(req.ServiceID)
	req.Package = strings.TrimSpace(req.Package)
	if req.ServiceID == "" {
		writeClientError(w, http.StatusBadRequest, "service_id is required")
		return
	}
	if core.GetManagedServiceByID(req.ServiceID) == nil {
		writeClientError(w, http.StatusBadRequest, "unknown managed service")
		return
	}
	existing, found, err := p.idempotentServiceOperation(
		r.Context(), req.RequestID, serviceOperationKindInstall, req.ServiceID, req.Package,
	)
	if err != nil {
		if errors.Is(err, errServiceOperationRequestConflict) {
			writeServiceOperationRequestConflict(w)
			return
		}
		writeServerError(w, err)
		return
	}
	if found {
		writeAcceptedServiceOperation(w, existing)
		return
	}
	if managedDNSEngineServiceID(req.ServiceID) {
		writeDNSEngineWorkflowRequired(w)
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
	op, err := p.createServiceOperationRequest(
		r.Context(), serviceOperationKindInstall, req.ServiceID, req.Package, req.RequestID, actor,
	)
	if errors.Is(err, errServiceOperationBusy) {
		writeServiceOperationBusy(w)
		return
	}
	if errors.Is(err, errServiceOperationReplay) {
		writeAcceptedServiceOperation(w, op)
		return
	}
	if errors.Is(err, errServiceOperationRequestConflict) {
		writeServiceOperationRequestConflict(w)
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

func writeServiceOperationRequestConflict(w http.ResponseWriter) {
	writeCodedError(
		w,
		http.StatusConflict,
		errCodeServiceOperationRequestConflict,
		"request_id already belongs to another package operation",
		"",
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
	statusCtx, statusCancel := context.WithTimeout(r.Context(), panelMutationFinishTimeout)
	agentJob, statusErr := p.statusAgentMutation(statusCtx, "")
	statusCancel()
	if statusErr != nil {
		release()
		writeServerError(w, fmt.Errorf("verify privileged service mutation admission: %w", statusErr))
		return nil, true
	}
	if agentJob != nil && agentMutationActive(agentJob.Status) {
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
	return p.createServiceOperationRequest(ctx, kind, serviceID, packageName, "", actor)
}

func (p *Panel) createServiceOperationRequest(
	ctx context.Context,
	kind, serviceID, packageName, requestID string,
	actor serviceOperationActor,
) (serviceOperation, error) {
	return p.createServiceOperationRequestWithState(
		ctx, kind, serviceID, packageName, requestID, actor, "queued", "",
	)
}

func (p *Panel) createServiceOperationRequestWithState(
	ctx context.Context,
	kind, serviceID, packageName, requestID string,
	actor serviceOperationActor,
	initialPhase, operationData string,
) (serviceOperation, error) {
	id, err := newServiceOperationID()
	if err != nil {
		return serviceOperation{}, err
	}
	if requestID == "" {
		// Older API clients do not know a pre-request id. Giving their operation
		// a server-generated identity preserves the schema invariant, although
		// only new clients can recover a lost POST response by that identity.
		// Eski API istemcileri istek öncesi kimliği bilmez. Sunucunun ürettiği
		// kimlik şema değişmezini korur; kayıp POST yanıtını bu kimlikle yalnız
		// yeni istemciler kurtarabilir.
		requestID = id
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	op := serviceOperation{
		ID: id, RequestID: requestID, Kind: kind, ServiceID: serviceID, PackageName: packageName,
		Status: serviceOperationQueued, Phase: initialPhase, StartedAt: now,
		OperationData: operationData,
	}
	_, err = p.db.GetDB().ExecContext(ctx, `
		INSERT INTO service_operations (
			id, request_id, kind, service_id, package_name, status, phase,
			operation_data,
			requested_by, request_ip, user_agent,
			started_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, requestID, kind, serviceID, nullableNonEmpty(packageName),
		serviceOperationQueued, initialPhase, nullableNonEmpty(operationData),
		nullablePositiveInt(actor.UserID), nullableNonEmpty(actor.IP), nullableNonEmpty(actor.UserAgent),
		now, now, now,
	)
	if err == nil {
		return op, nil
	}
	if requestID != "" {
		existing, found, lookupErr := p.idempotentServiceOperation(
			ctx, requestID, kind, serviceID, packageName,
		)
		if lookupErr != nil {
			return serviceOperation{}, lookupErr
		}
		if found {
			return existing, errServiceOperationReplay
		}
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
	p.launchServiceOperationWithAudit(
		op, actor, initialPhase, successAudit, failureAudit,
		releaseMutation, runner, p.auditServiceOperation,
	)
}

type serviceOperationAuditWriter func(context.Context, serviceOperationActor, string)

// launchServiceOperationWithAudit owns the mutation lock until a terminal row
// is durably persisted. The injected writer keeps that ordering testable even
// when an audit backend becomes slow.
// launchServiceOperationWithAudit, terminal satır kalıcı olarak yazılana dek
// değişiklik kilidini tutar. Enjekte edilen yazıcı, denetim altyapısı yavaşlasa
// bile bu sıralamanın test edilebilmesini sağlar.
func (p *Panel) launchServiceOperationWithAudit(
	op serviceOperation,
	actor serviceOperationActor,
	initialPhase, successAudit, failureAudit string,
	releaseMutation func(),
	runner serviceOperationRunner,
	auditWriter serviceOperationAuditWriter,
) {
	p.launchServiceOperationWithAuditMode(
		op, actor, initialPhase, successAudit, failureAudit,
		releaseMutation, runner, auditWriter, false, false,
	)
}

func wireGuardInstallNeedsPeerSync(op serviceOperation) bool {
	return op.Kind == serviceOperationKindInstall && op.ServiceID == "wireguard"
}

func powerDNSInstallNeedsZoneSync(op serviceOperation) bool {
	return op.Kind == serviceOperationKindInstall && op.ServiceID == "pdns"
}

const firewallChildPhasePrefix = "firewall-child/v1|"
const mailTLSChildPhasePrefix = "mail-tls-child/v1|"

type firewallChildIdentity struct {
	RequestID string
	OwnerID   string
	Qualifier string
}

type mailTLSChildIdentity struct {
	RequestID string
	OwnerID   string
	Qualifier string
}

func mailProfileNeedsMailTLSSync(op serviceOperation) bool {
	return op.Kind == serviceOperationKindMailProfileInstall
}

func encodeMailTLSChildPhase(identity mailTLSChildIdentity) (string, error) {
	if !validServiceOperationID(identity.RequestID) ||
		!validServiceOperationID(identity.OwnerID) ||
		!mutationpayload.ValidMailTLSSyncQualifier(identity.Qualifier) {
		return "", errors.New("invalid mail TLS child identity")
	}
	return mailTLSChildPhasePrefix + identity.RequestID + "|" +
		identity.OwnerID + "|" + identity.Qualifier, nil
}

func parseMailTLSChildPhase(phase string) (mailTLSChildIdentity, bool) {
	if !strings.HasPrefix(phase, mailTLSChildPhasePrefix) {
		return mailTLSChildIdentity{}, false
	}
	parts := strings.Split(strings.TrimPrefix(phase, mailTLSChildPhasePrefix), "|")
	if len(parts) != 3 {
		return mailTLSChildIdentity{}, false
	}
	identity := mailTLSChildIdentity{
		RequestID: parts[0], OwnerID: parts[1], Qualifier: parts[2],
	}
	if !validServiceOperationID(identity.RequestID) ||
		!validServiceOperationID(identity.OwnerID) ||
		!mutationpayload.ValidMailTLSSyncQualifier(identity.Qualifier) {
		return mailTLSChildIdentity{}, false
	}
	return identity, true
}

func serviceOperationNeedsFirewallSync(op serviceOperation) bool {
	return op.Kind == serviceOperationKindInstall ||
		op.Kind == serviceOperationKindMailProfileInstall
}

func serviceOperationFirewallPhase(op serviceOperation) string {
	if op.Kind == serviceOperationKindMailProfileInstall {
		return mailProfilePhase(op.ServiceID, "firewall")
	}
	return "firewall"
}

func encodeFirewallChildPhase(identity firewallChildIdentity) (string, error) {
	if !validServiceOperationID(identity.RequestID) ||
		!validServiceOperationID(identity.OwnerID) ||
		!mutationpayload.ValidFirewallApplyQualifier(identity.Qualifier) {
		return "", errors.New("invalid firewall child identity")
	}
	return firewallChildPhasePrefix + identity.RequestID + "|" +
		identity.OwnerID + "|" + identity.Qualifier, nil
}

func parseFirewallChildPhase(phase string) (firewallChildIdentity, bool) {
	if !strings.HasPrefix(phase, firewallChildPhasePrefix) {
		return firewallChildIdentity{}, false
	}
	parts := strings.Split(strings.TrimPrefix(phase, firewallChildPhasePrefix), "|")
	if len(parts) != 3 {
		return firewallChildIdentity{}, false
	}
	identity := firewallChildIdentity{
		RequestID: parts[0],
		OwnerID:   parts[1],
		Qualifier: parts[2],
	}
	if !validServiceOperationID(identity.RequestID) ||
		!validServiceOperationID(identity.OwnerID) ||
		!mutationpayload.ValidFirewallApplyQualifier(identity.Qualifier) {
		return firewallChildIdentity{}, false
	}
	return identity, true
}

// syncFirewallAfterOperation opens the direct firewall child only after the
// outer agent job is exact terminal success. The process mutation lock remains
// held by launchServiceOperationWithAuditMode through this call and panel-row
// terminalization.
func (p *Panel) syncFirewallAfterOperation(op serviceOperation) *serviceOperationFailure {
	if !serviceOperationNeedsFirewallSync(op) {
		return nil
	}
	syncCtx, cancelSync := context.WithTimeout(
		context.Background(),
		panelMutationRecoveryTimeout,
	)
	defer cancelSync()
	if err := p.syncFirewallForServiceOperation(syncCtx, op); err != nil {
		return firewallSyncFailure(err)
	}
	return nil
}

// syncMailTLSAfterOperation runs only after the mail-profile agent job is
// exact terminal success. serviceMutationMu remains held by the launcher while
// the helper takes mailTLSSyncMu and opens a fresh direct payload-bound lease.
func (p *Panel) syncMailTLSAfterOperation(
	op serviceOperation,
	result serviceOperationResult,
) *serviceOperationFailure {
	if !mailProfileNeedsMailTLSSync(op) {
		return nil
	}
	syncCtx, cancelSync := context.WithTimeout(
		context.Background(), panelMailTLSSyncTimeout,
	)
	defer cancelSync()
	if err := p.authorizeAgentRPCContext(syncCtx, "Agent.SyncMailTLSV2"); err != nil {
		return mailProfileInstallFailure(err)
	}
	if err := p.requireMailTLSSyncV2Agent(syncCtx); err != nil {
		return mailProfileInstallFailure(err)
	}
	mailTLSSyncMu.Lock()
	defer mailTLSSyncMu.Unlock()
	host, sni, err := p.loadMailTLSSnapshotLocked(syncCtx, 0)
	if err != nil {
		return mailProfileInstallFailure(fmt.Errorf("build mail TLS snapshot: %w", err))
	}
	commitment, err := mutationpayload.CanonicalMailTLSSync(
		panelMailTLSManagedRoot, host, sni,
	)
	if err != nil {
		return mailProfileInstallFailure(fmt.Errorf("canonicalize mail TLS snapshot: %w", err))
	}
	requestID, err := newServiceOperationID()
	if err != nil {
		return operationStartFailure(err)
	}
	ownerID, err := newServiceOperationID()
	if err != nil {
		return operationStartFailure(err)
	}
	phase, err := encodeMailTLSChildPhase(mailTLSChildIdentity{
		RequestID: requestID, OwnerID: ownerID, Qualifier: commitment.Qualifier,
	})
	if err != nil {
		return operationStartFailure(err)
	}
	if err := p.updateServiceOperationPhase(syncCtx, op.ID, phase); err != nil {
		return operationAdvanceFailure(err)
	}
	response, err := p.applyCanonicalMailTLSV2Identity(
		syncCtx, commitment, requestID, ownerID,
	)
	if err != nil {
		return mailProfileInstallFailure(fmt.Errorf("mail TLS synchronization: %w", err))
	}
	tlsResult := mailProfileTLSResult{
		Configured: true, SNICount: response.SNICount,
		FallbackOnly: response.SNICount == 0,
	}
	result["mail_tls"] = tlsResult
	if tlsResult.FallbackOnly {
		result["warnings"] = []string{mailProfileFallbackWarning}
	}
	return nil
}

// syncWireGuardPeersAfterInstall opens a fresh, unbound durable mutation only
// after the outer service_install job is terminal. The process mutation lock
// remains owned by the caller until this step and the panel row are terminal.
func (p *Panel) syncWireGuardPeersAfterInstall(
	op serviceOperation,
) *serviceOperationFailure {
	if !wireGuardInstallNeedsPeerSync(op) {
		return nil
	}
	syncCtx, cancelSync := context.WithTimeout(
		context.Background(),
		panelMutationRecoveryTimeout,
	)
	defer cancelSync()
	if err := p.updateServiceOperationPhase(syncCtx, op.ID, "syncing"); err != nil {
		return operationAdvanceFailure(err)
	}
	if err := p.syncVPNPeers(syncCtx); err != nil {
		return serviceInstallFailure(fmt.Errorf(
			"WireGuard peer synchronization: %w",
			err,
		))
	}
	return nil
}

// syncPowerDNSZonesAfterInstall opens fresh direct V2 children only after the
// outer pdns install is exact terminal success. The caller still owns
// serviceMutationMu, so this helper takes only dnsPublicationMu.
func (p *Panel) syncPowerDNSZonesAfterInstall(
	op serviceOperation,
) *serviceOperationFailure {
	syncCtx, cancelSync := dnsZoneBatchContext(context.Background())
	defer cancelSync()
	return p.syncPowerDNSZonesAfterInstallContext(syncCtx, op)
}

func (p *Panel) syncPowerDNSZonesAfterInstallContext(
	syncCtx context.Context,
	op serviceOperation,
) *serviceOperationFailure {
	if !powerDNSInstallNeedsZoneSync(op) {
		return nil
	}
	if err := p.updateServiceOperationPhase(syncCtx, op.ID, "syncing_dns"); err != nil {
		return operationAdvanceFailure(err)
	}
	dnsPublicationMu.Lock()
	result, err := p.syncAllZonesLocked(syncCtx)
	dnsPublicationMu.Unlock()
	if err != nil {
		return serviceInstallFailure(fmt.Errorf(
			"PowerDNS V2 zone synchronization (%d/%d applied): %w",
			result.Synced,
			result.Attempted,
			err,
		))
	}
	return nil
}

func (p *Panel) launchServiceOperationWithAuditMode(
	op serviceOperation,
	actor serviceOperationActor,
	initialPhase, successAudit, failureAudit string,
	releaseMutation func(),
	runner serviceOperationRunner,
	auditWriter serviceOperationAuditWriter,
	resumeAgent bool,
	panelAlreadyRunning bool,
) {
	go func() {
		var releaseOnce sync.Once
		release := func() { releaseOnce.Do(releaseMutation) }
		defer release()
		ownerID, err := newServiceOperationID()
		if err != nil {
			failure := operationStartFailure(err)
			if fallbackErr := p.forceFailActiveServiceOperation(
				context.Background(), op.ID, "lease_failed", failure,
			); fallbackErr != nil {
				log.Printf("service operation %s lease identity failure could not be persisted: %v", op.ID, fallbackErr)
			}
			return
		}
		beginCtx, beginCancel := context.WithTimeout(context.Background(), panelMutationFinishTimeout)
		agentJob, err := p.beginAgentMutation(beginCtx, op, ownerID, resumeAgent)
		beginCancel()
		if err != nil {
			log.Printf("service operation %s could not acquire the agent lease: %v", op.ID, err)
			failure := operationStartFailure(err)
			if fallbackErr := p.forceFailActiveServiceOperation(
				context.Background(), op.ID, "lease_failed", failure,
			); fallbackErr != nil {
				log.Printf("service operation %s lease failure could not be persisted: %v", op.ID, fallbackErr)
				return
			}
			auditWriter(context.Background(), actor, failureAudit+" — "+failure.Code)
			return
		}
		binding := agentMutationBinding{
			MutationRequestID: op.RequestID,
			MutationOwnerID:   ownerID,
		}
		identity := agentMutationIdentityForOperation(op, ownerID)
		deadline := agentJob.DeadlineAt
		if deadline.IsZero() {
			deadline = time.Now().Add(45 * time.Minute)
		}
		workerBase, cancelWorker := context.WithDeadline(context.Background(), deadline)
		ctx := withPanelMutationBinding(workerBase, binding)
		stopHeartbeat := p.startAgentMutationHeartbeat(workerBase, cancelWorker, binding)
		defer func() {
			_ = stopHeartbeat()
			cancelWorker()
		}()
		startErr := error(nil)
		if panelAlreadyRunning {
			startErr = p.updateServiceOperationPhase(ctx, op.ID, initialPhase)
		} else {
			startErr = p.markServiceOperationRunning(ctx, op.ID, initialPhase)
		}
		if startErr != nil {
			log.Printf("service operation %s could not start: %v", op.ID, startErr)
			failure := operationStartFailure(startErr)
			if _, finishErr := p.finishAgentMutation(binding, false, failure); finishErr != nil {
				log.Printf("service operation %s agent start failure could not be finalized: %v", op.ID, finishErr)
				return
			}
			terminalCtx, cancelTerminal := context.WithTimeout(context.Background(), panelMutationFinishTimeout)
			defer cancelTerminal()
			if fallbackErr := p.forceFailActiveServiceOperation(terminalCtx, op.ID, "start_failed", failure); fallbackErr != nil {
				log.Printf("service operation %s start failure could not be persisted: %v", op.ID, fallbackErr)
				return
			}
			release()
			auditWriter(terminalCtx, actor, failureAudit+" — "+failure.Code)
			return
		}
		phase := initialPhase
		advance := func(next string) error {
			if err := p.updateServiceOperationPhase(ctx, op.ID, next); err != nil {
				return err
			}
			pingCtx, pingCancel := context.WithTimeout(context.Background(), panelMutationHeartbeatInterval)
			_, heartbeatErr := p.heartbeatAgentMutation(pingCtx, binding, next)
			pingCancel()
			if heartbeatErr != nil {
				cancelWorker()
				return fmt.Errorf("agent service mutation heartbeat: %w", heartbeatErr)
			}
			phase = next
			return nil
		}

		result, failure := runServiceOperationRunner(ctx, advance, runner)
		if heartbeatErr := stopHeartbeat(); heartbeatErr != nil && failure == nil {
			failure = operationFailure(
				errCodeServiceOperationLeaseLost,
				"The package operation lost its privileged agent lease.",
				heartbeatErr,
			)
		}
		agentTerminal, finishErr := p.finishExpectedAgentMutation(
			binding,
			identity,
			failure == nil,
			failure,
		)
		if finishErr != nil {
			log.Printf("service operation %s agent terminal state could not be persisted: %v", op.ID, finishErr)
			return
		}
		if failure == nil && agentTerminal.Status != agentMutationSucceeded {
			failure = operationFailure(
				errCodeServiceOperationLeaseLost,
				"The privileged agent did not commit the package operation as successful.",
				fmt.Errorf("agent terminal status is %s", agentTerminal.Status),
			)
		}
		if failure == nil && serviceOperationNeedsFirewallSync(op) {
			phase = serviceOperationFirewallPhase(op)
			if syncFailure := p.syncFirewallAfterOperation(op); syncFailure != nil {
				if result == nil {
					result = serviceOperationResult{}
				}
				result["success"] = false
				failure = syncFailure
			}
		}
		if failure == nil && mailProfileNeedsMailTLSSync(op) {
			phase = mailProfilePhase(op.ServiceID, "mail-tls")
			if result == nil {
				result = serviceOperationResult{}
			}
			if syncFailure := p.syncMailTLSAfterOperation(op, result); syncFailure != nil {
				result["success"] = false
				failure = syncFailure
			}
		}
		if failure == nil && powerDNSInstallNeedsZoneSync(op) {
			phase = "syncing_dns"
			if syncFailure := p.syncPowerDNSZonesAfterInstall(op); syncFailure != nil {
				if result == nil {
					result = serviceOperationResult{}
				}
				result["success"] = false
				failure = syncFailure
			}
		}
		if failure == nil && wireGuardInstallNeedsPeerSync(op) {
			phase = "syncing"
			if syncFailure := p.syncWireGuardPeersAfterInstall(op); syncFailure != nil {
				if result == nil {
					result = serviceOperationResult{}
				}
				result["success"] = false
				failure = syncFailure
			}
		}
		terminalCtx, cancelTerminal := context.WithTimeout(context.Background(), panelMutationFinishTimeout)
		defer cancelTerminal()
		if failure != nil {
			if failure.Cause != nil {
				log.Printf("service operation %s (%s) failed in %s: %v", op.ID, op.ServiceID, phase, failure.Cause)
			}
			if err := p.finishServiceOperationFailed(terminalCtx, op.ID, phase, result, failure); err != nil {
				log.Printf("service operation %s failure could not be persisted: %v", op.ID, err)
				fallback := operationAdvanceFailure(err)
				if fallbackErr := p.forceFailActiveServiceOperation(terminalCtx, op.ID, phase, fallback); fallbackErr != nil {
					log.Printf("service operation %s failure fallback could not be persisted: %v", op.ID, fallbackErr)
					return
				}
			}
			release()
			auditWriter(terminalCtx, actor, failureAudit+" — "+failure.Code)
			return
		}
		if err := p.finishServiceOperationSucceeded(terminalCtx, op.ID, result); err != nil {
			log.Printf("service operation %s success could not be persisted: %v", op.ID, err)
			fallback := operationAdvanceFailure(err)
			if fallbackErr := p.forceFailActiveServiceOperation(terminalCtx, op.ID, phase, fallback); fallbackErr != nil {
				log.Printf("service operation %s success fallback could not be persisted: %v", op.ID, fallbackErr)
				return
			}
			release()
			auditWriter(terminalCtx, actor, failureAudit+" — "+fallback.Code)
			return
		}
		release()
		auditWriter(terminalCtx, actor, successAudit)
	}()
}

// runServiceOperationRunner converts a runner panic into the same sanitized
// failure contract as an ordinary runner error. The caller then CAS-transitions
// the active row before releasing the process-local mutation lock.
// runServiceOperationRunner, runner panic'ini sıradan runner hatasıyla aynı
// temizlenmiş hata sözleşmesine dönüştürür. Çağıran, süreç içi değişiklik
// kilidini bırakmadan önce etkin satırı CAS ile terminal duruma geçirir.
func runServiceOperationRunner(
	ctx context.Context,
	advance func(string) error,
	runner serviceOperationRunner,
) (result serviceOperationResult, failure *serviceOperationFailure) {
	defer func() {
		if recovered := recover(); recovered != nil {
			result = serviceOperationResult{"success": false}
			failure = operationFailure(
				errCodeServiceOperationRunnerPanicked,
				"The package operation stopped unexpectedly.",
				fmt.Errorf("service operation runner panic: %v", recovered),
			)
		}
	}()
	return runner(ctx, advance)
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
	var requestID, packageName, finishedAt, resultJSON, errorCode, errorMessage, operationData sql.NullString
	err := scanner.Scan(
		&op.ID, &requestID, &op.Kind, &op.ServiceID, &packageName, &op.Status, &op.Phase, &op.StartedAt,
		&finishedAt, &resultJSON, &errorCode, &errorMessage, &operationData,
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
	if requestID.Valid {
		op.RequestID = requestID.String
	}
	if resultJSON.Valid && json.Valid([]byte(resultJSON.String)) {
		op.Result = json.RawMessage(resultJSON.String)
	}
	if errorCode.Valid || errorMessage.Valid {
		op.Error = &serviceOperationError{Code: errorCode.String, Message: errorMessage.String}
	}
	if operationData.Valid {
		op.OperationData = operationData.String
	}
	return op, nil
}

const serviceOperationSelect = `
	SELECT id, request_id, kind, service_id, package_name, status, phase, started_at,
	       finished_at, result_json, error_code, error_message, operation_data
	FROM service_operations`

func (p *Panel) serviceOperationByID(ctx context.Context, id string) (serviceOperation, error) {
	return scanServiceOperation(p.db.GetDB().QueryRowContext(
		ctx, serviceOperationSelect+` WHERE id=?`, id,
	))
}

func (p *Panel) serviceOperationByRequestID(ctx context.Context, requestID string) (serviceOperation, error) {
	return scanServiceOperation(p.db.GetDB().QueryRowContext(
		ctx, serviceOperationSelect+` WHERE request_id=?`, requestID,
	))
}

// idempotentServiceOperation returns the prior operation only when both the
// opaque request id and its immutable target agree. Reusing one id for another
// mutation is a conflict and is never treated as success.
// idempotentServiceOperation, önceki işlemi yalnız opak istek kimliği ile
// değişmez hedefi birlikte eşleştiğinde döndürür. Bir kimliği başka değişiklik
// için yeniden kullanmak çakışmadır ve asla başarı sayılmaz.
func (p *Panel) idempotentServiceOperation(
	ctx context.Context,
	requestID, kind, serviceID, packageName string,
) (serviceOperation, bool, error) {
	op, err := p.serviceOperationByRequestID(ctx, requestID)
	if errors.Is(err, sql.ErrNoRows) {
		return serviceOperation{}, false, nil
	}
	if err != nil {
		return serviceOperation{}, false, err
	}
	if op.Kind != kind || op.ServiceID != serviceID || op.PackageName != packageName {
		return serviceOperation{}, false, errServiceOperationRequestConflict
	}
	return op, true, nil
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
	if strings.ToLower(id) != id {
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
	id := r.URL.Query().Get("id")
	requestID := r.URL.Query().Get("request_id")
	activeValues, activePresent := r.URL.Query()["active"]
	if activePresent && (len(activeValues) != 1 || activeValues[0] != "1") {
		writeClientError(w, http.StatusBadRequest, "active must be 1")
		return
	}
	selectorCount := 0
	for _, selected := range []bool{id != "", requestID != "", activePresent} {
		if selected {
			selectorCount++
		}
	}
	if selectorCount > 1 {
		writeClientError(w, http.StatusBadRequest, "choose id, request_id or active")
		return
	}
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
	if requestID != "" {
		if !validServiceOperationID(requestID) {
			writeClientError(w, http.StatusNotFound, "service operation not found")
			return
		}
		op, err := p.serviceOperationByRequestID(r.Context(), requestID)
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
	if activePresent {
		op, err := p.activeServiceOperation(r.Context())
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

func validDirectVPNPeerSync(job *agentMutationJob) bool {
	return job != nil && agentMutationActive(job.Status) &&
		validServiceOperationID(job.RequestID) &&
		validServiceOperationID(job.OwnerID) &&
		job.Kind == "vpn_peer_sync" &&
		job.Target == "wireguard" &&
		mutationpayload.ValidVPNPeerSyncQualifier(job.PackageName)
}

func validDirectFirewallMutation(job *agentMutationJob) bool {
	return job != nil && agentMutationActive(job.Status) &&
		validServiceOperationID(job.RequestID) &&
		validServiceOperationID(job.OwnerID) &&
		(job.Kind == "firewall_apply" || job.Kind == "firewall_sync") &&
		job.Target == "nftables" &&
		mutationpayload.ValidFirewallApplyQualifier(job.PackageName)
}

func validDirectMailTLSSync(job *agentMutationJob) bool {
	return job != nil && agentMutationActive(job.Status) &&
		validServiceOperationID(job.RequestID) &&
		validServiceOperationID(job.OwnerID) &&
		job.Kind == "mail_tls_sync" &&
		job.Target == "mail-tls" &&
		mutationpayload.ValidMailTLSSyncQualifier(job.PackageName)
}

func recoverableFirewallChild(
	op *serviceOperation,
	job *agentMutationJob,
) bool {
	if op == nil || !serviceOperationNeedsFirewallSync(*op) ||
		!validDirectFirewallMutation(job) || job.Kind != "firewall_sync" {
		return false
	}
	persisted, ok := parseFirewallChildPhase(op.Phase)
	return ok && job.RequestID == persisted.RequestID &&
		job.OwnerID == persisted.OwnerID &&
		job.PackageName == persisted.Qualifier
}

func recoverableMailTLSChild(
	op *serviceOperation,
	job *agentMutationJob,
) bool {
	if op == nil || !mailProfileNeedsMailTLSSync(*op) ||
		!validDirectMailTLSSync(job) {
		return false
	}
	persisted, ok := parseMailTLSChildPhase(op.Phase)
	return ok && job.RequestID == persisted.RequestID &&
		job.OwnerID == persisted.OwnerID &&
		job.PackageName == persisted.Qualifier
}

func (p *Panel) persistedMailTLSChildSucceeded(
	ctx context.Context,
	op serviceOperation,
) (bool, error) {
	identity, ok := parseMailTLSChildPhase(op.Phase)
	if !ok {
		return false, nil
	}
	job, err := p.statusAgentMutation(ctx, identity.RequestID)
	if err != nil {
		return false, err
	}
	if job == nil {
		// The child identity is persisted before BeginServiceMutation. A crash in
		// that narrow window is safe to retry because no agent-owned work exists.
		return false, nil
	}
	if job.RequestID != identity.RequestID || job.OwnerID != identity.OwnerID ||
		job.Kind != "mail_tls_sync" || job.Target != "mail-tls" ||
		job.PackageName != identity.Qualifier {
		return false, errors.New("persisted mail TLS child identity disagrees with the agent ledger")
	}
	if job.Status != agentMutationSucceeded {
		return false, nil
	}
	mutationIdentity := agentMutationIdentity{
		RequestID: identity.RequestID, OwnerID: identity.OwnerID,
		Kind: "mail_tls_sync", Target: "mail-tls",
		PackageName: identity.Qualifier,
	}
	if err := validateAgentMutationSucceededReceipt(job, mutationIdentity); err != nil {
		return false, err
	}
	return true, nil
}

func recoverableWireGuardPeerSync(
	op *serviceOperation,
	job *agentMutationJob,
) bool {
	return op != nil && wireGuardInstallNeedsPeerSync(*op) &&
		validDirectVPNPeerSync(job)
}

func (p *Panel) recoverablePowerDNSZoneSync(
	ctx context.Context,
	op *serviceOperation,
	job *agentMutationJob,
) bool {
	if op == nil || !powerDNSInstallNeedsZoneSync(*op) || !validDirectDNSZoneSync(job) {
		return false
	}
	_, err := p.exactDNSZoneLeaseForJob(ctx, job)
	return err == nil
}

func agentMutationJobIdentity(job *agentMutationJob) agentMutationIdentity {
	if job == nil {
		return agentMutationIdentity{}
	}
	return agentMutationIdentity{
		RequestID:   job.RequestID,
		OwnerID:     job.OwnerID,
		Kind:        job.Kind,
		Target:      job.Target,
		PackageName: job.PackageName,
	}
}

// waitIndependentPanelCertificateActivations consumes only the exact,
// agent-owned activation that can restart this process while verifying the
// newly published TLS leaf. The listener is already serving behind the startup
// HTTP gate when this runs, so the activation can finish without being
// cancelled and without admitting application traffic. Reloading the global
// slot after every terminal receipt also handles a queued activation retry.
func (p *Panel) waitIndependentPanelCertificateActivations(
	ctx context.Context,
	observed *agentMutationJob,
) (*agentMutationJob, error) {
	current := observed
	for current != nil && agentMutationActive(current.Status) &&
		current.Kind == "panel-certificate-activation" {
		if !validIndependentPanelCertificateActivation(current) {
			return current, errors.New(
				"active panel certificate activation has an invalid durable identity",
			)
		}
		identity := agentMutationJobIdentity(current)
		terminal, err := p.waitExpectedAgentMutationTerminal(ctx, identity)
		if err != nil {
			return terminal, fmt.Errorf(
				"wait for independent panel certificate activation: %w",
				err,
			)
		}
		if !identity.matches(terminal) ||
			(terminal.Status != agentMutationSucceeded &&
				terminal.Status != agentMutationFailed) {
			return terminal, errors.New(
				"independent panel certificate activation did not return an exact terminal receipt",
			)
		}
		current, err = p.statusAgentMutation(ctx, "")
		if err != nil {
			return current, fmt.Errorf(
				"reload privileged mutation ledger after panel certificate activation: %w",
				err,
			)
		}
	}
	return current, nil
}

func (p *Panel) refreshStartupGlobalMutation(
	ctx context.Context,
) (*agentMutationJob, error) {
	job, err := p.statusAgentMutation(ctx, "")
	if err != nil {
		return job, fmt.Errorf("read privileged mutation ledger during startup: %w", err)
	}
	return p.waitIndependentPanelCertificateActivations(ctx, job)
}

func (p *Panel) requireStartupAgentMutationSlot(ctx context.Context) error {
	job, err := p.refreshStartupGlobalMutation(ctx)
	if err != nil {
		return err
	}
	if job != nil && agentMutationActive(job.Status) {
		return fmt.Errorf(
			"agent mutation %s acquired the host lease during startup recovery",
			job.RequestID,
		)
	}
	return nil
}

func (p *Panel) terminalizeInterruptedWireGuardPeerSync(
	ctx context.Context,
	job *agentMutationJob,
) error {
	identity := agentMutationJobIdentity(job)
	if !validDirectVPNPeerSync(job) || !identity.matches(job) {
		return errors.New("interrupted WireGuard peer sync has an invalid durable identity")
	}
	current := job
	if current.Status == agentMutationRunning {
		cancelErr := p.cancelAgentMutation(
			ctx,
			current,
			"panel_restarted_during_vpn_peer_sync",
			"Panel restarted before WireGuard peer synchronization was reconciled.",
		)
		if cancelErr != nil {
			observed, statusErr := p.statusAgentMutation(ctx, identity.RequestID)
			if statusErr != nil {
				return errors.Join(
					fmt.Errorf("cancel interrupted WireGuard peer sync: %w", cancelErr),
					fmt.Errorf("verify interrupted WireGuard peer sync: %w", statusErr),
				)
			}
			if err := validateAgentMutationIdentity(observed, identity); err != nil {
				return err
			}
			current = observed
			if current.Status == agentMutationRunning {
				return fmt.Errorf("cancel interrupted WireGuard peer sync: %w", cancelErr)
			}
		}
	}
	if agentMutationActive(current.Status) {
		terminal, err := p.waitAgentMutationTerminal(ctx, identity.RequestID)
		if err != nil {
			return fmt.Errorf("wait for interrupted WireGuard peer sync: %w", err)
		}
		if err := validateAgentMutationIdentity(terminal, identity); err != nil {
			return err
		}
		current = terminal
	}
	if current == nil || agentMutationActive(current.Status) {
		return errors.New("interrupted WireGuard peer sync did not reach a terminal state")
	}
	return nil
}

func (p *Panel) terminalizeInterruptedFirewallMutation(
	ctx context.Context,
	job *agentMutationJob,
) error {
	identity := agentMutationJobIdentity(job)
	if !validDirectFirewallMutation(job) || !identity.matches(job) {
		return errors.New("interrupted direct firewall mutation has an invalid durable identity")
	}
	current := job
	if current.Status == agentMutationRunning {
		cancelErr := p.cancelAgentMutation(
			ctx,
			current,
			"panel_restarted_during_firewall_mutation",
			"Panel restarted before direct firewall synchronization was reconciled.",
		)
		if cancelErr != nil {
			observed, statusErr := p.statusAgentMutation(ctx, identity.RequestID)
			if statusErr != nil {
				return errors.Join(
					fmt.Errorf("cancel interrupted firewall mutation: %w", cancelErr),
					fmt.Errorf("verify interrupted firewall mutation: %w", statusErr),
				)
			}
			if err := validateAgentMutationIdentity(observed, identity); err != nil {
				return err
			}
			current = observed
			if current.Status == agentMutationRunning {
				return fmt.Errorf("cancel interrupted firewall mutation: %w", cancelErr)
			}
		}
	}
	if agentMutationActive(current.Status) {
		terminal, err := p.waitAgentMutationTerminal(ctx, identity.RequestID)
		if err != nil {
			return fmt.Errorf("wait for interrupted firewall mutation: %w", err)
		}
		if err := validateAgentMutationIdentity(terminal, identity); err != nil {
			return err
		}
		current = terminal
	}
	if current == nil || agentMutationActive(current.Status) {
		return errors.New("interrupted firewall mutation did not reach a terminal state")
	}
	return nil
}

func (p *Panel) terminalizeInterruptedMailTLSSync(
	ctx context.Context,
	job *agentMutationJob,
) (*agentMutationJob, error) {
	identity := agentMutationJobIdentity(job)
	if !validDirectMailTLSSync(job) || !identity.matches(job) {
		return job, errors.New("interrupted direct mail TLS sync has an invalid durable identity")
	}
	current := job
	if current.Status == agentMutationRunning {
		cancelErr := p.cancelAgentMutation(
			ctx,
			current,
			"panel_restarted_during_mail_tls_sync",
			"The panel restarted before direct mail TLS synchronization was reconciled.",
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
			if agentMutationActive(current.Status) && current.Status != agentMutationCancelling {
				return current, cancelErr
			}
		}
	}
	if agentMutationActive(current.Status) {
		terminal, err := p.waitAgentMutationTerminal(ctx, identity.RequestID)
		if err != nil {
			return terminal, fmt.Errorf("wait for interrupted mail TLS sync: %w", err)
		}
		if err := validateAgentMutationIdentity(terminal, identity); err != nil {
			return terminal, err
		}
		current = terminal
	}
	if current == nil || agentMutationActive(current.Status) {
		return current, errors.New("interrupted mail TLS sync did not reach a terminal state")
	}
	return current, nil
}

func (p *Panel) clearInterruptedVPNSyncLease(ctx context.Context) (bool, error) {
	result, err := p.db.GetDB().ExecContext(ctx, `
		UPDATE vpn_sync_state
		SET status = 'pending', last_error = NULL,
		    lease_token = NULL, lease_expires_at = NULL,
		    updated_at = datetime('now')
		WHERE id = 1 AND lease_token IS NOT NULL`)
	if err != nil {
		return false, fmt.Errorf("clear interrupted VPN synchronization lease: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("verify interrupted VPN synchronization lease cleanup: %w", err)
	}
	return affected == 1, nil
}

func (p *Panel) syncVPNPeersAfterRecovery() error {
	syncCtx, cancelSync := context.WithTimeout(
		context.Background(),
		panelMutationRecoveryTimeout,
	)
	defer cancelSync()
	return p.syncVPNPeers(syncCtx)
}

func (p *Panel) recoverInterruptedServiceOperations(ctx context.Context) (int64, error) {
	p.serviceMutationMu.Lock()
	lockTransferred := false
	defer func() {
		if !lockTransferred {
			p.serviceMutationMu.Unlock()
		}
	}()

	op, err := p.activeServiceOperation(ctx)
	if err != nil {
		return 0, err
	}
	recoveryCtx, cancel := context.WithTimeout(ctx, panelMutationRecoveryTimeout)
	defer cancel()

	globalJob, err := p.refreshStartupGlobalMutation(recoveryCtx)
	if err != nil {
		return 0, err
	}
	if handled, engineErr := p.recoverDNSEngineSwitchLocked(
		recoveryCtx, globalJob,
	); handled || engineErr != nil {
		if engineErr != nil {
			return 0, engineErr
		}
		return 0, nil
	}
	if op != nil && op.Kind == serviceOperationKindPanelCertificate {
		if !validServiceOperationID(op.RequestID) {
			return 0, errors.New("active panel certificate operation has no durable request identity")
		}
		if _, err := decodePanelCertificateSagaData(*op); err != nil {
			return 0, fmt.Errorf("validate active panel certificate operation: %w", err)
		}
		globalJob, err = p.refreshStartupGlobalMutation(recoveryCtx)
		if err != nil {
			return 0, err
		}
		if globalJob != nil && agentMutationActive(globalJob.Status) &&
			!recoverablePanelCertificateSagaChild(*op, globalJob) {
			return 0, fmt.Errorf(
				"active agent mutation %s does not match the persisted panel certificate operation",
				globalJob.RequestID,
			)
		}
		if err := p.resumePanelCertificateSagaLocked(*op); err != nil {
			return 0, err
		}
		lockTransferred = true
		return 0, nil
	}
	if op == nil {
		dnsClusterPending, observeErr := p.observeDNSClusterSagaStartup(
			recoveryCtx, globalJob,
		)
		if observeErr != nil {
			return 0, observeErr
		}
		if validDirectDNSClusterConfigure(globalJob) {
			globalJob, err = p.refreshStartupGlobalMutation(recoveryCtx)
			if err != nil {
				return 0, err
			}
		}
		if globalJob == nil || !agentMutationActive(globalJob.Status) {
			cleared, clearErr := p.clearInterruptedVPNSyncLease(recoveryCtx)
			if clearErr != nil {
				return 0, clearErr
			}
			if cleared {
				if err := p.requireStartupAgentMutationSlot(recoveryCtx); err != nil {
					return 0, err
				}
				if err := p.syncVPNPeersAfterRecovery(); err != nil {
					return 0, fmt.Errorf("resynchronize orphaned WireGuard peers: %w", err)
				}
			}
			if !dnsClusterPending {
				if err := p.recoverDNSZoneSyncStateLocked(recoveryCtx); err != nil {
					return 0, fmt.Errorf("reconcile pending DNS publications: %w", err)
				}
			}
			return 0, nil
		}
		if validDirectDNSZoneSync(globalJob) {
			if dnsClusterPending {
				return 0, nil
			}
			if err := p.recoverDirectDNSZoneSyncLocked(recoveryCtx, globalJob); err != nil {
				return 0, fmt.Errorf("reconcile interrupted DNS publication: %w", err)
			}
			return 0, nil
		}
		if validDirectDNSSECSecure(globalJob) {
			if dnsClusterPending {
				return 0, nil
			}
			if err := p.recoverDirectDNSSECSecureLocked(
				recoveryCtx, globalJob,
			); err != nil {
				return 0, fmt.Errorf("reconcile interrupted DNSSEC signing: %w", err)
			}
			return 0, nil
		}
		if validDirectFirewallMutation(globalJob) {
			if err := p.terminalizeInterruptedFirewallMutation(
				recoveryCtx,
				globalJob,
			); err != nil {
				return 0, err
			}
			// There is no active panel row carrying desired firewall work.
			// Reconstructing a payload from a digest would fabricate authority;
			// exact agent terminalization is the complete orphan recovery.
			if !dnsClusterPending {
				if err := p.recoverDNSZoneSyncStateLocked(recoveryCtx); err != nil {
					return 0, fmt.Errorf("reconcile pending DNS publications: %w", err)
				}
			}
			return 0, nil
		}
		if validDirectMailTLSSync(globalJob) {
			if _, err := p.terminalizeInterruptedMailTLSSync(
				recoveryCtx, globalJob,
			); err != nil {
				return 0, err
			}
			if !dnsClusterPending {
				if err := p.recoverDNSZoneSyncStateLocked(recoveryCtx); err != nil {
					return 0, fmt.Errorf("reconcile pending DNS publications: %w", err)
				}
			}
			return 0, nil
		}
		if validDirectVPNPeerSync(globalJob) {
			if err := p.terminalizeInterruptedWireGuardPeerSync(
				recoveryCtx,
				globalJob,
			); err != nil {
				return 0, err
			}
			if _, err := p.clearInterruptedVPNSyncLease(recoveryCtx); err != nil {
				return 0, err
			}
			if err := p.requireStartupAgentMutationSlot(recoveryCtx); err != nil {
				return 0, err
			}
			if err := p.syncVPNPeersAfterRecovery(); err != nil {
				return 0, fmt.Errorf("resynchronize interrupted WireGuard peers: %w", err)
			}
			if !dnsClusterPending {
				if err := p.recoverDNSZoneSyncStateLocked(recoveryCtx); err != nil {
					return 0, fmt.Errorf("reconcile pending DNS publications: %w", err)
				}
			}
			return 0, nil
		}
		if globalJob.Status == agentMutationRunning {
			if err := p.cancelAgentMutation(
				recoveryCtx,
				globalJob,
				"panel_operation_missing",
				"The agent mutation has no matching active panel operation after restart.",
			); err != nil {
				return 0, fmt.Errorf("cancel unmatched privileged mutation: %w", err)
			}
		}
		if _, err := p.waitAgentMutationTerminal(recoveryCtx, globalJob.RequestID); err != nil {
			return 0, fmt.Errorf("wait for unmatched privileged mutation: %w", err)
		}
		if !dnsClusterPending {
			if err := p.recoverDNSZoneSyncStateLocked(recoveryCtx); err != nil {
				return 0, fmt.Errorf("reconcile pending DNS publications: %w", err)
			}
		}
		return 0, nil
	}
	if !validServiceOperationID(op.RequestID) {
		return 0, errors.New("active panel operation has no durable request identity")
	}
	if strings.HasPrefix(op.Phase, firewallChildPhasePrefix) {
		if _, ok := parseFirewallChildPhase(op.Phase); !ok {
			return 0, errors.New("active panel operation has a malformed firewall child identity")
		}
	}
	if strings.HasPrefix(op.Phase, mailTLSChildPhasePrefix) {
		if _, ok := parseMailTLSChildPhase(op.Phase); !ok {
			return 0, errors.New("active panel operation has a malformed mail TLS child identity")
		}
	}
	var recoveryProfile mailProfileDefinition
	if op.Kind == serviceOperationKindMailProfileInstall {
		if op.PackageName != "" {
			return 0, errors.New("interrupted mail profile operation has an unexpected package target")
		}
		var ok bool
		recoveryProfile, ok = mailProfileByID(op.ServiceID)
		if !ok {
			return 0, errors.New("interrupted mail profile operation has an unknown profile target")
		}
	}
	recoveredPowerDNSChild := false
	recoveredMailTLSChild := false
	globalJob, err = p.refreshStartupGlobalMutation(recoveryCtx)
	if err != nil {
		return 0, err
	}
	if globalJob != nil && agentMutationActive(globalJob.Status) &&
		globalJob.RequestID != op.RequestID {
		switch {
		case recoverableFirewallChild(op, globalJob):
			if err := p.terminalizeInterruptedFirewallMutation(
				recoveryCtx,
				globalJob,
			); err != nil {
				return 0, err
			}
		case recoverableMailTLSChild(op, globalJob):
			terminal, err := p.terminalizeInterruptedMailTLSSync(
				recoveryCtx, globalJob,
			)
			if err != nil {
				return 0, err
			}
			recoveredMailTLSChild = false
			if terminal != nil && terminal.Status == agentMutationSucceeded {
				persisted, ok := parseMailTLSChildPhase(op.Phase)
				if !ok {
					return 0, errors.New("persisted mail TLS child identity is invalid")
				}
				identity := agentMutationIdentity{
					RequestID: persisted.RequestID, OwnerID: persisted.OwnerID,
					Kind: "mail_tls_sync", Target: "mail-tls",
					PackageName: persisted.Qualifier,
				}
				if err := validateAgentMutationSucceededReceipt(terminal, identity); err != nil {
					return 0, err
				}
				recoveredMailTLSChild = true
			}
		case recoverableWireGuardPeerSync(op, globalJob):
			if err := p.terminalizeInterruptedWireGuardPeerSync(
				recoveryCtx,
				globalJob,
			); err != nil {
				return 0, err
			}
		case p.recoverablePowerDNSZoneSync(recoveryCtx, op, globalJob):
			if err := p.recoverDirectDNSZoneSyncLocked(
				recoveryCtx,
				globalJob,
			); err != nil {
				return 0, err
			}
			recoveredPowerDNSChild = true
		default:
			return 0, fmt.Errorf(
				"agent mutation %s does not match active panel operation %s",
				globalJob.RequestID,
				op.RequestID,
			)
		}
	}
	if !recoveredMailTLSChild && mailProfileNeedsMailTLSSync(*op) {
		recoveredMailTLSChild, err = p.persistedMailTLSChildSucceeded(recoveryCtx, *op)
		if err != nil {
			return 0, fmt.Errorf("reconcile persisted mail TLS child: %w", err)
		}
	}

	job, err := p.statusAgentMutation(recoveryCtx, op.RequestID)
	if err != nil {
		return 0, fmt.Errorf("read matching privileged mutation: %w", err)
	}
	if job == nil {
		failure := operationFailure(
			"panel_restarted_without_agent_ledger",
			"Panel restarted and the privileged agent has no matching operation record.",
			nil,
		)
		if err := p.forceFailActiveServiceOperation(ctx, op.ID, "interrupted", failure); err != nil {
			return 0, err
		}
		return 1, nil
	}
	if job.Kind != op.Kind || job.Target != op.ServiceID ||
		job.PackageName != op.PackageName {
		return 0, errors.New("panel and agent service mutation identities disagree")
	}

	if agentMutationActive(job.Status) {
		if job.Status == agentMutationRunning {
			if err := p.cancelAgentMutation(
				recoveryCtx,
				job,
				"panel_restarted_during_mutation",
				"Panel restarted before the privileged operation result was committed.",
			); err != nil {
				return 0, fmt.Errorf("cancel interrupted privileged mutation: %w", err)
			}
		}
		job, err = p.waitAgentMutationTerminal(recoveryCtx, op.RequestID)
		if err != nil {
			return 0, fmt.Errorf("wait for interrupted privileged mutation: %w", err)
		}
		if job == nil {
			return 0, errors.New("agent lost the interrupted mutation while reconciling it")
		}
	}
	if wireGuardInstallNeedsPeerSync(*op) {
		if _, err := p.clearInterruptedVPNSyncLease(recoveryCtx); err != nil {
			return 0, err
		}
	}

	if job.Status == agentMutationSucceeded {
		result := serviceOperationResult{"success": true, "recovered": true}
		if op.Kind == serviceOperationKindMailProfileInstall {
			result, err = p.reconstructSucceededMailProfileResult(recoveryCtx, recoveryProfile)
			if err != nil {
				return 0, fmt.Errorf("reconstruct succeeded mail profile operation: %w", err)
			}
		}
		if op.Status == serviceOperationQueued {
			if err := p.markServiceOperationRunning(ctx, op.ID, "recovered_terminal"); err != nil {
				return 0, err
			}
		}
		if serviceOperationNeedsFirewallSync(*op) {
			if err := p.requireStartupAgentMutationSlot(recoveryCtx); err != nil {
				return 0, err
			}
		}
		if syncFailure := p.syncFirewallAfterOperation(*op); syncFailure != nil {
			if result == nil {
				result = serviceOperationResult{}
			}
			result["success"] = false
			terminalCtx, cancelTerminal := context.WithTimeout(
				context.Background(),
				panelMutationFinishTimeout,
			)
			defer cancelTerminal()
			if err := p.finishServiceOperationFailed(
				terminalCtx,
				op.ID,
				serviceOperationFirewallPhase(*op),
				result,
				syncFailure,
			); err != nil {
				return 0, err
			}
			return 1, nil
		}
		if mailProfileNeedsMailTLSSync(*op) && !recoveredMailTLSChild {
			if err := p.requireStartupAgentMutationSlot(recoveryCtx); err != nil {
				return 0, err
			}
			if syncFailure := p.syncMailTLSAfterOperation(*op, result); syncFailure != nil {
				if result == nil {
					result = serviceOperationResult{}
				}
				result["success"] = false
				terminalCtx, cancelTerminal := context.WithTimeout(
					context.Background(), panelMutationFinishTimeout,
				)
				defer cancelTerminal()
				if err := p.finishServiceOperationFailed(
					terminalCtx, op.ID, mailProfilePhase(op.ServiceID, "mail-tls"),
					result, syncFailure,
				); err != nil {
					return 0, err
				}
				return 1, nil
			}
		}
		if powerDNSInstallNeedsZoneSync(*op) && !recoveredPowerDNSChild {
			dnsBatchCtx, cancelDNSBatch := dnsZoneBatchContext(recoveryCtx)
			if err := p.requireStartupAgentMutationSlot(dnsBatchCtx); err != nil {
				cancelDNSBatch()
				return 0, err
			}
			syncFailure := p.syncPowerDNSZonesAfterInstallContext(dnsBatchCtx, *op)
			cancelDNSBatch()
			if syncFailure != nil {
				if result == nil {
					result = serviceOperationResult{}
				}
				result["success"] = false
				terminalCtx, cancelTerminal := context.WithTimeout(
					context.Background(),
					panelMutationFinishTimeout,
				)
				defer cancelTerminal()
				if err := p.finishServiceOperationFailed(
					terminalCtx,
					op.ID,
					"syncing_dns",
					result,
					syncFailure,
				); err != nil {
					return 0, err
				}
				return 1, nil
			}
		}
		if wireGuardInstallNeedsPeerSync(*op) {
			if err := p.requireStartupAgentMutationSlot(recoveryCtx); err != nil {
				return 0, err
			}
		}
		if syncFailure := p.syncWireGuardPeersAfterInstall(*op); syncFailure != nil {
			if result == nil {
				result = serviceOperationResult{}
			}
			result["success"] = false
			terminalCtx, cancelTerminal := context.WithTimeout(
				context.Background(),
				panelMutationFinishTimeout,
			)
			defer cancelTerminal()
			if err := p.finishServiceOperationFailed(
				terminalCtx,
				op.ID,
				"syncing",
				result,
				syncFailure,
			); err != nil {
				return 0, err
			}
			return 1, nil
		}
		if err := p.finishServiceOperationSucceeded(
			ctx,
			op.ID,
			result,
		); err != nil {
			return 0, err
		}
		return 1, nil
	}
	if !agentMutationCanResume(job) {
		failure := operationFailure(
			nonEmptyMutationValue(job.ErrorCode, "interrupted_service_mutation_failed"),
			nonEmptyMutationValue(job.ErrorMessage, "The interrupted privileged operation failed."),
			nil,
		)
		if err := p.forceFailActiveServiceOperation(ctx, op.ID, "interrupted", failure); err != nil {
			return 0, err
		}
		return 1, nil
	}
	if err := p.requireStartupAgentMutationSlot(recoveryCtx); err != nil {
		return 0, err
	}
	if err := p.resumeInterruptedServiceOperationLocked(*op); err != nil {
		return 0, err
	}
	lockTransferred = true
	return 0, nil
}

func nonEmptyMutationValue(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func agentMutationCanResume(job *agentMutationJob) bool {
	if job == nil || job.Status != agentMutationFailed {
		return false
	}
	switch job.ErrorCode {
	case "panel_restarted_during_mutation",
		"agent_restarted_before_completion",
		"service_mutation_lease_expired":
		return true
	default:
		return false
	}
}

// resumeInterruptedServiceOperationLocked transfers the already-owned process
// mutation lock to the resumed goroutine, which releases it only after the
// panel operation reaches a durable terminal state.
func (p *Panel) resumeInterruptedServiceOperationLocked(op serviceOperation) error {
	var (
		runner       serviceOperationRunner
		successAudit string
		failureAudit string
	)
	switch op.Kind {
	case serviceOperationKindInstall:
		request := serviceInstallRequest{
			ServiceID: op.ServiceID,
			Package:   op.PackageName,
			RequestID: op.RequestID,
		}
		runner = func(ctx context.Context, advance func(string) error) (serviceOperationResult, *serviceOperationFailure) {
			return p.runServiceInstall(ctx, request, advance)
		}
		successAudit = "service.install.recovered:" + op.ServiceID
		failureAudit = "service.install.recovered.failed:" + op.ServiceID
	case serviceOperationKindRuntimeInstall:
		if op.ServiceID != "node" || !nodeSemverRe.MatchString(op.PackageName) {
			return errors.New("interrupted runtime operation has an invalid target")
		}
		runner = func(ctx context.Context, advance func(string) error) (serviceOperationResult, *serviceOperationFailure) {
			return p.runNodeInstall(ctx, op.PackageName, advance)
		}
		successAudit = "runtime.node.install.recovered:" + op.PackageName
		failureAudit = "runtime.node.install.recovered.failed:" + op.PackageName
	case serviceOperationKindMailProfileInstall:
		if op.PackageName != "" {
			return errors.New("interrupted mail profile operation has an unexpected package target")
		}
		profile, ok := mailProfileByID(op.ServiceID)
		if !ok {
			return errors.New("interrupted mail profile operation has an unknown profile target")
		}
		runner = func(ctx context.Context, advance func(string) error) (serviceOperationResult, *serviceOperationFailure) {
			return p.runMailProfileInstall(ctx, profile.ID, advance)
		}
		successAudit = "mail.profile.install.recovered:" + profile.ID
		failureAudit = "mail.profile.install.recovered.failed:" + profile.ID
	default:
		return fmt.Errorf("cannot resume unsupported service operation kind %q", op.Kind)
	}

	p.launchServiceOperationWithAuditMode(
		op,
		serviceOperationActor{},
		"reconciling",
		successAudit,
		failureAudit,
		p.serviceMutationMu.Unlock,
		runner,
		p.auditServiceOperation,
		true,
		op.Status == serviceOperationRunning,
	)
	return nil
}

func (p *Panel) resumeInterruptedServiceOperation(op serviceOperation) error {
	p.serviceMutationMu.Lock()
	if err := p.resumeInterruptedServiceOperationLocked(op); err != nil {
		p.serviceMutationMu.Unlock()
		return err
	}
	return nil
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
	if classification, ok := classifyHostMutationError(err); ok {
		return operationFailure(classification.Code, classification.Message, err)
	}
	return operationFailure(
		"operation_start_failed",
		"The package operation could not be started.",
		err,
	)
}

func platformServiceOperationFailure(cause error) *serviceOperationFailure {
	classification, ok := classifyAgentRPCPlatformError(cause)
	if !ok {
		return nil
	}
	return operationFailure(classification.Code, classification.Message, cause)
}

func serviceInstallFailure(cause error) *serviceOperationFailure {
	if failure := platformServiceOperationFailure(cause); failure != nil {
		return failure
	}
	return operationFailure(
		"service_install_failed",
		"The service could not be installed and verified.",
		cause,
	)
}

func firewallSyncFailure(cause error) *serviceOperationFailure {
	return operationFailure(
		"firewall_sync_failed",
		"The service changed successfully, but the active firewall policy could not be synchronized.",
		cause,
	)
}

func nodeInstallFailure(cause error) *serviceOperationFailure {
	if failure := platformServiceOperationFailure(cause); failure != nil {
		return failure
	}
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
	binding, err := panelMutationBinding(ctx)
	if err != nil {
		return result, nodeInstallFailure(err)
	}
	if err := p.preflightManagedServiceInstall(ctx, "node", ""); err != nil {
		return result, nodeInstallFailure(err)
	}
	var response transport.NodeInstallResponse
	if err := p.callAgentContext(ctx, "Agent.InstallNodeVersion", &transport.NodeInstallRequest{
		ServiceMutationBinding: transport.ServiceMutationBinding{
			MutationRequestID: binding.MutationRequestID,
			MutationOwnerID:   binding.MutationOwnerID,
		},
		Version: version,
	}, &response); err != nil {
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
