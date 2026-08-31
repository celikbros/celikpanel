package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

type SyncDNSZoneV3Request = transport.SyncDNSZoneV3Request
type SyncDNSZoneV3Response = transport.SyncDNSZoneV3Response
type RecoverDNSZoneV3Request = transport.RecoverDNSZoneV3Request
type RecoverDNSZoneV3Response = transport.RecoverDNSZoneV3Response
type SwitchDNSEngineV1Request = transport.SwitchDNSEngineV1Request
type SwitchDNSEngineV1Response = transport.SwitchDNSEngineV1Response
type DNSBackendReadinessResponse = transport.DNSBackendReadinessResponse

type dnsEngineSwitchRecoveryOutcome string

const (
	dnsEngineSwitchRecoveryAbsent     dnsEngineSwitchRecoveryOutcome = "absent"
	dnsEngineSwitchRecoveryRolledBack dnsEngineSwitchRecoveryOutcome = "rolled-back"
	dnsEngineSwitchRecoveryCommitted  dnsEngineSwitchRecoveryOutcome = "committed"
	dnsEngineSwitchRecoveryFinalized  dnsEngineSwitchRecoveryOutcome = "finalized"
)

const (
	dnsEngineSwitchPublishedPhasePrefix = "commit/dns-engine-switch/v1/published/"
	dnsEngineSwitchFinalizedPhasePrefix = "commit/dns-engine-switch/v2/finalized/"
)

const dnsBackendReadinessTimeout = 10 * time.Second

const (
	dnsZoneSyncV3CommitPhasePrefix    = "commit/dns-zone-sync/v3/"
	dnsZoneSyncV3Applied              = "applied"
	dnsZoneSyncV3PropagationPending   = "propagation-pending"
	dnsZoneSyncV3Recovering           = "recovering"
	dnsZoneSyncV3Published            = "published"
	dnsZoneSyncV3PublishedPhasePrefix = dnsZoneSyncV3CommitPhasePrefix +
		dnsZoneSyncV3Published + "/"
)

// dnsZoneV3RecoveryPendingError is emitted only after the exact local V3 host
// receipt is durable. It is never used for staging, activation, or local
// authority failures, which remain ordinary terminal attempt failures.
type dnsZoneV3RecoveryPendingError struct{ err error }

func (e *dnsZoneV3RecoveryPendingError) Error() string { return e.err.Error() }
func (e *dnsZoneV3RecoveryPendingError) Unwrap() error { return e.err }

func dnsZoneV3RecoveryPending(err error) error {
	if err == nil {
		return nil
	}
	return &dnsZoneV3RecoveryPendingError{err: err}
}

type dnsZoneV3RecoveryAmbiguousError struct{ err error }

func (e *dnsZoneV3RecoveryAmbiguousError) Error() string { return e.err.Error() }
func (e *dnsZoneV3RecoveryAmbiguousError) Unwrap() error { return e.err }

func dnsZoneV3RecoveryAmbiguous(err error) error {
	if err == nil {
		return nil
	}
	return &dnsZoneV3RecoveryAmbiguousError{err: err}
}

type dnsEngineBackend interface {
	Readiness(context.Context) (transport.DNSBackendReadinessResponse, error)
	Sync(
		context.Context,
		mutationpayload.DNSZoneSyncV3Commitment,
		transport.ServiceMutationBinding,
	) (string, error)
	RecoverZone(
		context.Context,
		string,
		string,
		transport.ServiceMutationBinding,
	) (bool, error)
	Switch(
		context.Context,
		mutationpayload.DNSEngineSwitchManifestCommitment,
		transport.ServiceMutationBinding,
	) (transport.SwitchDNSEngineV1Response, error)
	RecoverSwitch(
		context.Context,
		transport.DNSEngine,
		string,
		transport.ServiceMutationBinding,
	) (dnsEngineSwitchRecoveryOutcome, error)
	FinalizeSwitch(
		context.Context,
		transport.DNSEngine,
		string,
		transport.ServiceMutationBinding,
	) error
}

var agentDNSEngineBackend dnsEngineBackend = hostDNSEngineBackend{}

// DNSBackendReadiness is advisory and read-only. It reports bounded facts for
// both engines; a privileged switch still has to acquire and satisfy its exact
// durable service-mutation lease.
func (a *Agent) DNSBackendReadiness(_ *transport.Empty, response *DNSBackendReadinessResponse) error {
	if response == nil {
		return errors.New("DNS backend readiness response is required")
	}
	*response = DNSBackendReadinessResponse{}
	readinessCtx, readinessCancel := context.WithTimeout(
		context.Background(), dnsBackendReadinessTimeout,
	)
	readiness, err := agentDNSEngineBackend.Readiness(readinessCtx)
	readinessCancel()
	if err != nil {
		log.Printf("DNS backend readiness probe failed: %v", err)
		response.Error = "DNS backend readiness could not be verified"
		return nil
	}
	readiness.Error = ""
	// A readiness probe that reports engine state but hides that every mutation
	// is being refused lets the panel diagnose the wrong thing: an engine the
	// panel installed reads as Managed=false while the transaction that would
	// have claimed it is stuck, and the screen says a foreign DNS server was
	// found. Carry the hold so the panel can tell a stuck transaction from an
	// intruder.
	// Motor durumunu bildirip her mutasyonun reddedildiğini gizleyen bir hazırlık
	// yoklaması, panelin yanlış teşhis koymasına yol açar: panelin kurduğu bir
	// motor, onu sahiplenecek işlem takılıyken Managed=false görünür ve ekran
	// yabancı bir DNS sunucusu bulunduğunu söyler. Tutmayı taşı ki panel takılmış
	// bir işlemi davetsiz bir misafirden ayırabilsin.
	readiness.MutationHold = agentMutationHold()
	*response = readiness
	return nil
}

// SyncDNSZoneV3 publishes one complete full-zone snapshot to the engine and
// epoch selected by the panel. BIND applies a delta to the verified immutable
// current generation, so unrelated zones never have to cross the RPC again.
func (a *Agent) SyncDNSZoneV3(request *SyncDNSZoneV3Request, response *SyncDNSZoneV3Response) error {
	if response == nil {
		return errors.New("DNS zone V3 sync response is required")
	}
	*response = SyncDNSZoneV3Response{}
	if request == nil {
		response.Error = "DNS zone V3 sync request is required"
		return nil
	}
	commitment, err := mutationpayload.CanonicalDNSZoneSyncV3(
		request.Engine,
		request.EngineEpoch,
		request.DesiredGeneration,
		request.Domain,
		request.Delete,
		request.ZoneType,
		request.Records,
	)
	if err != nil {
		response.Error = err.Error()
		return nil
	}
	action := dnsZoneSyncActionSync
	if commitment.Delete {
		action = dnsZoneSyncActionDelete
	}
	ctx, finishStep, err := a.requiredServiceMutationStep(
		request.ServiceMutationBinding,
		newServiceMutationStepClaim(
			serviceMutationStepSyncDNSZoneV3,
			commitment.Domain,
			commitment.Qualifier,
			action,
		),
	)
	if err != nil {
		response.Error = err.Error()
		return nil
	}
	defer finishStep()

	generation, err := agentDNSEngineBackend.Sync(
		ctx, commitment, request.ServiceMutationBinding,
	)
	if err != nil {
		var pending *dnsZoneV3RecoveryPendingError
		if errors.As(err, &pending) {
			if pendingErr := publishDNSZoneSyncV3Pending(
				ctx, commitment.Domain, commitment.Qualifier,
			); pendingErr != nil {
				poisonErr := poisonDNSZoneSyncV3ProtocolViolation(
					ctx, commitment.Domain, commitment.Qualifier, pendingErr,
				)
				log.Printf("%s zone publication pending receipt failed for %s at epoch %d: %v", commitment.Engine, commitment.Domain, commitment.EngineEpoch, errors.Join(pendingErr, poisonErr))
				response.Error = "DNS zone publication became ambiguous; inspect the agent log"
				return nil
			}
			log.Printf("%s zone publication remains pending for %s at epoch %d: %v", commitment.Engine, commitment.Domain, commitment.EngineEpoch, err)
			response.RecoveryPending = true
			response.Engine = request.Engine
			response.EngineEpoch = request.EngineEpoch
			response.AppliedGeneration = commitment.DesiredGeneration
			return nil
		}
		var ambiguous *dnsZoneV3RecoveryAmbiguousError
		if errors.As(err, &ambiguous) {
			poisonErr := poisonDNSZoneSyncV3Runtime(
				ctx, commitment.Domain, commitment.Qualifier, err,
			)
			log.Printf("%s zone publication became ambiguous for %s at epoch %d: %v", commitment.Engine, commitment.Domain, commitment.EngineEpoch, errors.Join(err, poisonErr))
			response.Error = "DNS zone publication became ambiguous; inspect the agent log"
			return nil
		}
		log.Printf("%s zone publication failed for %s at epoch %d: %v", commitment.Engine, commitment.Domain, commitment.EngineEpoch, err)
		response.Error = "DNS zone publication failed; inspect the agent log"
		return nil
	}
	if err := publishDNSZoneSyncV3Terminal(ctx, commitment.Domain, commitment.Qualifier); err != nil {
		poisonErr := poisonDNSZoneSyncV3ProtocolViolation(
			ctx, commitment.Domain, commitment.Qualifier, err,
		)
		log.Printf("DNS zone V3 terminal receipt publication failed: %v", errors.Join(err, poisonErr))
		response.Error = "DNS zone publication finished but its durable receipt could not be verified"
		return nil
	}
	response.Synced = true
	response.Engine = request.Engine
	response.EngineEpoch = request.EngineEpoch
	response.AppliedGeneration = commitment.DesiredGeneration
	_ = generation // Generation identity is retained in the immutable host receipt.
	return nil
}

// RecoverDNSZoneV3 re-drives only the immutable host receipt selected by a
// canonical propagation-pending attempt. Any non-success returns the attempt
// to the same recoverable pending state; it can never retire that authority as
// a pre-commit failure or accept a replacement binding.
func (a *Agent) RecoverDNSZoneV3(
	request *RecoverDNSZoneV3Request,
	response *RecoverDNSZoneV3Response,
) error {
	if response == nil {
		return errors.New("DNS zone V3 recovery response is required")
	}
	*response = RecoverDNSZoneV3Response{}
	if request == nil || !serviceMutationCanonicalFQDN(request.Domain) ||
		!mutationpayload.ValidDNSZoneSyncV3Qualifier(request.Qualifier) {
		response.Error = "DNS zone V3 recovery request is invalid"
		return nil
	}
	ctx, finishStep, err := a.requiredServiceMutationStep(
		request.ServiceMutationBinding,
		newServiceMutationStepClaim(
			serviceMutationStepRecoverDNSZoneV3,
			request.Domain,
			request.Qualifier,
			"recover",
		),
	)
	if err != nil {
		response.Error = err.Error()
		return nil
	}
	defer finishStep()
	if err := requireDNSZoneSyncV3RecoveryRuntime(
		ctx, request.Domain, request.Qualifier,
	); err != nil {
		response.Error = err.Error()
		return nil
	}
	exact, recoverErr := agentDNSEngineBackend.RecoverZone(
		ctx, request.Domain, request.Qualifier,
		request.ServiceMutationBinding,
	)
	if recoverErr != nil {
		var pending *dnsZoneV3RecoveryPendingError
		if !errors.As(recoverErr, &pending) {
			poisonErr := poisonDNSZoneSyncV3Runtime(
				ctx, request.Domain, request.Qualifier, recoverErr,
			)
			log.Printf("DNS zone V3 recovery became ambiguous for %s: %v", request.Domain, errors.Join(recoverErr, poisonErr))
			response.Error = "DNS zone recovery became ambiguous; inspect the agent log"
			return nil
		}
		log.Printf("DNS zone V3 recovery remains pending for %s: %v", request.Domain, recoverErr)
		if pendingErr := publishDNSZoneSyncV3Pending(
			ctx, request.Domain, request.Qualifier,
		); pendingErr != nil {
			poisonErr := poisonDNSZoneSyncV3ProtocolViolation(
				ctx, request.Domain, request.Qualifier, pendingErr,
			)
			log.Printf("DNS zone V3 recovery pending receipt failed for %s: %v", request.Domain, errors.Join(pendingErr, poisonErr))
			response.Error = "DNS zone recovery became ambiguous; inspect the agent log"
			return nil
		}
		response.RecoveryPending = true
		return nil
	}
	if !exact {
		recoverErr = errors.New("exact DNS zone V3 host receipt is unavailable")
		poisonErr := poisonDNSZoneSyncV3Runtime(
			ctx, request.Domain, request.Qualifier, recoverErr,
		)
		log.Printf("DNS zone V3 recovery lost its exact host receipt for %s: %v", request.Domain, errors.Join(recoverErr, poisonErr))
		response.Error = "DNS zone recovery became ambiguous; inspect the agent log"
		return nil
	}
	if err := publishDNSZoneSyncV3Terminal(
		ctx, request.Domain, request.Qualifier,
	); err != nil {
		poisonErr := poisonDNSZoneSyncV3ProtocolViolation(
			ctx, request.Domain, request.Qualifier, err,
		)
		log.Printf("DNS zone V3 recovered terminal receipt publication failed: %v", errors.Join(err, poisonErr))
		response.Error = "DNS zone recovery finished but its durable receipt could not be verified"
		return nil
	}
	response.Recovered = true
	return nil
}

func formatDNSZoneSyncV3Phase(state, requestID, domain, qualifier string) (string, error) {
	if (state != dnsZoneSyncV3Applied && state != dnsZoneSyncV3PropagationPending &&
		state != dnsZoneSyncV3Recovering && state != dnsZoneSyncV3Published) ||
		!validMutationIdentity(requestID) ||
		!serviceMutationCanonicalFQDN(domain) ||
		!mutationpayload.ValidDNSZoneSyncV3Qualifier(qualifier) {
		return "", errors.New("invalid DNS zone V3 phase identity")
	}
	return dnsZoneSyncV3CommitPhasePrefix + state + "/" + requestID +
		"/" + domain + "/" + qualifier, nil
}

func parseDNSZoneSyncV3Phase(value string) (state, requestID, domain, qualifier string, err error) {
	if !strings.HasPrefix(value, dnsZoneSyncV3CommitPhasePrefix) {
		return "", "", "", "", errors.New("not a DNS zone V3 phase")
	}
	remainder := strings.TrimPrefix(value, dnsZoneSyncV3CommitPhasePrefix)
	state, remainder, found := strings.Cut(remainder, "/")
	if !found {
		return "", "", "", "", errors.New("invalid DNS zone V3 phase")
	}
	requestID, remainder, found = strings.Cut(remainder, "/")
	if !found {
		return "", "", "", "", errors.New("invalid DNS zone V3 phase")
	}
	domain, qualifier, found = strings.Cut(remainder, "/")
	if !found {
		return "", "", "", "", errors.New("invalid DNS zone V3 phase")
	}
	canonical, formatErr := formatDNSZoneSyncV3Phase(state, requestID, domain, qualifier)
	if formatErr != nil || canonical != value {
		return "", "", "", "", errors.New("invalid DNS zone V3 phase")
	}
	return state, requestID, domain, qualifier, nil
}

func formatDNSZoneSyncV3PublishedPhase(requestID, domain, qualifier string) (string, error) {
	return formatDNSZoneSyncV3Phase(
		dnsZoneSyncV3Published, requestID, domain, qualifier,
	)
}

func parseDNSZoneSyncV3PublishedPhase(value string) (requestID, domain, qualifier string, err error) {
	state, requestID, domain, qualifier, err := parseDNSZoneSyncV3Phase(value)
	if err != nil || state != dnsZoneSyncV3Published {
		return "", "", "", errors.New("invalid DNS zone V3 published phase")
	}
	return requestID, domain, qualifier, nil
}

func markDNSZoneSyncV3Applied(ctx context.Context, domain, qualifier string) error {
	tracker, _ := ctx.Value(serviceMutationExecutionTrackerKey{}).(*serviceMutationExecutionTracker)
	if tracker == nil || tracker.manager == nil || tracker.runtime == nil {
		return errors.New("DNS zone V3 applied publication requires a durable execution tracker")
	}
	m, runtime := tracker.manager, tracker.runtime
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.healthErrorLocked(); err != nil {
		return err
	}
	job := runtime.job
	if m.active != runtime || runtime.steps != 1 || job == nil ||
		(job.Status != serviceMutationStatusRunning &&
			job.Status != serviceMutationStatusCancelling) ||
		job.WorkerPID != 0 || job.Kind != "dns_zone_sync" ||
		job.Target != domain || job.PackageName != qualifier {
		return errors.New("DNS zone V3 applied publication lost its exact mutation identity")
	}
	phase, err := formatDNSZoneSyncV3Phase(
		dnsZoneSyncV3Applied, job.RequestID, domain, qualifier,
	)
	if err != nil {
		return err
	}
	if job.Phase == phase && runtime.dnsZoneSyncV3AppliedPhase == phase {
		return nil
	}
	before := cloneServiceMutationLedger(m.ledger)
	job.Phase = phase
	job.UpdatedAt = m.now()
	if err := m.persistLedgerMutationLocked(before); err != nil {
		if m.poisoned == nil && m.active == runtime {
			return m.poisonLocked(fmt.Errorf(
				"persist applied DNS zone V3 receipt: %w", err,
			))
		}
		return err
	}
	runtime.dnsZoneSyncV3AppliedPhase = phase
	return nil
}

func poisonDNSZoneSyncV3Runtime(
	ctx context.Context,
	domain, qualifier string,
	cause error,
) error {
	tracker, _ := ctx.Value(serviceMutationExecutionTrackerKey{}).(*serviceMutationExecutionTracker)
	if tracker == nil || tracker.manager == nil || tracker.runtime == nil || cause == nil {
		return errors.New("DNS zone V3 ambiguity lacks a durable execution tracker")
	}
	m, runtime := tracker.manager, tracker.runtime
	m.mu.Lock()
	defer m.mu.Unlock()
	job := runtime.job
	appliedPhase := ""
	if job != nil {
		appliedPhase, _ = formatDNSZoneSyncV3Phase(
			dnsZoneSyncV3Applied, job.RequestID, domain, qualifier,
		)
	}
	if m.active != runtime || runtime.steps != 1 || job == nil ||
		(job.Status != serviceMutationStatusRunning &&
			job.Status != serviceMutationStatusCancelling) ||
		job.Kind != "dns_zone_sync" || job.Target != domain ||
		job.PackageName != qualifier ||
		(!runtime.dnsZoneSyncV3Recovery &&
			(runtime.dnsZoneSyncV3AppliedPhase != appliedPhase ||
				job.Phase != appliedPhase)) {
		return errors.New("DNS zone V3 ambiguity lost its exact committed identity")
	}
	return m.poisonLocked(fmt.Errorf(
		"DNS zone V3 committed state is ambiguous: %w", cause,
	))
}

func poisonDNSZoneSyncV3ProtocolViolation(
	ctx context.Context,
	domain, qualifier string,
	cause error,
) error {
	tracker, _ := ctx.Value(serviceMutationExecutionTrackerKey{}).(*serviceMutationExecutionTracker)
	if tracker == nil || tracker.manager == nil || tracker.runtime == nil || cause == nil {
		return errors.New("DNS zone V3 protocol violation lacks a durable execution tracker")
	}
	m, runtime := tracker.manager, tracker.runtime
	m.mu.Lock()
	defer m.mu.Unlock()
	job := runtime.job
	if m.active != runtime || job == nil || job.Kind != "dns_zone_sync" ||
		job.Target != domain || job.PackageName != qualifier {
		return errors.New("DNS zone V3 protocol violation lost its exact mutation identity")
	}
	if m.poisoned != nil {
		return m.healthErrorLocked()
	}
	return m.poisonLocked(fmt.Errorf(
		"DNS zone V3 publication protocol violated durable authority: %w", cause,
	))
}

func publishDNSZoneSyncV3Pending(ctx context.Context, domain, qualifier string) error {
	tracker, _ := ctx.Value(serviceMutationExecutionTrackerKey{}).(*serviceMutationExecutionTracker)
	if tracker == nil || tracker.manager == nil || tracker.runtime == nil {
		return errors.New("DNS zone V3 pending publication requires a durable execution tracker")
	}
	m, runtime := tracker.manager, tracker.runtime
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.healthErrorLocked(); err != nil {
		return err
	}
	job := runtime.job
	if m.active != runtime || runtime.steps != 1 || job == nil ||
		(job.Status != serviceMutationStatusRunning &&
			job.Status != serviceMutationStatusCancelling) ||
		job.WorkerPID != 0 ||
		job.Kind != "dns_zone_sync" || job.Target != domain ||
		job.PackageName != qualifier {
		return errors.New("DNS zone V3 pending publication lost its exact mutation identity")
	}
	appliedPhase, err := formatDNSZoneSyncV3Phase(
		dnsZoneSyncV3Applied, job.RequestID, domain, qualifier,
	)
	if err != nil || (!runtime.dnsZoneSyncV3Recovery &&
		(runtime.dnsZoneSyncV3AppliedPhase != appliedPhase ||
			job.Phase != appliedPhase)) {
		return errors.New("DNS zone V3 pending publication lacks exact local commit authority")
	}
	phase, err := formatDNSZoneSyncV3Phase(
		dnsZoneSyncV3PropagationPending, job.RequestID, domain, qualifier,
	)
	if err != nil {
		return err
	}
	if err := m.finishRuntimeDNSZoneV3PendingLocked(runtime, phase); err != nil {
		if m.poisoned == nil && m.active == runtime {
			return m.poisonLocked(fmt.Errorf(
				"persist pending DNS zone V3 receipt: %w", err,
			))
		}
		return err
	}
	return nil
}

func requireDNSZoneSyncV3RecoveryRuntime(
	ctx context.Context, domain, qualifier string,
) error {
	tracker, _ := ctx.Value(serviceMutationExecutionTrackerKey{}).(*serviceMutationExecutionTracker)
	if tracker == nil || tracker.manager == nil || tracker.runtime == nil {
		return errors.New("DNS zone V3 recovery requires a durable execution tracker")
	}
	m, runtime := tracker.manager, tracker.runtime
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.healthErrorLocked(); err != nil {
		return err
	}
	job := runtime.job
	if job == nil {
		return errors.New("DNS zone V3 recovery lost its pending job")
	}
	want, err := formatDNSZoneSyncV3Phase(
		dnsZoneSyncV3Recovering, job.RequestID, domain, qualifier,
	)
	if err != nil || m.active != runtime || !runtime.dnsZoneSyncV3Recovery ||
		runtime.steps != 1 || job.Status != serviceMutationStatusRunning ||
		job.Kind != "dns_zone_sync" || job.Target != domain ||
		job.PackageName != qualifier || job.Phase != want {
		return errors.New("DNS zone V3 recovery lost its exact pending identity")
	}
	return nil
}

func publishDNSZoneSyncV3Terminal(ctx context.Context, domain, qualifier string) error {
	tracker, _ := ctx.Value(serviceMutationExecutionTrackerKey{}).(*serviceMutationExecutionTracker)
	if tracker == nil || tracker.manager == nil || tracker.runtime == nil {
		return errors.New("DNS zone V3 publication requires a durable execution tracker")
	}
	m, runtime := tracker.manager, tracker.runtime
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.healthErrorLocked(); err != nil {
		return err
	}
	job := runtime.job
	if m.active != runtime || runtime.steps != 1 || job == nil ||
		(job.Status != serviceMutationStatusRunning &&
			job.Status != serviceMutationStatusCancelling) ||
		job.WorkerPID != 0 ||
		job.Kind != "dns_zone_sync" || job.Target != domain ||
		job.PackageName != qualifier {
		return errors.New("DNS zone V3 terminal publication lost its exact mutation identity")
	}
	appliedPhase, err := formatDNSZoneSyncV3Phase(
		dnsZoneSyncV3Applied, job.RequestID, domain, qualifier,
	)
	if err != nil || (!runtime.dnsZoneSyncV3Recovery &&
		(runtime.dnsZoneSyncV3AppliedPhase != appliedPhase ||
			job.Phase != appliedPhase)) {
		return errors.New("DNS zone V3 terminal publication lacks exact local commit authority")
	}
	phase, err := formatDNSZoneSyncV3PublishedPhase(job.RequestID, domain, qualifier)
	if err != nil {
		return err
	}
	runtime.dnsZoneSyncPublishedPhase = phase
	if err := m.finishRuntimeTerminalLocked(runtime, true, phase, "", ""); err != nil {
		if m.active == runtime {
			return m.poisonLocked(fmt.Errorf("persist terminal DNS zone V3 receipt: %w", err))
		}
		return err
	}
	return nil
}

func (m *serviceMutationManager) recoverPersistedDNSZoneSyncV3Locked(
	job *ServiceMutationJob,
	lock *serviceMutationFileLock,
) (bool, error) {
	if job == nil || job.Kind != "dns_zone_sync" ||
		!mutationpayload.ValidDNSZoneSyncV3Qualifier(job.PackageName) {
		return false, nil
	}
	if !serviceMutationCanonicalFQDN(job.Target) {
		m.poisonLock = lock
		return true, m.poisonLocked(errors.New("active DNS zone V3 mutation has an invalid target"))
	}
	priorCommitted := false
	if strings.HasPrefix(job.Phase, dnsZoneSyncV3CommitPhasePrefix) {
		state, requestID, domain, qualifier, phaseErr :=
			parseDNSZoneSyncV3Phase(job.Phase)
		if phaseErr != nil ||
			(state != dnsZoneSyncV3Applied && state != dnsZoneSyncV3Recovering) ||
			requestID != job.RequestID || domain != job.Target ||
			qualifier != job.PackageName {
			m.poisonLock = lock
			return true, m.poisonLocked(errors.New(
				"active DNS zone V3 recovery has an invalid durable phase",
			))
		}
		priorCommitted = true
	}
	if serviceMutationWorkerMatches(job.WorkerPID, job.WorkerStarted) {
		before := cloneServiceMutationLedger(m.ledger)
		job.Status = serviceMutationStatusOrphaned
		if !priorCommitted {
			job.Phase = "waiting_for_orphaned_process"
		}
		job.ErrorCode = "agent_restart_worker_alive"
		job.ErrorMessage = "The previous DNS zone V3 worker is still alive."
		job.UpdatedAt = m.now()
		writeErr := m.persistLedgerMutationLocked(before)
		if m.poisoned != nil {
			m.poisonLock = lock
			return true, writeErr
		}
		return true, errors.Join(writeErr, lock.Close())
	}

	binding := transport.ServiceMutationBinding{
		MutationRequestID: job.RequestID,
		MutationOwnerID:   job.OwnerID,
	}
	verifyCtx, cancel := context.WithTimeout(context.Background(), dnsZoneSyncFinalizeTimeout)
	m.mu.Unlock()
	exact, verifyErr := agentDNSEngineBackend.RecoverZone(
		verifyCtx, job.Target, job.PackageName, binding,
	)
	cancel()
	m.mu.Lock()
	if verifyErr != nil {
		var pendingErr *dnsZoneV3RecoveryPendingError
		if errors.As(verifyErr, &pendingErr) {
			return true, m.finishPersistedDNSZoneSyncV3PendingLocked(job, lock)
		}
		m.poisonLock = lock
		return true, m.poisonLocked(fmt.Errorf("recover DNS zone V3 host receipt: %w", verifyErr))
	}
	if !exact {
		if priorCommitted {
			m.poisonLock = lock
			return true, m.poisonLocked(errors.New(
				"recovering DNS zone V3 mutation lost its exact host receipt",
			))
		}
		writeErr := m.finishPersistedOrphanLocked(
			job,
			"agent_restarted_before_dns_zone_v3_commit",
			"The agent restarted before the DNS zone V3 publication reached an exact host receipt.",
		)
		if m.poisoned != nil {
			m.poisonLock = lock
			return true, writeErr
		}
		return true, errors.Join(writeErr, lock.Close())
	}
	phase, err := formatDNSZoneSyncV3PublishedPhase(job.RequestID, job.Target, job.PackageName)
	if err != nil {
		m.poisonLock = lock
		return true, m.poisonLocked(err)
	}
	before := cloneServiceMutationLedger(m.ledger)
	now := m.now()
	job.Status = serviceMutationStatusSucceeded
	job.Phase = phase
	job.ErrorCode = ""
	job.ErrorMessage = ""
	job.UpdatedAt = now
	job.FinishedAt = now
	job.LeaseExpiresAt = time.Time{}
	job.WorkerPID = 0
	job.WorkerStarted = ""
	job.WorkerCommand = ""
	m.ledger.ActiveRequestID = ""
	writeErr := m.persistLedgerMutationProtectedLocked(before, job.RequestID)
	if m.poisoned != nil {
		m.poisonLock = lock
		return true, writeErr
	}
	m.trimHistoryLocked(job.RequestID)
	return true, errors.Join(writeErr, lock.Close())
}

func (m *serviceMutationManager) finishPersistedDNSZoneSyncV3PendingLocked(
	job *ServiceMutationJob,
	lock *serviceMutationFileLock,
) error {
	if job == nil || lock == nil || job.Kind != "dns_zone_sync" ||
		!serviceMutationCanonicalFQDN(job.Target) ||
		!mutationpayload.ValidDNSZoneSyncV3Qualifier(job.PackageName) {
		m.poisonLock = lock
		return m.poisonLocked(errors.New(
			"persisted DNS zone V3 pending identity is invalid",
		))
	}
	phase, err := formatDNSZoneSyncV3Phase(
		dnsZoneSyncV3PropagationPending,
		job.RequestID,
		job.Target,
		job.PackageName,
	)
	if err != nil {
		m.poisonLock = lock
		return m.poisonLocked(err)
	}
	before := cloneServiceMutationLedger(m.ledger)
	now := m.now()
	job.Status = serviceMutationStatusPending
	job.Phase = phase
	job.ErrorCode = "dns_zone_v3_propagation_pending"
	job.ErrorMessage =
		"The exact local DNS publication is waiting for paired propagation recovery."
	job.UpdatedAt = now
	job.FinishedAt = now
	job.LeaseExpiresAt = time.Time{}
	job.WorkerPID = 0
	job.WorkerStarted = ""
	job.WorkerCommand = ""
	m.ledger.ActiveRequestID = ""
	writeErr := m.persistLedgerMutationLocked(before)
	if m.poisoned != nil {
		m.poisonLock = lock
		return writeErr
	}
	closeErr := lock.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}

func (m *serviceMutationManager) recoverPersistedCommittedDNSEngineSwitchLocked(
	lock *serviceMutationFileLock,
) (bool, error) {
	journal, manifest, exists, err :=
		m.exactIdleCommittedDNSEngineSwitchLocked(nil)
	if err != nil {
		m.poisonLock = lock
		return true, m.poisonLocked(err)
	}
	if !exists {
		return false, nil
	}
	binding := switchJournalBinding(journal)
	recoveryCtx, cancel := context.WithTimeout(
		context.Background(), dnsEngineSwitchRecoveryLimit,
	)
	m.mu.Unlock()
	outcome, recoveryErr := agentDNSEngineBackend.RecoverSwitch(
		recoveryCtx, journal.TargetEngine,
		journal.ManifestQualifier, binding,
	)
	cancel()
	m.mu.Lock()
	if recoveryErr != nil {
		m.poisonLock = lock
		return true, m.poisonLocked(fmt.Errorf(
			"recover exact committed DNS engine switch during startup: %w",
			recoveryErr,
		))
	}
	if outcome != dnsEngineSwitchRecoveryCommitted &&
		outcome != dnsEngineSwitchRecoveryFinalized {
		m.poisonLock = lock
		return true, m.poisonLocked(errors.New(
			"exact committed DNS engine switch did not remain committed during startup",
		))
	}
	_, verifiedManifest, stillExists, err :=
		m.exactIdleCommittedDNSEngineSwitchLocked(&journal)
	if err != nil {
		m.poisonLock = lock
		return true, m.poisonLocked(err)
	}
	if !stillExists {
		m.poisonLock = lock
		return true, m.poisonLocked(errors.New(
			"committed DNS engine journal disappeared before startup finalization",
		))
	}
	if !reflect.DeepEqual(manifest, verifiedManifest) {
		m.poisonLock = lock
		return true, m.poisonLocked(errors.New(
			"committed DNS engine manifest changed before startup finalization",
		))
	}
	finalizeCtx, finalizeCancel := context.WithTimeout(
		context.Background(), dnsEngineSwitchRecoveryLimit,
	)
	m.mu.Unlock()
	finalizeErr := agentDNSEngineBackend.FinalizeSwitch(
		finalizeCtx, journal.TargetEngine,
		journal.ManifestQualifier, binding,
	)
	finalizeCancel()
	m.mu.Lock()
	if finalizeErr != nil {
		m.poisonLock = lock
		return true, m.poisonLocked(fmt.Errorf(
			"finalize exact committed DNS engine switch during startup: %w",
			finalizeErr,
		))
	}
	if err := m.reproveFinalizedCommittedDNSEngineSwitchLocked(
		journal, manifest,
	); err != nil {
		m.poisonLock = lock
		return true, m.poisonLocked(err)
	}
	if err := m.persistFinalizedDNSEngineSwitchReceiptAfterHostReleaseLocked(
		lock, journal, manifest,
	); err != nil {
		return true, m.poisonLocked(fmt.Errorf(
			"publish committed DNS recovery receipt after host lock release: %w", err,
		))
	}
	return true, nil
}

func (m *serviceMutationManager) exactIdleCommittedDNSEngineSwitchLocked(
	expected *dnsEngineSwitchJournal,
) (
	dnsEngineSwitchJournal,
	mutationpayload.DNSEngineSwitchManifestCommitment,
	bool,
	error,
) {
	if err := m.reloadLedgerUnderHostLockLocked(); err != nil {
		return dnsEngineSwitchJournal{},
			mutationpayload.DNSEngineSwitchManifestCommitment{},
			false, fmt.Errorf("reload service mutation ledger before committed DNS recovery: %w", err)
	}
	journalPath := filepath.Join(
		filepath.Dir(m.ledgerPath), dnsEngineSwitchJournalFile,
	)
	journal, exists, err := readDNSEngineSwitchJournalAt(journalPath)
	if err != nil {
		return dnsEngineSwitchJournal{},
			mutationpayload.DNSEngineSwitchManifestCommitment{},
			false, fmt.Errorf("read committed DNS engine journal during startup: %w", err)
	}
	if !exists {
		return dnsEngineSwitchJournal{},
			mutationpayload.DNSEngineSwitchManifestCommitment{},
			false, nil
	}
	if expected != nil {
		if !reflect.DeepEqual(*expected, journal) {
			return dnsEngineSwitchJournal{},
				mutationpayload.DNSEngineSwitchManifestCommitment{},
				true, errors.New("committed DNS engine journal changed during startup recovery")
		}
	}
	if err := validateDNSEngineSwitchJournal(journal); err != nil {
		return dnsEngineSwitchJournal{},
			mutationpayload.DNSEngineSwitchManifestCommitment{},
			true, err
	}
	if journal.Phase != dnsSwitchPhaseCommitted {
		return dnsEngineSwitchJournal{},
			mutationpayload.DNSEngineSwitchManifestCommitment{},
			true, errors.New("idle service ledger has a non-committed DNS engine journal")
	}
	manifest, err := switchJournalManifest(journal)
	if err != nil {
		return dnsEngineSwitchJournal{},
			mutationpayload.DNSEngineSwitchManifestCommitment{},
			true, err
	}
	if err := exactRecoverableIdleCommittedDNSEngineLedger(
		m.ledger, journal, manifest,
	); err != nil {
		return dnsEngineSwitchJournal{},
			mutationpayload.DNSEngineSwitchManifestCommitment{},
			true, err
	}
	return journal, manifest, true, nil
}

func (m *serviceMutationManager) reproveFinalizedCommittedDNSEngineSwitchLocked(
	journal dnsEngineSwitchJournal,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
) error {
	if err := m.reloadLedgerUnderHostLockLocked(); err != nil {
		return fmt.Errorf(
			"reload service mutation ledger after committed DNS recovery: %w",
			err,
		)
	}
	if err := exactRecoverableIdleCommittedDNSEngineLedger(
		m.ledger, journal, manifest,
	); err != nil {
		return err
	}
	journalPath := filepath.Join(
		filepath.Dir(m.ledgerPath), dnsEngineSwitchJournalFile,
	)
	_, journalExists, err := readDNSEngineSwitchJournalAt(journalPath)
	if err != nil {
		return fmt.Errorf(
			"verify committed DNS journal removal during startup: %w",
			err,
		)
	}
	if journalExists {
		return errors.New(
			"committed DNS engine journal remains after startup finalization",
		)
	}
	return nil
}

func exactRecoverableIdleCommittedDNSEngineLedger(
	ledger serviceMutationLedger,
	journal dnsEngineSwitchJournal,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
) error {
	if err := exactSucceededSignedUpdateDNSEngineLedger(
		ledger, journal, manifest,
	); err == nil {
		return nil
	}
	if err := validateServiceMutationLedger(&ledger); err != nil {
		return fmt.Errorf("validate recoverable DNS engine ledger: %w", err)
	}
	if ledger.ActiveRequestID != "" {
		return errors.New("recoverable DNS engine ledger unexpectedly has an active request")
	}
	request := signedUpdateRollbackEvidenceRequest(journal, manifest)
	if !exactFailedDNSEngineEvidenceJob(
		ledger.Jobs[journal.MutationRequestID], request, manifest,
	) {
		return errors.New("committed DNS engine journal lacks an exact recoverable terminal receipt")
	}
	return nil
}

func (m *serviceMutationManager) exactActiveCommittedDNSEngineSwitchLocked(
	journal dnsEngineSwitchJournal,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
) error {
	if err := m.reloadLedgerUnderHostLockLocked(); err != nil {
		return fmt.Errorf("reload active DNS engine ledger during recovery: %w", err)
	}
	if err := validateServiceMutationLedger(&m.ledger); err != nil {
		return fmt.Errorf("validate active DNS engine ledger during recovery: %w", err)
	}
	job := m.ledger.Jobs[journal.MutationRequestID]
	if m.ledger.ActiveRequestID != journal.MutationRequestID ||
		(!exactActiveDNSEngineSwitchJob(
			job,
			journal.MutationRequestID,
			journal.MutationOwnerID,
			manifest.TargetEngine,
			manifest.Qualifier,
		) && !exactExpiredCancellingDNSEngineSwitchJob(
			job,
			journal.MutationRequestID,
			journal.MutationOwnerID,
			manifest.TargetEngine,
			manifest.Qualifier,
			m.now(),
		)) {
		return errors.New("committed DNS engine recovery lost its exact active ledger identity")
	}
	return nil
}

func exactFinalizedDNSEngineSwitchLedger(
	ledger serviceMutationLedger,
	journal dnsEngineSwitchJournal,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
) error {
	if err := validateServiceMutationLedger(&ledger); err != nil {
		return fmt.Errorf("validate finalized DNS engine ledger: %w", err)
	}
	if ledger.ActiveRequestID != "" {
		return errors.New("finalized DNS engine ledger has an active request")
	}
	job := ledger.Jobs[journal.MutationRequestID]
	wantPhase, err := formatDNSEngineSwitchFinalizedPhase(
		journal.MutationRequestID, manifest.Qualifier,
	)
	if err != nil {
		return err
	}
	if job == nil || job.RequestID != journal.MutationRequestID ||
		job.OwnerID != journal.MutationOwnerID ||
		job.Kind != "dns_engine_switch" ||
		job.Target != string(manifest.TargetEngine) ||
		job.PackageName != manifest.Qualifier ||
		job.Status != serviceMutationStatusSucceeded || job.Phase != wantPhase ||
		job.Attempt <= 0 || job.StartedAt.IsZero() || job.UpdatedAt.IsZero() ||
		job.DeadlineAt.IsZero() || job.FinishedAt.IsZero() ||
		job.UpdatedAt.Before(job.StartedAt) ||
		job.DeadlineAt.Before(job.StartedAt) ||
		job.FinishedAt.Before(job.StartedAt) ||
		!job.UpdatedAt.Equal(job.FinishedAt) ||
		!job.LeaseExpiresAt.IsZero() || job.WorkerPID != 0 ||
		strings.TrimSpace(job.WorkerStarted) != "" ||
		strings.TrimSpace(job.WorkerCommand) != "" ||
		strings.TrimSpace(job.ErrorCode) != "" ||
		strings.TrimSpace(job.ErrorMessage) != "" {
		return errors.New("DNS engine ledger lacks its exact finalized receipt")
	}
	return nil
}

func (m *serviceMutationManager) persistFinalizedDNSEngineSwitchReceiptLocked(
	journal dnsEngineSwitchJournal,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
) error {
	job := m.ledger.Jobs[journal.MutationRequestID]
	if job == nil || job.RequestID != journal.MutationRequestID ||
		job.OwnerID != journal.MutationOwnerID ||
		job.Kind != "dns_engine_switch" ||
		job.Target != string(manifest.TargetEngine) ||
		job.PackageName != manifest.Qualifier {
		return errors.New("finalized DNS engine receipt lost its exact ledger identity")
	}
	phase, err := formatDNSEngineSwitchFinalizedPhase(
		journal.MutationRequestID, manifest.Qualifier,
	)
	if err != nil {
		return err
	}
	before := cloneServiceMutationLedger(m.ledger)
	now := m.now()
	job.Status = serviceMutationStatusSucceeded
	job.Phase = phase
	job.ErrorCode = ""
	job.ErrorMessage = ""
	job.UpdatedAt = now
	job.FinishedAt = now
	job.LeaseExpiresAt = time.Time{}
	job.WorkerPID = 0
	job.WorkerStarted = ""
	job.WorkerCommand = ""
	m.ledger.ActiveRequestID = ""
	if err := m.persistLedgerMutationProtectedLocked(
		before, journal.MutationRequestID,
	); err != nil {
		return fmt.Errorf("persist finalized DNS engine receipt: %w", err)
	}
	durable, err := m.loadLedgerFromDisk()
	if err != nil {
		return fmt.Errorf("reread finalized DNS engine receipt: %w", err)
	}
	if !reflect.DeepEqual(durable, m.ledger) {
		return errors.New("finalized DNS engine receipt changed after publication")
	}
	return exactFinalizedDNSEngineSwitchLedger(durable, journal, manifest)
}

func (m *serviceMutationManager) persistFinalizedDNSEngineSwitchReceiptAfterHostReleaseLocked(
	lock *serviceMutationFileLock,
	journal dnsEngineSwitchJournal,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
) error {
	if lock == nil {
		return errors.New("DNS engine finalization lost its host and publication locks")
	}
	publicationLock, hostCloseErr := lock.closeHostRetainingPublication()
	if hostCloseErr != nil {
		if publicationLock != nil {
			hostCloseErr = errors.Join(hostCloseErr, publicationLock.Close())
		}
		return fmt.Errorf("release DNS engine host lock before receipt publication: %w", hostCloseErr)
	}
	if publicationLock == nil {
		return errors.New("DNS engine finalization lacks its ledger publication lock")
	}
	persistErr := m.persistFinalizedDNSEngineSwitchReceiptLocked(journal, manifest)
	publicationCloseErr := publicationLock.Close()
	if persistErr != nil {
		return errors.Join(persistErr, publicationCloseErr)
	}
	if publicationCloseErr != nil {
		return fmt.Errorf(
			"release DNS engine ledger publication lock after receipt readback: %w",
			publicationCloseErr,
		)
	}
	return nil
}

// recoverPersistedDNSEngineSwitchLocked reconciles the durable host switch
// journal before the common orphan path is allowed to discard the mutation.
// The caller owns m.mu and the common host mutation lock. An ambiguous host
// result poisons the manager and deliberately retains that lock.
func (m *serviceMutationManager) recoverPersistedDNSEngineSwitchLocked(
	job *ServiceMutationJob,
	lock *serviceMutationFileLock,
) (bool, error) {
	if job == nil || job.Kind != "dns_engine_switch" {
		return false, nil
	}
	target := transport.DNSEngine(job.Target)
	if !transport.ValidDNSEngine(target) || !mutationpayload.ValidDNSEngineSwitchQualifier(job.PackageName) {
		m.poisonLock = lock
		return true, m.poisonLocked(errors.New("active DNS engine switch has an invalid durable identity"))
	}
	if serviceMutationWorkerMatches(job.WorkerPID, job.WorkerStarted) {
		before := cloneServiceMutationLedger(m.ledger)
		job.Status = serviceMutationStatusOrphaned
		job.Phase = "waiting_for_orphaned_process"
		job.ErrorCode = "agent_restart_worker_alive"
		job.ErrorMessage = "The previous DNS engine switch worker is still alive."
		job.UpdatedAt = m.now()
		writeErr := m.persistLedgerMutationLocked(before)
		if m.poisoned != nil {
			m.poisonLock = lock
			return true, writeErr
		}
		return true, errors.Join(writeErr, lock.Close())
	}
	binding := transport.ServiceMutationBinding{
		MutationRequestID: job.RequestID,
		MutationOwnerID:   job.OwnerID,
	}
	recoveryCtx, cancel := context.WithTimeout(context.Background(), dnsEngineSwitchRecoveryLimit)
	m.mu.Unlock()
	outcome, recoveryErr := agentDNSEngineBackend.RecoverSwitch(
		recoveryCtx, target, job.PackageName, binding,
	)
	cancel()
	m.mu.Lock()
	if recoveryErr != nil {
		m.poisonLock = lock
		return true, m.poisonLocked(fmt.Errorf("recover DNS engine switch host transaction: %w", recoveryErr))
	}
	if outcome == dnsEngineSwitchRecoveryFinalized {
		journal := dnsEngineSwitchJournal{
			MutationRequestID: job.RequestID,
			MutationOwnerID:   job.OwnerID,
			TargetEngine:      target,
			ManifestQualifier: job.PackageName,
		}
		manifest := mutationpayload.DNSEngineSwitchManifestCommitment{
			TargetEngine: target,
			Qualifier:    job.PackageName,
		}
		if err := m.exactActiveCommittedDNSEngineSwitchLocked(
			journal, manifest,
		); err != nil {
			m.poisonLock = lock
			return true, m.poisonLocked(err)
		}
		if err := m.persistFinalizedDNSEngineSwitchReceiptAfterHostReleaseLocked(
			lock, journal, manifest,
		); err != nil {
			return true, m.poisonLocked(fmt.Errorf(
				"publish already-finalized DNS engine recovery receipt: %w", err,
			))
		}
		return true, nil
	}
	if outcome != dnsEngineSwitchRecoveryCommitted {
		code := "agent_restarted_before_dns_engine_switch_commit"
		message := "The agent restarted before the DNS engine switch reached a verified target."
		if outcome == dnsEngineSwitchRecoveryRolledBack {
			code = "dns_engine_switch_rolled_back_after_restart"
			message = "The interrupted DNS engine switch was rolled back to the verified previous state."
		} else if outcome != dnsEngineSwitchRecoveryAbsent {
			m.poisonLock = lock
			return true, m.poisonLocked(errors.New("DNS engine switch recovery returned an unsupported outcome"))
		}
		writeErr := m.finishPersistedOrphanLocked(job, code, message)
		if m.poisoned != nil {
			m.poisonLock = lock
			return true, writeErr
		}
		return true, errors.Join(writeErr, lock.Close())
	}

	journalPath := filepath.Join(
		filepath.Dir(m.ledgerPath), dnsEngineSwitchJournalFile,
	)
	journal, exists, err := readDNSEngineSwitchJournalAt(journalPath)
	if err != nil {
		m.poisonLock = lock
		return true, m.poisonLocked(fmt.Errorf(
			"read committed DNS engine journal after startup recovery: %w",
			err,
		))
	}
	if !exists {
		m.poisonLock = lock
		return true, m.poisonLocked(errors.New(
			"committed DNS engine recovery has no durable journal",
		))
	}
	if err := validateDNSEngineSwitchJournal(journal); err != nil {
		m.poisonLock = lock
		return true, m.poisonLocked(err)
	}
	if journal.Phase != dnsSwitchPhaseCommitted ||
		!exactSwitchJournalIdentity(
			journal, target, job.PackageName, binding,
		) {
		m.poisonLock = lock
		return true, m.poisonLocked(errors.New(
			"committed DNS engine recovery journal lost its exact active mutation identity",
		))
	}
	manifest, err := switchJournalManifest(journal)
	if err != nil {
		m.poisonLock = lock
		return true, m.poisonLocked(err)
	}
	if err := m.exactActiveCommittedDNSEngineSwitchLocked(
		journal, manifest,
	); err != nil {
		m.poisonLock = lock
		return true, m.poisonLocked(err)
	}

	// Finalization changes ownership and removes the committed journal. Keep the
	// same host-lock OFD through that handoff and its durable reproof so no
	// concurrent mutation can observe or replace the intermediate state.
	finalizeCtx, finalizeCancel := context.WithTimeout(context.Background(), dnsEngineSwitchRecoveryLimit)
	m.mu.Unlock()
	finalizeErr := agentDNSEngineBackend.FinalizeSwitch(
		finalizeCtx, target, job.PackageName, binding,
	)
	finalizeCancel()
	m.mu.Lock()
	if finalizeErr != nil {
		m.poisonLock = lock
		return true, m.poisonLocked(fmt.Errorf(
			"finalize recovered committed DNS engine switch: %w",
			finalizeErr,
		))
	}
	if err := m.exactActiveCommittedDNSEngineSwitchLocked(
		journal, manifest,
	); err != nil {
		m.poisonLock = lock
		return true, m.poisonLocked(err)
	}
	_, journalExists, err := readDNSEngineSwitchJournalAt(journalPath)
	if err != nil {
		m.poisonLock = lock
		return true, m.poisonLocked(fmt.Errorf(
			"verify recovered DNS engine journal removal: %w", err,
		))
	}
	if journalExists {
		m.poisonLock = lock
		return true, m.poisonLocked(errors.New(
			"recovered DNS engine journal remains after finalization",
		))
	}
	if err := m.persistFinalizedDNSEngineSwitchReceiptAfterHostReleaseLocked(
		lock, journal, manifest,
	); err != nil {
		return true, m.poisonLocked(fmt.Errorf(
			"publish recovered DNS engine receipt after host lock release: %w", err,
		))
	}
	return true, nil
}

// SwitchDNSEngineV1 stages the complete target snapshot before touching the
// active port-53 authority. The host backend owns service/config rollback; the
// terminal phase below is published only after exact target verification.
func (a *Agent) SwitchDNSEngineV1(request *SwitchDNSEngineV1Request, response *SwitchDNSEngineV1Response) error {
	if response == nil {
		return errors.New("DNS engine switch response is required")
	}
	*response = SwitchDNSEngineV1Response{}
	if request == nil {
		response.Error = "DNS engine switch request is required"
		return nil
	}
	commitment, err := mutationpayload.CanonicalDNSEngineSwitchManifestWithPairIdentity(
		request.Mode,
		request.SourceEngine,
		request.TargetEngine,
		request.SourceEpoch,
		request.TargetEpoch,
		request.SourceRevision,
		request.Topology,
		request.PairRole,
		request.LocalIP,
		request.LocalNS,
		request.PeerIP,
		request.PeerNS,
		request.Zones,
	)
	if err != nil {
		response.Error = err.Error()
		return nil
	}
	if request.ManifestQualifier != commitment.Qualifier ||
		request.SnapshotBytes != commitment.SnapshotBytes ||
		!equalDNSEngineSwitchWireZones(request.Zones, commitment.Zones) {
		response.Error = "DNS engine switch request is not the exact canonical manifest"
		return nil
	}
	ctx, finishStep, err := a.requiredServiceMutationStep(
		request.ServiceMutationBinding,
		newServiceMutationStepClaim(
			serviceMutationStepSwitchDNSEngine,
			string(commitment.TargetEngine),
			commitment.Qualifier,
			commitment.Mode,
		),
	)
	if err != nil {
		response.Error = err.Error()
		return nil
	}
	defer finishStep()

	// Enter the critical interval before the backend can durably commit its
	// journal. Lease expiry, heartbeat, cancellation, and generic completion
	// must not race the host transaction between its last pre-commit check and
	// the committed journal write.
	if err := markDNSEngineSwitchFinalizing(
		ctx, commitment.TargetEngine, commitment.Qualifier,
		request.ServiceMutationBinding,
	); err != nil {
		poisonErr := poisonUnfinalizedDNSEngineSwitch(
			ctx, commitment.TargetEngine, commitment.Qualifier,
			request.ServiceMutationBinding, err,
		)
		log.Printf(
			"DNS engine switch critical admission failed closed: %v",
			errors.Join(err, poisonErr),
		)
		response.Error = "DNS engine switch could not safely enter its durable transaction"
		return nil
	}

	result, err := agentDNSEngineBackend.Switch(
		ctx, commitment, request.ServiceMutationBinding,
	)
	if err != nil {
		abortErr := releaseDNSEngineSwitchCriticalGuardAfterProvenAbort(
			ctx, commitment.TargetEngine, commitment.Qualifier,
			request.ServiceMutationBinding,
		)
		if abortErr != nil {
			poisonErr := poisonUnfinalizedDNSEngineSwitch(
				ctx, commitment.TargetEngine, commitment.Qualifier,
				request.ServiceMutationBinding, errors.Join(err, abortErr),
			)
			log.Printf(
				"DNS engine switch failure could not prove a pre-commit abort: %v",
				errors.Join(err, abortErr, poisonErr),
			)
			response.Error = "DNS engine switch outcome could not be verified; inspect the agent log"
			return nil
		}
		log.Printf("DNS engine switch to %s at epoch %d failed: %v", commitment.TargetEngine, commitment.TargetEpoch, err)
		response.Error = "DNS engine switch did not complete; inspect the agent log"
		return nil
	}
	if !result.Applied || result.ActiveEngine != commitment.TargetEngine ||
		result.ActiveEpoch != commitment.TargetEpoch ||
		result.AppliedZones != len(commitment.Zones) {
		resultErr := errors.New("DNS engine switch did not return the exact verified target receipt")
		abortErr := releaseDNSEngineSwitchCriticalGuardAfterProvenAbort(
			ctx, commitment.TargetEngine, commitment.Qualifier,
			request.ServiceMutationBinding,
		)
		if abortErr != nil {
			poisonErr := poisonUnfinalizedDNSEngineSwitch(
				ctx, commitment.TargetEngine, commitment.Qualifier,
				request.ServiceMutationBinding, errors.Join(resultErr, abortErr),
			)
			log.Printf(
				"DNS engine switch receipt mismatch could not prove a pre-commit abort: %v",
				errors.Join(resultErr, abortErr, poisonErr),
			)
			response.Error = "DNS engine switch outcome could not be verified; inspect the agent log"
			return nil
		}
		response.Error = "DNS engine switch did not return the exact verified target receipt"
		return nil
	}
	finalizeCtx, finalizeCancel := context.WithTimeout(
		context.Background(), dnsEngineSwitchRecoveryLimit,
	)
	finalizeErr := agentDNSEngineBackend.FinalizeSwitch(
		finalizeCtx, commitment.TargetEngine,
		commitment.Qualifier, request.ServiceMutationBinding,
	)
	finalizeCancel()
	if finalizeErr != nil {
		poisonErr := poisonUnfinalizedDNSEngineSwitch(
			ctx, commitment.TargetEngine, commitment.Qualifier,
			request.ServiceMutationBinding, finalizeErr,
		)
		log.Printf(
			"DNS engine switch finalization failed closed: %v",
			errors.Join(finalizeErr, poisonErr),
		)
		response.Error = "DNS engine switch reached its verified target but finalization did not complete"
		return nil
	}
	if err := publishFinalizedDNSEngineSwitchTerminal(
		ctx, commitment.TargetEngine, commitment.Qualifier,
		request.ServiceMutationBinding,
	); err != nil {
		log.Printf("DNS engine switch finalized receipt publication failed: %v", err)
		response.Error = "DNS engine switch finished but its durable receipt could not be reverified"
		return nil
	}
	*response = result
	return nil
}

// equalDNSEngineSwitchWireZones preserves the exact canonical comparison while
// accepting gob's wire representation for a zero-zone manifest: net/rpc
// decodes its explicit empty top-level slice as nil. Nil and empty commit to
// the same zero-zone manifest.
func equalDNSEngineSwitchWireZones(
	wire, canonical []transport.DNSEngineSwitchZoneSnapshot,
) bool {
	if len(wire) == 0 && len(canonical) == 0 {
		return true
	}
	return reflect.DeepEqual(wire, canonical)
}

func formatDNSEngineSwitchPublishedPhase(requestID, qualifier string) (string, error) {
	if !validMutationIdentity(requestID) ||
		!mutationpayload.ValidDNSEngineSwitchQualifier(qualifier) {
		return "", errors.New("invalid DNS engine switch terminal receipt identity")
	}
	return dnsEngineSwitchPublishedPhasePrefix + requestID + "/" + qualifier, nil
}

func formatDNSEngineSwitchFinalizedPhase(requestID, qualifier string) (string, error) {
	if !validMutationIdentity(requestID) ||
		!mutationpayload.ValidDNSEngineSwitchQualifier(qualifier) {
		return "", errors.New("invalid finalized DNS engine switch receipt identity")
	}
	return dnsEngineSwitchFinalizedPhasePrefix + requestID + "/" + qualifier, nil
}

func exactActiveDNSEngineSwitchRuntimeLocked(
	m *serviceMutationManager,
	runtime *serviceMutationRuntime,
	target transport.DNSEngine,
	qualifier string,
	binding transport.ServiceMutationBinding,
) error {
	if m.active != runtime || runtime.steps != 1 || runtime.lock == nil ||
		runtime.lock.publication == nil || runtime.job == nil {
		return errors.New("DNS engine switch lost its exact active runtime ownership")
	}
	job := runtime.job
	if !exactActiveDNSEngineSwitchJob(
		job,
		binding.MutationRequestID,
		binding.MutationOwnerID,
		target,
		qualifier,
	) ||
		m.ledger.ActiveRequestID != job.RequestID ||
		m.ledger.Jobs[job.RequestID] != job {
		return errors.New("DNS engine switch lost its exact active ledger identity")
	}
	durable, err := m.loadLedgerFromDisk()
	if err != nil {
		return fmt.Errorf("reread active DNS engine switch ledger: %w", err)
	}
	if !reflect.DeepEqual(durable, m.ledger) {
		return errors.New("active DNS engine switch ledger changed during finalization")
	}
	return nil
}

func markDNSEngineSwitchFinalizing(
	ctx context.Context,
	target transport.DNSEngine,
	qualifier string,
	binding transport.ServiceMutationBinding,
) error {
	tracker, _ := ctx.Value(serviceMutationExecutionTrackerKey{}).(*serviceMutationExecutionTracker)
	if tracker == nil || tracker.manager == nil || tracker.runtime == nil {
		return errors.New("DNS engine finalization requires a durable execution tracker")
	}
	m, runtime := tracker.manager, tracker.runtime
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.healthErrorLocked(); err != nil {
		return err
	}
	if err := exactActiveDNSEngineSwitchRuntimeLocked(
		m, runtime, target, qualifier, binding,
	); err != nil {
		return err
	}
	runtime.dnsEngineSwitchFinalizing = true
	return nil
}

// releaseDNSEngineSwitchCriticalGuardAfterProvenAbort is the only path that
// may clear the pre-commit critical guard. The host lock remains owned by the
// exact runtime while RecoverSwitch proves either clean absence or an exact
// rollback. Committed, finalized, unsupported, unreadable, or identity-drifted
// state is ambiguous and deliberately leaves the guard set for fail-closed
// poisoning by the caller.
func releaseDNSEngineSwitchCriticalGuardAfterProvenAbort(
	ctx context.Context,
	target transport.DNSEngine,
	qualifier string,
	binding transport.ServiceMutationBinding,
) error {
	tracker, _ := ctx.Value(serviceMutationExecutionTrackerKey{}).(*serviceMutationExecutionTracker)
	if tracker == nil || tracker.manager == nil || tracker.runtime == nil {
		return errors.New("DNS engine abort recovery requires a durable execution tracker")
	}
	m, runtime := tracker.manager, tracker.runtime
	m.mu.Lock()
	if err := m.healthErrorLocked(); err != nil {
		m.mu.Unlock()
		return err
	}
	if err := exactActiveDNSEngineSwitchRuntimeLocked(
		m, runtime, target, qualifier, binding,
	); err != nil {
		m.mu.Unlock()
		return err
	}
	if !runtime.dnsEngineSwitchFinalizing {
		m.mu.Unlock()
		return errors.New("DNS engine abort recovery lost its critical guard")
	}
	m.mu.Unlock()

	recoveryCtx, cancel := context.WithTimeout(
		context.Background(), dnsEngineSwitchRecoveryLimit,
	)
	outcome, recoveryErr := agentDNSEngineBackend.RecoverSwitch(
		recoveryCtx, target, qualifier, binding,
	)
	cancel()

	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.healthErrorLocked(); err != nil {
		return errors.Join(recoveryErr, err)
	}
	if err := exactActiveDNSEngineSwitchRuntimeLocked(
		m, runtime, target, qualifier, binding,
	); err != nil {
		return errors.Join(recoveryErr, err)
	}
	if !runtime.dnsEngineSwitchFinalizing {
		return errors.Join(
			recoveryErr,
			errors.New("DNS engine abort recovery critical guard changed during host reproof"),
		)
	}
	if recoveryErr != nil {
		return fmt.Errorf("reprove DNS engine switch abort: %w", recoveryErr)
	}
	if outcome != dnsEngineSwitchRecoveryAbsent &&
		outcome != dnsEngineSwitchRecoveryRolledBack {
		return fmt.Errorf(
			"DNS engine switch abort reproof returned ambiguous outcome %q",
			outcome,
		)
	}
	runtime.dnsEngineSwitchFinalizing = false
	return nil
}

// protectCommittedDNSEngineSwitchFinalizationLocked preserves the critical
// guard installed before the backend starts. The committed-journal fallback
// also protects transactions created by older binaries that did not install
// that guard before entering the host backend.
func (m *serviceMutationManager) protectCommittedDNSEngineSwitchFinalizationLocked(
	runtime *serviceMutationRuntime,
) (bool, error) {
	if runtime == nil || runtime.job == nil ||
		runtime.job.Kind != "dns_engine_switch" {
		return false, nil
	}
	job := runtime.job
	target := transport.DNSEngine(job.Target)
	binding := transport.ServiceMutationBinding{
		MutationRequestID: job.RequestID,
		MutationOwnerID:   job.OwnerID,
	}
	if runtime.dnsEngineSwitchFinalizing {
		if err := exactActiveDNSEngineSwitchRuntimeLocked(
			m, runtime, target, job.PackageName, binding,
		); err != nil {
			return false, err
		}
		return true, nil
	}
	journalPath := filepath.Join(
		filepath.Dir(m.ledgerPath), dnsEngineSwitchJournalFile,
	)
	journal, exists, err := readDNSEngineSwitchJournalAt(journalPath)
	if err != nil {
		return false, fmt.Errorf("read committed DNS engine journal: %w", err)
	}
	if !exists || journal.Phase != dnsSwitchPhaseCommitted {
		return false, nil
	}
	if !exactSwitchJournalIdentity(
		journal, target, job.PackageName, binding,
	) {
		return false, errors.New(
			"committed DNS engine journal differs from the active mutation",
		)
	}
	if err := exactActiveDNSEngineSwitchRuntimeLocked(
		m, runtime, target, job.PackageName, binding,
	); err != nil {
		return false, err
	}
	runtime.dnsEngineSwitchFinalizing = true
	return true, nil
}

func exactActiveDNSEngineSwitchJob(
	job *ServiceMutationJob,
	requestID, ownerID string,
	target transport.DNSEngine,
	qualifier string,
) bool {
	return job != nil &&
		job.RequestID == requestID &&
		job.OwnerID == ownerID &&
		job.Kind == "dns_engine_switch" &&
		job.Target == string(target) &&
		job.PackageName == qualifier &&
		job.Status == serviceMutationStatusRunning &&
		job.Phase == "leased" &&
		job.Attempt > 0 &&
		!job.StartedAt.IsZero() &&
		!job.UpdatedAt.IsZero() &&
		!job.LeaseExpiresAt.IsZero() &&
		!job.DeadlineAt.IsZero() &&
		job.FinishedAt.IsZero() &&
		!job.UpdatedAt.Before(job.StartedAt) &&
		!job.LeaseExpiresAt.Before(job.UpdatedAt) &&
		!job.DeadlineAt.Before(job.LeaseExpiresAt) &&
		job.WorkerPID == 0 &&
		strings.TrimSpace(job.WorkerStarted) == "" &&
		strings.TrimSpace(job.WorkerCommand) == "" &&
		strings.TrimSpace(job.ErrorCode) == "" &&
		strings.TrimSpace(job.ErrorMessage) == ""
}

func exactExpiredCancellingDNSEngineSwitchJob(
	job *ServiceMutationJob,
	requestID, ownerID string,
	target transport.DNSEngine,
	qualifier string,
	now time.Time,
) bool {
	return job != nil &&
		job.RequestID == requestID &&
		job.OwnerID == ownerID &&
		job.Kind == "dns_engine_switch" &&
		job.Target == string(target) &&
		job.PackageName == qualifier &&
		job.Status == serviceMutationStatusCancelling &&
		job.Phase == serviceMutationPhaseCancellingExpiredLease &&
		job.Attempt > 0 &&
		!job.StartedAt.IsZero() &&
		!job.UpdatedAt.IsZero() &&
		!job.LeaseExpiresAt.IsZero() &&
		!job.DeadlineAt.IsZero() &&
		job.FinishedAt.IsZero() &&
		!job.UpdatedAt.Before(job.StartedAt) &&
		!job.LeaseExpiresAt.Before(job.StartedAt) &&
		!job.UpdatedAt.Before(job.LeaseExpiresAt) &&
		!job.DeadlineAt.Before(job.LeaseExpiresAt) &&
		!now.Before(job.LeaseExpiresAt) &&
		job.WorkerPID == 0 &&
		strings.TrimSpace(job.WorkerStarted) == "" &&
		strings.TrimSpace(job.WorkerCommand) == "" &&
		job.ErrorCode == serviceMutationErrorLeaseExpired &&
		job.ErrorMessage == serviceMutationMessageLeaseExpired
}

func poisonUnfinalizedDNSEngineSwitch(
	ctx context.Context,
	target transport.DNSEngine,
	qualifier string,
	binding transport.ServiceMutationBinding,
	cause error,
) error {
	tracker, _ := ctx.Value(serviceMutationExecutionTrackerKey{}).(*serviceMutationExecutionTracker)
	if tracker == nil || tracker.manager == nil || tracker.runtime == nil {
		return errors.New("DNS engine switch poison requires a durable execution tracker")
	}
	m, runtime := tracker.manager, tracker.runtime
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.healthErrorLocked(); err != nil {
		return err
	}
	if err := exactActiveDNSEngineSwitchRuntimeLocked(
		m, runtime, target, qualifier, binding,
	); err != nil {
		cause = errors.Join(cause, fmt.Errorf(
			"reverify active DNS engine switch before poison: %w",
			err,
		))
	}
	m.poisonLock = runtime.lock
	return m.poisonLocked(fmt.Errorf(
		"finalize active DNS engine switch: %w", cause,
	))
}

func publishFinalizedDNSEngineSwitchTerminal(
	ctx context.Context,
	target transport.DNSEngine,
	qualifier string,
	binding transport.ServiceMutationBinding,
) error {
	tracker, _ := ctx.Value(serviceMutationExecutionTrackerKey{}).(*serviceMutationExecutionTracker)
	if tracker == nil || tracker.manager == nil || tracker.runtime == nil {
		return errors.New("DNS engine switch release requires a durable execution tracker")
	}
	m, runtime := tracker.manager, tracker.runtime
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.healthErrorLocked(); err != nil {
		return err
	}
	if err := exactActiveDNSEngineSwitchRuntimeLocked(
		m, runtime, target, qualifier, binding,
	); err != nil {
		m.poisonLock = runtime.lock
		return m.poisonLocked(fmt.Errorf("reverify finalized DNS engine switch: %w", err))
	}
	journalPath := filepath.Join(
		filepath.Dir(m.ledgerPath), dnsEngineSwitchJournalFile,
	)
	_, journalExists, err := readDNSEngineSwitchJournalAt(journalPath)
	if err != nil {
		return m.poisonLocked(fmt.Errorf(
			"verify finalized DNS engine journal removal: %w", err,
		))
	}
	if journalExists {
		m.poisonLock = runtime.lock
		return m.poisonLocked(errors.New(
			"finalized DNS engine journal remains before host lock release",
		))
	}
	journal := dnsEngineSwitchJournal{
		MutationRequestID: binding.MutationRequestID,
		MutationOwnerID:   binding.MutationOwnerID,
		TargetEngine:      target,
		ManifestQualifier: qualifier,
	}
	manifest := mutationpayload.DNSEngineSwitchManifestCommitment{
		TargetEngine: target,
		Qualifier:    qualifier,
	}
	persistErr := m.persistFinalizedDNSEngineSwitchReceiptAfterHostReleaseLocked(
		runtime.lock, journal, manifest,
	)
	runtime.lock = nil
	if persistErr != nil {
		return m.poisonLocked(fmt.Errorf(
			"persist finalized DNS engine switch receipt after host lock release: %w",
			persistErr,
		))
	}
	runtime.cancel()
	m.active = nil
	m.trimHistoryLocked(runtime.job.RequestID)
	return nil
}
