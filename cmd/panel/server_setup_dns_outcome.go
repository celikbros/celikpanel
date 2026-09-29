package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
)

// Setup DNS step outcome classification (D-024, pair2 finding P-B).
//
// When the Panel cannot record the exact DNS setup operation as committed or
// rolled back, the Agent's ledger for that exact request decides what setup
// shows. A terminal or held result is a verified observation and is shown as
// such; "reconciling" is kept for a result that is genuinely unknown to the
// Panel (the Agent is unreachable, its status cannot be read, or it has no
// record of the request), and that unknown carries a time bound. Every path
// here is a read: it never starts, retries or finishes a mutation.
//
// Kurulumun DNS adımında Panel işlemi kaydedemediğinde, aynı isteğin Agent
// kaydı gösterilecek durumu belirler. Doğrulanmış hata ya da bekletme böyle
// gösterilir; "uzlaştırılıyor" yalnız gerçekten bilinmeyen sonuç içindir ve
// süre sınırı taşır. Buradaki her yol okumadır; hiçbir değişiklik başlatmaz.

const (
	serverSetupDNSAgentFailedCode    = "server_setup_dns_agent_failed"
	serverSetupDNSRecoveryHeldCode   = "server_setup_dns_recovery_held"
	serverSetupDNSResultUnknownCode  = "server_setup_dns_result_unknown"
	serverSetupDNSStatusCommandFront = "sudo /usr/libexec/celikpanel/recovery dns-switch-status --quiesced --request-id "
)

// serverSetupDNSUnknownBound is how long the setup DNS step may say that the
// result is "being checked" before it says the result is still unknown and
// names what the owner can check. It reuses panelDNSCommitReconcileTimeout,
// the Panel's window for an exact DNS commit receipt to become readable
// after its RPC reply was lost. The switch's own 30-minute budget
// (dnsEngineSwitchTimeout) applies only while the Agent reports the job
// active, which is no longer shown as an unknown.
const serverSetupDNSUnknownBound = panelDNSCommitReconcileTimeout

// errServerSetupDNSAgentRunning: the Agent reports the exact DNS request as
// still running. Its result is not unknown; setup shows normal progress.
var errServerSetupDNSAgentRunning = errors.New("the Agent is still running the exact DNS setup operation")

// serverSetupDNSAgentFailedError is a terminal failed Agent job for the exact
// DNS setup request that the Panel could not record as rolled back.
type serverSetupDNSAgentFailedError struct {
	RequestID string
	AgentCode string
	// Reason is the job's own message, bounded. It is Panel-authored, the
	// host operator sentence, or the Agent's release guidance; never raw
	// command output.
	Reason string
	// Held: the Agent released the job without a verified native result and
	// keeps the operation's journal, so DNS changes stay blocked.
	Held  bool
	cause error
}

func (e *serverSetupDNSAgentFailedError) Error() string {
	state := "failed"
	if e.Held {
		state = "held"
	}
	message := fmt.Sprintf("the Agent recorded DNS setup request %s as %s (%s)", e.RequestID, state, e.AgentCode)
	if e.cause != nil {
		message += ": " + e.cause.Error()
	}
	return message
}

func (e *serverSetupDNSAgentFailedError) Unwrap() error { return e.cause }

// serverSetupDNSReleasedAgentCode reports the Agent's release codes: the job
// ended without a verified native result and its journal is retained.
func serverSetupDNSReleasedAgentCode(code string) bool {
	switch code {
	case dnsengineartifact.ReleasedNativeUnknownCode,
		dnsengineartifact.ReleasedUnsupportedHostCode,
		dnsengineartifact.ReleasedHostWindowCode:
		return true
	}
	return false
}

// serverSetupDNSAgentOutcome reads the Agent's ledger for the exact request
// after the Panel's own reconciliation could not finish. It returns
// errServerSetupDNSAgentRunning, a *serverSetupDNSAgentFailedError, or
// errServerSetupDNSReconciliationRequired (unknown). Read only.
func (p *Panel) serverSetupDNSAgentOutcome(ctx context.Context, persisted persistedDNSEngineSwitch, cause error) error {
	pending := func(err error) error { return fmt.Errorf("%w: %v", errServerSetupDNSReconciliationRequired, err) }
	identity := agentMutationIdentity{
		RequestID: persisted.RequestID, OwnerID: persisted.OwnerID,
		Kind: dnsEngineSwitchKind, Target: string(persisted.TargetEngine),
		PackageName: persisted.Qualifier,
	}
	job, err := p.statusAgentMutation(ctx, persisted.RequestID)
	if err != nil {
		return pending(errors.Join(cause, fmt.Errorf("read the Agent's record of this request: %w", err)))
	}
	if job == nil || !identity.matches(job) {
		return pending(cause)
	}
	switch {
	case agentMutationActive(job.Status):
		return fmt.Errorf("%w: %v", errServerSetupDNSAgentRunning, cause)
	case job.Status == agentMutationFailed:
		return &serverSetupDNSAgentFailedError{
			RequestID: persisted.RequestID,
			AgentCode: boundedOperatorSentence(job.ErrorCode),
			Reason:    boundedOperatorSentence(job.ErrorMessage),
			Held:      serverSetupDNSReleasedAgentCode(job.ErrorCode),
			cause:     cause,
		}
	}
	return pending(cause)
}

// serverSetupDNSAgentFailedMessage is the setup error for a terminal Agent
// job: the stable code, the Agent's bounded reason, who acts, the next action
// and how setup resumes.
func serverSetupDNSAgentFailedMessage(failed *serverSetupDNSAgentFailedError) *serviceOperationError {
	command := serverSetupDNSStatusCommandFront + failed.RequestID
	reason := strings.TrimSpace(failed.Reason)
	if failed.Held {
		message := "The CelikPanel Agent stopped this DNS operation (request " + failed.RequestID + ") without being able to verify its result, and keeps its record so that DNS changes stay blocked until it is resolved."
		if reason != "" {
			message += " Agent: " + reason
		}
		message += " Setup is paused at this step and will not start the operation again. The server administrator should run `" + command + "` on this server and follow what it reports. Setup rechecks this same operation automatically."
		return &serviceOperationError{Code: serverSetupDNSRecoveryHeldCode, Message: message}
	}
	message := "The CelikPanel Agent recorded this DNS operation (request " + failed.RequestID + ") as failed."
	if reason != "" {
		message += " Reason: " + reason
	}
	message += " CelikPanel has not confirmed that this server is back to its state before the operation, so setup is paused at this step and will not start the operation again. The server administrator should run `" + command + "` on this server and follow what it reports. Setup rechecks this same operation automatically and continues, or offers a new plan, once its result is confirmed."
	return &serviceOperationError{Code: serverSetupDNSAgentFailedCode, Message: message}
}

// serverSetupDNSUnknownFirstSeen keeps, per execution and request, when this
// Panel process first saw the result as unknown. It is used only when the
// DNS operation's own durable record cannot be read.
var serverSetupDNSUnknownFirstSeen sync.Map

// serverSetupDNSUnknownSince is the durable time of the operation's last
// recorded change (the Panel's DNS switch row), or, if that row cannot be
// read, the first time this process saw the result as unknown.
func (p *Panel) serverSetupDNSUnknownSince(ctx context.Context, executionID, requestID string, now time.Time) time.Time {
	if persisted, err := readDNSEngineSwitchByRequest(ctx, p.db.GetDB(), requestID); err == nil {
		for _, layout := range []string{"2006-01-02 15:04:05", time.RFC3339Nano} {
			if at, err := time.Parse(layout, persisted.UpdatedAt); err == nil {
				return at.UTC()
			}
		}
	}
	first, _ := serverSetupDNSUnknownFirstSeen.LoadOrStore(executionID+"/"+requestID, now.UTC())
	return first.(time.Time)
}

// serverSetupDNSReconcilingError is the setup error for a DNS result that is
// unknown to the Panel. Within serverSetupDNSUnknownBound it keeps the
// existing "being checked" code; after it, the text says the result is still
// unknown, since when, what the owner can check and that nothing will be
// started twice.
func serverSetupDNSReconcilingError(requestID string, since, now time.Time) *serviceOperationError {
	elapsed := now.Sub(since)
	if since.IsZero() || elapsed < serverSetupDNSUnknownBound {
		return &serviceOperationError{Code: "server_setup_reconciling", Message: "The previous DNS operation is being reconciled. Its exact receipt must be verified before the setup plan can change."}
	}
	minutes := int(elapsed / time.Minute)
	message := fmt.Sprintf("The result of this DNS operation (request %s) is still unknown: CelikPanel has not been able to confirm it since %s (%d minutes). This is not a verified failure, and nothing will be started twice. The server administrator can check the operation on this server with `%s%s` and check that the CelikPanel Agent is running (`systemctl status celikpanel-agent`). Setup keeps checking the same operation.",
		requestID, since.UTC().Format("2006-01-02 15:04 UTC"), minutes, serverSetupDNSStatusCommandFront, requestID)
	return &serviceOperationError{Code: serverSetupDNSResultUnknownCode, Message: message}
}
