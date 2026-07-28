package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	serviceMutationLedgerVersion = 1

	serviceMutationStatusRunning    = "running"
	serviceMutationStatusCancelling = "cancelling"
	serviceMutationStatusOrphaned   = "orphaned"
	serviceMutationStatusSucceeded  = "succeeded"
	serviceMutationStatusFailed     = "failed"

	serviceMutationLeaseDuration = 20 * time.Second
	serviceMutationOverallLimit  = 45 * time.Minute
	serviceMutationHistoryLimit  = 128
	serviceMutationLedgerMaxSize = 1 << 20
)

var (
	errServiceMutationBusy     = errors.New("another service mutation owns the host lease")
	errServiceMutationHostBusy = errors.New("the host package manager or mutation lock is busy")

	globalServiceMutationMu      sync.Mutex
	globalServiceMutationManager *serviceMutationManager
	globalServiceMutationErr     error
)

type ServiceMutationJob struct {
	RequestID      string    `json:"request_id"`
	OwnerID        string    `json:"owner_id"`
	Kind           string    `json:"kind"`
	Target         string    `json:"target"`
	PackageName    string    `json:"package_name,omitempty"`
	Status         string    `json:"status"`
	Phase          string    `json:"phase"`
	Attempt        int       `json:"attempt"`
	StartedAt      time.Time `json:"started_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	LeaseExpiresAt time.Time `json:"lease_expires_at"`
	DeadlineAt     time.Time `json:"deadline_at"`
	FinishedAt     time.Time `json:"finished_at,omitempty"`
	ErrorCode      string    `json:"error_code,omitempty"`
	ErrorMessage   string    `json:"error_message,omitempty"`
	WorkerPID      int       `json:"worker_pid,omitempty"`
	WorkerStarted  string    `json:"worker_started,omitempty"`
	WorkerCommand  string    `json:"worker_command,omitempty"`
}

type serviceMutationLedger struct {
	Version         int                            `json:"version"`
	ActiveRequestID string                         `json:"active_request_id,omitempty"`
	Jobs            map[string]*ServiceMutationJob `json:"jobs"`
}

type ServiceMutationBeginRequest struct {
	RequestID   string `json:"request_id"`
	OwnerID     string `json:"owner_id"`
	Kind        string `json:"kind"`
	Target      string `json:"target"`
	PackageName string `json:"package_name,omitempty"`
	Resume      bool   `json:"resume,omitempty"`
}

type ServiceMutationBinding struct {
	MutationRequestID string `json:"mutation_request_id,omitempty"`
	MutationOwnerID   string `json:"mutation_owner_id,omitempty"`
}

// ServiceMutationRequest is the common request envelope for privileged RPCs
// that do not otherwise need arguments.
// ServiceMutationRequest, başka argümana ihtiyaç duymayan ayrıcalıklı RPC'ler
// için ortak istek zarfıdır.
type ServiceMutationRequest struct {
	ServiceMutationBinding
}

type ServiceMutationHeartbeatRequest struct {
	RequestID string `json:"request_id"`
	OwnerID   string `json:"owner_id"`
	Phase     string `json:"phase,omitempty"`
}

type ServiceMutationStatusRequest struct {
	RequestID string `json:"request_id,omitempty"`
}

type ServiceMutationCancelRequest struct {
	RequestID      string `json:"request_id"`
	ExpectedOwner  string `json:"expected_owner"`
	Reason         string `json:"reason,omitempty"`
	FailureCode    string `json:"failure_code,omitempty"`
	FailureMessage string `json:"failure_message,omitempty"`
}

type ServiceMutationFinishRequest struct {
	RequestID   string `json:"request_id"`
	OwnerID     string `json:"owner_id"`
	Success     bool   `json:"success"`
	FailureCode string `json:"failure_code,omitempty"`
	Message     string `json:"message,omitempty"`
}

type ServiceMutationResponse struct {
	Job   *ServiceMutationJob `json:"job,omitempty"`
	Error string              `json:"error,omitempty"`
}

type serviceMutationRuntime struct {
	job    *ServiceMutationJob
	lock   *serviceMutationFileLock
	ctx    context.Context
	cancel context.CancelFunc
	stepMu sync.Mutex
	steps  int
}

type serviceMutationManager struct {
	mu sync.Mutex

	ledgerPath string
	lockPath   string
	ledger     serviceMutationLedger
	active     *serviceMutationRuntime

	now             func() time.Time
	leaseDuration   time.Duration
	overallDuration time.Duration
}

func serviceMutationStateDirectory() string {
	if value := strings.TrimSpace(os.Getenv("CELIKPANEL_AGENT_STATE_DIR")); value != "" {
		return value
	}
	return "/var/lib/celikpanel-agent"
}

func serviceMutationLockFile() string {
	if value := strings.TrimSpace(os.Getenv("CELIKPANEL_MUTATION_LOCK")); value != "" {
		return value
	}
	return "/run/celikpanel/service-mutation.lock"
}

func newServiceMutationManager(stateDir, lockPath string) (*serviceMutationManager, error) {
	if strings.TrimSpace(stateDir) == "" {
		stateDir = serviceMutationStateDirectory()
	}
	if strings.TrimSpace(lockPath) == "" {
		lockPath = serviceMutationLockFile()
	}
	manager := &serviceMutationManager{
		ledgerPath:      filepath.Join(stateDir, "service-mutations.json"),
		lockPath:        lockPath,
		now:             func() time.Time { return time.Now().UTC() },
		leaseDuration:   serviceMutationLeaseDuration,
		overallDuration: serviceMutationOverallLimit,
		ledger: serviceMutationLedger{
			Version: serviceMutationLedgerVersion,
			Jobs:    map[string]*ServiceMutationJob{},
		},
	}
	if err := manager.load(); err != nil {
		return nil, err
	}
	if err := manager.reconcilePersistedActive(); err != nil {
		return nil, err
	}
	return manager, nil
}

func agentServiceMutationManager() (*serviceMutationManager, error) {
	globalServiceMutationMu.Lock()
	defer globalServiceMutationMu.Unlock()
	if globalServiceMutationManager == nil && globalServiceMutationErr == nil {
		globalServiceMutationManager, globalServiceMutationErr = newServiceMutationManager("", "")
	}
	return globalServiceMutationManager, globalServiceMutationErr
}

func loadedAgentServiceMutationManager() *serviceMutationManager {
	globalServiceMutationMu.Lock()
	defer globalServiceMutationMu.Unlock()
	return globalServiceMutationManager
}

func (m *serviceMutationManager) load() error {
	stateDir := filepath.Dir(m.ledgerPath)
	if err := ensureSecureServiceMutationStateDirectory(stateDir); err != nil {
		return fmt.Errorf("secure service mutation state directory: %w", err)
	}
	raw, exists, err := readSecureServiceMutationLedger(
		m.ledgerPath,
		serviceMutationLedgerMaxSize,
	)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	ledger, err := decodeServiceMutationLedger(raw)
	if err != nil {
		return err
	}
	m.ledger = ledger
	return nil
}

func decodeServiceMutationLedger(raw []byte) (serviceMutationLedger, error) {
	var ledger serviceMutationLedger
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&ledger); err != nil {
		return serviceMutationLedger{}, fmt.Errorf("decode service mutation ledger: %w", err)
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return serviceMutationLedger{}, errors.New("service mutation ledger contains more than one JSON value")
		}
		return serviceMutationLedger{}, fmt.Errorf("decode service mutation ledger trailer: %w", err)
	}
	if ledger.Version != serviceMutationLedgerVersion || ledger.Jobs == nil {
		return serviceMutationLedger{}, errors.New("service mutation ledger has an unsupported schema")
	}
	canonical, err := json.Marshal(&ledger)
	if err != nil {
		return serviceMutationLedger{}, fmt.Errorf("canonicalize service mutation ledger: %w", err)
	}
	if !bytes.Equal(raw, canonical) {
		return serviceMutationLedger{}, errors.New("service mutation ledger is not canonical")
	}
	if ledger.ActiveRequestID != "" {
		job := ledger.Jobs[ledger.ActiveRequestID]
		if job == nil || !serviceMutationStatusActive(job.Status) {
			return serviceMutationLedger{}, errors.New("service mutation ledger active pointer is inconsistent")
		}
	}
	return ledger, nil
}

func (m *serviceMutationManager) reconcilePersistedActive() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ledger.ActiveRequestID == "" {
		return nil
	}
	job := m.ledger.Jobs[m.ledger.ActiveRequestID]
	if job == nil {
		return errors.New("service mutation ledger lost its active job")
	}
	if serviceMutationWorkerMatches(job.WorkerPID, job.WorkerStarted) {
		job.Status = serviceMutationStatusOrphaned
		job.Phase = "waiting_for_orphaned_process"
		job.ErrorCode = "agent_restart_worker_alive"
		job.ErrorMessage = "The previous privileged worker is still alive with the recorded process identity."
		job.UpdatedAt = m.now()
		return m.writeLocked()
	}
	busy, err := packageManagerMutationBusy()
	if err != nil {
		job.Status = serviceMutationStatusOrphaned
		job.Phase = "host_state_unverified"
		job.ErrorCode = "package_manager_probe_failed"
		job.ErrorMessage = "The agent could not prove that the previous host mutation stopped."
		job.UpdatedAt = m.now()
		return m.writeLocked()
	}
	lock, lockErr := acquireServiceMutationFileLock(m.lockPath)
	if busy || errors.Is(lockErr, errServiceMutationHostBusy) {
		job.Status = serviceMutationStatusOrphaned
		job.Phase = "waiting_for_orphaned_process"
		job.ErrorCode = "agent_restart_host_busy"
		job.ErrorMessage = "The previous agent exited while a trusted host mutation may still be running."
		job.UpdatedAt = m.now()
		return m.writeLocked()
	}
	if lockErr != nil {
		return lockErr
	}
	defer lock.Close()
	return m.finishPersistedOrphanLocked(
		job,
		"agent_restarted_before_completion",
		"The privileged agent restarted before the mutation reached a verified terminal state.",
	)
}

func (m *serviceMutationManager) tryResolvePersistedOrphan() error {
	m.mu.Lock()
	if m.active != nil || m.ledger.ActiveRequestID == "" {
		m.mu.Unlock()
		return nil
	}
	job := m.ledger.Jobs[m.ledger.ActiveRequestID]
	if job == nil || job.Status != serviceMutationStatusOrphaned {
		m.mu.Unlock()
		return nil
	}
	requestID := job.RequestID
	workerPID := job.WorkerPID
	workerStarted := job.WorkerStarted
	m.mu.Unlock()

	if serviceMutationWorkerMatches(workerPID, workerStarted) {
		return errServiceMutationHostBusy
	}
	busy, err := packageManagerMutationBusy()
	if err != nil {
		return fmt.Errorf("verify orphaned service mutation: %w", err)
	}
	if busy {
		return errServiceMutationHostBusy
	}
	lock, err := acquireServiceMutationFileLock(m.lockPath)
	if err != nil {
		return err
	}
	defer lock.Close()

	m.mu.Lock()
	defer m.mu.Unlock()
	job = m.ledger.Jobs[requestID]
	if m.active != nil || m.ledger.ActiveRequestID != requestID ||
		job == nil || job.Status != serviceMutationStatusOrphaned {
		return nil
	}
	return m.finishPersistedOrphanLocked(
		job,
		"agent_restarted_before_completion",
		"The previous privileged process is no longer running; the interrupted mutation may now be resumed.",
	)
}

func (m *serviceMutationManager) finishPersistedOrphanLocked(
	job *ServiceMutationJob,
	code, message string,
) error {
	now := m.now()
	job.Status = serviceMutationStatusFailed
	job.Phase = "interrupted"
	job.ErrorCode = code
	job.ErrorMessage = message
	job.UpdatedAt = now
	job.FinishedAt = now
	job.LeaseExpiresAt = time.Time{}
	m.ledger.ActiveRequestID = ""
	return m.writeLocked()
}

func serviceMutationStatusActive(status string) bool {
	return status == serviceMutationStatusRunning ||
		status == serviceMutationStatusCancelling ||
		status == serviceMutationStatusOrphaned
}

func validMutationIdentity(value string) bool {
	if len(value) != 32 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func newMutationOwnerID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}

func serviceMutationIdentityMatches(job *ServiceMutationJob, request *ServiceMutationBeginRequest) bool {
	return job != nil &&
		job.RequestID == request.RequestID &&
		job.Kind == request.Kind &&
		job.Target == request.Target &&
		job.PackageName == request.PackageName
}

func (m *serviceMutationManager) begin(request *ServiceMutationBeginRequest) (*ServiceMutationJob, error) {
	if request == nil || !validMutationIdentity(request.RequestID) ||
		!validMutationIdentity(request.OwnerID) ||
		strings.TrimSpace(request.Kind) == "" ||
		strings.TrimSpace(request.Target) == "" {
		return nil, errors.New("invalid service mutation identity")
	}
	if err := m.tryResolvePersistedOrphan(); err != nil &&
		!errors.Is(err, errServiceMutationHostBusy) {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ledger.ActiveRequestID != "" {
		job := m.ledger.Jobs[m.ledger.ActiveRequestID]
		if serviceMutationIdentityMatches(job, request) && job.OwnerID == request.OwnerID &&
			job.Status == serviceMutationStatusRunning {
			now := m.now()
			job.UpdatedAt = now
			job.LeaseExpiresAt = minMutationTime(now.Add(m.leaseDuration), job.DeadlineAt)
			if err := m.writeLocked(); err != nil {
				return nil, err
			}
			return cloneServiceMutationJob(job), nil
		}
		return cloneServiceMutationJob(job), errServiceMutationBusy
	}
	previous := m.ledger.Jobs[request.RequestID]
	if previous != nil {
		if !serviceMutationIdentityMatches(previous, request) {
			return cloneServiceMutationJob(previous), errors.New("request_id belongs to another service mutation")
		}
		if !request.Resume {
			return cloneServiceMutationJob(previous), nil
		}
		if previous.Status != serviceMutationStatusFailed {
			return cloneServiceMutationJob(previous), errors.New("only an interrupted failed mutation can be resumed")
		}
	}
	busy, err := packageManagerMutationBusy()
	if err != nil {
		return nil, fmt.Errorf("verify package manager lease: %w", err)
	}
	if busy {
		return nil, errServiceMutationHostBusy
	}
	lock, err := acquireServiceMutationFileLock(m.lockPath)
	if err != nil {
		return nil, err
	}
	now := m.now()
	attempt := 1
	startedAt := now
	if previous != nil {
		attempt = previous.Attempt + 1
		startedAt = previous.StartedAt
	}
	deadline := now.Add(m.overallDuration)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	job := &ServiceMutationJob{
		RequestID:      request.RequestID,
		OwnerID:        request.OwnerID,
		Kind:           request.Kind,
		Target:         request.Target,
		PackageName:    request.PackageName,
		Status:         serviceMutationStatusRunning,
		Phase:          "leased",
		Attempt:        attempt,
		StartedAt:      startedAt,
		UpdatedAt:      now,
		LeaseExpiresAt: minMutationTime(now.Add(m.leaseDuration), deadline),
		DeadlineAt:     deadline,
	}
	runtime := &serviceMutationRuntime{
		job: job, lock: lock, ctx: ctx, cancel: cancel,
	}
	m.ledger.ActiveRequestID = job.RequestID
	m.ledger.Jobs[job.RequestID] = job
	m.active = runtime
	if err := m.writeLocked(); err != nil {
		m.active = nil
		m.ledger.ActiveRequestID = ""
		cancel()
		_ = lock.Close()
		return nil, err
	}
	go m.watch(runtime)
	return cloneServiceMutationJob(job), nil
}

func minMutationTime(left, right time.Time) time.Time {
	if right.IsZero() || left.Before(right) {
		return left
	}
	return right
}

func (m *serviceMutationManager) watch(runtime *serviceMutationRuntime) {
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	for {
		select {
		case <-runtime.ctx.Done():
			m.expire(runtime)
			return
		case <-timer.C:
			m.mu.Lock()
			active := m.active == runtime && runtime.job.Status == serviceMutationStatusRunning
			expired := active && !m.now().Before(runtime.job.LeaseExpiresAt)
			m.mu.Unlock()
			if expired {
				m.expire(runtime)
				return
			}
			if !active {
				return
			}
			timer.Reset(time.Second)
		}
	}
}

func (m *serviceMutationManager) expire(runtime *serviceMutationRuntime) {
	m.mu.Lock()
	if m.active != runtime || !serviceMutationStatusActive(runtime.job.Status) {
		m.mu.Unlock()
		return
	}
	now := m.now()
	runtime.job.Status = serviceMutationStatusCancelling
	runtime.job.Phase = "cancelling_expired_lease"
	runtime.job.ErrorCode = "service_mutation_lease_expired"
	runtime.job.ErrorMessage = "The panel stopped heartbeating before the service mutation completed."
	runtime.job.UpdatedAt = now
	runtime.cancel()
	_ = m.writeLocked()
	steps := runtime.steps
	m.mu.Unlock()
	if steps == 0 {
		m.finishExpired(runtime)
	}
}

func (m *serviceMutationManager) finishExpired(runtime *serviceMutationRuntime) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active != runtime || runtime.steps != 0 ||
		runtime.job.Status != serviceMutationStatusCancelling {
		return
	}
	_ = m.finishRuntimeLocked(
		runtime,
		false,
		runtime.job.ErrorCode,
		runtime.job.ErrorMessage,
	)
}

func (m *serviceMutationManager) heartbeat(
	request *ServiceMutationHeartbeatRequest,
) (*ServiceMutationJob, error) {
	if request == nil || !validMutationIdentity(request.RequestID) ||
		!validMutationIdentity(request.OwnerID) {
		return nil, errors.New("invalid service mutation heartbeat")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	runtime := m.active
	if runtime == nil || runtime.job.RequestID != request.RequestID ||
		runtime.job.OwnerID != request.OwnerID ||
		runtime.job.Status != serviceMutationStatusRunning {
		return m.jobLocked(request.RequestID), errors.New("service mutation lease is not owned by this panel")
	}
	now := m.now()
	if !now.Before(runtime.job.DeadlineAt) {
		return cloneServiceMutationJob(runtime.job), errors.New("service mutation deadline has expired")
	}
	if phase := strings.TrimSpace(request.Phase); phase != "" {
		runtime.job.Phase = phase
	}
	runtime.job.UpdatedAt = now
	runtime.job.LeaseExpiresAt = minMutationTime(now.Add(m.leaseDuration), runtime.job.DeadlineAt)
	if err := m.writeLocked(); err != nil {
		return nil, err
	}
	return cloneServiceMutationJob(runtime.job), nil
}

func (m *serviceMutationManager) status(requestID string) *ServiceMutationJob {
	_ = m.tryResolvePersistedOrphan()
	m.mu.Lock()
	defer m.mu.Unlock()
	if requestID == "" {
		requestID = m.ledger.ActiveRequestID
	}
	return m.jobLocked(requestID)
}

func (m *serviceMutationManager) jobLocked(requestID string) *ServiceMutationJob {
	return cloneServiceMutationJob(m.ledger.Jobs[requestID])
}

func cloneServiceMutationJob(job *ServiceMutationJob) *ServiceMutationJob {
	if job == nil {
		return nil
	}
	copy := *job
	return &copy
}

func (m *serviceMutationManager) cancelJob(
	request *ServiceMutationCancelRequest,
) (*ServiceMutationJob, error) {
	if request == nil || !validMutationIdentity(request.RequestID) ||
		!validMutationIdentity(request.ExpectedOwner) {
		return nil, errors.New("invalid service mutation cancellation")
	}
	m.mu.Lock()
	runtime := m.active
	if runtime == nil || runtime.job.RequestID != request.RequestID ||
		runtime.job.OwnerID != request.ExpectedOwner {
		job := m.jobLocked(request.RequestID)
		m.mu.Unlock()
		return job, errors.New("active service mutation identity changed")
	}
	if runtime.job.Status != serviceMutationStatusRunning {
		job := cloneServiceMutationJob(runtime.job)
		m.mu.Unlock()
		return job, nil
	}
	code := strings.TrimSpace(request.FailureCode)
	if code == "" {
		code = "panel_restarted_during_mutation"
	}
	message := strings.TrimSpace(request.FailureMessage)
	if message == "" {
		message = "The panel restarted while the agent still owned the service mutation."
	}
	runtime.job.Status = serviceMutationStatusCancelling
	runtime.job.Phase = "cancelling"
	if reason := strings.TrimSpace(request.Reason); reason != "" {
		runtime.job.Phase = reason
	}
	runtime.job.ErrorCode = code
	runtime.job.ErrorMessage = message
	runtime.job.UpdatedAt = m.now()
	runtime.cancel()
	err := m.writeLocked()
	steps := runtime.steps
	job := cloneServiceMutationJob(runtime.job)
	m.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if steps == 0 {
		m.finishExpired(runtime)
		job = m.status(request.RequestID)
	}
	return job, nil
}

func (m *serviceMutationManager) finish(
	request *ServiceMutationFinishRequest,
) (*ServiceMutationJob, error) {
	if request == nil || !validMutationIdentity(request.RequestID) ||
		!validMutationIdentity(request.OwnerID) {
		return nil, errors.New("invalid service mutation completion")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	runtime := m.active
	if runtime == nil {
		job := m.ledger.Jobs[request.RequestID]
		if job != nil && !serviceMutationStatusActive(job.Status) {
			return cloneServiceMutationJob(job), nil
		}
		return cloneServiceMutationJob(job), errors.New("service mutation is not active")
	}
	if runtime.job.RequestID != request.RequestID || runtime.job.OwnerID != request.OwnerID {
		return cloneServiceMutationJob(runtime.job), errors.New("service mutation lease is owned by another request")
	}
	if runtime.steps != 0 {
		return cloneServiceMutationJob(runtime.job), errors.New("service mutation still has an active privileged step")
	}
	if runtime.job.WorkerPID != 0 {
		return cloneServiceMutationJob(runtime.job), errors.New("service mutation still has a recorded privileged worker")
	}
	if err := m.finishRuntimeLocked(
		runtime,
		request.Success,
		request.FailureCode,
		request.Message,
	); err != nil {
		return cloneServiceMutationJob(runtime.job), err
	}
	return cloneServiceMutationJob(runtime.job), nil
}

func (m *serviceMutationManager) finishRuntimeLocked(
	runtime *serviceMutationRuntime,
	success bool,
	code, message string,
) error {
	if m.active != runtime {
		return errors.New("service mutation runtime changed")
	}
	now := m.now()
	if success {
		runtime.job.Status = serviceMutationStatusSucceeded
		runtime.job.Phase = "completed"
		runtime.job.ErrorCode = ""
		runtime.job.ErrorMessage = ""
	} else {
		runtime.job.Status = serviceMutationStatusFailed
		runtime.job.Phase = "failed"
		if strings.TrimSpace(code) == "" {
			code = "service_mutation_failed"
		}
		if strings.TrimSpace(message) == "" {
			message = "The service mutation did not complete."
		}
		runtime.job.ErrorCode = code
		runtime.job.ErrorMessage = message
	}
	runtime.job.UpdatedAt = now
	runtime.job.FinishedAt = now
	runtime.job.LeaseExpiresAt = time.Time{}
	runtime.job.WorkerPID = 0
	runtime.job.WorkerStarted = ""
	runtime.job.WorkerCommand = ""
	m.ledger.ActiveRequestID = ""
	if err := m.writeLocked(); err != nil {
		runtime.job.Status = serviceMutationStatusCancelling
		runtime.job.Phase = "terminal_state_persist_failed"
		m.ledger.ActiveRequestID = runtime.job.RequestID
		runtime.cancel()
		return err
	}
	runtime.cancel()
	lockErr := runtime.lock.Close()
	m.active = nil
	m.trimHistoryLocked()
	if lockErr != nil {
		return fmt.Errorf("release service mutation host lock: %w", lockErr)
	}
	return nil
}

func (m *serviceMutationManager) acquireStep(
	binding ServiceMutationBinding,
) (context.Context, func(), error) {
	if !validMutationIdentity(binding.MutationRequestID) ||
		!validMutationIdentity(binding.MutationOwnerID) {
		return nil, nil, errors.New("a valid durable service mutation lease is required")
	}
	m.mu.Lock()
	runtime := m.active
	if runtime == nil || runtime.job.RequestID != binding.MutationRequestID ||
		runtime.job.OwnerID != binding.MutationOwnerID ||
		runtime.job.Status != serviceMutationStatusRunning {
		m.mu.Unlock()
		return nil, nil, errors.New("service mutation step does not own the active lease")
	}
	m.mu.Unlock()

	runtime.stepMu.Lock()
	m.mu.Lock()
	if m.active != runtime || runtime.job.Status != serviceMutationStatusRunning {
		m.mu.Unlock()
		runtime.stepMu.Unlock()
		return nil, nil, errors.New("service mutation lease expired before the step started")
	}
	runtime.steps++
	ctx := context.WithValue(
		runtime.ctx,
		serviceMutationExecutionTrackerKey{},
		&serviceMutationExecutionTracker{manager: m, runtime: runtime},
	)
	m.mu.Unlock()

	var once sync.Once
	done := func() {
		once.Do(func() {
			m.mu.Lock()
			runtime.steps--
			shouldFinish := m.active == runtime && runtime.steps == 0 &&
				runtime.job.Status == serviceMutationStatusCancelling
			m.mu.Unlock()
			runtime.stepMu.Unlock()
			if shouldFinish {
				m.finishExpired(runtime)
			}
		})
	}
	return ctx, done, nil
}

func (m *serviceMutationManager) trimHistoryLocked() {
	if len(m.ledger.Jobs) <= serviceMutationHistoryLimit {
		return
	}
	type finishedJob struct {
		id   string
		when time.Time
	}
	var terminal []finishedJob
	for id, job := range m.ledger.Jobs {
		if id == m.ledger.ActiveRequestID || serviceMutationStatusActive(job.Status) {
			continue
		}
		terminal = append(terminal, finishedJob{id: id, when: job.FinishedAt})
	}
	sort.Slice(terminal, func(i, j int) bool { return terminal[i].when.Before(terminal[j].when) })
	for len(m.ledger.Jobs) > serviceMutationHistoryLimit && len(terminal) > 0 {
		delete(m.ledger.Jobs, terminal[0].id)
		terminal = terminal[1:]
	}
}

func (m *serviceMutationManager) writeLocked() error {
	m.trimHistoryLocked()
	if err := ensureSecureServiceMutationStateDirectory(filepath.Dir(m.ledgerPath)); err != nil {
		return fmt.Errorf("secure service mutation state directory: %w", err)
	}
	raw, err := json.Marshal(&m.ledger)
	if err != nil {
		return fmt.Errorf("encode service mutation ledger: %w", err)
	}
	dir := filepath.Dir(m.ledgerPath)
	stage, err := os.CreateTemp(dir, ".service-mutations-*.json")
	if err != nil {
		return fmt.Errorf("stage service mutation ledger: %w", err)
	}
	stagePath := stage.Name()
	ok := false
	defer func() {
		_ = stage.Close()
		if !ok {
			_ = os.Remove(stagePath)
		}
	}()
	if err := stage.Chmod(0o600); err != nil {
		return err
	}
	if _, err := stage.Write(raw); err != nil {
		return err
	}
	if err := stage.Sync(); err != nil {
		return err
	}
	if err := stage.Close(); err != nil {
		return err
	}
	if err := os.Rename(stagePath, m.ledgerPath); err != nil {
		return fmt.Errorf("publish service mutation ledger: %w", err)
	}
	if err := syncServiceMutationDirectory(m.ledgerPath); err != nil {
		return fmt.Errorf("sync service mutation ledger directory: %w", err)
	}
	ok = true
	return nil
}

func (a *Agent) BeginServiceMutation(
	request *ServiceMutationBeginRequest,
	response *ServiceMutationResponse,
) error {
	manager, managerErr := agentServiceMutationManager()
	if managerErr != nil {
		return managerErr
	}
	job, err := manager.begin(request)
	response.Job = job
	if err != nil {
		response.Error = err.Error()
	}
	return nil
}

func (a *Agent) HeartbeatServiceMutation(
	request *ServiceMutationHeartbeatRequest,
	response *ServiceMutationResponse,
) error {
	manager, managerErr := agentServiceMutationManager()
	if managerErr != nil {
		return managerErr
	}
	job, err := manager.heartbeat(request)
	response.Job = job
	if err != nil {
		response.Error = err.Error()
	}
	return nil
}

func (a *Agent) ServiceMutationStatus(
	request *ServiceMutationStatusRequest,
	response *ServiceMutationResponse,
) error {
	manager, managerErr := agentServiceMutationManager()
	if managerErr != nil {
		return managerErr
	}
	response.Job = manager.status(strings.TrimSpace(request.RequestID))
	return nil
}

func (a *Agent) CancelServiceMutation(
	request *ServiceMutationCancelRequest,
	response *ServiceMutationResponse,
) error {
	manager, managerErr := agentServiceMutationManager()
	if managerErr != nil {
		return managerErr
	}
	job, err := manager.cancelJob(request)
	response.Job = job
	if err != nil {
		response.Error = err.Error()
	}
	return nil
}

func (a *Agent) FinishServiceMutation(
	request *ServiceMutationFinishRequest,
	response *ServiceMutationResponse,
) error {
	manager, managerErr := agentServiceMutationManager()
	if managerErr != nil {
		return managerErr
	}
	job, err := manager.finish(request)
	response.Job = job
	if err != nil {
		response.Error = err.Error()
	}
	return nil
}

func (a *Agent) requiredServiceMutationStep(
	binding ServiceMutationBinding,
) (context.Context, func(), error) {
	if !validMutationIdentity(binding.MutationRequestID) ||
		!validMutationIdentity(binding.MutationOwnerID) {
		return nil, nil, errors.New("a valid durable service mutation lease is required")
	}
	manager := loadedAgentServiceMutationManager()
	if manager == nil {
		return nil, nil, errors.New("service mutation manager is unavailable")
	}
	return manager.acquireStep(binding)
}
