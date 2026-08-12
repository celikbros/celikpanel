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
	panelCertificateIssueReceiptName       = ".panel-certificate-issue-receipt.json"
	panelCertificateIssueReceiptSchema     = "panel-certificate-issue-receipt/v1"
	panelCertificateIssueReceiptMaxSize    = 1024
	panelCertificateIssueCommitPhasePrefix = "commit/panel-certificate-issue/v1/"
	panelCertificateIssueCommitIntent      = "intent"
	panelCertificateIssueCommitPublished   = "published"
	panelCertificateIssueRecoveryTimeout   = 30 * time.Second
)

type panelCertificateIssueReceipt struct {
	Schema     string `json:"schema"`
	RequestID  string `json:"request_id"`
	Qualifier  string `json:"qualifier"`
	Domain     string `json:"domain"`
	LeafSHA256 string `json:"leaf_sha256"`
}

type panelCertificateIssueStage struct {
	publishAction func() (bool, error)
	cleanupAction func(bool) error
	published     bool
	closed        bool
}

var (
	panelCertificateIssueVerifyPublished    = verifyPublishedPanelCertificateIssueReceipt
	panelCertificateIssueStabilizePublished = stabilizePublishedPanelCertificateIssue
)

func newPanelCertificateIssueReceipt(
	requestID, qualifier, domain string,
	leafDER []byte,
) (panelCertificateIssueReceipt, error) {
	receipt := panelCertificateIssueReceipt{
		Schema:     panelCertificateIssueReceiptSchema,
		RequestID:  requestID,
		Qualifier:  qualifier,
		Domain:     domain,
		LeafSHA256: panelCertificateLeafSHA256(leafDER),
	}
	if err := validatePanelCertificateIssueReceipt(receipt); err != nil {
		return panelCertificateIssueReceipt{}, err
	}
	return receipt, nil
}

func validatePanelCertificateIssueReceipt(receipt panelCertificateIssueReceipt) error {
	if receipt.Schema != panelCertificateIssueReceiptSchema ||
		!validMutationIdentity(receipt.RequestID) ||
		!mutationpayload.ValidPanelCertificateIssueQualifier(receipt.Qualifier) ||
		!validPanelCertDomain.MatchString(receipt.Domain) ||
		receipt.Domain != strings.ToLower(strings.TrimSpace(receipt.Domain)) {
		return errors.New("invalid panel certificate issue receipt identity")
	}
	if err := validatePanelCertificateLeafSHA256(receipt.LeafSHA256); err != nil {
		return err
	}
	return nil
}

func canonicalPanelCertificateIssueReceipt(
	receipt panelCertificateIssueReceipt,
) ([]byte, error) {
	if err := validatePanelCertificateIssueReceipt(receipt); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(receipt)
	if err != nil {
		return nil, fmt.Errorf("encode panel certificate issue receipt: %w", err)
	}
	raw = append(raw, '\n')
	if len(raw) > panelCertificateIssueReceiptMaxSize {
		return nil, errors.New("panel certificate issue receipt exceeds size limit")
	}
	return raw, nil
}

func decodePanelCertificateIssueReceipt(raw []byte) (panelCertificateIssueReceipt, error) {
	if len(raw) == 0 || len(raw) > panelCertificateIssueReceiptMaxSize {
		return panelCertificateIssueReceipt{}, errors.New(
			"panel certificate issue receipt has invalid size",
		)
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	var receipt panelCertificateIssueReceipt
	if err := decoder.Decode(&receipt); err != nil {
		return panelCertificateIssueReceipt{}, fmt.Errorf(
			"decode panel certificate issue receipt: %w", err,
		)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("multiple JSON values")
		}
		return panelCertificateIssueReceipt{}, fmt.Errorf(
			"decode panel certificate issue receipt trailer: %w", err,
		)
	}
	canonical, err := canonicalPanelCertificateIssueReceipt(receipt)
	if err != nil {
		return panelCertificateIssueReceipt{}, err
	}
	if subtle.ConstantTimeCompare(raw, canonical) != 1 {
		return panelCertificateIssueReceipt{}, errors.New(
			"panel certificate issue receipt is not canonical JSON",
		)
	}
	return receipt, nil
}

func formatPanelCertificateIssueCommitPhase(
	state, requestID, domain, qualifier string,
) (string, error) {
	if (state != panelCertificateIssueCommitIntent &&
		state != panelCertificateIssueCommitPublished) ||
		!validMutationIdentity(requestID) ||
		!validPanelCertDomain.MatchString(domain) ||
		domain != strings.ToLower(strings.TrimSpace(domain)) ||
		!mutationpayload.ValidPanelCertificateIssueQualifier(qualifier) {
		return "", errors.New("invalid panel certificate issue commit phase identity")
	}
	return panelCertificateIssueCommitPhasePrefix + state + "/" +
		requestID + "/" + domain + "/" + qualifier, nil
}

func parsePanelCertificateIssueCommitPhase(value string) (
	state, requestID, domain, qualifier string,
	err error,
) {
	if !strings.HasPrefix(value, panelCertificateIssueCommitPhasePrefix) {
		return "", "", "", "", errors.New(
			"not a panel certificate issue commit phase",
		)
	}
	remainder := strings.TrimPrefix(value, panelCertificateIssueCommitPhasePrefix)
	state, remainder, found := strings.Cut(remainder, "/")
	if !found {
		return "", "", "", "", errors.New(
			"invalid panel certificate issue commit phase",
		)
	}
	requestID, remainder, found = strings.Cut(remainder, "/")
	if !found {
		return "", "", "", "", errors.New(
			"invalid panel certificate issue commit phase",
		)
	}
	domain, qualifier, found = strings.Cut(remainder, "/")
	if !found {
		return "", "", "", "", errors.New(
			"invalid panel certificate issue commit phase",
		)
	}
	canonical, phaseErr := formatPanelCertificateIssueCommitPhase(
		state,
		requestID,
		domain,
		qualifier,
	)
	if phaseErr != nil || canonical != value {
		return "", "", "", "", errors.New(
			"invalid panel certificate issue commit phase",
		)
	}
	return state, requestID, domain, qualifier, nil
}

func (stage *panelCertificateIssueStage) publish() error {
	if stage == nil || stage.publishAction == nil || stage.cleanupAction == nil || stage.closed {
		return errors.New("invalid panel certificate issue stage")
	}
	if stage.published {
		return errors.New("panel certificate issue stage is already published")
	}
	published, err := stage.publishAction()
	stage.published = published
	return err
}

func (stage *panelCertificateIssueStage) close() error {
	if stage == nil || stage.closed {
		return nil
	}
	stage.closed = true
	if stage.cleanupAction == nil {
		return errors.New("invalid panel certificate issue stage cleanup")
	}
	return stage.cleanupAction(stage.published)
}

func activeDirectPanelCertificateIssueJob(job *ServiceMutationJob) bool {
	return job != nil && serviceMutationStatusActive(job.Status) &&
		job.Kind == "panel_certificate_issue" &&
		validPanelCertDomain.MatchString(job.Target)
}

// commitStandalonePanelCertificateIssueStep is the sole linearization gate
// for interactive V2 publication. The callback may only rename the already
// fsynced staged version to current and fsync the TLS directory.
func commitStandalonePanelCertificateIssueStep(
	ctx context.Context,
	commit func() error,
) (hostPublished bool, err error) {
	if ctx == nil || commit == nil {
		return false, errors.New("invalid panel certificate issue commit gate")
	}
	tracker, _ := ctx.Value(serviceMutationExecutionTrackerKey{}).(*serviceMutationExecutionTracker)
	if tracker == nil || tracker.manager == nil || tracker.runtime == nil {
		return false, errors.New("panel certificate issue commit gate requires a durable execution tracker")
	}
	m, runtime := tracker.manager, tracker.runtime
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.healthErrorLocked(); err != nil {
		return false, err
	}
	if m.active != runtime || runtime.job == nil || runtime.steps != 1 ||
		runtime.job.WorkerPID != 0 || runtime.job.Status != serviceMutationStatusRunning {
		return false, errors.New("panel certificate issue commit gate lost the active mutation step")
	}
	job := runtime.job
	if job.Kind != "panel_certificate_issue" ||
		!validPanelCertDomain.MatchString(job.Target) ||
		!mutationpayload.ValidPanelCertificateIssueQualifier(job.PackageName) {
		return false, errors.New("panel certificate issue commit gate rejected the mutation identity")
	}
	now := m.now()
	if ctx.Err() != nil || !now.Before(job.LeaseExpiresAt) || !now.Before(job.DeadlineAt) {
		return false, errors.New("service mutation lease ended before the panel certificate commit point")
	}
	intentPhase, err := formatPanelCertificateIssueCommitPhase(
		panelCertificateIssueCommitIntent,
		job.RequestID,
		job.Target,
		job.PackageName,
	)
	if err != nil {
		return false, err
	}
	publishedPhase, err := formatPanelCertificateIssueCommitPhase(
		panelCertificateIssueCommitPublished,
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
	published, verifyErr := panelCertificateIssueVerifyPublished(
		job.RequestID, job.PackageName, job.Target,
	)
	if verifyErr != nil {
		return false, m.poisonLocked(fmt.Errorf(
			"verify panel certificate publication after commit callback: %w",
			verifyErr,
		))
	}
	if !published {
		if commitErr != nil {
			return false, commitErr
		}
		return false, m.poisonLocked(errors.New(
			"panel certificate commit callback did not publish its exact receipt",
		))
	}
	runtime.panelCertificateIssuePublishedPhase = publishedPhase
	if commitErr != nil {
		if syncErr := panelCertificateIssueStabilizePublished(); syncErr != nil {
			return true, m.poisonLocked(fmt.Errorf(
				"stabilize verified panel certificate publication: %w", syncErr,
			))
		}
	}
	if err := m.finishRuntimeTerminalLocked(runtime, true, publishedPhase, "", ""); err != nil {
		if m.active == runtime {
			return true, m.poisonLocked(fmt.Errorf(
				"persist terminal panel certificate receipt after publication: %w", err,
			))
		}
		return true, err
	}
	return true, nil
}

// recoverPersistedPanelCertificateIssueLocked is wired into manager startup by
// the shared integration. It never promotes a stage: only an exact receipt
// already reachable through current is success.
func (m *serviceMutationManager) recoverPersistedPanelCertificateIssueLocked(
	job *ServiceMutationJob,
	lock *serviceMutationFileLock,
) (handled bool, err error) {
	if !activeDirectPanelCertificateIssueJob(job) ||
		serviceMutationWorkerMatches(job.WorkerPID, job.WorkerStarted) {
		return false, nil
	}
	if !mutationpayload.ValidPanelCertificateIssueQualifier(job.PackageName) {
		m.poisonLock = lock
		if job.PackageName == "certbot" {
			return true, m.poisonLocked(errors.New(
				"active legacy panel certificate issue cannot be recovered safely",
			))
		}
		return true, m.poisonLocked(errors.New(
			"active panel certificate issue has an invalid payload qualifier",
		))
	}
	intent := false
	if strings.HasPrefix(job.Phase, panelCertificateIssueCommitPhasePrefix) {
		state, requestID, domain, qualifier, phaseErr :=
			parsePanelCertificateIssueCommitPhase(job.Phase)
		if phaseErr != nil ||
			requestID != job.RequestID ||
			domain != job.Target ||
			qualifier != job.PackageName {
			m.poisonLock = lock
			return true, m.poisonLocked(errors.New(
				"active panel certificate issue has an invalid commit receipt",
			))
		}
		intent = state == panelCertificateIssueCommitIntent
	}

	recoveryBase, cancel := context.WithTimeout(context.Background(), panelCertificateIssueRecoveryTimeout)
	runtime := &serviceMutationRuntime{job: job, lock: lock, ctx: recoveryBase, cancel: cancel}
	m.mu.Unlock()
	runtime.stepMu.Lock()
	m.mu.Lock()
	if m.active != nil || m.ledger.ActiveRequestID != job.RequestID {
		cancel()
		m.poisonLock = lock
		identityErr := m.poisonLocked(errors.New("panel certificate issue recovery identity changed"))
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
		runtime.job.Phase = "recovering_panel_certificate_issue"
	}
	runtime.job.ErrorCode = "agent_restart_during_panel_certificate_issue"
	runtime.job.ErrorMessage = "The agent is reconciling panel certificate publication after a restart."
	runtime.job.WorkerPID = 0
	runtime.job.WorkerStarted = ""
	runtime.job.WorkerCommand = ""
	runtime.job.UpdatedAt = m.now()
	if persistErr := m.persistLedgerMutationLocked(before); persistErr != nil {
		poisonErr := m.poisonLocked(fmt.Errorf(
			"persist panel certificate issue recovery intent: %w", persistErr,
		))
		runtime.steps = 0
		m.mu.Unlock()
		runtime.stepMu.Unlock()
		m.mu.Lock()
		return true, poisonErr
	}

	m.mu.Unlock()
	success, recoveryErr := reconcilePersistedPanelCertificateIssueHost(
		recoveryBase, runtime.job.RequestID, runtime.job.PackageName, runtime.job.Target,
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
		publishedPhase, phaseErr := formatPanelCertificateIssueCommitPhase(
			panelCertificateIssueCommitPublished,
			runtime.job.RequestID,
			runtime.job.Target,
			runtime.job.PackageName,
		)
		if phaseErr != nil {
			return true, m.poisonLocked(phaseErr)
		}
		runtime.panelCertificateIssuePublishedPhase = publishedPhase
		if finishErr := m.finishRuntimeTerminalLocked(
			runtime, true, publishedPhase, "", "",
		); finishErr != nil {
			return true, m.poisonLocked(fmt.Errorf(
				"persist recovered panel certificate issue success: %w", finishErr,
			))
		}
		return true, nil
	}
	if finishErr := m.finishRuntimeTerminalLocked(
		runtime,
		false,
		"interrupted",
		"agent_restarted_before_panel_certificate_commit",
		"The agent removed an uncommitted panel certificate stage after restart.",
	); finishErr != nil {
		return true, m.poisonLocked(fmt.Errorf(
			"persist recovered panel certificate issue failure: %w", finishErr,
		))
	}
	return true, nil
}
