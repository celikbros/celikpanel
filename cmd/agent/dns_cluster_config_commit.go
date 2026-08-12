package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
)

const (
	dnsClusterConfigCommitPhasePrefix = "commit/dns-cluster-config/v1/"
	dnsClusterConfigJournalFileName   = "dns-cluster-config-journal.json"
	dnsClusterConfigJournalVersion    = 1
	dnsClusterConfigJournalMaxSize    = 16 << 10
	dnsClusterConfigJournalStageLimit = 8

	dnsClusterConfigCommitIntent    = "intent"
	dnsClusterConfigCommitPublished = "published"
	dnsClusterConfigRecoveryTimeout = 2 * time.Minute
)

type dnsClusterConfigJournal struct {
	Version   int    `json:"version"`
	RequestID string `json:"request_id"`
	Role      string `json:"role"`
	PeerIP    string `json:"peer_ip"`
	PeerNS    string `json:"peer_ns"`
	Qualifier string `json:"qualifier"`
}

func formatDNSClusterConfigCommitPhase(state, requestID, qualifier string) (string, error) {
	if (state != dnsClusterConfigCommitIntent && state != dnsClusterConfigCommitPublished) ||
		!validMutationIdentity(requestID) ||
		!mutationpayload.ValidDNSClusterConfigQualifier(qualifier) {
		return "", errors.New("invalid DNS cluster commit phase identity")
	}
	return dnsClusterConfigCommitPhasePrefix + state + "/" + requestID + "/" + qualifier, nil
}

func parseDNSClusterConfigCommitPhase(value string) (state, requestID, qualifier string, err error) {
	if !strings.HasPrefix(value, dnsClusterConfigCommitPhasePrefix) {
		return "", "", "", errors.New("not a DNS cluster commit phase")
	}
	remainder := strings.TrimPrefix(value, dnsClusterConfigCommitPhasePrefix)
	state, remainder, found := strings.Cut(remainder, "/")
	if !found {
		return "", "", "", errors.New("invalid DNS cluster commit phase")
	}
	requestID, qualifier, found = strings.Cut(remainder, "/")
	if !found {
		return "", "", "", errors.New("invalid DNS cluster commit phase")
	}
	canonical, formatErr := formatDNSClusterConfigCommitPhase(state, requestID, qualifier)
	if formatErr != nil || canonical != value {
		return "", "", "", errors.New("invalid DNS cluster commit phase")
	}
	return state, requestID, qualifier, nil
}

func validateDNSClusterConfigJournal(journal *dnsClusterConfigJournal) error {
	if journal == nil || journal.Version != dnsClusterConfigJournalVersion ||
		!validMutationIdentity(journal.RequestID) ||
		!mutationpayload.ValidDNSClusterConfigQualifier(journal.Qualifier) {
		return errors.New("DNS cluster journal identity is invalid")
	}
	commitment, err := mutationpayload.CanonicalDNSClusterConfig(
		journal.Role, journal.PeerIP, journal.PeerNS,
	)
	if err != nil || commitment.Qualifier != journal.Qualifier {
		return errors.New("DNS cluster journal payload is not canonical")
	}
	return nil
}

func encodeDNSClusterConfigJournal(journal *dnsClusterConfigJournal) ([]byte, error) {
	if err := validateDNSClusterConfigJournal(journal); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(journal)
	if err != nil {
		return nil, err
	}
	if len(raw) > dnsClusterConfigJournalMaxSize {
		return nil, errors.New("DNS cluster journal exceeds the size limit")
	}
	return raw, nil
}

func decodeDNSClusterConfigJournal(raw []byte) (*dnsClusterConfigJournal, error) {
	if len(raw) == 0 || len(raw) > dnsClusterConfigJournalMaxSize {
		return nil, errors.New("DNS cluster journal has invalid size")
	}
	var journal dnsClusterConfigJournal
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&journal); err != nil {
		return nil, fmt.Errorf("decode DNS cluster journal: %w", err)
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("DNS cluster journal contains trailing data")
	}
	canonical, err := encodeDNSClusterConfigJournal(&journal)
	if err != nil || !bytes.Equal(raw, canonical) {
		return nil, errors.New("DNS cluster journal is not canonical")
	}
	return &journal, nil
}

func dnsClusterConfigJournalPath(manager *serviceMutationManager) string {
	if manager == nil {
		return ""
	}
	return filepath.Join(filepath.Dir(manager.ledgerPath), dnsClusterConfigJournalFileName)
}

func readDNSClusterConfigJournal(path string) (*dnsClusterConfigJournal, bool, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path ||
		filepath.Base(path) != dnsClusterConfigJournalFileName {
		return nil, false, errors.New("invalid DNS cluster journal path")
	}
	raw, exists, err := readSecureServiceMutationLedger(path, dnsClusterConfigJournalMaxSize)
	if err != nil || !exists {
		return nil, exists, err
	}
	journal, err := decodeDNSClusterConfigJournal(raw)
	return journal, true, err
}

func writeDNSClusterConfigJournal(path string, journal *dnsClusterConfigJournal) error {
	raw, err := encodeDNSClusterConfigJournal(journal)
	if err != nil {
		return err
	}
	if !filepath.IsAbs(path) || filepath.Clean(path) != path ||
		filepath.Base(path) != dnsClusterConfigJournalFileName {
		return errors.New("invalid DNS cluster journal path")
	}
	dir := filepath.Dir(path)
	if err := ensureSecureServiceMutationStateDirectory(dir); err != nil {
		return err
	}
	if _, _, err := readDNSClusterConfigJournal(path); err != nil {
		return fmt.Errorf("validate existing DNS cluster journal: %w", err)
	}
	stage, err := os.CreateTemp(dir, ".dns-cluster-config-journal-*.json")
	if err != nil {
		return err
	}
	stagePath := stage.Name()
	published := false
	defer func() {
		_ = stage.Close()
		if !published {
			_ = os.Remove(stagePath)
		}
	}()
	if err := stage.Chown(int(serviceMutationRequiredOwnerUID), int(serviceMutationRequiredOwnerGID)); err != nil {
		return err
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
	if err := os.Rename(stagePath, path); err != nil {
		return err
	}
	published = true
	return syncServiceMutationDirectory(path)
}

func cleanupAbandonedDNSClusterConfigJournalStages(stateDir string) error {
	entries, err := os.ReadDir(stateDir)
	if err != nil {
		return err
	}
	var stages []string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".dns-cluster-config-journal-") &&
			strings.HasSuffix(entry.Name(), ".json") {
			stages = append(stages, filepath.Join(stateDir, entry.Name()))
		}
	}
	if len(stages) > dnsClusterConfigJournalStageLimit {
		return errors.New("abandoned DNS cluster journal stage count exceeds the limit")
	}
	sort.Strings(stages)
	for _, stage := range stages {
		raw, exists, err := readSecureServiceMutationLedger(stage, dnsClusterConfigJournalMaxSize)
		if err != nil || !exists {
			return errors.New("invalid abandoned DNS cluster journal stage")
		}
		if _, err := decodeDNSClusterConfigJournal(raw); err != nil {
			return err
		}
	}
	for _, stage := range stages {
		if err := os.Remove(stage); err != nil {
			return err
		}
	}
	if len(stages) != 0 {
		return syncServiceMutationDirectory(
			filepath.Join(stateDir, dnsClusterConfigJournalFileName),
		)
	}
	return nil
}

func activeDirectDNSClusterConfigJob(job *ServiceMutationJob) bool {
	return job != nil && serviceMutationStatusActive(job.Status) &&
		job.Kind == "dns_cluster_configure" && job.Target == "pdns"
}

func dnsClusterConfigJobMatchesJournal(job *ServiceMutationJob, journal *dnsClusterConfigJournal) bool {
	return activeDirectDNSClusterConfigJob(job) && journal != nil &&
		job.RequestID == journal.RequestID && job.PackageName == journal.Qualifier
}

func commitDNSClusterConfigIntent(
	ctx context.Context,
	commitment mutationpayload.DNSClusterConfigCommitment,
) (*dnsClusterConfigJournal, error) {
	tracker, _ := ctx.Value(serviceMutationExecutionTrackerKey{}).(*serviceMutationExecutionTracker)
	if tracker == nil || tracker.manager == nil || tracker.runtime == nil {
		return nil, errors.New("DNS cluster commit requires a durable execution tracker")
	}
	m, runtime := tracker.manager, tracker.runtime
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.healthErrorLocked(); err != nil {
		return nil, err
	}
	job := runtime.job
	if m.active != runtime || runtime.steps != 1 || job == nil ||
		job.Status != serviceMutationStatusRunning || job.WorkerPID != 0 ||
		job.Kind != "dns_cluster_configure" || job.Target != "pdns" ||
		job.PackageName != commitment.Qualifier {
		return nil, errors.New("DNS cluster commit lost its exact direct identity")
	}
	if ctx.Err() != nil || !m.now().Before(job.LeaseExpiresAt) ||
		!m.now().Before(job.DeadlineAt) {
		return nil, errors.New("service mutation lease ended before DNS cluster commit intent")
	}
	journal := &dnsClusterConfigJournal{
		Version: dnsClusterConfigJournalVersion, RequestID: job.RequestID,
		Role: commitment.Role, PeerIP: commitment.PeerIP,
		PeerNS: commitment.PeerNS, Qualifier: commitment.Qualifier,
	}
	if err := writeDNSClusterConfigJournal(dnsClusterConfigJournalPath(m), journal); err != nil {
		return nil, m.poisonLocked(fmt.Errorf("persist DNS cluster journal: %w", err))
	}
	phase, err := formatDNSClusterConfigCommitPhase(
		dnsClusterConfigCommitIntent, job.RequestID, job.PackageName,
	)
	if err != nil {
		return nil, m.poisonLocked(err)
	}
	before := cloneServiceMutationLedger(m.ledger)
	job.Phase = phase
	job.UpdatedAt = m.now()
	if err := m.persistLedgerMutationLocked(before); err != nil {
		return nil, err
	}
	runtime.dnsClusterConfigCommittedPhase = phase
	return journal, nil
}

func poisonDNSClusterConfigConvergence(ctx context.Context, cause error) error {
	tracker, _ := ctx.Value(serviceMutationExecutionTrackerKey{}).(*serviceMutationExecutionTracker)
	if tracker == nil || tracker.manager == nil || tracker.runtime == nil || cause == nil {
		return errors.New("DNS cluster poison requires a committed durable tracker")
	}
	m, runtime := tracker.manager, tracker.runtime
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active != runtime || runtime.steps != 1 ||
		runtime.dnsClusterConfigCommittedPhase == "" {
		return errors.New("DNS cluster poison lost the committed mutation")
	}
	return m.poisonLocked(fmt.Errorf("DNS cluster host convergence is ambiguous: %w", cause))
}

func publishDNSClusterConfig(ctx context.Context, journal *dnsClusterConfigJournal) error {
	tracker, _ := ctx.Value(serviceMutationExecutionTrackerKey{}).(*serviceMutationExecutionTracker)
	if tracker == nil || tracker.manager == nil || tracker.runtime == nil {
		return errors.New("DNS cluster publication requires a durable execution tracker")
	}
	m, runtime := tracker.manager, tracker.runtime
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.healthErrorLocked(); err != nil {
		return err
	}
	if m.active != runtime || runtime.steps != 1 ||
		runtime.dnsClusterConfigCommittedPhase == "" ||
		!dnsClusterConfigJobMatchesJournal(runtime.job, journal) {
		return errors.New("DNS cluster publication lost the committed identity")
	}
	persisted, exists, err := readDNSClusterConfigJournal(dnsClusterConfigJournalPath(m))
	if err != nil || !exists || !dnsClusterConfigJobMatchesJournal(runtime.job, persisted) {
		if err == nil {
			err = errors.New("DNS cluster journal identity changed")
		}
		return m.poisonLocked(err)
	}
	persistedRaw, persistedErr := encodeDNSClusterConfigJournal(persisted)
	journalRaw, journalErr := encodeDNSClusterConfigJournal(journal)
	if persistedErr != nil || journalErr != nil || !bytes.Equal(persistedRaw, journalRaw) {
		return m.poisonLocked(errors.New("DNS cluster journal payload changed before publication"))
	}
	phase, err := formatDNSClusterConfigCommitPhase(
		dnsClusterConfigCommitPublished, runtime.job.RequestID, runtime.job.PackageName,
	)
	if err != nil {
		return m.poisonLocked(err)
	}
	runtime.dnsClusterConfigCommittedPhase = phase
	if err := m.finishRuntimeTerminalLocked(runtime, true, phase, "", ""); err != nil {
		if m.active == runtime {
			return m.poisonLocked(fmt.Errorf("persist terminal DNS cluster receipt: %w", err))
		}
		return err
	}
	return nil
}

var recoverDNSClusterConfigHost = func(
	ctx context.Context,
	journal *dnsClusterConfigJournal,
) error {
	commitment, err := mutationpayload.CanonicalDNSClusterConfig(
		journal.Role, journal.PeerIP, journal.PeerNS,
	)
	if err != nil || commitment.Qualifier != journal.Qualifier {
		return errors.New("DNS cluster recovery journal payload is invalid")
	}
	_, err = convergeDNSClusterConfig(ctx, commitment)
	return err
}

func (m *serviceMutationManager) recoverPersistedDNSClusterConfigLocked(
	job *ServiceMutationJob,
	lock *serviceMutationFileLock,
) (bool, error) {
	if !activeDirectDNSClusterConfigJob(job) {
		return false, nil
	}
	if !mutationpayload.ValidDNSClusterConfigQualifier(job.PackageName) {
		m.poisonLock = lock
		return true, m.poisonLocked(errors.New("active DNS cluster mutation has an invalid qualifier"))
	}
	intent := false
	if strings.HasPrefix(job.Phase, dnsClusterConfigCommitPhasePrefix) {
		state, requestID, qualifier, err := parseDNSClusterConfigCommitPhase(job.Phase)
		if err != nil || state != dnsClusterConfigCommitIntent ||
			requestID != job.RequestID || qualifier != job.PackageName {
			m.poisonLock = lock
			return true, m.poisonLocked(errors.New("active DNS cluster mutation has an invalid commit receipt"))
		}
		intent = true
	}
	if serviceMutationWorkerMatches(job.WorkerPID, job.WorkerStarted) {
		before := cloneServiceMutationLedger(m.ledger)
		job.Status = serviceMutationStatusOrphaned
		if !intent {
			job.Phase = "waiting_for_orphaned_process"
		}
		job.ErrorCode = "agent_restart_worker_alive"
		job.ErrorMessage = "The previous DNS cluster worker is still alive."
		job.UpdatedAt = m.now()
		err := m.persistLedgerMutationLocked(before)
		if m.poisoned != nil {
			m.poisonLock = lock
			return true, err
		}
		return true, errors.Join(err, lock.Close())
	}
	journal, exists, err := readDNSClusterConfigJournal(dnsClusterConfigJournalPath(m))
	if err != nil {
		m.poisonLock = lock
		return true, m.poisonLocked(err)
	}
	if !intent {
		err := m.finishPersistedOrphanLocked(job,
			"agent_restarted_before_dns_cluster_commit",
			"The agent restarted before the DNS cluster commit decision was durable.")
		if m.poisoned != nil {
			m.poisonLock = lock
			return true, err
		}
		return true, errors.Join(err, lock.Close())
	}
	if !exists || !dnsClusterConfigJobMatchesJournal(job, journal) {
		m.poisonLock = lock
		return true, m.poisonLocked(errors.New("committed DNS cluster mutation lost its exact recovery journal"))
	}
	recoveryBase, cancel := context.WithTimeout(context.Background(), dnsClusterConfigRecoveryTimeout)
	runtime := &serviceMutationRuntime{
		job: job, lock: lock, ctx: recoveryBase, cancel: cancel,
		dnsClusterConfigCommittedPhase: job.Phase,
	}
	m.mu.Unlock()
	runtime.stepMu.Lock()
	m.mu.Lock()
	if m.active != nil || m.ledger.ActiveRequestID != job.RequestID {
		cancel()
		m.poisonLock = lock
		err := m.poisonLocked(errors.New("DNS cluster recovery identity changed"))
		m.mu.Unlock()
		runtime.stepMu.Unlock()
		m.mu.Lock()
		return true, err
	}
	m.active = runtime
	runtime.steps = 1
	before := cloneServiceMutationLedger(m.ledger)
	job.Status = serviceMutationStatusCancelling
	job.ErrorCode = "agent_restart_during_dns_cluster_commit"
	job.ErrorMessage = "The agent is completing a durable DNS cluster commit after restart."
	job.WorkerPID, job.WorkerStarted, job.WorkerCommand = 0, "", ""
	job.UpdatedAt = m.now()
	if err := m.persistLedgerMutationLocked(before); err != nil {
		poisonErr := m.poisonLocked(err)
		runtime.steps = 0
		m.mu.Unlock()
		runtime.stepMu.Unlock()
		m.mu.Lock()
		return true, poisonErr
	}
	tracker := &serviceMutationExecutionTracker{
		manager: m, runtime: runtime, allowCancellingRecovery: true,
	}
	recoveryCtx := context.WithValue(recoveryBase, serviceMutationExecutionTrackerKey{}, tracker)
	m.mu.Unlock()
	recoveryErr := recoverDNSClusterConfigHost(recoveryCtx, journal)
	cancel()
	m.mu.Lock()
	runtime.steps = 0
	m.mu.Unlock()
	runtime.stepMu.Unlock()
	m.mu.Lock()
	if recoveryErr != nil {
		return true, m.poisonLocked(fmt.Errorf("recover committed DNS cluster plan: %w", recoveryErr))
	}
	phase, err := formatDNSClusterConfigCommitPhase(
		dnsClusterConfigCommitPublished, job.RequestID, job.PackageName,
	)
	if err != nil {
		return true, m.poisonLocked(err)
	}
	runtime.dnsClusterConfigCommittedPhase = phase
	if err := m.finishRuntimeTerminalLocked(runtime, true, phase, "", ""); err != nil {
		return true, m.poisonLocked(fmt.Errorf("persist recovered DNS cluster success: %w", err))
	}
	return true, nil
}
