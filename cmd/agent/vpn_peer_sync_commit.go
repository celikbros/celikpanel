package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
)

const (
	vpnPeerSyncCommitPhasePrefix   = "commit/vpn-peer-sync/v1/"
	vpnPeerSyncReceiptMarkerPrefix = "# CelikPanel VPN peer sync receipt v1: "

	vpnPeerSyncCommitIntent    = "intent"
	vpnPeerSyncCommitPublished = "published"

	vpnPeerSyncRecoveryTimeout = 30 * time.Second
	vpnPeerSyncConfigMaxSize   = 1 << 20
	vpnPeerSyncStageMaxCount   = 32
)

func formatVPNPeerSyncCommitPhase(state, requestID, qualifier string) (string, error) {
	if (state != vpnPeerSyncCommitIntent && state != vpnPeerSyncCommitPublished) ||
		!validMutationIdentity(requestID) ||
		!mutationpayload.ValidVPNPeerSyncQualifier(qualifier) {
		return "", errors.New("invalid VPN peer sync commit phase identity")
	}
	return vpnPeerSyncCommitPhasePrefix + state + "/" + requestID + "/" + qualifier, nil
}

func parseVPNPeerSyncCommitPhase(value string) (
	state, requestID, qualifier string,
	err error,
) {
	if !strings.HasPrefix(value, vpnPeerSyncCommitPhasePrefix) {
		return "", "", "", errors.New("not a VPN peer sync commit phase")
	}
	remainder := strings.TrimPrefix(value, vpnPeerSyncCommitPhasePrefix)
	state, remainder, found := strings.Cut(remainder, "/")
	if !found {
		return "", "", "", errors.New("invalid VPN peer sync commit phase")
	}
	requestID, qualifier, found = strings.Cut(remainder, "/")
	if !found {
		return "", "", "", errors.New("invalid VPN peer sync commit phase")
	}
	canonical, err := formatVPNPeerSyncCommitPhase(state, requestID, qualifier)
	if err != nil || canonical != value {
		return "", "", "", errors.New("invalid VPN peer sync commit phase")
	}
	return state, requestID, qualifier, nil
}

func formatVPNPeerSyncReceiptMarker(requestID, qualifier string) (string, error) {
	if !validMutationIdentity(requestID) ||
		!mutationpayload.ValidVPNPeerSyncQualifier(qualifier) {
		return "", errors.New("invalid VPN peer sync receipt identity")
	}
	return vpnPeerSyncReceiptMarkerPrefix + requestID + " " + qualifier, nil
}

func parseVPNPeerSyncReceiptMarker(config []byte) (
	requestID, qualifier string,
	found bool,
	err error,
) {
	for _, line := range strings.Split(string(config), "\n") {
		if !strings.HasPrefix(line, vpnPeerSyncReceiptMarkerPrefix) {
			continue
		}
		if found {
			return "", "", false, errors.New("VPN config contains multiple peer sync receipts")
		}
		fields := strings.Split(strings.TrimPrefix(line, vpnPeerSyncReceiptMarkerPrefix), " ")
		if len(fields) != 2 {
			return "", "", false, errors.New("VPN config contains an invalid peer sync receipt")
		}
		canonical, markerErr := formatVPNPeerSyncReceiptMarker(fields[0], fields[1])
		if markerErr != nil || canonical != line {
			return "", "", false, errors.New("VPN config contains an invalid peer sync receipt")
		}
		requestID, qualifier, found = fields[0], fields[1], true
	}
	return requestID, qualifier, found, nil
}

func replaceVPNPeerSyncReceiptMarker(
	interfaceConfig, requestID, qualifier string,
) (string, error) {
	marker, err := formatVPNPeerSyncReceiptMarker(requestID, qualifier)
	if err != nil {
		return "", err
	}
	withoutReceipt, err := removeVPNPeerSyncReceiptMarker(interfaceConfig)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(withoutReceipt, "\n") + "\n" + marker + "\n", nil
}

func removeVPNPeerSyncReceiptMarker(interfaceConfig string) (string, error) {
	lines := strings.Split(strings.TrimRight(interfaceConfig, "\n"), "\n")
	filtered := make([]string, 0, len(lines))
	receiptSeen := false
	for _, line := range lines {
		if !strings.HasPrefix(line, vpnPeerSyncReceiptMarkerPrefix) {
			filtered = append(filtered, line)
			continue
		}
		if receiptSeen {
			return "", errors.New("VPN config contains multiple peer sync receipts")
		}
		if _, _, found, parseErr := parseVPNPeerSyncReceiptMarker([]byte(line)); parseErr != nil || !found {
			return "", errors.New("VPN config contains an invalid peer sync receipt")
		}
		receiptSeen = true
	}
	return strings.Join(filtered, "\n") + "\n", nil
}

func vpnPeerSyncCommitIdentity(
	ctx context.Context,
	qualifier string,
) (requestID string, err error) {
	tracker, _ := ctx.Value(serviceMutationExecutionTrackerKey{}).(*serviceMutationExecutionTracker)
	if tracker == nil || tracker.manager == nil || tracker.runtime == nil {
		return "", errors.New("VPN peer sync commit identity requires a durable execution tracker")
	}
	m, runtime := tracker.manager, tracker.runtime
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.healthErrorLocked(); err != nil {
		return "", err
	}
	if m.active != runtime || runtime.job == nil || runtime.steps != 1 ||
		runtime.job.Status != serviceMutationStatusRunning {
		return "", errors.New("VPN peer sync commit identity lost the active mutation step")
	}
	job := runtime.job
	if job.Kind != "vpn_peer_sync" || job.Target != "wireguard" ||
		job.PackageName != qualifier ||
		!mutationpayload.ValidVPNPeerSyncQualifier(job.PackageName) {
		return "", errors.New("VPN peer sync commit identity does not match the active job")
	}
	return job.RequestID, nil
}

// poisonVPNPeerSyncRollback keeps the active host flock when a failed or
// cancelled live mutation cannot prove that both live and durable state were
// restored. The deferred step release then only drops the step counter; a
// panel Finish(false) and every later Begin remain fail-closed.
func poisonVPNPeerSyncRollback(ctx context.Context, cause error) error {
	if ctx == nil || cause == nil {
		return errors.New("invalid VPN peer sync rollback poison request")
	}
	tracker, _ := ctx.Value(serviceMutationExecutionTrackerKey{}).(*serviceMutationExecutionTracker)
	if tracker == nil || tracker.manager == nil || tracker.runtime == nil {
		return errors.New("VPN peer sync rollback poison requires a durable execution tracker")
	}
	m, runtime := tracker.manager, tracker.runtime
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active != runtime || runtime.job == nil || runtime.steps != 1 ||
		(runtime.job.Status != serviceMutationStatusRunning &&
			runtime.job.Status != serviceMutationStatusCancelling) {
		return errors.New("VPN peer sync rollback poison lost the active mutation step")
	}
	return m.poisonLocked(fmt.Errorf("VPN peer sync rollback could not prove the previous host state: %w", cause))
}

func verifyPublishedVPNPeerSyncReceipt(requestID, qualifier string) (bool, error) {
	config, err := readSecureVPNConfig()
	if err != nil {
		return false, err
	}
	actualRequestID, actualQualifier, found, err := parseVPNPeerSyncReceiptMarker(config)
	if err != nil {
		return false, err
	}
	return found && actualRequestID == requestID && actualQualifier == qualifier, nil
}

func activeDirectVPNPeerSyncJob(job *ServiceMutationJob) bool {
	return job != nil && serviceMutationStatusActive(job.Status) &&
		job.Kind == "vpn_peer_sync" && job.Target == "wireguard"
}

func canonicalVPNPeerSyncStageName(name string) bool {
	prefix := "." + filepath.Base(wgConfPath()) + ".tmp-"
	if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".conf") {
		return false
	}
	random := strings.TrimSuffix(strings.TrimPrefix(name, prefix), ".conf")
	if random == "" || len(random) > 20 {
		return false
	}
	for index := range random {
		if random[index] < '0' || random[index] > '9' {
			return false
		}
	}
	return true
}

func readSecureVPNPeerSyncStage(path string) ([]byte, error) {
	if filepath.Dir(filepath.Clean(path)) != filepath.Clean(wgConfDir) ||
		!canonicalVPNPeerSyncStageName(filepath.Base(path)) {
		return nil, errors.New("invalid VPN peer sync stage path")
	}
	file, info, err := secureOpenRegular(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	if err := validateRepoFileMetadata(info, 0o600); err != nil {
		return nil, errors.New("VPN peer sync stage failed security validation")
	}
	data, err := io.ReadAll(io.LimitReader(file, vpnPeerSyncConfigMaxSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > vpnPeerSyncConfigMaxSize {
		return nil, errors.New("VPN peer sync stage exceeds the size limit")
	}
	after, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(info, after) || after.Mode() != info.Mode() {
		return nil, errors.New("VPN peer sync stage changed while it was read")
	}
	return data, nil
}

func findVPNPeerSyncRecoveryStage(requestID, qualifier string) (string, error) {
	if err := validateVPNDirectory(wgConfDir); err != nil {
		return "", err
	}
	entries, err := os.ReadDir(wgConfDir)
	if err != nil {
		return "", err
	}
	candidateCount := 0
	matching := ""
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, "."+filepath.Base(wgConfPath())+".tmp-") ||
			!strings.HasSuffix(name, ".conf") {
			continue
		}
		candidateCount++
		if candidateCount > vpnPeerSyncStageMaxCount || !canonicalVPNPeerSyncStageName(name) {
			return "", errors.New("VPN peer sync recovery stages are ambiguous")
		}
		path := filepath.Join(wgConfDir, name)
		config, err := readSecureVPNPeerSyncStage(path)
		if err != nil {
			return "", err
		}
		stageRequestID, stageQualifier, found, err := parseVPNPeerSyncReceiptMarker(config)
		if err != nil {
			return "", err
		}
		if !found || stageRequestID != requestID || stageQualifier != qualifier {
			continue
		}
		if matching != "" {
			return "", errors.New("multiple VPN peer sync recovery stages match the durable intent")
		}
		matching = path
	}
	return matching, nil
}

func removeVPNPeerSyncRecoveryStage(path string) error {
	if path == "" {
		return nil
	}
	if err := secureRemoveRegular(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return syncAtomicParentDirectory(filepath.Dir(path))
}

func removeLegacyVPNPeerSyncRecoveryStages() error {
	if err := validateVPNDirectory(wgConfDir); err != nil {
		return err
	}
	entries, err := os.ReadDir(wgConfDir)
	if err != nil {
		return err
	}
	candidateCount := 0
	removed := false
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, "."+filepath.Base(wgConfPath())+".tmp-") ||
			!strings.HasSuffix(name, ".conf") {
			continue
		}
		candidateCount++
		if candidateCount > vpnPeerSyncStageMaxCount || !canonicalVPNPeerSyncStageName(name) {
			return errors.New("legacy VPN peer sync recovery stages are ambiguous")
		}
		path := filepath.Join(wgConfDir, name)
		config, err := readSecureVPNPeerSyncStage(path)
		if err != nil {
			return err
		}
		if _, _, _, err := parseVPNPeerSyncReceiptMarker(config); err != nil {
			return err
		}
		if err := secureRemoveRegular(path); err != nil {
			return err
		}
		removed = true
	}
	if removed {
		return syncAtomicParentDirectory(filepath.Clean(wgConfDir))
	}
	return nil
}

// reconcilePersistedVPNPeerSyncHost makes the durable wg0.conf authoritative
// again after an agent crash. The atomic target rename is the only commit
// point: an intent never authorizes forward publication of a leftover stage.
func reconcilePersistedVPNPeerSyncHost(
	ctx context.Context,
	requestID, qualifier string,
) (success bool, err error) {
	diskConfig, err := readSecureVPNConfig()
	if err != nil {
		return false, fmt.Errorf("read VPN recovery target: %w", err)
	}
	targetRequestID, targetQualifier, markerFound, err := parseVPNPeerSyncReceiptMarker(diskConfig)
	if err != nil {
		return false, err
	}
	targetPublished := markerFound && targetRequestID == requestID && targetQualifier == qualifier
	if targetPublished {
		if err := syncAtomicParentDirectory(filepath.Dir(wgConfPath())); err != nil {
			return false, fmt.Errorf("stabilize published VPN recovery target: %w", err)
		}
	} else {
		stage, err := findVPNPeerSyncRecoveryStage(requestID, qualifier)
		if err != nil {
			return false, err
		}
		if stage != "" {
			if removeErr := removeVPNPeerSyncRecoveryStage(stage); removeErr != nil {
				return false, fmt.Errorf("remove uncommitted VPN peer recovery stage: %w", removeErr)
			}
		}
	}

	interfaceUp, err := probeWireGuardInterface(ctx)
	if err != nil {
		return false, fmt.Errorf("probe VPN interface during recovery: %w", err)
	}
	if interfaceUp {
		if err := applyWireGuardBytes(ctx, diskConfig); err != nil {
			return false, fmt.Errorf("reconcile live VPN interface from durable config: %w", err)
		}
	}
	return targetPublished, nil
}

func reconcilePersistedLegacyVPNPeerSyncHost(ctx context.Context) error {
	diskConfig, err := readSecureVPNConfig()
	if err != nil {
		return fmt.Errorf("read legacy VPN recovery target: %w", err)
	}
	// A valid receipt may belong to an earlier completed bound update, but it
	// can never turn an unbound legacy job into success. Malformed receipts are
	// still ambiguous and fail closed.
	if _, _, _, err := parseVPNPeerSyncReceiptMarker(diskConfig); err != nil {
		return err
	}
	if err := removeLegacyVPNPeerSyncRecoveryStages(); err != nil {
		return err
	}
	interfaceUp, err := probeWireGuardInterface(ctx)
	if err != nil {
		return fmt.Errorf("probe VPN interface during legacy recovery: %w", err)
	}
	if interfaceUp {
		if err := applyWireGuardBytes(ctx, diskConfig); err != nil {
			return fmt.Errorf("reconcile legacy live VPN interface from durable config: %w", err)
		}
	}
	return nil
}

// recoverPersistedVPNPeerSyncLocked handles every active direct peer-sync job
// with no live recorded worker. It temporarily installs a tracked runtime so
// recovery probes and syncconf commands retain the same crash-safe PID ledger.
// The caller holds m.mu and the host flock. The helper always returns with
// m.mu held; on ambiguity it deliberately retains the flock through m.active.
func (m *serviceMutationManager) recoverPersistedVPNPeerSyncLocked(
	job *ServiceMutationJob,
	lock *serviceMutationFileLock,
) (handled bool, err error) {
	if !activeDirectVPNPeerSyncJob(job) ||
		serviceMutationWorkerMatches(job.WorkerPID, job.WorkerStarted) {
		return false, nil
	}
	bound := mutationpayload.ValidVPNPeerSyncQualifier(job.PackageName)
	legacy := job.PackageName == ""
	if !bound && !legacy {
		m.poisonLock = lock
		return true, m.poisonLocked(errors.New("active VPN peer sync has an invalid payload qualifier"))
	}
	intent := false
	if bound {
		state, _, _, phaseErr := parseVPNPeerSyncCommitPhase(job.Phase)
		intent = phaseErr == nil && state == vpnPeerSyncCommitIntent
	}

	recoveryBase, cancel := context.WithTimeout(context.Background(), vpnPeerSyncRecoveryTimeout)
	runtime := &serviceMutationRuntime{job: job, lock: lock, ctx: recoveryBase, cancel: cancel}
	// Preserve the global lock order used by normal steps: stepMu, then m.mu.
	m.mu.Unlock()
	runtime.stepMu.Lock()
	m.mu.Lock()
	if m.active != nil || m.ledger.ActiveRequestID != job.RequestID {
		cancel()
		m.poisonLock = lock
		identityErr := m.poisonLocked(errors.New("VPN peer sync recovery identity changed"))
		m.mu.Unlock()
		runtime.stepMu.Unlock()
		m.mu.Lock()
		return true, identityErr
	}
	m.active = runtime
	runtime.steps = 1
	before := cloneServiceMutationLedger(m.ledger)
	runtime.job.Status = serviceMutationStatusCancelling
	if !intent {
		runtime.job.Phase = "recovering_vpn_peer_sync"
	}
	runtime.job.ErrorCode = "agent_restart_during_vpn_peer_sync"
	runtime.job.ErrorMessage = "The agent is reconciling durable and live VPN state after a restart."
	runtime.job.WorkerPID = 0
	runtime.job.WorkerStarted = ""
	runtime.job.WorkerCommand = ""
	runtime.job.UpdatedAt = m.now()
	if persistErr := m.persistLedgerMutationLocked(before); persistErr != nil {
		poisonErr := m.poisonLocked(fmt.Errorf("persist VPN peer sync recovery intent: %w", persistErr))
		runtime.steps = 0
		m.mu.Unlock()
		runtime.stepMu.Unlock()
		m.mu.Lock()
		return true, poisonErr
	}
	tracker := &serviceMutationExecutionTracker{
		manager:                 m,
		runtime:                 runtime,
		allowCancellingRecovery: true,
	}
	recoveryCtx := context.WithValue(
		recoveryBase,
		serviceMutationExecutionTrackerKey{},
		tracker,
	)
	m.mu.Unlock()
	success := false
	var recoveryErr error
	if bound {
		success, recoveryErr = reconcilePersistedVPNPeerSyncHost(
			recoveryCtx,
			runtime.job.RequestID,
			runtime.job.PackageName,
		)
	} else {
		recoveryErr = reconcilePersistedLegacyVPNPeerSyncHost(recoveryCtx)
	}
	m.mu.Lock()
	runtime.steps = 0
	m.mu.Unlock()
	runtime.stepMu.Unlock()
	m.mu.Lock()
	if recoveryErr != nil {
		return true, m.poisonLocked(recoveryErr)
	}
	if success {
		publishedPhase, phaseErr := formatVPNPeerSyncCommitPhase(
			vpnPeerSyncCommitPublished,
			runtime.job.RequestID,
			runtime.job.PackageName,
		)
		if phaseErr != nil {
			return true, m.poisonLocked(phaseErr)
		}
		if finishErr := m.finishRuntimeTerminalLocked(runtime, true, publishedPhase, "", ""); finishErr != nil {
			return true, m.poisonLocked(fmt.Errorf("persist recovered VPN peer sync success: %w", finishErr))
		}
		return true, nil
	}
	if finishErr := m.finishRuntimeTerminalLocked(
		runtime,
		false,
		"interrupted",
		"agent_restarted_before_vpn_peer_commit",
		"The agent restored the durable VPN configuration after a restart before the peer update commit point.",
	); finishErr != nil {
		return true, m.poisonLocked(fmt.Errorf("persist recovered VPN peer sync failure: %w", finishErr))
	}
	return true, nil
}

// commitStandaloneVPNPeerSyncStep is the direct V2 mutation's linearization
// gate. The caller already holds runtime.stepMu through acquireStep; m.mu gives
// cancel, expiry, heartbeat, and finish a strict before/after order around the
// durable intent, host rename+fsync, and terminal receipt writes.
func commitStandaloneVPNPeerSyncStep(
	ctx context.Context,
	commit func() error,
) (hostPublished bool, err error) {
	if ctx == nil || commit == nil {
		return false, errors.New("invalid VPN peer sync commit gate")
	}
	tracker, _ := ctx.Value(serviceMutationExecutionTrackerKey{}).(*serviceMutationExecutionTracker)
	if tracker == nil || tracker.manager == nil || tracker.runtime == nil {
		return false, errors.New("VPN peer sync commit gate requires a durable execution tracker")
	}
	m, runtime := tracker.manager, tracker.runtime
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.healthErrorLocked(); err != nil {
		return false, err
	}
	if m.active != runtime || runtime.job == nil || runtime.steps != 1 ||
		runtime.job.WorkerPID != 0 || runtime.job.Status != serviceMutationStatusRunning {
		return false, errors.New("VPN peer sync commit gate lost the active mutation step")
	}
	job := runtime.job
	if job.Kind != "vpn_peer_sync" || job.Target != "wireguard" ||
		!mutationpayload.ValidVPNPeerSyncQualifier(job.PackageName) {
		return false, errors.New("VPN peer sync commit gate rejected the mutation identity")
	}
	now := m.now()
	if ctx.Err() != nil || !now.Before(job.LeaseExpiresAt) || !now.Before(job.DeadlineAt) {
		return false, errors.New("service mutation lease ended before the VPN peer commit point")
	}
	intentPhase, err := formatVPNPeerSyncCommitPhase(
		vpnPeerSyncCommitIntent,
		job.RequestID,
		job.PackageName,
	)
	if err != nil {
		return false, err
	}
	publishedPhase, err := formatVPNPeerSyncCommitPhase(
		vpnPeerSyncCommitPublished,
		job.RequestID,
		job.PackageName,
	)
	if err != nil {
		return false, err
	}
	before := cloneServiceMutationLedger(m.ledger)
	job.Phase = intentPhase
	job.UpdatedAt = now
	if err := m.persistLedgerMutationLocked(before); err != nil {
		return false, err
	}
	commitErr := commit()
	if commitErr != nil {
		published, verifyErr := verifyPublishedVPNPeerSyncReceipt(job.RequestID, job.PackageName)
		if verifyErr != nil {
			return false, m.poisonLocked(fmt.Errorf(
				"verify VPN peer sync publication after commit error: %w",
				verifyErr,
			))
		}
		if !published {
			return false, commitErr
		}
		if syncErr := syncAtomicParentDirectory(filepath.Dir(wgConfPath())); syncErr != nil {
			runtime.vpnPeerSyncPublishedPhase = publishedPhase
			return true, m.poisonLocked(fmt.Errorf(
				"stabilize verified VPN peer sync publication: %w",
				syncErr,
			))
		}
	}
	runtime.vpnPeerSyncPublishedPhase = publishedPhase
	if err := m.finishRuntimeTerminalLocked(runtime, true, publishedPhase, "", ""); err != nil {
		if m.active == runtime {
			return true, m.poisonLocked(fmt.Errorf(
				"persist terminal VPN peer sync receipt after host publication: %w",
				err,
			))
		}
		return true, err
	}
	return true, nil
}
