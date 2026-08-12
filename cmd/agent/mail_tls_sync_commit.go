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
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

const (
	mailTLSSyncCommitPhasePrefix = "commit/mail-tls-sync/v1/"
	mailTLSSyncJournalFileName   = "mail-tls-sync-journal.json"
	mailTLSSyncJournalVersion    = 1
	mailTLSSyncJournalMaxSize    = 2 << 20
	mailTLSSyncJournalStageLimit = 8

	mailTLSSyncCommitIntent    = "intent"
	mailTLSSyncCommitPublished = "published"
	mailTLSSyncConvergenceTime = 2 * time.Minute
)

const mailTLSSyncReceiptUnavailableError = "mail TLS host state converged but its terminal receipt is unavailable"

type mailTLSSyncJournal struct {
	Version     int                      `json:"version"`
	RequestID   string                   `json:"request_id"`
	Qualifier   string                   `json:"qualifier"`
	ManagedRoot string                   `json:"managed_root"`
	Myhostname  string                   `json:"myhostname"`
	SNI         []transport.MailSNIEntry `json:"sni,omitempty"`
}

var recoverMailTLSSyncHost = func(ctx context.Context, journal *mailTLSSyncJournal) error {
	mailMutex.Lock()
	defer mailMutex.Unlock()
	return convergeMailTLSSyncPlan(ctx, journal)
}

func formatMailTLSSyncCommitPhase(state, requestID, qualifier string) (string, error) {
	if (state != mailTLSSyncCommitIntent && state != mailTLSSyncCommitPublished) ||
		!validMutationIdentity(requestID) ||
		!mutationpayload.ValidMailTLSSyncQualifier(qualifier) {
		return "", errors.New("invalid mail TLS sync commit phase identity")
	}
	return mailTLSSyncCommitPhasePrefix + state + "/" + requestID + "/" + qualifier, nil
}

func parseMailTLSSyncCommitPhase(value string) (state, requestID, qualifier string, err error) {
	if !strings.HasPrefix(value, mailTLSSyncCommitPhasePrefix) {
		return "", "", "", errors.New("not a mail TLS sync commit phase")
	}
	remainder := strings.TrimPrefix(value, mailTLSSyncCommitPhasePrefix)
	state, remainder, found := strings.Cut(remainder, "/")
	if !found {
		return "", "", "", errors.New("invalid mail TLS sync commit phase")
	}
	requestID, qualifier, found = strings.Cut(remainder, "/")
	if !found {
		return "", "", "", errors.New("invalid mail TLS sync commit phase")
	}
	canonical, formatErr := formatMailTLSSyncCommitPhase(state, requestID, qualifier)
	if formatErr != nil || canonical != value {
		return "", "", "", errors.New("invalid mail TLS sync commit phase")
	}
	return state, requestID, qualifier, nil
}

func equalMailTLSSNI(left, right []transport.MailSNIEntry) bool {
	leftRaw, leftErr := json.Marshal(left)
	rightRaw, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftRaw, rightRaw)
}

func equalMailTLSSyncJournals(left, right *mailTLSSyncJournal) bool {
	if left == nil || right == nil {
		return left == right
	}
	leftRaw, leftErr := encodeMailTLSSyncJournal(left)
	rightRaw, rightErr := encodeMailTLSSyncJournal(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftRaw, rightRaw)
}

func validateMailTLSSyncJournal(journal *mailTLSSyncJournal) error {
	if journal == nil || journal.Version != mailTLSSyncJournalVersion ||
		!validMutationIdentity(journal.RequestID) ||
		!mutationpayload.ValidMailTLSSyncQualifier(journal.Qualifier) {
		return errors.New("mail TLS sync journal identity is invalid")
	}
	canonical, err := mutationpayload.CanonicalMailTLSSync(
		journal.ManagedRoot, journal.Myhostname, journal.SNI,
	)
	if err != nil || canonical.Qualifier != journal.Qualifier ||
		canonical.ManagedRoot != journal.ManagedRoot ||
		canonical.Myhostname != journal.Myhostname ||
		!equalMailTLSSNI(canonical.SNI, journal.SNI) {
		return errors.New("mail TLS sync journal payload is not canonical")
	}
	return nil
}

func encodeMailTLSSyncJournal(journal *mailTLSSyncJournal) ([]byte, error) {
	if err := validateMailTLSSyncJournal(journal); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(journal)
	if err != nil {
		return nil, err
	}
	if len(raw) > mailTLSSyncJournalMaxSize {
		return nil, errors.New("mail TLS sync journal exceeds the size limit")
	}
	return raw, nil
}

func decodeMailTLSSyncJournal(raw []byte) (*mailTLSSyncJournal, error) {
	if len(raw) == 0 || len(raw) > mailTLSSyncJournalMaxSize {
		return nil, errors.New("mail TLS sync journal has invalid size")
	}
	var journal mailTLSSyncJournal
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&journal); err != nil {
		return nil, fmt.Errorf("decode mail TLS sync journal: %w", err)
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("mail TLS sync journal contains trailing JSON")
	}
	canonical, err := encodeMailTLSSyncJournal(&journal)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(raw, canonical) {
		return nil, errors.New("mail TLS sync journal is not canonical")
	}
	return &journal, nil
}

func mailTLSSyncJournalPath(manager *serviceMutationManager) string {
	if manager == nil {
		return ""
	}
	return filepath.Join(filepath.Dir(manager.ledgerPath), mailTLSSyncJournalFileName)
}

func readMailTLSSyncJournal(path string) (*mailTLSSyncJournal, bool, error) {
	if filepath.Base(path) != mailTLSSyncJournalFileName ||
		!filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, false, errors.New("invalid mail TLS sync journal path")
	}
	raw, exists, err := readSecureServiceMutationLedger(path, mailTLSSyncJournalMaxSize)
	if err != nil || !exists {
		return nil, exists, err
	}
	journal, err := decodeMailTLSSyncJournal(raw)
	return journal, true, err
}

func writeMailTLSSyncJournal(path string, journal *mailTLSSyncJournal) error {
	raw, err := encodeMailTLSSyncJournal(journal)
	if err != nil {
		return err
	}
	if filepath.Base(path) != mailTLSSyncJournalFileName ||
		!filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("invalid mail TLS sync journal path")
	}
	dir := filepath.Dir(path)
	if err := ensureSecureServiceMutationStateDirectory(dir); err != nil {
		return err
	}
	if _, _, err := readMailTLSSyncJournal(path); err != nil {
		return fmt.Errorf("validate existing mail TLS sync journal: %w", err)
	}
	stage, err := os.CreateTemp(dir, ".mail-tls-sync-journal-*.json")
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

func cleanupAbandonedMailTLSSyncJournalStages(stateDir string) error {
	entries, err := os.ReadDir(stateDir)
	if err != nil {
		return err
	}
	stages := make([]string, 0)
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".mail-tls-sync-journal-") &&
			strings.HasSuffix(entry.Name(), ".json") {
			stages = append(stages, filepath.Join(stateDir, entry.Name()))
		}
	}
	if len(stages) > mailTLSSyncJournalStageLimit {
		return errors.New("abandoned mail TLS journal stage count exceeds the limit")
	}
	for _, stage := range stages {
		raw, exists, err := readSecureServiceMutationLedger(stage, mailTLSSyncJournalMaxSize)
		if err != nil || !exists {
			return errors.New("invalid abandoned mail TLS journal stage")
		}
		if _, err := decodeMailTLSSyncJournal(raw); err != nil {
			return err
		}
	}
	for _, stage := range stages {
		if err := os.Remove(stage); err != nil {
			return err
		}
	}
	if len(stages) != 0 {
		return syncServiceMutationDirectory(filepath.Join(stateDir, mailTLSSyncJournalFileName))
	}
	return nil
}

func prepareMailTLSSyncJournal(
	ctx context.Context,
	commitment mutationpayload.MailTLSSyncCommitment,
) (*mailTLSSyncJournal, error) {
	canonical, err := mutationpayload.CanonicalMailTLSSync(
		commitment.ManagedRoot, commitment.Myhostname, commitment.SNI,
	)
	if err != nil || canonical.Qualifier != commitment.Qualifier ||
		!equalMailTLSSNI(canonical.SNI, commitment.SNI) {
		return nil, errors.New("mail TLS sync commitment is not canonical")
	}
	validatedSNI, err := validateMailSNIEntries(commitment.SNI)
	if err != nil {
		return nil, fmt.Errorf("verify immutable mail TLS certificate snapshot: %w", err)
	}
	if !equalMailTLSSNI(validatedSNI, commitment.SNI) {
		return nil, errors.New("mail TLS sync certificate snapshot is not exact canonical input")
	}
	runner := func(name string, args ...string) ([]byte, error) {
		return runMailTLSMutationCommand(ctx, name, args...)
	}
	preflight, err := preflightMailTLSCommands(len(commitment.SNI) > 0, runner)
	if err != nil {
		return nil, err
	}
	_, err = snapshotMailTLSState(preflight.run)
	if err != nil {
		return nil, err
	}
	journal := &mailTLSSyncJournal{
		Version: mailTLSSyncJournalVersion, Qualifier: commitment.Qualifier,
		ManagedRoot: commitment.ManagedRoot, Myhostname: commitment.Myhostname,
		SNI: append([]transport.MailSNIEntry(nil), commitment.SNI...),
	}
	for index := range journal.SNI {
		journal.SNI[index].Names = append([]string(nil), journal.SNI[index].Names...)
	}
	return journal, validateMailTLSSyncJournalWithoutRequest(journal)
}

func validateMailTLSSyncJournalWithoutRequest(journal *mailTLSSyncJournal) error {
	journal.RequestID = strings.Repeat("0", 32)
	err := validateMailTLSSyncJournal(journal)
	journal.RequestID = ""
	return err
}

func activeDirectMailTLSSyncJob(job *ServiceMutationJob) bool {
	return job != nil && serviceMutationStatusActive(job.Status) &&
		job.Kind == "mail_tls_sync" && job.Target == "mail-tls"
}

func commitStandaloneMailTLSSyncIntent(
	ctx context.Context,
	prepared *mailTLSSyncJournal,
) (*mailTLSSyncJournal, error) {
	tracker, _ := ctx.Value(serviceMutationExecutionTrackerKey{}).(*serviceMutationExecutionTracker)
	if tracker == nil || tracker.manager == nil || tracker.runtime == nil || prepared == nil {
		return nil, errors.New("mail TLS sync intent requires a durable execution tracker")
	}
	m, runtime := tracker.manager, tracker.runtime
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.healthErrorLocked(); err != nil {
		return nil, err
	}
	job := runtime.job
	if m.active != runtime || job == nil || runtime.steps != 1 ||
		job.WorkerPID != 0 || job.Status != serviceMutationStatusRunning ||
		!activeDirectMailTLSSyncJob(job) ||
		job.PackageName != prepared.Qualifier {
		return nil, errors.New("mail TLS sync intent rejected the mutation identity")
	}
	if strings.HasPrefix(job.Phase, mailTLSSyncCommitPhasePrefix) ||
		ctx.Err() != nil || !m.now().Before(job.LeaseExpiresAt) ||
		!m.now().Before(job.DeadlineAt) {
		return nil, errors.New("service mutation lease ended before mail TLS sync intent")
	}
	journal := *prepared
	journal.SNI = append([]transport.MailSNIEntry(nil), prepared.SNI...)
	journal.RequestID = job.RequestID
	if err := validateMailTLSSyncJournal(&journal); err != nil {
		return nil, err
	}
	if err := writeMailTLSSyncJournal(mailTLSSyncJournalPath(m), &journal); err != nil {
		return nil, m.poisonLocked(fmt.Errorf("persist mail TLS sync journal: %w", err))
	}
	phase, err := formatMailTLSSyncCommitPhase(mailTLSSyncCommitIntent, job.RequestID, job.PackageName)
	if err != nil {
		return nil, m.poisonLocked(err)
	}
	before := cloneServiceMutationLedger(m.ledger)
	job.Phase, job.UpdatedAt = phase, m.now()
	if err := m.persistLedgerMutationLocked(before); err != nil {
		return nil, err
	}
	// The intent is the irreversible forward-convergence decision. Keep the
	// runtime running, but make cancel, lease expiry, and Finish(false) unable
	// to turn the exact committed plan into a terminal failure while its host
	// commands are still being applied.
	runtime.mailTLSSyncCommittedPhase = phase
	return &journal, nil
}

func poisonMailTLSSyncConvergence(ctx context.Context, cause error) error {
	tracker, _ := ctx.Value(serviceMutationExecutionTrackerKey{}).(*serviceMutationExecutionTracker)
	if tracker == nil || tracker.manager == nil {
		return cause
	}
	tracker.manager.mu.Lock()
	defer tracker.manager.mu.Unlock()
	return tracker.manager.poisonLocked(fmt.Errorf("mail TLS sync convergence: %w", cause))
}

func publishStandaloneMailTLSSync(ctx context.Context, journal *mailTLSSyncJournal) error {
	tracker, _ := ctx.Value(serviceMutationExecutionTrackerKey{}).(*serviceMutationExecutionTracker)
	if tracker == nil || tracker.manager == nil || tracker.runtime == nil {
		return errors.New("mail TLS sync publication requires a durable tracker")
	}
	m, runtime := tracker.manager, tracker.runtime
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.healthErrorLocked(); err != nil {
		return err
	}
	if m.active != runtime || runtime.job == nil || runtime.steps != 1 ||
		runtime.job.WorkerPID != 0 || runtime.mailTLSSyncCommittedPhase == "" ||
		!mailTLSSyncJobMatchesJournal(runtime.job, journal) {
		return errors.New("mail TLS publication lost the committed mutation")
	}
	persisted, exists, err := readMailTLSSyncJournal(mailTLSSyncJournalPath(m))
	if err != nil || !exists || !equalMailTLSSyncJournals(persisted, journal) {
		if err == nil {
			err = errors.New("mail TLS sync journal identity changed before publication")
		}
		return m.poisonLocked(err)
	}
	phase, err := formatMailTLSSyncCommitPhase(
		mailTLSSyncCommitPublished, runtime.job.RequestID, runtime.job.PackageName,
	)
	if err != nil {
		return m.poisonLocked(err)
	}
	runtime.mailTLSSyncCommittedPhase = phase
	if err := m.finishRuntimeTerminalLocked(runtime, true, phase, "", ""); err != nil {
		if m.active == runtime {
			return m.poisonLocked(fmt.Errorf("persist terminal mail TLS receipt: %w", err))
		}
		return err
	}
	return nil
}

func publishMailTLSSyncResponse(
	ctx context.Context,
	journal *mailTLSSyncJournal,
	response *SecureMailTLSResponse,
) bool {
	if err := publishStandaloneMailTLSSync(ctx, journal); err != nil {
		log.Printf("Mail TLS V2 convergence completed with receipt error: %v", err)
		*response = SecureMailTLSResponse{Error: mailTLSSyncReceiptUnavailableError}
		return false
	}
	*response = SecureMailTLSResponse{
		Configured: true, DefaultCert: defaultMailCert, SNICount: len(journal.SNI),
		Detail: fmt.Sprintf("mail TLS active (%d SNI entries)", len(journal.SNI)),
	}
	return true
}

func syncMailTLSV2(
	ctx context.Context,
	commitment mutationpayload.MailTLSSyncCommitment,
	response *SecureMailTLSResponse,
) error {
	prepared, err := prepareMailTLSSyncJournal(ctx, commitment)
	if err != nil {
		response.Error = err.Error()
		return nil
	}
	journal, err := commitStandaloneMailTLSSyncIntent(ctx, prepared)
	if err != nil {
		response.Error = err.Error()
		return nil
	}
	convergenceCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), mailTLSSyncConvergenceTime)
	defer cancel()
	if err := convergeMailTLSSyncPlan(convergenceCtx, journal); err != nil {
		poisonErr := poisonMailTLSSyncConvergence(ctx, err)
		log.Printf("Mail TLS V2 convergence failed after durable intent: %v; poison: %v", err, poisonErr)
		response.Error = "mail TLS commit requires startup recovery"
		return nil
	}
	publishMailTLSSyncResponse(ctx, journal, response)
	return nil
}

func convergeMailTLSSyncPlan(ctx context.Context, journal *mailTLSSyncJournal) error {
	if err := validateMailTLSSyncJournal(journal); err != nil {
		return err
	}
	trustedRoot, err := configuredMailTLSManagedRoot()
	if err != nil || trustedRoot != journal.ManagedRoot {
		return errors.New("trusted managed certificate root does not match committed mail TLS plan")
	}
	request := &SecureMailTLSRequest{
		Myhostname: journal.Myhostname,
		SNI:        append([]transport.MailSNIEntry(nil), journal.SNI...),
	}
	runner := func(name string, args ...string) ([]byte, error) {
		return runMailTLSMutationCommand(ctx, name, args...)
	}
	var response SecureMailTLSResponse
	if err := reconcileMailTLS(request, &response, runner); err != nil {
		return err
	}
	if response.Error != "" || !response.Configured ||
		response.SNICount != len(journal.SNI) || response.DefaultCert != defaultMailCert {
		return fmt.Errorf("mail TLS convergence did not confirm the committed plan: %s", response.Error)
	}
	return verifyMailTLSSyncPlan(journal, runner)
}

func expectedPostfixSNIMap(sni []transport.MailSNIEntry) []byte {
	var builder strings.Builder
	builder.WriteString("# Managed by CelikPanel — per-domain mail certificates (SNI).\n")
	for _, entry := range sni {
		for _, name := range entry.Names {
			fmt.Fprintf(&builder, "%s %s %s\n", name, entry.KeyPath, entry.CertPath)
		}
	}
	return []byte(builder.String())
}

func verifyMailTLSSyncPlan(journal *mailTLSSyncJournal, runner mailTLSCommandRunner) error {
	if err := validateDefaultMailCertPair(defaultMailCert, defaultMailKey, journal.Myhostname, time.Now()); err != nil {
		return fmt.Errorf("verify default mail TLS certificate: %w", err)
	}
	for _, entry := range journal.SNI {
		if _, _, err := requireManagedMailTLSCertificateFile(entry.CertPath, "fullchain.pem", 0o644); err != nil {
			return err
		}
		if _, _, err := requireManagedMailTLSCertificateFile(entry.KeyPath, "privkey.pem", 0o600); err != nil {
			return err
		}
		certDomain, _, err := requireManagedMailTLSCertificateFile(entry.CertPath, "fullchain.pem", 0o644)
		if err != nil {
			return err
		}
		if err := verifyManagedCertificateSnapshot(
			certDomain, entry.CertPath, entry.KeyPath, "",
		); err != nil {
			return fmt.Errorf("verify committed immutable mail TLS snapshot: %w", err)
		}
	}
	expectedSettings := map[string]string{
		"smtpd_tls_cert_file":      defaultMailCert,
		"smtpd_tls_key_file":       defaultMailKey,
		"smtpd_tls_security_level": "may",
		"smtp_tls_security_level":  "may",
		"smtpd_tls_protocols":      ">=TLSv1.2",
		"smtp_tls_protocols":       ">=TLSv1.2",
		"smtpd_tls_loglevel":       "1",
		"myhostname":               journal.Myhostname,
	}
	for setting, expected := range expectedSettings {
		out, err := runner("postconf", "-h", setting)
		if err != nil {
			return mailTLSCommandError("read back postconf "+setting, out, err)
		}
		if strings.TrimSpace(string(out)) != expected {
			return fmt.Errorf("Postfix setting %s does not match the committed snapshot", setting)
		}
	}
	sniSettingOut, err := runner("postconf", "-h", "tls_server_sni_maps")
	if err != nil {
		return mailTLSCommandError("read back postconf tls_server_sni_maps", sniSettingOut, err)
	}
	sniSetting := strings.TrimSpace(string(sniSettingOut))
	if len(journal.SNI) == 0 {
		if sniSetting != "" {
			return errors.New("Postfix SNI setting is not empty for the committed fallback-only snapshot")
		}
	} else {
		validSNISetting := false
		for _, mapType := range []string{"lmdb", "hash", "btree"} {
			if sniSetting == mapType+":"+postfixSNIPath {
				validSNISetting = true
				break
			}
		}
		if !validSNISetting {
			return errors.New("Postfix SNI setting does not reference the committed managed map")
		}
		actualSNI, err := secureReadConfig(postfixSNIPath)
		if err != nil {
			return fmt.Errorf("read back Postfix SNI source: %w", err)
		}
		if !bytes.Equal(actualSNI, expectedPostfixSNIMap(journal.SNI)) {
			return errors.New("Postfix SNI source does not match the committed snapshot")
		}
	}
	expectedDovecot := buildDovecotTLSConf(
		dovecotIs24WithRunner(runner), defaultMailCert, defaultMailKey, journal.SNI,
	)
	actualDovecot, err := secureReadConfig(dovecotTLSConf)
	if err != nil || string(actualDovecot) != expectedDovecot {
		return errors.New("Dovecot TLS readback does not match the committed snapshot")
	}
	if err := validatePostfixTLSConfig(runner); err != nil {
		return err
	}
	return validateDovecotTLSConfig(runner)
}

func mailTLSSyncJobMatchesJournal(job *ServiceMutationJob, journal *mailTLSSyncJournal) bool {
	return activeDirectMailTLSSyncJob(job) && journal != nil &&
		job.RequestID == journal.RequestID && job.PackageName == journal.Qualifier
}

func (m *serviceMutationManager) recoverPersistedMailTLSSyncLocked(
	job *ServiceMutationJob,
	lock *serviceMutationFileLock,
) (bool, error) {
	if !activeDirectMailTLSSyncJob(job) {
		return false, nil
	}
	if !mutationpayload.ValidMailTLSSyncQualifier(job.PackageName) {
		m.poisonLock = lock
		return true, m.poisonLocked(errors.New("active mail TLS mutation has an invalid or legacy payload qualifier"))
	}
	intent := false
	if strings.HasPrefix(job.Phase, mailTLSSyncCommitPhasePrefix) {
		state, requestID, qualifier, err := parseMailTLSSyncCommitPhase(job.Phase)
		if err != nil || state != mailTLSSyncCommitIntent ||
			requestID != job.RequestID || qualifier != job.PackageName {
			m.poisonLock = lock
			return true, m.poisonLocked(errors.New("active mail TLS mutation has an invalid commit receipt"))
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
		job.ErrorMessage = "The previous mail TLS worker is still alive."
		job.UpdatedAt = m.now()
		err := m.persistLedgerMutationLocked(before)
		if m.poisoned != nil {
			m.poisonLock = lock
			return true, err
		}
		return true, errors.Join(err, lock.Close())
	}
	journal, exists, err := readMailTLSSyncJournal(mailTLSSyncJournalPath(m))
	if err != nil {
		m.poisonLock = lock
		return true, m.poisonLocked(err)
	}
	if !intent {
		err := m.finishPersistedOrphanLocked(
			job,
			"agent_restarted_before_mail_tls_commit",
			"The agent restarted before the mail TLS commit decision was durable.",
		)
		if m.poisoned != nil {
			m.poisonLock = lock
			return true, err
		}
		return true, errors.Join(err, lock.Close())
	}
	if !exists || !mailTLSSyncJobMatchesJournal(job, journal) {
		m.poisonLock = lock
		return true, m.poisonLocked(errors.New("committed mail TLS mutation lost its exact recovery journal"))
	}
	recoveryBase, cancel := context.WithTimeout(context.Background(), mailTLSSyncConvergenceTime)
	runtime := &serviceMutationRuntime{
		job: job, lock: lock, ctx: recoveryBase, cancel: cancel,
	}
	// Preserve normal acquisition order: stepMu, then manager state.
	m.mu.Unlock()
	runtime.stepMu.Lock()
	m.mu.Lock()
	if m.active != nil || m.ledger.ActiveRequestID != job.RequestID {
		cancel()
		m.poisonLock = lock
		identityErr := m.poisonLocked(errors.New("mail TLS sync recovery identity changed"))
		m.mu.Unlock()
		runtime.stepMu.Unlock()
		m.mu.Lock()
		return true, identityErr
	}
	m.active = runtime
	runtime.steps = 1
	before := cloneServiceMutationLedger(m.ledger)
	runtime.job.Status = serviceMutationStatusCancelling
	runtime.job.ErrorCode = "agent_restart_during_mail_tls_sync"
	runtime.job.ErrorMessage = "The agent is completing a durable mail TLS commit after restart."
	runtime.job.WorkerPID = 0
	runtime.job.WorkerStarted = ""
	runtime.job.WorkerCommand = ""
	runtime.job.UpdatedAt = m.now()
	if persistErr := m.persistLedgerMutationLocked(before); persistErr != nil {
		poisonErr := m.poisonLocked(fmt.Errorf(
			"persist mail TLS sync recovery state: %w", persistErr,
		))
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
	recoveryErr := recoverMailTLSSyncHost(recoveryCtx, journal)
	cancel()
	m.mu.Lock()
	runtime.steps = 0
	m.mu.Unlock()
	runtime.stepMu.Unlock()
	m.mu.Lock()
	if recoveryErr != nil {
		return true, m.poisonLocked(fmt.Errorf("recover committed mail TLS plan: %w", recoveryErr))
	}
	phase, err := formatMailTLSSyncCommitPhase(mailTLSSyncCommitPublished, job.RequestID, job.PackageName)
	if err != nil {
		return true, m.poisonLocked(err)
	}
	runtime.mailTLSSyncCommittedPhase = phase
	if err := m.finishRuntimeTerminalLocked(runtime, true, phase, "", ""); err != nil {
		return true, m.poisonLocked(fmt.Errorf("persist recovered mail TLS success: %w", err))
	}
	return true, nil
}
