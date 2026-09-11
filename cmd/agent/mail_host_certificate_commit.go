package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
)

const (
	mailHostCertificateReceiptName       = ".mail-host-certificate-receipt.json"
	mailHostCertificateReceiptSchema     = "mail-host-certificate-receipt/v1"
	mailHostCertificateReceiptMaxSize    = 1024
	mailHostCertificateCommitPhasePrefix = "commit/mail-host-certificate/v1/"
	mailHostCertificateCommitIntent      = "intent"
	mailHostCertificateCommitPublished   = "published"
	mailHostCertificateRecoveryTimeout   = 30 * time.Second
)

type mailHostCertificateReceipt struct {
	Schema     string `json:"schema"`
	RequestID  string `json:"request_id"`
	Qualifier  string `json:"qualifier"`
	Domain     string `json:"domain"`
	LeafSHA256 string `json:"leaf_sha256"`
}

type mailHostCertificateStage struct {
	publishAction func() (bool, error)
	cleanupAction func(bool) error
	published     bool
	closed        bool
}

var (
	mailHostCertificateVerifyPublished    = verifyPublishedMailHostCertificateReceipt
	mailHostCertificateStabilizePublished = stabilizePublishedMailHostCertificate
)

func newMailHostCertificateReceipt(
	requestID, qualifier, domain string,
	leafDER []byte,
) (mailHostCertificateReceipt, error) {
	receipt := mailHostCertificateReceipt{
		Schema:     mailHostCertificateReceiptSchema,
		RequestID:  requestID,
		Qualifier:  qualifier,
		Domain:     domain,
		LeafSHA256: panelCertificateLeafSHA256(leafDER),
	}
	if err := validateMailHostCertificateReceipt(receipt); err != nil {
		return mailHostCertificateReceipt{}, err
	}
	return receipt, nil
}

func validateMailHostCertificateReceipt(receipt mailHostCertificateReceipt) error {
	if receipt.Schema != mailHostCertificateReceiptSchema ||
		!validMutationIdentity(receipt.RequestID) ||
		!mutationpayload.ValidMailHostCertificateQualifier(receipt.Qualifier) ||
		!validPanelCertDomain.MatchString(receipt.Domain) ||
		receipt.Domain != strings.ToLower(strings.TrimSpace(receipt.Domain)) {
		return errors.New("invalid mail host certificate issue receipt identity")
	}
	if err := validatePanelCertificateLeafSHA256(receipt.LeafSHA256); err != nil {
		return err
	}
	return nil
}

func canonicalMailHostCertificateReceipt(
	receipt mailHostCertificateReceipt,
) ([]byte, error) {
	if err := validateMailHostCertificateReceipt(receipt); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(receipt)
	if err != nil {
		return nil, fmt.Errorf("encode mail host certificate issue receipt: %w", err)
	}
	raw = append(raw, '\n')
	if len(raw) > mailHostCertificateReceiptMaxSize {
		return nil, errors.New("mail host certificate issue receipt exceeds size limit")
	}
	return raw, nil
}

func decodeMailHostCertificateReceipt(raw []byte) (mailHostCertificateReceipt, error) {
	if len(raw) == 0 || len(raw) > mailHostCertificateReceiptMaxSize {
		return mailHostCertificateReceipt{}, errors.New(
			"mail host certificate issue receipt has invalid size",
		)
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	var receipt mailHostCertificateReceipt
	if err := decoder.Decode(&receipt); err != nil {
		return mailHostCertificateReceipt{}, fmt.Errorf(
			"decode mail host certificate issue receipt: %w", err,
		)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("multiple JSON values")
		}
		return mailHostCertificateReceipt{}, fmt.Errorf(
			"decode mail host certificate issue receipt trailer: %w", err,
		)
	}
	canonical, err := canonicalMailHostCertificateReceipt(receipt)
	if err != nil {
		return mailHostCertificateReceipt{}, err
	}
	if subtle.ConstantTimeCompare(raw, canonical) != 1 {
		return mailHostCertificateReceipt{}, errors.New(
			"mail host certificate issue receipt is not canonical JSON",
		)
	}
	return receipt, nil
}

func formatMailHostCertificateCommitPhase(
	state, requestID, domain, qualifier string,
) (string, error) {
	if (state != mailHostCertificateCommitIntent &&
		state != mailHostCertificateCommitPublished) ||
		!validMutationIdentity(requestID) ||
		!validPanelCertDomain.MatchString(domain) ||
		domain != strings.ToLower(strings.TrimSpace(domain)) ||
		!mutationpayload.ValidMailHostCertificateQualifier(qualifier) {
		return "", errors.New("invalid mail host certificate issue commit phase identity")
	}
	return mailHostCertificateCommitPhasePrefix + state + "/" +
		requestID + "/" + domain + "/" + qualifier, nil
}

func parseMailHostCertificateCommitPhase(value string) (
	state, requestID, domain, qualifier string,
	err error,
) {
	if !strings.HasPrefix(value, mailHostCertificateCommitPhasePrefix) {
		return "", "", "", "", errors.New(
			"not a mail host certificate issue commit phase",
		)
	}
	remainder := strings.TrimPrefix(value, mailHostCertificateCommitPhasePrefix)
	state, remainder, found := strings.Cut(remainder, "/")
	if !found {
		return "", "", "", "", errors.New(
			"invalid mail host certificate issue commit phase",
		)
	}
	requestID, remainder, found = strings.Cut(remainder, "/")
	if !found {
		return "", "", "", "", errors.New(
			"invalid mail host certificate issue commit phase",
		)
	}
	domain, qualifier, found = strings.Cut(remainder, "/")
	if !found {
		return "", "", "", "", errors.New(
			"invalid mail host certificate issue commit phase",
		)
	}
	canonical, phaseErr := formatMailHostCertificateCommitPhase(
		state,
		requestID,
		domain,
		qualifier,
	)
	if phaseErr != nil || canonical != value {
		return "", "", "", "", errors.New(
			"invalid mail host certificate issue commit phase",
		)
	}
	return state, requestID, domain, qualifier, nil
}

func (stage *mailHostCertificateStage) publish() error {
	if stage == nil || stage.publishAction == nil || stage.cleanupAction == nil || stage.closed {
		return errors.New("invalid mail host certificate issue stage")
	}
	if stage.published {
		return errors.New("mail host certificate issue stage is already published")
	}
	published, err := stage.publishAction()
	stage.published = published
	return err
}

func (stage *mailHostCertificateStage) close() error {
	if stage == nil || stage.closed {
		return nil
	}
	stage.closed = true
	if stage.cleanupAction == nil {
		return errors.New("invalid mail host certificate issue stage cleanup")
	}
	return stage.cleanupAction(stage.published)
}

func activeDirectMailHostCertificateJob(job *ServiceMutationJob) bool {
	return job != nil && serviceMutationStatusActive(job.Status) &&
		job.Kind == "mail_host_certificate" &&
		validPanelCertDomain.MatchString(job.Target)
}

// commitStandaloneMailHostCertificateStep is the sole linearization gate
// for host publication. The first callback only switches the fsynced version.
// Forward convergence then retains the host lease and uses tracked subprocesses.
func commitStandaloneMailHostCertificateStep(
	ctx context.Context,
	commit func() error,
	converge ...func(context.Context) error,
) (hostPublished bool, err error) {
	if ctx == nil || commit == nil || len(converge) > 1 {
		return false, errors.New("invalid mail host certificate issue commit gate")
	}
	tracker, _ := ctx.Value(serviceMutationExecutionTrackerKey{}).(*serviceMutationExecutionTracker)
	if tracker == nil || tracker.manager == nil || tracker.runtime == nil {
		return false, errors.New("mail host certificate issue commit gate requires a durable execution tracker")
	}
	m, runtime := tracker.manager, tracker.runtime
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.healthErrorLocked(); err != nil {
		return false, err
	}
	if m.active != runtime || runtime.job == nil || runtime.steps != 1 ||
		runtime.job.WorkerPID != 0 || runtime.job.Status != serviceMutationStatusRunning {
		return false, errors.New("mail host certificate issue commit gate lost the active mutation step")
	}
	job := runtime.job
	if job.Kind != "mail_host_certificate" ||
		!validPanelCertDomain.MatchString(job.Target) ||
		!mutationpayload.ValidMailHostCertificateQualifier(job.PackageName) {
		return false, errors.New("mail host certificate issue commit gate rejected the mutation identity")
	}
	now := m.now()
	if ctx.Err() != nil || !now.Before(job.LeaseExpiresAt) || !now.Before(job.DeadlineAt) {
		return false, errors.New("service mutation lease ended before the mail host certificate commit point")
	}
	intentPhase, err := formatMailHostCertificateCommitPhase(
		mailHostCertificateCommitIntent,
		job.RequestID,
		job.Target,
		job.PackageName,
	)
	if err != nil {
		return false, err
	}
	publishedPhase, err := formatMailHostCertificateCommitPhase(
		mailHostCertificateCommitPublished,
		job.RequestID,
		job.Target,
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
	published, verifyErr := mailHostCertificateVerifyPublished(
		job.RequestID, job.PackageName, job.Target,
	)
	if verifyErr != nil {
		return false, m.poisonLocked(fmt.Errorf(
			"verify mail host certificate publication after commit callback: %w",
			verifyErr,
		))
	}
	if !published {
		if commitErr != nil {
			return false, commitErr
		}
		return false, m.poisonLocked(errors.New(
			"mail host certificate commit callback did not publish its exact receipt",
		))
	}
	if commitErr != nil {
		if syncErr := mailHostCertificateStabilizePublished(); syncErr != nil {
			commitErr = errors.Join(commitErr, syncErr)
		}
		return true, m.poisonLocked(fmt.Errorf("published mail host certificate requires recovery: %w", commitErr))
	}
	runtime.mailHostCertificateCommittedPhase = intentPhase
	if len(converge) == 1 {
		convergenceCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), mailTLSSyncConvergenceTime)
		m.mu.Unlock()
		convergenceErr := converge[0](convergenceCtx)
		cancel()
		m.mu.Lock()
		if convergenceErr != nil {
			return true, m.poisonLocked(fmt.Errorf("mail host certificate activation requires recovery: %w", convergenceErr))
		}
		if m.active != runtime || runtime.job.WorkerPID != 0 {
			return true, m.poisonLocked(errors.New("host certificate convergence lost mutation ownership"))
		}
	}
	runtime.mailHostCertificatePublishedPhase = publishedPhase

	if err := m.finishRuntimeTerminalLocked(runtime, true, publishedPhase, "", ""); err != nil {
		if m.active == runtime {
			return true, m.poisonLocked(fmt.Errorf(
				"persist terminal mail host certificate receipt after publication: %w", err,
			))
		}
		return true, err
	}
	return true, nil
}

// recoverPersistedMailHostCertificateLocked is wired into manager startup by
// the shared integration. It never promotes a stage: only an exact receipt
// already reachable through current is success.
func (m *serviceMutationManager) recoverPersistedMailHostCertificateLocked(
	job *ServiceMutationJob,
	lock *serviceMutationFileLock,
) (handled bool, err error) {
	if !activeDirectMailHostCertificateJob(job) ||
		serviceMutationWorkerMatches(job.WorkerPID, job.WorkerStarted) {
		return false, nil
	}
	if !mutationpayload.ValidMailHostCertificateQualifier(job.PackageName) {
		m.poisonLock = lock
		if job.PackageName == "certbot" {
			return true, m.poisonLocked(errors.New(
				"active legacy mail host certificate issue cannot be recovered safely",
			))
		}
		return true, m.poisonLocked(errors.New(
			"active mail host certificate issue has an invalid payload qualifier",
		))
	}
	intent := false
	if strings.HasPrefix(job.Phase, mailHostCertificateCommitPhasePrefix) {
		state, requestID, domain, qualifier, phaseErr :=
			parseMailHostCertificateCommitPhase(job.Phase)
		if phaseErr != nil ||
			requestID != job.RequestID ||
			domain != job.Target ||
			qualifier != job.PackageName {
			m.poisonLock = lock
			return true, m.poisonLocked(errors.New(
				"active mail host certificate issue has an invalid commit receipt",
			))
		}
		intent = state == mailHostCertificateCommitIntent
	}

	recoveryBase, cancel := context.WithTimeout(context.Background(), mailHostCertificateRecoveryTimeout)
	runtime := &serviceMutationRuntime{job: job, lock: lock, ctx: recoveryBase, cancel: cancel}
	m.mu.Unlock()
	runtime.stepMu.Lock()
	m.mu.Lock()
	if m.active != nil || m.ledger.ActiveRequestID != job.RequestID {
		cancel()
		m.poisonLock = lock
		identityErr := m.poisonLocked(errors.New("mail host certificate issue recovery identity changed"))
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
		runtime.job.Phase = "recovering_mail_host_certificate"
	}
	runtime.job.ErrorCode = "agent_restart_during_mail_host_certificate"
	runtime.job.ErrorMessage = "The agent is reconciling mail host certificate publication after a restart."
	runtime.job.WorkerPID = 0
	runtime.job.WorkerStarted = ""
	runtime.job.WorkerCommand = ""
	runtime.job.UpdatedAt = m.now()
	if persistErr := m.persistLedgerMutationLocked(before); persistErr != nil {
		poisonErr := m.poisonLocked(fmt.Errorf(
			"persist mail host certificate issue recovery intent: %w", persistErr,
		))
		runtime.steps = 0
		m.mu.Unlock()
		runtime.stepMu.Unlock()
		m.mu.Lock()
		return true, poisonErr
	}

	tracker := &serviceMutationExecutionTracker{manager: m, runtime: runtime, allowCancellingRecovery: true}
	recoveryCtx := context.WithValue(recoveryBase, serviceMutationExecutionTrackerKey{}, tracker)
	m.mu.Unlock()
	success, recoveryErr := reconcilePersistedMailHostCertificateHost(
		recoveryCtx, runtime.job.RequestID, runtime.job.PackageName, runtime.job.Target,
	)
	m.mu.Lock()
	runtime.steps = 0
	m.mu.Unlock()
	runtime.stepMu.Unlock()
	m.mu.Lock()
	if recoveryErr != nil {
		return true, m.poisonLocked(recoveryErr)
	}
	if success {
		publishedPhase, phaseErr := formatMailHostCertificateCommitPhase(
			mailHostCertificateCommitPublished,
			runtime.job.RequestID,
			runtime.job.Target,
			runtime.job.PackageName,
		)
		if phaseErr != nil {
			return true, m.poisonLocked(phaseErr)
		}
		runtime.mailHostCertificatePublishedPhase = publishedPhase
		if finishErr := m.finishRuntimeTerminalLocked(
			runtime, true, publishedPhase, "", "",
		); finishErr != nil {
			return true, m.poisonLocked(fmt.Errorf(
				"persist recovered mail host certificate issue success: %w", finishErr,
			))
		}
		return true, nil
	}
	if finishErr := m.finishRuntimeTerminalLocked(
		runtime,
		false,
		"interrupted",
		"agent_restarted_before_mail_host_certificate_commit",
		"The agent removed an uncommitted mail host certificate stage after restart.",
	); finishErr != nil {
		return true, m.poisonLocked(fmt.Errorf(
			"persist recovered mail host certificate issue failure: %w", finishErr,
		))
	}
	return true, nil
}
