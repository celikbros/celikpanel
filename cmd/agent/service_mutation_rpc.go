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

	"github.com/alicelik/celikpanel/internal/hostingpath"
	"github.com/alicelik/celikpanel/internal/transport"
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
	serviceMutationStageLimit    = 16
)

var (
	errServiceMutationBusy                     = errors.New("another service mutation owns the host lease")
	errServiceMutationHostBusy                 = errors.New("the host package manager or mutation lock is busy")
	errServiceMutationLedgerAlreadyInitialized = errors.New("service mutation ledger is already initialized")
	errServiceMutationManagerPoisoned          = errors.New("service mutation manager is fail-closed after an ambiguous ledger write")

	globalServiceMutationMu      sync.Mutex
	globalServiceMutationManager *serviceMutationManager
	globalServiceMutationErr     error
)

const (
	serviceMutationWriteFaultBeforeRename = "before_rename"
	serviceMutationWriteFaultAfterRename  = "after_rename_before_directory_sync"
	serviceMutationWriteFaultAfterSync    = "after_directory_sync"
)

type serviceMutationLedgerWriteState uint8

const (
	serviceMutationLedgerWriteNotPublished serviceMutationLedgerWriteState = iota
	serviceMutationLedgerWritePublished
	serviceMutationLedgerWriteAmbiguous
)

type serviceMutationLedgerWriteError struct {
	state serviceMutationLedgerWriteState
	err   error
}

func (e *serviceMutationLedgerWriteError) Error() string {
	return e.err.Error()
}

func (e *serviceMutationLedgerWriteError) Unwrap() error {
	return e.err
}

type ServiceMutationJob = transport.ServiceMutationJob

type serviceMutationLedger struct {
	Version         int                            `json:"version"`
	ActiveRequestID string                         `json:"active_request_id,omitempty"`
	Jobs            map[string]*ServiceMutationJob `json:"jobs"`
}

type ServiceMutationBeginRequest = transport.ServiceMutationBeginRequest

type ServiceMutationBinding = transport.ServiceMutationBinding

// ServiceMutationRequest is the common request envelope for privileged RPCs
// that do not otherwise need arguments.
// ServiceMutationRequest, başka argümana ihtiyaç duymayan ayrıcalıklı RPC'ler
// için ortak istek zarfıdır.
type ServiceMutationRequest = transport.ServiceMutationRequest

type ServiceMutationHeartbeatRequest = transport.ServiceMutationHeartbeatRequest

type ServiceMutationStatusRequest = transport.ServiceMutationStatusRequest

type ServiceMutationCancelRequest = transport.ServiceMutationCancelRequest

type ServiceMutationFinishRequest = transport.ServiceMutationFinishRequest

type ServiceMutationResponse = transport.ServiceMutationResponse

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
	poisoned   error
	poisonLock *serviceMutationFileLock
	writeFault func(string) error

	now             func() time.Time
	leaseDuration   time.Duration
	overallDuration time.Duration
}

func serviceMutationStateDirectory() string {
	if value := strings.TrimSpace(os.Getenv("CELIKPANEL_AGENT_STATE_DIR")); value != "" {
		return value
	}
	return hostingpath.ServiceMutationStateRoot()
}

func serviceMutationLockFile() string {
	if value := strings.TrimSpace(os.Getenv("CELIKPANEL_MUTATION_LOCK")); value != "" {
		return value
	}
	return "/run/celikpanel/service-mutation.lock"
}

// initializeServiceMutationLedger publishes the canonical empty ledger exactly once while holding the host mutation lock.
// initializeServiceMutationLedger, ana makine mutation kilidini tutarken kanonik boş ledger'ı yalnızca bir kez yayımlar.
func initializeServiceMutationLedger(stateDir, lockPath string) (returnErr error) {
	if strings.TrimSpace(stateDir) == "" {
		stateDir = serviceMutationStateDirectory()
	}
	if strings.TrimSpace(lockPath) == "" {
		lockPath = serviceMutationLockFile()
	}
	stateDir = filepath.Clean(stateDir)
	lockPath = filepath.Clean(lockPath)
	if !filepath.IsAbs(stateDir) || !filepath.IsAbs(lockPath) {
		return errors.New("service mutation initialization paths must be absolute")
	}

	lock, err := acquireServiceMutationFileLock(lockPath)
	if err != nil {
		return fmt.Errorf("acquire service mutation initialization lock: %w", err)
	}
	defer func() {
		if err := lock.Close(); err != nil && returnErr == nil {
			returnErr = fmt.Errorf("release service mutation initialization lock: %w", err)
		}
	}()

	if err := ensureSecureServiceMutationStateDirectory(stateDir); err != nil {
		return fmt.Errorf("secure service mutation state directory: %w", err)
	}
	ledgerPath := filepath.Join(stateDir, serviceMutationLedgerFileName)
	if _, err := os.Lstat(ledgerPath); err == nil {
		return errServiceMutationLedgerAlreadyInitialized
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect initial service mutation ledger: %w", err)
	}
	raw, err := canonicalInitialServiceMutationLedger()
	if err != nil {
		return fmt.Errorf("encode initial service mutation ledger: %w", err)
	}
	if err := cleanupAbandonedInitialServiceMutationStage(stateDir, raw); err != nil {
		return fmt.Errorf("clean abandoned initial service mutation stage: %w", err)
	}
	file, err := os.CreateTemp(stateDir, initialServiceMutationStagePattern)
	if err != nil {
		return fmt.Errorf("stage initial service mutation ledger: %w", err)
	}
	stagePath := file.Name()
	defer func() {
		_ = file.Close()
		_ = os.Remove(stagePath)
	}()
	if err := file.Chown(int(serviceMutationRequiredOwnerUID), int(serviceMutationRequiredOwnerGID)); err != nil {
		return fmt.Errorf("set initial service mutation ledger owner: %w", err)
	}
	if err := file.Chmod(0o600); err != nil {
		return fmt.Errorf("secure initial service mutation ledger: %w", err)
	}
	if _, err := file.Write(raw); err != nil {
		return fmt.Errorf("write initial service mutation ledger: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync initial service mutation ledger: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close initial service mutation ledger: %w", err)
	}
	// A same-directory no-replace rename publishes exactly one final link in one
	// atomic step; the platform helper also fsyncs the containing directory.
	// Aynı dizindeki üzerine-yazmayan rename, tek atomik adımda tam bir nihai bağlantı
	// yayımlar; platform yardımcısı ayrıca kapsayan dizini fsync eder.
	if err := publishInitialServiceMutationLedger(stagePath, ledgerPath); err != nil {
		if os.IsExist(err) {
			return errServiceMutationLedgerAlreadyInitialized
		}
		return fmt.Errorf("publish initial service mutation ledger: %w", err)
	}
	return nil
}

func newServiceMutationManager(stateDir, lockPath string) (*serviceMutationManager, error) {
	return newServiceMutationManagerWithWriteFault(stateDir, lockPath, nil)
}

func newServiceMutationManagerWithWriteFault(
	stateDir, lockPath string,
	writeFault func(string) error,
) (*serviceMutationManager, error) {
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
		writeFault: writeFault,
	}
	if err := manager.load(); err != nil {
		return nil, err
	}
	if err := manager.reconcilePersistedActive(); err != nil {
		// Return a poisoned manager with its retained lock so the process-global
		// owner cannot lose fail-closed state through garbage collection.
		// Zehirlenmiş manager'ı tuttuğu kilitle birlikte döndür; böylece süreç-geneli
		// sahibi fail-closed durumunu çöp toplama nedeniyle kaybetmesin.
		if manager.poisoned != nil {
			return manager, err
		}
		return nil, err
	}
	return manager, nil
}

func agentServiceMutationManager() (*serviceMutationManager, error) {
	globalServiceMutationMu.Lock()
	defer globalServiceMutationMu.Unlock()
	if globalServiceMutationManager == nil && globalServiceMutationErr == nil {
		manager, err := newServiceMutationManager("", "")
		if errors.Is(err, errServiceMutationHostBusy) && manager == nil {
			// Another process holding the host lock is normal, transient startup
			// contention. Do not cache it for the lifetime of this agent: the next
			// RPC can retry after that mutation releases the lock.
			// Host kilidini başka bir sürecin tutması normal, geçici bir başlangıç
			// yarışıdır. Bunu agent ömrü boyunca önbellekleme; sonraki RPC, mutation
			// kilidi bıraktıktan sonra yeniden deneyebilsin.
			return nil, err
		}
		globalServiceMutationManager, globalServiceMutationErr = manager, err
	}
	return globalServiceMutationManager, globalServiceMutationErr
}

func loadedAgentServiceMutationManager() *serviceMutationManager {
	globalServiceMutationMu.Lock()
	defer globalServiceMutationMu.Unlock()
	return globalServiceMutationManager
}

func (m *serviceMutationManager) load() error {
	ledger, err := m.loadLedgerFromDisk()
	if err != nil {
		return err
	}
	m.ledger = ledger
	return nil
}

func (m *serviceMutationManager) loadLedgerFromDisk() (serviceMutationLedger, error) {
	stateDir := filepath.Dir(m.ledgerPath)
	if !filepath.IsAbs(stateDir) {
		return serviceMutationLedger{}, errors.New("service mutation state directory must be absolute")
	}
	info, err := os.Lstat(stateDir)
	if err != nil {
		return serviceMutationLedger{}, fmt.Errorf("inspect service mutation state directory: %w", err)
	}
	if err := secureServiceMutationStateDirectoryStat(stateDir, info); err != nil {
		return serviceMutationLedger{}, fmt.Errorf("validate service mutation state directory: %w", err)
	}
	raw, exists, err := readSecureServiceMutationLedger(
		m.ledgerPath,
		serviceMutationLedgerMaxSize,
	)
	if err != nil {
		return serviceMutationLedger{}, err
	}
	if !exists {
		return serviceMutationLedger{}, errors.New("service mutation ledger is not initialized; run --initialize-service-mutation-ledger")
	}
	ledger, err := decodeServiceMutationLedger(raw)
	if err != nil {
		return serviceMutationLedger{}, err
	}
	return ledger, nil
}

// reloadLedgerUnderHostLockLocked must be called only after this manager has
// acquired the common host flock. It closes the read-before-lock race between
// independent agent instances: every writer starts from the latest committed
// history instead of overwriting jobs published by the previous lock holder.
// reloadLedgerUnderHostLockLocked yalnızca bu manager ortak host flock'unu
// aldıktan sonra çağrılmalıdır. Bağımsız agent örnekleri arasındaki kilitten
// önce okuma yarışını kapatır: her yazıcı önceki kilit sahibinin yayımladığı
// işleri ezmek yerine en son kaydedilmiş geçmişten başlar.
func (m *serviceMutationManager) reloadLedgerUnderHostLockLocked() error {
	if m.active != nil {
		return errors.New("cannot reload service mutation ledger while this manager owns an active runtime")
	}
	ledger, err := m.loadLedgerFromDisk()
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
	if err := validateServiceMutationLedger(&ledger); err != nil {
		return serviceMutationLedger{}, err
	}
	return ledger, nil
}

// validateServiceMutationLedger enforces identity and bidirectional active-pointer invariants for the complete ledger.
// validateServiceMutationLedger, ledger'ın tamamı için kimlik ve çift yönlü aktif işaretçi değişmezlerini uygular.
func validateServiceMutationLedger(ledger *serviceMutationLedger) error {
	activeRequestID := ""
	for requestID, job := range ledger.Jobs {
		if job == nil || job.RequestID != requestID {
			return errors.New("service mutation ledger job identity is inconsistent")
		}
		if !validMutationIdentity(job.RequestID) || !validMutationIdentity(job.OwnerID) {
			return errors.New("service mutation ledger job identity is invalid")
		}
		if strings.TrimSpace(job.Kind) == "" ||
			strings.TrimSpace(job.Target) == "" ||
			strings.TrimSpace(job.Phase) == "" ||
			job.Attempt <= 0 {
			return errors.New("service mutation ledger job metadata is incomplete")
		}
		hasWorkerPID := job.WorkerPID > 0
		hasWorkerStarted := strings.TrimSpace(job.WorkerStarted) != ""
		hasWorkerCommand := strings.TrimSpace(job.WorkerCommand) != ""
		if job.WorkerPID < 0 ||
			hasWorkerPID != hasWorkerStarted ||
			hasWorkerPID != hasWorkerCommand {
			return errors.New("service mutation ledger worker identity is inconsistent")
		}

		if job.StartedAt.IsZero() || job.UpdatedAt.IsZero() || job.DeadlineAt.IsZero() {
			return errors.New("service mutation ledger lifecycle timestamps are incomplete")
		}
		if job.UpdatedAt.Before(job.StartedAt) || job.DeadlineAt.Before(job.StartedAt) {
			return errors.New("service mutation ledger lifecycle timestamps are out of order")
		}
		if !job.LeaseExpiresAt.IsZero() &&
			(job.LeaseExpiresAt.Before(job.StartedAt) ||
				job.LeaseExpiresAt.After(job.DeadlineAt)) {
			return errors.New("service mutation ledger lease timestamp is out of range")
		}
		switch job.Status {
		case serviceMutationStatusRunning,
			serviceMutationStatusCancelling,
			serviceMutationStatusOrphaned:
			if job.LeaseExpiresAt.IsZero() {
				return errors.New("active service mutation ledger job has no lease timestamp")
			}
			if !job.FinishedAt.IsZero() {
				return errors.New("active service mutation ledger job has a finish timestamp")
			}
			if activeRequestID != "" {
				return errors.New("service mutation ledger contains multiple active jobs")
			}
			activeRequestID = requestID
		case serviceMutationStatusSucceeded, serviceMutationStatusFailed:
			if hasWorkerPID {
				return errors.New("terminal service mutation ledger job retains a worker")
			}
			if !job.LeaseExpiresAt.IsZero() {
				return errors.New("terminal service mutation ledger job retains a lease")
			}
			if job.FinishedAt.IsZero() ||
				job.FinishedAt.Before(job.StartedAt) ||
				job.UpdatedAt.After(job.FinishedAt) {
				return errors.New("terminal service mutation ledger timestamps are inconsistent")
			}
			// Terminal jobs remain as history and must not be selected by the active pointer.
			// Sonlandırılmış işler geçmiş olarak kalır ve aktif işaretçi tarafından seçilmemelidir.
		default:
			return errors.New("service mutation ledger job has an unsupported status")
		}
	}
	if ledger.ActiveRequestID != activeRequestID {
		return errors.New("service mutation ledger active pointer is inconsistent")
	}
	return nil
}

func cloneServiceMutationLedger(ledger serviceMutationLedger) serviceMutationLedger {
	copy := serviceMutationLedger{
		Version:         ledger.Version,
		ActiveRequestID: ledger.ActiveRequestID,
		Jobs:            make(map[string]*ServiceMutationJob, len(ledger.Jobs)),
	}
	for requestID, job := range ledger.Jobs {
		copy.Jobs[requestID] = cloneServiceMutationJob(job)
	}
	return copy
}

func (m *serviceMutationManager) restoreLedgerLocked(ledger serviceMutationLedger) {
	m.ledger = ledger
	if m.active != nil && m.active.job != nil {
		m.active.job = m.ledger.Jobs[m.active.job.RequestID]
	}
}

func (m *serviceMutationManager) healthErrorLocked() error {
	if m.poisoned == nil {
		return nil
	}
	return errors.Join(errServiceMutationManagerPoisoned, m.poisoned)
}

// Once publication may have happened, memory can no longer be rolled back to
// a provably matching state. Cancel execution but deliberately retain the host
// lock and active runtime so no second mutation can begin in this process.
// Yayım gerçekleşmiş olabilirse bellek artık kanıtlanabilir biçimde eşleşen bir
// duruma geri alınamaz. Yürütmeyi iptal et; ancak bu süreçte ikinci mutation
// başlayamasın diye host kilidini ve aktif runtime'ı bilinçli olarak koru.
func (m *serviceMutationManager) poisonLocked(cause error) error {
	if m.poisoned == nil {
		m.poisoned = cause
		if m.active != nil {
			m.active.cancel()
		}
	}
	return errors.Join(errServiceMutationManagerPoisoned, cause)
}

func serviceMutationWriteMayHavePublished(err error) bool {
	var writeErr *serviceMutationLedgerWriteError
	return errors.As(err, &writeErr) &&
		writeErr.state != serviceMutationLedgerWriteNotPublished
}

func (m *serviceMutationManager) persistLedgerMutationLocked(
	before serviceMutationLedger,
) error {
	err := m.writeLocked()
	if err != nil && m.poisoned == nil {
		m.restoreLedgerLocked(before)
	}
	return err
}

func (m *serviceMutationManager) handleLedgerWriteErrorLocked(err error) error {
	if serviceMutationWriteMayHavePublished(err) {
		return m.poisonLocked(err)
	}
	return err
}

func (m *serviceMutationManager) reconcilePersistedActive() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.healthErrorLocked(); err != nil {
		return err
	}
	// No startup reconciliation result is published until this process proves
	// exclusive ownership of the same host mutation lock used by workers.
	// Bu süreç, worker'ların kullandığı aynı host mutation kilidinin münhasır
	// sahipliğini kanıtlamadan hiçbir başlangıç uzlaştırma sonucunu yayımlamaz.
	lock, err := acquireServiceMutationFileLock(m.lockPath)
	if err != nil {
		return fmt.Errorf("acquire service mutation reconciliation lock: %w", err)
	}
	if err := m.reloadLedgerUnderHostLockLocked(); err != nil {
		closeErr := lock.Close()
		if closeErr != nil {
			return errors.Join(err, closeErr)
		}
		return fmt.Errorf("reload service mutation ledger under reconciliation lock: %w", err)
	}
	if err := cleanupAbandonedServiceMutationWriteStages(filepath.Dir(m.ledgerPath)); err != nil {
		closeErr := lock.Close()
		if closeErr != nil {
			return errors.Join(err, closeErr)
		}
		return err
	}
	if m.ledger.ActiveRequestID == "" {
		return lock.Close()
	}
	job := m.ledger.Jobs[m.ledger.ActiveRequestID]
	if job == nil {
		_ = lock.Close()
		return errors.New("service mutation ledger lost its active job")
	}

	var writeErr error
	switch {
	case serviceMutationWorkerMatches(job.WorkerPID, job.WorkerStarted):
		before := cloneServiceMutationLedger(m.ledger)
		job.Status = serviceMutationStatusOrphaned
		job.Phase = "waiting_for_orphaned_process"
		job.ErrorCode = "agent_restart_worker_alive"
		job.ErrorMessage = "The previous privileged worker is still alive with the recorded process identity."
		job.UpdatedAt = m.now()
		writeErr = m.persistLedgerMutationLocked(before)
	default:
		busy, probeErr := packageManagerMutationBusy()
		switch {
		case probeErr != nil:
			before := cloneServiceMutationLedger(m.ledger)
			job.Status = serviceMutationStatusOrphaned
			job.Phase = "host_state_unverified"
			job.ErrorCode = "package_manager_probe_failed"
			job.ErrorMessage = "The agent could not prove that the previous host mutation stopped."
			job.UpdatedAt = m.now()
			writeErr = m.persistLedgerMutationLocked(before)
		case busy:
			before := cloneServiceMutationLedger(m.ledger)
			job.Status = serviceMutationStatusOrphaned
			job.Phase = "waiting_for_orphaned_process"
			job.ErrorCode = "agent_restart_host_busy"
			job.ErrorMessage = "The previous agent exited while a trusted host mutation may still be running."
			job.UpdatedAt = m.now()
			writeErr = m.persistLedgerMutationLocked(before)
		default:
			writeErr = m.finishPersistedOrphanLocked(
				job,
				"agent_restarted_before_completion",
				"The privileged agent restarted before the mutation reached a verified terminal state.",
			)
		}
	}
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

func (m *serviceMutationManager) tryResolvePersistedOrphan() error {
	m.mu.Lock()
	if err := m.healthErrorLocked(); err != nil {
		m.mu.Unlock()
		return err
	}
	if m.active != nil {
		m.mu.Unlock()
		return nil
	}
	m.mu.Unlock()

	lock, err := acquireServiceMutationFileLock(m.lockPath)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.healthErrorLocked(); err != nil {
		return errors.Join(err, lock.Close())
	}
	if m.active != nil {
		return lock.Close()
	}
	if err := m.reloadLedgerUnderHostLockLocked(); err != nil {
		return errors.Join(fmt.Errorf("reload service mutation ledger under orphan lock: %w", err), lock.Close())
	}
	requestID := m.ledger.ActiveRequestID
	job := m.ledger.Jobs[requestID]
	if requestID == "" || job == nil || job.Status != serviceMutationStatusOrphaned {
		return lock.Close()
	}
	if serviceMutationWorkerMatches(job.WorkerPID, job.WorkerStarted) {
		return errors.Join(errServiceMutationHostBusy, lock.Close())
	}
	busy, err := packageManagerMutationBusy()
	if err != nil {
		return errors.Join(fmt.Errorf("verify orphaned service mutation: %w", err), lock.Close())
	}
	if busy {
		return errors.Join(errServiceMutationHostBusy, lock.Close())
	}
	writeErr := m.finishPersistedOrphanLocked(
		job,
		"agent_restarted_before_completion",
		"The previous privileged process is no longer running; the interrupted mutation may now be resumed.",
	)
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

func (m *serviceMutationManager) finishPersistedOrphanLocked(
	job *ServiceMutationJob,
	code, message string,
) error {
	before := cloneServiceMutationLedger(m.ledger)
	now := m.now()
	job.Status = serviceMutationStatusFailed
	job.Phase = "interrupted"
	job.ErrorCode = code
	job.ErrorMessage = message
	job.UpdatedAt = now
	job.FinishedAt = now
	job.LeaseExpiresAt = time.Time{}
	job.WorkerPID = 0
	job.WorkerStarted = ""
	job.WorkerCommand = ""
	m.ledger.ActiveRequestID = ""
	return m.persistLedgerMutationLocked(before)
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
	if err := m.healthErrorLocked(); err != nil {
		return nil, err
	}
	if m.active != nil {
		job := m.active.job
		if serviceMutationIdentityMatches(job, request) && job.OwnerID == request.OwnerID &&
			job.Status == serviceMutationStatusRunning {
			before := cloneServiceMutationLedger(m.ledger)
			now := m.now()
			job.UpdatedAt = now
			job.LeaseExpiresAt = minMutationTime(now.Add(m.leaseDuration), job.DeadlineAt)
			if err := m.persistLedgerMutationLocked(before); err != nil {
				return nil, err
			}
			return cloneServiceMutationJob(job), nil
		}
		return cloneServiceMutationJob(job), errServiceMutationBusy
	}
	lock, err := acquireServiceMutationFileLock(m.lockPath)
	if err != nil {
		return nil, err
	}
	closeLock := func(job *ServiceMutationJob, resultErr error) (*ServiceMutationJob, error) {
		if closeErr := lock.Close(); closeErr != nil {
			if resultErr == nil {
				resultErr = closeErr
			} else {
				resultErr = errors.Join(resultErr, closeErr)
			}
		}
		return cloneServiceMutationJob(job), resultErr
	}
	if err := m.reloadLedgerUnderHostLockLocked(); err != nil {
		return closeLock(nil, fmt.Errorf("reload service mutation ledger under begin lock: %w", err))
	}
	if m.ledger.ActiveRequestID != "" {
		return closeLock(m.ledger.Jobs[m.ledger.ActiveRequestID], errServiceMutationBusy)
	}
	previous := m.ledger.Jobs[request.RequestID]
	if previous != nil {
		if !serviceMutationIdentityMatches(previous, request) {
			return closeLock(previous, errors.New("request_id belongs to another service mutation"))
		}
		if !request.Resume {
			return closeLock(previous, nil)
		}
		if previous.Status != serviceMutationStatusFailed {
			return closeLock(previous, errors.New("only an interrupted failed mutation can be resumed"))
		}
	}
	busy, err := packageManagerMutationBusy()
	if err != nil {
		return closeLock(nil, fmt.Errorf("verify package manager lease: %w", err))
	}
	if busy {
		return closeLock(nil, errServiceMutationHostBusy)
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
	before := cloneServiceMutationLedger(m.ledger)
	m.ledger.ActiveRequestID = job.RequestID
	m.ledger.Jobs[job.RequestID] = job
	m.active = runtime
	if err := m.persistLedgerMutationLocked(before); err != nil {
		if m.poisoned != nil {
			return cloneServiceMutationJob(runtime.job), err
		}
		m.active = nil
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
			active := m.poisoned == nil && m.active == runtime &&
				runtime.job.Status == serviceMutationStatusRunning
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
	if m.poisoned != nil || m.active != runtime ||
		!serviceMutationStatusActive(runtime.job.Status) {
		m.mu.Unlock()
		return
	}
	before := cloneServiceMutationLedger(m.ledger)
	now := m.now()
	runtime.job.Status = serviceMutationStatusCancelling
	runtime.job.Phase = "cancelling_expired_lease"
	runtime.job.ErrorCode = "service_mutation_lease_expired"
	runtime.job.ErrorMessage = "The panel stopped heartbeating before the service mutation completed."
	runtime.job.UpdatedAt = now
	if err := m.persistLedgerMutationLocked(before); err != nil {
		if m.poisoned == nil {
			_ = m.poisonLocked(err)
		}
		m.mu.Unlock()
		return
	}
	runtime.cancel()
	steps := runtime.steps
	m.mu.Unlock()
	if steps == 0 {
		m.finishExpired(runtime)
	}
}

func (m *serviceMutationManager) finishExpired(runtime *serviceMutationRuntime) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.poisoned != nil || m.active != runtime || runtime.steps != 0 ||
		runtime.job.Status != serviceMutationStatusCancelling {
		return
	}
	if err := m.finishRuntimeLocked(
		runtime,
		false,
		runtime.job.ErrorCode,
		runtime.job.ErrorMessage,
	); err != nil && m.poisoned == nil {
		_ = m.poisonLocked(err)
	}
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
	if err := m.healthErrorLocked(); err != nil {
		return nil, err
	}
	runtime := m.active
	if runtime == nil || runtime.job.RequestID != request.RequestID ||
		runtime.job.OwnerID != request.OwnerID ||
		runtime.job.Status != serviceMutationStatusRunning {
		return m.jobLocked(request.RequestID), errors.New("service mutation lease is not owned by this panel")
	}
	before := cloneServiceMutationLedger(m.ledger)
	now := m.now()
	if !now.Before(runtime.job.DeadlineAt) {
		return cloneServiceMutationJob(runtime.job), errors.New("service mutation deadline has expired")
	}
	if phase := strings.TrimSpace(request.Phase); phase != "" {
		runtime.job.Phase = phase
	}
	runtime.job.UpdatedAt = now
	runtime.job.LeaseExpiresAt = minMutationTime(now.Add(m.leaseDuration), runtime.job.DeadlineAt)
	if err := m.persistLedgerMutationLocked(before); err != nil {
		return cloneServiceMutationJob(runtime.job), err
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
	if err := m.healthErrorLocked(); err != nil {
		m.mu.Unlock()
		return nil, err
	}
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
	before := cloneServiceMutationLedger(m.ledger)
	runtime.job.Status = serviceMutationStatusCancelling
	runtime.job.Phase = "cancelling"
	if reason := strings.TrimSpace(request.Reason); reason != "" {
		runtime.job.Phase = reason
	}
	runtime.job.ErrorCode = code
	runtime.job.ErrorMessage = message
	runtime.job.UpdatedAt = m.now()
	err := m.persistLedgerMutationLocked(before)
	steps := runtime.steps
	job := cloneServiceMutationJob(runtime.job)
	if err == nil {
		runtime.cancel()
	}
	m.mu.Unlock()
	if err != nil {
		return job, err
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
	if err := m.healthErrorLocked(); err != nil {
		return nil, err
	}
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
	if runtime.job.Status != serviceMutationStatusRunning {
		return cloneServiceMutationJob(runtime.job), errors.New("only a running service mutation may be finished")
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
	before := cloneServiceMutationLedger(m.ledger)
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
	if err := m.persistLedgerMutationLocked(before); err != nil {
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
	if err := m.healthErrorLocked(); err != nil {
		m.mu.Unlock()
		return nil, nil, err
	}
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
	if m.poisoned != nil || m.active != runtime ||
		runtime.job.Status != serviceMutationStatusRunning {
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
			shouldFinish := m.poisoned == nil && m.active == runtime && runtime.steps == 0 &&
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
	if err := m.healthErrorLocked(); err != nil {
		return err
	}
	m.trimHistoryLocked()
	if err := validateServiceMutationLedger(&m.ledger); err != nil {
		return fmt.Errorf("validate service mutation ledger before write: %w", err)
	}
	if err := ensureSecureServiceMutationStateDirectory(filepath.Dir(m.ledgerPath)); err != nil {
		return fmt.Errorf("secure service mutation state directory: %w", err)
	}
	if err := cleanupAbandonedServiceMutationWriteStages(filepath.Dir(m.ledgerPath)); err != nil {
		return err
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
	if err := stage.Chown(int(serviceMutationRequiredOwnerUID), int(serviceMutationRequiredOwnerGID)); err != nil {
		return fmt.Errorf("set service mutation ledger owner: %w", err)
	}
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
	if m.writeFault != nil {
		if err := m.writeFault(serviceMutationWriteFaultBeforeRename); err != nil {
			return &serviceMutationLedgerWriteError{
				state: serviceMutationLedgerWriteNotPublished,
				err:   fmt.Errorf("injected failure before service mutation ledger rename: %w", err),
			}
		}
	}
	if err := os.Rename(stagePath, m.ledgerPath); err != nil {
		return m.handleLedgerWriteErrorLocked(&serviceMutationLedgerWriteError{
			state: serviceMutationLedgerWriteAmbiguous,
			err:   fmt.Errorf("publish service mutation ledger: %w", err),
		})
	}
	if m.writeFault != nil {
		if err := m.writeFault(serviceMutationWriteFaultAfterRename); err != nil {
			return m.handleLedgerWriteErrorLocked(&serviceMutationLedgerWriteError{
				state: serviceMutationLedgerWritePublished,
				err:   fmt.Errorf("injected failure after service mutation ledger rename: %w", err),
			})
		}
	}
	if err := syncServiceMutationDirectory(m.ledgerPath); err != nil {
		return m.handleLedgerWriteErrorLocked(&serviceMutationLedgerWriteError{
			state: serviceMutationLedgerWritePublished,
			err:   fmt.Errorf("sync service mutation ledger directory: %w", err),
		})
	}
	if m.writeFault != nil {
		if err := m.writeFault(serviceMutationWriteFaultAfterSync); err != nil {
			return m.handleLedgerWriteErrorLocked(&serviceMutationLedgerWriteError{
				state: serviceMutationLedgerWritePublished,
				err:   fmt.Errorf("injected failure after service mutation ledger directory sync: %w", err),
			})
		}
	}
	ok = true
	return nil
}

// cleanupAbandonedServiceMutationWriteStages removes only complete canonical
// writer stages with strict ledger metadata. The caller must hold the common
// host mutation flock, and any ambiguous artifact fails closed.
// cleanupAbandonedServiceMutationWriteStages yalnızca katı ledger meta verisine
// sahip eksiksiz kanonik yazıcı stage'lerini kaldırır. Çağıran ortak host
// mutation flock kilidini tutmalıdır; belirsiz her kalıntı fail-closed sonuçlanır.
func cleanupAbandonedServiceMutationWriteStages(stateDir string) error {
	entries, err := os.ReadDir(stateDir)
	if err != nil {
		return fmt.Errorf("inspect abandoned service mutation write stages: %w", err)
	}
	stagePaths := make([]string, 0)
	for _, entry := range entries {
		name := entry.Name()
		if isInitialServiceMutationStageName(name) {
			return errors.New("initialized service mutation state retains an initializer stage")
		}
		if strings.HasPrefix(name, ".service-mutations-") &&
			strings.HasSuffix(name, ".json") &&
			!strings.HasPrefix(name, initialServiceMutationStagePrefix) {
			stagePaths = append(stagePaths, filepath.Join(stateDir, name))
		}
	}
	if len(stagePaths) > serviceMutationStageLimit {
		return fmt.Errorf(
			"abandoned service mutation write stage count %d exceeds limit %d",
			len(stagePaths),
			serviceMutationStageLimit,
		)
	}
	sort.Strings(stagePaths)
	for _, stagePath := range stagePaths {
		raw, exists, err := readSecureServiceMutationLedger(
			stagePath,
			serviceMutationLedgerMaxSize,
		)
		if err != nil {
			return fmt.Errorf("validate abandoned service mutation write stage: %w", err)
		}
		if !exists {
			return errors.New("abandoned service mutation write stage disappeared during validation")
		}
		if _, err := decodeServiceMutationLedger(raw); err != nil {
			return fmt.Errorf("validate abandoned service mutation write stage content: %w", err)
		}
	}
	for _, stagePath := range stagePaths {
		if err := os.Remove(stagePath); err != nil {
			return fmt.Errorf("remove abandoned service mutation write stage: %w", err)
		}
	}
	if len(stagePaths) != 0 {
		if err := syncServiceMutationDirectory(
			filepath.Join(stateDir, serviceMutationLedgerFileName),
		); err != nil {
			return fmt.Errorf("sync abandoned service mutation write stage cleanup: %w", err)
		}
	}
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
