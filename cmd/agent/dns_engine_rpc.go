package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"reflect"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

type SyncDNSZoneV3Request = transport.SyncDNSZoneV3Request
type SyncDNSZoneV3Response = transport.SyncDNSZoneV3Response
type SwitchDNSEngineV1Request = transport.SwitchDNSEngineV1Request
type SwitchDNSEngineV1Response = transport.SwitchDNSEngineV1Response
type DNSBackendReadinessResponse = transport.DNSBackendReadinessResponse

type dnsEngineSwitchRecoveryOutcome string

const (
	dnsEngineSwitchRecoveryAbsent     dnsEngineSwitchRecoveryOutcome = "absent"
	dnsEngineSwitchRecoveryRolledBack dnsEngineSwitchRecoveryOutcome = "rolled-back"
	dnsEngineSwitchRecoveryCommitted  dnsEngineSwitchRecoveryOutcome = "committed"
)

const dnsEngineSwitchPublishedPhasePrefix = "commit/dns-engine-switch/v1/published/"

const dnsZoneSyncV3PublishedPhasePrefix = "commit/dns-zone-sync/v3/published/"

type dnsEngineBackend interface {
	Readiness(context.Context) ([]transport.DNSBackendRuntimeState, error)
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
	states, err := agentDNSEngineBackend.Readiness(context.Background())
	if err != nil {
		response.Error = "DNS backend readiness could not be verified"
		return nil
	}
	response.Engines = states
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

	response.Engine = request.Engine
	response.EngineEpoch = request.EngineEpoch
	generation, err := agentDNSEngineBackend.Sync(
		ctx, commitment, request.ServiceMutationBinding,
	)
	if err != nil {
		log.Printf("%s zone publication failed for %s at epoch %d: %v", commitment.Engine, commitment.Domain, commitment.EngineEpoch, err)
		response.Error = "DNS zone publication failed; inspect the agent log"
		return nil
	}
	if err := publishDNSZoneSyncV3Terminal(ctx, commitment.Domain, commitment.Qualifier); err != nil {
		log.Printf("DNS zone V3 terminal receipt publication failed: %v", err)
		response.Error = "DNS zone publication finished but its durable receipt could not be verified"
		return nil
	}
	response.Synced = true
	response.AppliedGeneration = commitment.DesiredGeneration
	_ = generation // Generation identity is retained in the immutable host receipt.
	return nil
}

func formatDNSZoneSyncV3PublishedPhase(requestID, domain, qualifier string) (string, error) {
	if !validMutationIdentity(requestID) ||
		!serviceMutationCanonicalFQDN(domain) ||
		!mutationpayload.ValidDNSZoneSyncV3Qualifier(qualifier) {
		return "", errors.New("invalid DNS zone V3 terminal receipt identity")
	}
	return dnsZoneSyncV3PublishedPhasePrefix + requestID + "/" + domain + "/" + qualifier, nil
}

func parseDNSZoneSyncV3PublishedPhase(value string) (requestID, domain, qualifier string, err error) {
	if !strings.HasPrefix(value, dnsZoneSyncV3PublishedPhasePrefix) {
		return "", "", "", errors.New("not a DNS zone V3 published phase")
	}
	remainder := strings.TrimPrefix(value, dnsZoneSyncV3PublishedPhasePrefix)
	requestID, remainder, found := strings.Cut(remainder, "/")
	if !found {
		return "", "", "", errors.New("invalid DNS zone V3 published phase")
	}
	domain, qualifier, found = strings.Cut(remainder, "/")
	if !found {
		return "", "", "", errors.New("invalid DNS zone V3 published phase")
	}
	canonical, formatErr := formatDNSZoneSyncV3PublishedPhase(requestID, domain, qualifier)
	if formatErr != nil || canonical != value {
		return "", "", "", errors.New("invalid DNS zone V3 published phase")
	}
	return requestID, domain, qualifier, nil
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
		job.Status != serviceMutationStatusRunning || job.WorkerPID != 0 ||
		job.Kind != "dns_zone_sync" || job.Target != domain ||
		job.PackageName != qualifier {
		return errors.New("DNS zone V3 terminal publication lost its exact mutation identity")
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
	if serviceMutationWorkerMatches(job.WorkerPID, job.WorkerStarted) {
		before := cloneServiceMutationLedger(m.ledger)
		job.Status = serviceMutationStatusOrphaned
		job.Phase = "waiting_for_orphaned_process"
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
		m.poisonLock = lock
		return true, m.poisonLocked(fmt.Errorf("recover DNS zone V3 host receipt: %w", verifyErr))
	}
	if !exact {
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
	writeErr := m.persistLedgerMutationLocked(before)
	if m.poisoned != nil {
		m.poisonLock = lock
		return true, writeErr
	}
	return true, errors.Join(writeErr, lock.Close())
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

	phase, err := formatDNSEngineSwitchPublishedPhase(job.RequestID, job.PackageName)
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
	writeErr := m.persistLedgerMutationLocked(before)
	if m.poisoned != nil {
		m.poisonLock = lock
		return true, writeErr
	}
	closeErr := lock.Close()
	if writeErr != nil || closeErr != nil {
		return true, errors.Join(writeErr, closeErr)
	}

	// Cleanup follows terminal ledger publication. A failure leaves the exact
	// committed journal for the next bounded reconciliation; it must not turn a
	// verified and durably published switch into a false client-side failure.
	finalizeCtx, finalizeCancel := context.WithTimeout(context.Background(), dnsEngineSwitchRecoveryLimit)
	m.mu.Unlock()
	finalizeErr := agentDNSEngineBackend.FinalizeSwitch(
		finalizeCtx, target, job.PackageName, binding,
	)
	finalizeCancel()
	m.mu.Lock()
	if finalizeErr != nil {
		log.Printf("DNS engine switch recovery journal cleanup deferred: %v", finalizeErr)
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
	commitment, err := mutationpayload.CanonicalDNSEngineSwitchManifest(
		request.Mode,
		request.SourceEngine,
		request.TargetEngine,
		request.SourceEpoch,
		request.TargetEpoch,
		request.SourceRevision,
		request.Topology,
		request.Zones,
	)
	if err != nil {
		response.Error = err.Error()
		return nil
	}
	if request.ManifestQualifier != commitment.Qualifier ||
		request.SnapshotBytes != commitment.SnapshotBytes ||
		!reflect.DeepEqual(request.Zones, commitment.Zones) {
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

	result, err := agentDNSEngineBackend.Switch(
		ctx, commitment, request.ServiceMutationBinding,
	)
	if err != nil {
		log.Printf("DNS engine switch to %s at epoch %d failed: %v", commitment.TargetEngine, commitment.TargetEpoch, err)
		response.Error = "DNS engine switch did not complete; inspect the agent log"
		return nil
	}
	if !result.Applied || result.ActiveEngine != commitment.TargetEngine ||
		result.ActiveEpoch != commitment.TargetEpoch ||
		result.AppliedZones != len(commitment.Zones) {
		response.Error = "DNS engine switch did not return the exact verified target receipt"
		return nil
	}
	if err := publishDNSEngineSwitchTerminal(
		ctx, commitment.TargetEngine, commitment.Qualifier,
	); err != nil {
		log.Printf("DNS engine switch terminal receipt publication failed: %v", err)
		response.Error = "DNS engine switch finished but its durable receipt could not be verified"
		return nil
	}
	if err := agentDNSEngineBackend.FinalizeSwitch(
		context.WithoutCancel(ctx), commitment.TargetEngine,
		commitment.Qualifier, request.ServiceMutationBinding,
	); err != nil {
		log.Printf("DNS engine switch journal cleanup deferred: %v", err)
	}
	*response = result
	return nil
}

func formatDNSEngineSwitchPublishedPhase(requestID, qualifier string) (string, error) {
	if !validMutationIdentity(requestID) ||
		!mutationpayload.ValidDNSEngineSwitchQualifier(qualifier) {
		return "", errors.New("invalid DNS engine switch terminal receipt identity")
	}
	return dnsEngineSwitchPublishedPhasePrefix + requestID + "/" + qualifier, nil
}

func publishDNSEngineSwitchTerminal(
	ctx context.Context,
	target transport.DNSEngine,
	qualifier string,
) error {
	tracker, _ := ctx.Value(serviceMutationExecutionTrackerKey{}).(*serviceMutationExecutionTracker)
	if tracker == nil || tracker.manager == nil || tracker.runtime == nil {
		return errors.New("DNS engine switch publication requires a durable execution tracker")
	}
	m, runtime := tracker.manager, tracker.runtime
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.healthErrorLocked(); err != nil {
		return err
	}
	job := runtime.job
	if m.active != runtime || runtime.steps != 1 || job == nil ||
		job.Status != serviceMutationStatusRunning || job.WorkerPID != 0 ||
		job.Kind != "dns_engine_switch" || job.Target != string(target) ||
		job.PackageName != qualifier {
		return errors.New("DNS engine switch terminal publication lost its exact mutation identity")
	}
	phase, err := formatDNSEngineSwitchPublishedPhase(job.RequestID, qualifier)
	if err != nil {
		return err
	}
	if err := m.finishRuntimeTerminalLocked(runtime, true, phase, "", ""); err != nil {
		if m.active == runtime {
			return m.poisonLocked(fmt.Errorf("persist terminal DNS engine switch receipt: %w", err))
		}
		return err
	}
	return nil
}
