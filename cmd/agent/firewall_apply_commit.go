package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

const (
	firewallApplyCommitPhasePrefix = "commit/firewall-apply/v1/"
	firewallApplyJournalFileName   = "firewall-apply-journal.json"
	firewallApplyJournalVersion    = 1
	firewallApplyJournalMaxSize    = 128 << 10
	firewallApplyJournalStageLimit = 8

	firewallApplyCommitIntent    = "intent"
	firewallApplyCommitPublished = "published"

	firewallApplyConvergenceTimeout = 45 * time.Second

	firewallApplyJournalFaultBeforeRename = "before_rename"
	firewallApplyJournalFaultAfterRename  = "after_rename_before_directory_sync"
	firewallApplyJournalFaultAfterSync    = "after_directory_sync"
)

var firewallApplyJournalFaultHook func(string) error

// Replaceable only by focused startup-recovery tests. Production always uses
// the fixed nft/systemctl runner and fixed root-owned snapshot path.
var recoverFirewallApplyHost = func(
	ctx context.Context,
	journal *firewallApplyJournal,
) error {
	firewallMu.Lock()
	defer firewallMu.Unlock()
	return convergeFirewallApplyPlan(
		ctx,
		journal,
		hostFirewallCommandRunner{ctx: ctx},
		fileFirewallStateStore{path: firewallSnapshotPath},
	)
}

type firewallApplyJournal struct {
	Version   int    `json:"version"`
	RequestID string `json:"request_id"`
	Qualifier string `json:"qualifier"`
	Enabled   bool   `json:"enabled"`
	Persist   bool   `json:"persist"`
	TCPPorts  []int  `json:"tcp_ports,omitempty"`
	UDPPorts  []int  `json:"udp_ports,omitempty"`
	SSHPorts  []int  `json:"ssh_ports,omitempty"`
	// NoSSHService records that this host was proven to carry no SSH service
	// when the intent was written. It is the only thing that makes an enabled
	// journal with an empty SSH port set valid, so recovery after a crash
	// replays exactly the plan the operator accepted and nothing wider.
	// NoSSHService, niyet yazıldığında bu sunucuda hiç SSH servisi olmadığının
	// kanıtlandığını kaydeder. Açık bir günlüğü boş SSH port kümesiyle geçerli
	// kılan tek şey budur; böylece bir çökme sonrası kurtarma, operatörün kabul
	// ettiği planın tam olarak aynısını, daha genişini değil, yeniden oynatır.
	NoSSHService        bool   `json:"no_ssh_service,omitempty"`
	PriorSnapshotExists bool   `json:"prior_snapshot_exists"`
	PriorSnapshot       []byte `json:"prior_snapshot,omitempty"`
}

func formatFirewallApplyCommitPhase(state, requestID, qualifier string) (string, error) {
	if (state != firewallApplyCommitIntent && state != firewallApplyCommitPublished) ||
		!validMutationIdentity(requestID) ||
		!mutationpayload.ValidFirewallApplyQualifier(qualifier) {
		return "", errors.New("invalid firewall apply commit phase identity")
	}
	return firewallApplyCommitPhasePrefix + state + "/" + requestID + "/" + qualifier, nil
}

func parseFirewallApplyCommitPhase(value string) (
	state, requestID, qualifier string,
	err error,
) {
	if !strings.HasPrefix(value, firewallApplyCommitPhasePrefix) {
		return "", "", "", errors.New("not a firewall apply commit phase")
	}
	remainder := strings.TrimPrefix(value, firewallApplyCommitPhasePrefix)
	state, remainder, found := strings.Cut(remainder, "/")
	if !found {
		return "", "", "", errors.New("invalid firewall apply commit phase")
	}
	requestID, qualifier, found = strings.Cut(remainder, "/")
	if !found {
		return "", "", "", errors.New("invalid firewall apply commit phase")
	}
	canonical, formatErr := formatFirewallApplyCommitPhase(state, requestID, qualifier)
	if formatErr != nil || canonical != value {
		return "", "", "", errors.New("invalid firewall apply commit phase")
	}
	return state, requestID, qualifier, nil
}

func equalFirewallPorts(left, right []int) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func canonicalAgentFirewallPorts(ports []int) ([]int, error) {
	frozen := append([]int(nil), ports...)
	if len(frozen) > 4096 {
		return nil, errors.New("agent-derived firewall port set exceeds the limit")
	}
	for _, port := range frozen {
		if port < 1 || port > 65535 {
			return nil, errors.New("agent-derived firewall port is invalid")
		}
	}
	sort.Ints(frozen)
	result := frozen[:0]
	for _, port := range frozen {
		if len(result) == 0 || result[len(result)-1] != port {
			result = append(result, port)
		}
	}
	return result, nil
}

func validateFirewallApplyJournal(journal *firewallApplyJournal) error {
	if journal == nil || journal.Version != firewallApplyJournalVersion ||
		!validMutationIdentity(journal.RequestID) ||
		!mutationpayload.ValidFirewallApplyQualifier(journal.Qualifier) {
		return errors.New("firewall apply journal identity is invalid")
	}
	commitment, err := mutationpayload.CanonicalFirewallApply(
		journal.Enabled,
		journal.Persist,
		journal.TCPPorts,
		journal.UDPPorts,
	)
	if err != nil || commitment.Qualifier != journal.Qualifier ||
		!equalFirewallPorts(commitment.TCPPorts, journal.TCPPorts) ||
		!equalFirewallPorts(commitment.UDPPorts, journal.UDPPorts) {
		return errors.New("firewall apply journal payload is not canonical")
	}
	if !journal.Enabled && !journal.Persist {
		return errors.New("firewall apply journal contains forbidden live-only disable")
	}
	if !journal.Enabled &&
		(journal.PriorSnapshotExists || len(journal.PriorSnapshot) != 0) {
		return errors.New("disabled firewall journal contains unused prior snapshot data")
	}
	sshPorts, err := canonicalAgentFirewallPorts(journal.SSHPorts)
	if err != nil || !equalFirewallPorts(sshPorts, journal.SSHPorts) ||
		(journal.Enabled && len(sshPorts) == 0 && !journal.NoSSHService) ||
		(!journal.Enabled && len(sshPorts) != 0) {
		return errors.New("firewall apply journal SSH snapshot is invalid")
	}
	// The no-SSH escape is narrow on purpose: it may only stand on an enabled
	// journal that protects no SSH port at all. It can never widen a plan.
	// SSH'sız kaçış bilerek dardır: yalnız hiçbir SSH portunu korumayan, açık
	// bir günlükte durabilir. Bir planı asla genişletemez.
	if journal.NoSSHService && (!journal.Enabled || len(sshPorts) != 0) {
		return errors.New("firewall apply journal no-SSH marker is invalid")
	}
	if journal.PriorSnapshotExists {
		if len(journal.PriorSnapshot) == 0 ||
			len(journal.PriorSnapshot) > maxFirewallSnapshotSize {
			return errors.New("firewall apply journal prior snapshot is invalid")
		}
	} else if len(journal.PriorSnapshot) != 0 {
		return errors.New("firewall apply journal has bytes for an absent prior snapshot")
	}
	if journal.Enabled && journal.PriorSnapshotExists {
		if err := validateFirewallSnapshot(journal.PriorSnapshot); err != nil {
			return fmt.Errorf("firewall apply journal prior snapshot: %w", err)
		}
	}
	return nil
}

func encodeFirewallApplyJournal(journal *firewallApplyJournal) ([]byte, error) {
	if err := validateFirewallApplyJournal(journal); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(journal)
	if err != nil {
		return nil, fmt.Errorf("encode firewall apply journal: %w", err)
	}
	if len(raw) > firewallApplyJournalMaxSize {
		return nil, errors.New("firewall apply journal exceeds the size limit")
	}
	return raw, nil
}

func decodeFirewallApplyJournal(raw []byte) (*firewallApplyJournal, error) {
	if len(raw) == 0 || len(raw) > firewallApplyJournalMaxSize {
		return nil, errors.New("firewall apply journal has invalid size")
	}
	var journal firewallApplyJournal
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&journal); err != nil {
		return nil, fmt.Errorf("decode firewall apply journal: %w", err)
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("firewall apply journal contains multiple JSON values")
		}
		return nil, fmt.Errorf("decode firewall apply journal trailer: %w", err)
	}
	canonical, err := encodeFirewallApplyJournal(&journal)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(raw, canonical) {
		return nil, errors.New("firewall apply journal is not canonical")
	}
	return &journal, nil
}

func firewallApplyJournalPath(manager *serviceMutationManager) string {
	if manager == nil {
		return ""
	}
	return filepath.Join(filepath.Dir(manager.ledgerPath), firewallApplyJournalFileName)
}

func readFirewallApplyJournal(path string) (*firewallApplyJournal, bool, error) {
	if filepath.Base(path) != firewallApplyJournalFileName ||
		!filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, false, errors.New("invalid firewall apply journal path")
	}
	raw, exists, err := readSecureServiceMutationLedger(path, firewallApplyJournalMaxSize)
	if err != nil || !exists {
		return nil, exists, err
	}
	journal, err := decodeFirewallApplyJournal(raw)
	if err != nil {
		return nil, true, err
	}
	return journal, true, nil
}

func writeFirewallApplyJournal(path string, journal *firewallApplyJournal) error {
	raw, err := encodeFirewallApplyJournal(journal)
	if err != nil {
		return err
	}
	if filepath.Base(path) != firewallApplyJournalFileName ||
		!filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("invalid firewall apply journal path")
	}
	dir := filepath.Dir(path)
	if err := ensureSecureServiceMutationStateDirectory(dir); err != nil {
		return fmt.Errorf("secure firewall apply journal directory: %w", err)
	}
	if _, _, err := readFirewallApplyJournal(path); err != nil {
		return fmt.Errorf("validate existing firewall apply journal: %w", err)
	}
	stage, err := os.CreateTemp(dir, ".firewall-apply-journal-*.json")
	if err != nil {
		return fmt.Errorf("stage firewall apply journal: %w", err)
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
		return fmt.Errorf("set firewall apply journal owner: %w", err)
	}
	if err := stage.Chmod(0o600); err != nil {
		return fmt.Errorf("set firewall apply journal mode: %w", err)
	}
	if _, err := stage.Write(raw); err != nil {
		return fmt.Errorf("write firewall apply journal: %w", err)
	}
	if err := stage.Sync(); err != nil {
		return fmt.Errorf("sync firewall apply journal: %w", err)
	}
	if err := stage.Close(); err != nil {
		return fmt.Errorf("close firewall apply journal: %w", err)
	}
	if firewallApplyJournalFaultHook != nil {
		if err := firewallApplyJournalFaultHook(firewallApplyJournalFaultBeforeRename); err != nil {
			return fmt.Errorf("injected failure before firewall apply journal rename: %w", err)
		}
	}
	if err := os.Rename(stagePath, path); err != nil {
		return fmt.Errorf("publish firewall apply journal: %w", err)
	}
	published = true
	if firewallApplyJournalFaultHook != nil {
		if err := firewallApplyJournalFaultHook(firewallApplyJournalFaultAfterRename); err != nil {
			return fmt.Errorf("injected failure after firewall apply journal rename: %w", err)
		}
	}
	if err := syncServiceMutationDirectory(path); err != nil {
		return fmt.Errorf("sync firewall apply journal directory: %w", err)
	}
	if firewallApplyJournalFaultHook != nil {
		if err := firewallApplyJournalFaultHook(firewallApplyJournalFaultAfterSync); err != nil {
			return fmt.Errorf("injected failure after firewall apply journal sync: %w", err)
		}
	}
	return nil
}

func cleanupAbandonedFirewallApplyJournalStages(stateDir string) error {
	entries, err := os.ReadDir(stateDir)
	if err != nil {
		return fmt.Errorf("inspect abandoned firewall journal stages: %w", err)
	}
	var stages []string
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".firewall-apply-journal-") &&
			strings.HasSuffix(name, ".json") {
			stages = append(stages, filepath.Join(stateDir, name))
		}
	}
	if len(stages) > firewallApplyJournalStageLimit {
		return errors.New("abandoned firewall journal stage count exceeds the limit")
	}
	sort.Strings(stages)
	for _, stage := range stages {
		raw, exists, err := readSecureServiceMutationLedger(stage, firewallApplyJournalMaxSize)
		if err != nil || !exists {
			if err == nil {
				err = errors.New("firewall journal stage disappeared")
			}
			return fmt.Errorf("validate abandoned firewall journal stage: %w", err)
		}
		if _, err := decodeFirewallApplyJournal(raw); err != nil {
			return fmt.Errorf("validate abandoned firewall journal stage: %w", err)
		}
	}
	for _, stage := range stages {
		if err := os.Remove(stage); err != nil {
			return fmt.Errorf("remove abandoned firewall journal stage: %w", err)
		}
	}
	if len(stages) != 0 {
		return syncServiceMutationDirectory(
			filepath.Join(stateDir, firewallApplyJournalFileName),
		)
	}
	return nil
}

func prepareFirewallApplyJournal(
	ctx context.Context,
	commitment mutationpayload.FirewallApplyCommitment,
	runner firewallCommandRunner,
	store firewallStateStore,
) (*firewallApplyJournal, error) {
	if ctx == nil || runner == nil || store == nil ||
		!mutationpayload.ValidFirewallApplyQualifier(commitment.Qualifier) {
		return nil, errors.New("invalid firewall apply preparation")
	}
	recomputed, err := mutationpayload.CanonicalFirewallApply(
		commitment.Enabled,
		commitment.Persist,
		commitment.TCPPorts,
		commitment.UDPPorts,
	)
	if err != nil || recomputed.Qualifier != commitment.Qualifier ||
		!equalFirewallPorts(recomputed.TCPPorts, commitment.TCPPorts) ||
		!equalFirewallPorts(recomputed.UDPPorts, commitment.UDPPorts) {
		return nil, errors.New("firewall apply commitment is not canonical")
	}
	if !commitment.Enabled && !commitment.Persist {
		return nil, errors.New("live-only firewall disable is not authorized")
	}
	if _, err := runner.LookPath("nft"); err != nil {
		return nil, errors.New("firewall engine (nftables) is not installed")
	}
	if commitment.Persist {
		if _, err := runner.LookPath("systemctl"); err != nil {
			return nil, errors.New("systemd client failed security validation")
		}
	}
	priorSnapshot, priorSnapshotExists, err := store.Load()
	if err != nil {
		return nil, err
	}
	if commitment.Enabled && priorSnapshotExists {
		if err := validateFirewallSnapshot(priorSnapshot); err != nil {
			return nil, err
		}
	}
	if !commitment.Enabled {
		priorSnapshot = nil
		priorSnapshotExists = false
	}
	var sshPorts []int
	noSSHService := false
	if commitment.Enabled {
		sshPorts, err = detectSSHPortsWithRunner(runner)
		if err != nil {
			// A host proven to have no SSH service has no door for this
			// firewall to lock, so the escape-hatch proof is moot rather than
			// violated. Every other reason — including an SSH service that is
			// merely not listening right now — is still a refusal.
			// Hiç SSH servisi olmadığı kanıtlanmış bir sunucuda bu güvenlik
			// duvarının kilitleyeceği bir kapı yoktur; kaçış yolu kanıtı
			// çiğnenmiş değil, konusuz kalmıştır. Diğer her neden — şu an
			// dinlemeyen bir SSH servisi dâhil — hâlâ bir reddir.
			refusal := classifySSHDiscovery(runner, err)
			if refusal.reason != transport.SSHDiscoveryNoService {
				return nil, refusal
			}
			noSSHService = true
			sshPorts = nil
		} else {
			sshPorts, err = canonicalAgentFirewallPorts(sshPorts)
			if err != nil || len(sshPorts) == 0 {
				return nil, errors.New("SSH listener discovery returned an invalid canonical snapshot")
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.New("service mutation lease ended before firewall commit intent")
	}
	return &firewallApplyJournal{
		Version:             firewallApplyJournalVersion,
		Qualifier:           commitment.Qualifier,
		Enabled:             commitment.Enabled,
		Persist:             commitment.Persist,
		TCPPorts:            append([]int(nil), commitment.TCPPorts...),
		UDPPorts:            append([]int(nil), commitment.UDPPorts...),
		SSHPorts:            append([]int(nil), sshPorts...),
		NoSSHService:        noSSHService,
		PriorSnapshotExists: priorSnapshotExists,
		PriorSnapshot:       append([]byte(nil), priorSnapshot...),
	}, nil
}

func activeDirectFirewallApplyJob(job *ServiceMutationJob) bool {
	return job != nil && serviceMutationStatusActive(job.Status) &&
		(job.Kind == "firewall_apply" || job.Kind == "firewall_sync") &&
		job.Target == "nftables"
}

func firewallApplyJobMatchesJournal(
	job *ServiceMutationJob,
	journal *firewallApplyJournal,
) bool {
	return activeDirectFirewallApplyJob(job) && journal != nil &&
		job.RequestID == journal.RequestID &&
		job.PackageName == journal.Qualifier &&
		mutationpayload.ValidFirewallApplyQualifier(job.PackageName)
}

func cloneFirewallApplyJournal(journal *firewallApplyJournal) *firewallApplyJournal {
	if journal == nil {
		return nil
	}
	copy := *journal
	copy.TCPPorts = append([]int(nil), journal.TCPPorts...)
	copy.UDPPorts = append([]int(nil), journal.UDPPorts...)
	copy.SSHPorts = append([]int(nil), journal.SSHPorts...)
	copy.PriorSnapshot = append([]byte(nil), journal.PriorSnapshot...)
	return &copy
}

// commitStandaloneFirewallApplyIntent durably records the complete derived
// plan before any host effect. A successful intent write is the transaction's
// linearization point and makes forward convergence non-cancellable.
func commitStandaloneFirewallApplyIntent(
	ctx context.Context,
	prepared *firewallApplyJournal,
) (*firewallApplyJournal, error) {
	if ctx == nil || prepared == nil {
		return nil, errors.New("invalid firewall apply commit intent")
	}
	tracker, _ := ctx.Value(serviceMutationExecutionTrackerKey{}).(*serviceMutationExecutionTracker)
	if tracker == nil || tracker.manager == nil || tracker.runtime == nil {
		return nil, errors.New("firewall apply commit intent requires a durable execution tracker")
	}
	m, runtime := tracker.manager, tracker.runtime
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.healthErrorLocked(); err != nil {
		return nil, err
	}
	if m.active != runtime || runtime.job == nil || runtime.steps != 1 ||
		runtime.job.WorkerPID != 0 ||
		runtime.job.Status != serviceMutationStatusRunning {
		return nil, errors.New("firewall apply commit intent lost the active mutation step")
	}
	job := runtime.job
	if !activeDirectFirewallApplyJob(job) ||
		job.PackageName != prepared.Qualifier ||
		!mutationpayload.ValidFirewallApplyQualifier(job.PackageName) {
		return nil, errors.New("firewall apply commit intent rejected the mutation identity")
	}
	if strings.HasPrefix(job.Phase, firewallApplyCommitPhasePrefix) {
		return nil, errors.New("firewall apply job already crossed its commit point")
	}
	now := m.now()
	if ctx.Err() != nil || !now.Before(job.LeaseExpiresAt) || !now.Before(job.DeadlineAt) {
		return nil, errors.New("service mutation lease ended before firewall commit intent")
	}
	journal := cloneFirewallApplyJournal(prepared)
	journal.RequestID = job.RequestID
	if err := validateFirewallApplyJournal(journal); err != nil {
		return nil, err
	}
	if err := writeFirewallApplyJournal(firewallApplyJournalPath(m), journal); err != nil {
		return nil, m.poisonLocked(fmt.Errorf("persist firewall apply journal: %w", err))
	}
	intentPhase, err := formatFirewallApplyCommitPhase(
		firewallApplyCommitIntent,
		job.RequestID,
		job.PackageName,
	)
	if err != nil {
		return nil, m.poisonLocked(err)
	}
	before := cloneServiceMutationLedger(m.ledger)
	job.Phase = intentPhase
	job.UpdatedAt = now
	if err := m.persistLedgerMutationLocked(before); err != nil {
		return nil, err
	}
	runtime.firewallApplyCommittedPhase = intentPhase
	return journal, nil
}

func poisonFirewallApplyConvergence(ctx context.Context, cause error) error {
	if ctx == nil || cause == nil {
		return errors.New("invalid firewall convergence poison request")
	}
	tracker, _ := ctx.Value(serviceMutationExecutionTrackerKey{}).(*serviceMutationExecutionTracker)
	if tracker == nil || tracker.manager == nil || tracker.runtime == nil {
		return errors.New("firewall convergence poison requires a durable execution tracker")
	}
	m, runtime := tracker.manager, tracker.runtime
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active != runtime || runtime.job == nil || runtime.steps != 1 ||
		runtime.firewallApplyCommittedPhase == "" {
		return errors.New("firewall convergence poison lost the committed mutation")
	}
	return m.poisonLocked(fmt.Errorf("firewall host convergence is ambiguous: %w", cause))
}

func publishStandaloneFirewallApply(
	ctx context.Context,
	journal *firewallApplyJournal,
) error {
	tracker, _ := ctx.Value(serviceMutationExecutionTrackerKey{}).(*serviceMutationExecutionTracker)
	if tracker == nil || tracker.manager == nil || tracker.runtime == nil {
		return errors.New("firewall publication requires a durable execution tracker")
	}
	m, runtime := tracker.manager, tracker.runtime
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.healthErrorLocked(); err != nil {
		return err
	}
	if m.active != runtime || runtime.job == nil || runtime.steps != 1 ||
		runtime.job.WorkerPID != 0 ||
		runtime.firewallApplyCommittedPhase == "" ||
		!firewallApplyJobMatchesJournal(runtime.job, journal) {
		return errors.New("firewall publication lost the committed mutation")
	}
	persisted, exists, err := readFirewallApplyJournal(firewallApplyJournalPath(m))
	if err != nil || !exists || !firewallApplyJobMatchesJournal(runtime.job, persisted) ||
		!equalFirewallApplyJournals(persisted, journal) {
		if err == nil {
			err = errors.New("firewall apply journal identity changed before publication")
		}
		return m.poisonLocked(err)
	}
	publishedPhase, err := formatFirewallApplyCommitPhase(
		firewallApplyCommitPublished,
		runtime.job.RequestID,
		runtime.job.PackageName,
	)
	if err != nil {
		return m.poisonLocked(err)
	}
	runtime.firewallApplyCommittedPhase = publishedPhase
	if err := m.finishRuntimeTerminalLocked(runtime, true, publishedPhase, "", ""); err != nil {
		if m.active == runtime {
			return m.poisonLocked(fmt.Errorf(
				"persist terminal firewall receipt after host convergence: %w",
				err,
			))
		}
		return err
	}
	return nil
}

func equalFirewallApplyJournals(left, right *firewallApplyJournal) bool {
	if left == nil || right == nil {
		return left == right
	}
	leftRaw, leftErr := encodeFirewallApplyJournal(left)
	rightRaw, rightErr := encodeFirewallApplyJournal(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftRaw, rightRaw)
}

func firewallRunnerWithContext(
	runner firewallCommandRunner,
	ctx context.Context,
) firewallCommandRunner {
	if host, ok := runner.(hostFirewallCommandRunner); ok {
		host.ctx = ctx
		return host
	}
	return runner
}

func applyStandaloneFirewallV2(
	ctx context.Context,
	commitment mutationpayload.FirewallApplyCommitment,
	runner firewallCommandRunner,
	store firewallStateStore,
	response *FirewallStatusResponse,
) error {
	firewallMu.Lock()
	defer firewallMu.Unlock()
	response.EngineAvailable = false
	prepared, err := prepareFirewallApplyJournal(ctx, commitment, runner, store)
	if err != nil {
		// A refused SSH discovery names its reason so the panel can offer the
		// operator the exact way forward instead of one opaque sentence.
		// Reddedilen bir SSH keşfi nedenini adlandırır; böylece panel
		// operatöre kapalı tek bir cümle yerine tam olarak izlenecek yolu
		// sunabilir.
		var refusal *sshDiscoveryRefusal
		if errors.As(err, &refusal) {
			response.SSHDiscoveryReason = refusal.reason
		}
		response.PersistenceState = firewallPersistenceUnverified
		response.PersistenceError = err.Error()
		response.Error = err.Error()
		return nil
	}
	response.EngineAvailable = true
	journal, err := commitStandaloneFirewallApplyIntent(ctx, prepared)
	if err != nil {
		response.PersistenceState = firewallPersistenceUnverified
		response.PersistenceError = err.Error()
		response.Error = err.Error()
		return nil
	}

	convergenceCtx, cancel := context.WithTimeout(
		context.WithoutCancel(ctx),
		firewallApplyConvergenceTimeout,
	)
	defer cancel()
	convergenceRunner := firewallRunnerWithContext(runner, convergenceCtx)
	if err := convergeFirewallApplyPlan(convergenceCtx, journal, convergenceRunner, store); err != nil {
		poisonErr := poisonFirewallApplyConvergence(ctx, err)
		log.Printf("Firewall V2 convergence failed after durable intent: %v; poison: %v", err, poisonErr)
		response.PersistenceState = firewallPersistenceUnverified
		response.PersistenceError = err.Error()
		response.Error = "firewall commit requires startup recovery"
		return nil
	}
	populateFirewallApplyResponse(journal, response)
	if err := publishStandaloneFirewallApply(ctx, journal); err != nil {
		log.Printf("Firewall V2 host convergence completed with receipt error: %v", err)
	}
	return nil
}

func convergeFirewallApplyPlan(
	ctx context.Context,
	journal *firewallApplyJournal,
	runner firewallCommandRunner,
	store firewallStateStore,
) error {
	if err := validateFirewallApplyJournal(journal); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	switch {
	case journal.Enabled && journal.Persist:
		if err := store.Save(firewallDesiredSnapshot(journal)); err != nil {
			return fmt.Errorf("write desired firewall snapshot: %w", err)
		}
		if err := setFirewallRestoreUnitEnabled(runner, true); err != nil {
			return err
		}
	case journal.Enabled && journal.PriorSnapshotExists:
		if err := store.Save(firewallDesiredSnapshot(journal)); err != nil {
			return fmt.Errorf("update existing firewall snapshot: %w", err)
		}
	case !journal.Enabled && journal.Persist:
		if err := setFirewallRestoreUnitEnabled(runner, false); err != nil {
			return err
		}
		if err := store.Remove(); err != nil {
			return fmt.Errorf("remove firewall snapshot: %w", err)
		}
	}
	if err := applyFirewallLivePlan(runner, journal); err != nil {
		return err
	}
	if err := verifyFirewallSnapshotPlan(store, journal); err != nil {
		return err
	}
	if journal.Persist {
		if err := verifyFirewallRestoreUnitState(runner, journal.Enabled); err != nil {
			return err
		}
	}
	if err := verifyFirewallLivePlan(runner, journal); err != nil {
		return err
	}
	return nil
}

func firewallDesiredSnapshot(journal *firewallApplyJournal) []byte {
	return encodeFirewallSnapshot(journal.TCPPorts, journal.UDPPorts, journal.SSHPorts)
}

func firewallEffectiveTCPPorts(journal *firewallApplyJournal) []int {
	return dedupeSorted(append(
		append([]int(nil), journal.TCPPorts...),
		journal.SSHPorts...,
	))
}

func applyFirewallLivePlan(
	runner firewallCommandRunner,
	journal *firewallApplyJournal,
) error {
	tables, err := runner.Output("nft", "list", "tables")
	if err != nil {
		return fmt.Errorf("nft table discovery failed: %w", err)
	}
	present := firewallTablePresent(tables)
	if !journal.Enabled {
		if !present {
			return nil
		}
		out, err := runner.CombinedOutput(
			"nft",
			[]string{"delete", "table", "inet", fwTable},
			"",
		)
		if err != nil {
			return errors.New(commandFailureDetail("nft disable failed", out, err))
		}
		return nil
	}
	rules := buildFirewallRuleset(
		present,
		firewallEffectiveTCPPorts(journal),
		journal.UDPPorts,
	)
	out, err := runner.CombinedOutput("nft", []string{"-f", "-"}, rules)
	if err != nil {
		return errors.New(commandFailureDetail("nft apply failed", out, err))
	}
	return nil
}

func verifyFirewallSnapshotPlan(
	store firewallStateStore,
	journal *firewallApplyJournal,
) error {
	actual, exists, err := store.Load()
	if err != nil {
		return fmt.Errorf("read back firewall snapshot: %w", err)
	}
	var expected []byte
	expectedExists := journal.PriorSnapshotExists
	switch {
	case journal.Enabled && (journal.Persist || journal.PriorSnapshotExists):
		expected, expectedExists = firewallDesiredSnapshot(journal), true
	case !journal.Enabled && journal.Persist:
		expected, expectedExists = nil, false
	default:
		expected = journal.PriorSnapshot
	}
	if exists != expectedExists || (exists && !bytes.Equal(actual, expected)) {
		return errors.New("firewall snapshot readback does not match the committed plan")
	}
	return nil
}

func verifyFirewallRestoreUnitState(
	runner firewallCommandRunner,
	enabled bool,
) error {
	out, err := runner.Output(
		"systemctl",
		"show",
		"--no-pager",
		"--property=UnitFileState",
		"--value",
		firewallRestoreUnitName,
	)
	if err != nil {
		return fmt.Errorf("read back firewall restore unit state: %w", err)
	}
	want := "disabled"
	if enabled {
		want = "enabled"
	}
	if strings.TrimSpace(string(out)) != want {
		return fmt.Errorf(
			"firewall restore unit readback is %q, want %q",
			strings.TrimSpace(string(out)),
			want,
		)
	}
	return nil
}

func parseExactFirewallPortRule(line, protocol string) ([]int, bool, error) {
	prefix := protocol + " dport "
	if !strings.HasPrefix(line, prefix) {
		return nil, false, nil
	}
	body := strings.TrimSpace(strings.TrimPrefix(line, prefix))
	if !strings.HasSuffix(body, " accept") {
		return nil, true, errors.New("firewall port rule has an unexpected verdict")
	}
	body = strings.TrimSpace(strings.TrimSuffix(body, " accept"))
	var tokens []string
	if strings.HasPrefix(body, "{") && strings.HasSuffix(body, "}") {
		body = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(body, "{"), "}"))
		tokens = strings.Split(body, ",")
	} else {
		tokens = []string{body}
	}
	if len(tokens) == 0 {
		return nil, true, errors.New("firewall port rule is empty")
	}
	ports := make([]int, 0, len(tokens))
	for _, token := range tokens {
		port, err := strconv.Atoi(strings.TrimSpace(token))
		if err != nil || port < 1 || port > 65535 {
			return nil, true, errors.New("firewall port rule contains an invalid port")
		}
		ports = append(ports, port)
	}
	canonical, err := canonicalAgentFirewallPorts(ports)
	if err != nil || len(canonical) != len(ports) {
		return nil, true, errors.New("firewall port rule is not canonical")
	}
	return canonical, true, nil
}

func verifyFirewallLivePlan(
	runner firewallCommandRunner,
	journal *firewallApplyJournal,
) error {
	tables, err := runner.Output("nft", "list", "tables")
	if err != nil {
		return fmt.Errorf("read back nft tables: %w", err)
	}
	present := firewallTablePresent(tables)
	if present != journal.Enabled {
		return errors.New("live firewall presence does not match the committed plan")
	}
	if !present {
		return nil
	}
	out, err := runner.Output("nft", "list", "table", "inet", fwTable)
	if err != nil {
		return fmt.Errorf("read back live firewall table: %w", err)
	}
	actual, err := canonicalFirewallRulesetReadback(out)
	if err != nil {
		return err
	}
	expected, err := canonicalFirewallRulesetReadback([]byte(buildFirewallRuleset(
		false,
		firewallEffectiveTCPPorts(journal),
		journal.UDPPorts,
	)))
	if err != nil {
		return fmt.Errorf("canonicalize expected firewall ruleset: %w", err)
	}
	if actual != expected {
		return errors.New("live firewall ruleset readback does not exactly match the committed plan")
	}
	return nil
}

func canonicalFirewallRulesetReadback(raw []byte) (string, error) {
	lines := make([]string, 0, 16)
	tcpFound, udpFound, loopbackFound := false, false, false
	for _, rawLine := range strings.Split(string(raw), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			return "", errors.New("live firewall readback contains unsupported annotations")
		}
		if line == "iif lo accept" || line == "iif \"lo\" accept" {
			if loopbackFound {
				return "", errors.New("live firewall contains multiple loopback rules")
			}
			loopbackFound = true
			// nft renders the interface-index symbol with quotes even when the
			// accepted input used the unquoted spelling. Both exact spellings
			// describe the same fixed loopback rule and no other iif expression
			// is accepted below.
			line = "iif lo accept"
		} else if strings.HasPrefix(line, "iif ") {
			return "", errors.New("live firewall contains an unexpected input-interface rule")
		} else if ports, found, err := parseExactFirewallPortRule(line, "tcp"); found {
			if err != nil || tcpFound {
				if err == nil {
					err = errors.New("live firewall contains multiple TCP port rules")
				}
				return "", err
			}
			tcpFound = true
			line = "tcp dport { " + joinInts(ports) + " } accept"
		} else if ports, found, err := parseExactFirewallPortRule(line, "udp"); found {
			if err != nil || udpFound {
				if err == nil {
					err = errors.New("live firewall contains multiple UDP port rules")
				}
				return "", err
			}
			udpFound = true
			line = "udp dport { " + joinInts(ports) + " } accept"
		} else {
			line = strings.Join(strings.Fields(line), " ")
			line = strings.Replace(
				line,
				"priority filter; policy drop;",
				"priority 0; policy drop;",
				1,
			)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n"), nil
}

func populateFirewallApplyResponse(
	journal *firewallApplyJournal,
	response *FirewallStatusResponse,
) {
	*response = FirewallStatusResponse{
		Enabled:          journal.Enabled,
		EngineAvailable:  true,
		TCPPorts:         nil,
		UDPPorts:         nil,
		SSHPorts:         nil,
		PersistenceState: "",
	}
	if journal.Enabled {
		response.TCPPorts = firewallEffectiveTCPPorts(journal)
		response.UDPPorts = append([]int(nil), journal.UDPPorts...)
		response.SSHPorts = append([]int(nil), journal.SSHPorts...)
		// An applied policy that protects no SSH port must say why, or the
		// empty port list reads as a discovery the operator never saw.
		// Hiçbir SSH portunu korumayan, uygulanmış bir politika nedenini
		// söylemelidir; yoksa boş port listesi, operatörün hiç görmediği bir
		// keşif gibi okunur.
		if journal.NoSSHService {
			response.SSHDiscoveryReason = transport.SSHDiscoveryNoService
		}
	}
	firewallLastPersistenceError = ""
	firewallLastRestoreError = ""
	var snapshot []byte
	snapshotExists := journal.PriorSnapshotExists
	switch {
	case journal.Enabled && (journal.Persist || journal.PriorSnapshotExists):
		snapshot, snapshotExists = firewallDesiredSnapshot(journal), true
	case !journal.Enabled && journal.Persist:
		snapshot, snapshotExists = nil, false
	default:
		snapshot = append([]byte(nil), journal.PriorSnapshot...)
	}
	setFirewallPersistenceStatus(
		response,
		snapshot,
		snapshotExists,
		nil,
		journal.Enabled,
	)
}

// recoverPersistedFirewallApplyLocked either proves that no firewall commit
// decision existed or completes the exact journaled forward plan. The caller
// holds m.mu and the common host flock. On ambiguity this function retains the
// flock in the poisoned manager.
func (m *serviceMutationManager) recoverPersistedFirewallApplyLocked(
	job *ServiceMutationJob,
	lock *serviceMutationFileLock,
) (handled bool, err error) {
	if !activeDirectFirewallApplyJob(job) {
		return false, nil
	}
	if !mutationpayload.ValidFirewallApplyQualifier(job.PackageName) {
		m.poisonLock = lock
		return true, m.poisonLocked(
			errors.New("active firewall mutation has an invalid or legacy payload qualifier"),
		)
	}
	intent := false
	if strings.HasPrefix(job.Phase, firewallApplyCommitPhasePrefix) {
		state, requestID, qualifier, phaseErr := parseFirewallApplyCommitPhase(job.Phase)
		if phaseErr != nil || requestID != job.RequestID ||
			qualifier != job.PackageName || state != firewallApplyCommitIntent {
			m.poisonLock = lock
			return true, m.poisonLocked(errors.New("active firewall mutation has an invalid commit receipt"))
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
		job.ErrorMessage = "The previous firewall worker is still alive with the recorded process identity."
		job.UpdatedAt = m.now()
		writeErr := m.persistLedgerMutationLocked(before)
		if m.poisoned != nil {
			m.poisonLock = lock
			return true, writeErr
		}
		closeErr := lock.Close()
		if writeErr != nil {
			return true, errors.Join(writeErr, closeErr)
		}
		return true, closeErr
	}

	journalPath := firewallApplyJournalPath(m)
	journal, exists, journalErr := readFirewallApplyJournal(journalPath)
	if journalErr != nil {
		m.poisonLock = lock
		return true, m.poisonLocked(fmt.Errorf(
			"read firewall recovery journal: %w",
			journalErr,
		))
	}
	if !intent {
		if exists {
			// A journal without a ledger intent is an uncommitted or older
			// receipt. Its strict validation above is enough; it authorizes no
			// host effect for this interrupted job.
		}
		writeErr := m.finishPersistedOrphanLocked(
			job,
			"agent_restarted_before_firewall_commit",
			"The agent restarted before the firewall commit decision was durable.",
		)
		if m.poisoned != nil {
			m.poisonLock = lock
			return true, writeErr
		}
		closeErr := lock.Close()
		if writeErr != nil {
			return true, errors.Join(writeErr, closeErr)
		}
		return true, closeErr
	}
	if !exists || !firewallApplyJobMatchesJournal(job, journal) {
		m.poisonLock = lock
		return true, m.poisonLocked(
			errors.New("committed firewall mutation lost its exact recovery journal"),
		)
	}

	recoveryBase, cancel := context.WithTimeout(
		context.Background(),
		firewallApplyConvergenceTimeout,
	)
	runtime := &serviceMutationRuntime{
		job:                         job,
		lock:                        lock,
		ctx:                         recoveryBase,
		cancel:                      cancel,
		firewallApplyCommittedPhase: job.Phase,
	}
	m.mu.Unlock()
	runtime.stepMu.Lock()
	m.mu.Lock()
	if m.active != nil || m.ledger.ActiveRequestID != job.RequestID {
		cancel()
		m.poisonLock = lock
		identityErr := m.poisonLocked(
			errors.New("firewall recovery identity changed"),
		)
		m.mu.Unlock()
		runtime.stepMu.Unlock()
		m.mu.Lock()
		return true, identityErr
	}
	m.active = runtime
	runtime.steps = 1
	before := cloneServiceMutationLedger(m.ledger)
	runtime.job.Status = serviceMutationStatusCancelling
	runtime.job.ErrorCode = "agent_restart_during_firewall_commit"
	runtime.job.ErrorMessage = "The agent is completing a durable firewall commit after restart."
	runtime.job.WorkerPID = 0
	runtime.job.WorkerStarted = ""
	runtime.job.WorkerCommand = ""
	runtime.job.UpdatedAt = m.now()
	if persistErr := m.persistLedgerMutationLocked(before); persistErr != nil {
		poisonErr := m.poisonLocked(fmt.Errorf(
			"persist firewall recovery state: %w",
			persistErr,
		))
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
	recoveryErr := recoverFirewallApplyHost(recoveryCtx, journal)
	cancel()
	m.mu.Lock()
	runtime.steps = 0
	m.mu.Unlock()
	runtime.stepMu.Unlock()
	m.mu.Lock()
	if recoveryErr != nil {
		return true, m.poisonLocked(fmt.Errorf(
			"recover committed firewall plan: %w",
			recoveryErr,
		))
	}
	publishedPhase, phaseErr := formatFirewallApplyCommitPhase(
		firewallApplyCommitPublished,
		runtime.job.RequestID,
		runtime.job.PackageName,
	)
	if phaseErr != nil {
		return true, m.poisonLocked(phaseErr)
	}
	runtime.firewallApplyCommittedPhase = publishedPhase
	if finishErr := m.finishRuntimeTerminalLocked(
		runtime,
		true,
		publishedPhase,
		"",
		"",
	); finishErr != nil {
		return true, m.poisonLocked(fmt.Errorf(
			"persist recovered firewall success: %w",
			finishErr,
		))
	}
	return true, nil
}
