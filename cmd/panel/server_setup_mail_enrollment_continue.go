package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/alicelik/celikpanel/internal/transport"
)

// Only the reviewed execution and step are accepted from the browser. Native
// owner, generation and request authority are derived from immutable intent.
func setupMailEnrollmentContinuation(plan serverSetupPlan, execution *serverSetupExecution, executionID, stepID string) (transport.MailEnrollmentStartRequest, error) {
	if execution != nil && execution.ID == executionID && execution.Status == "running" && execution.Phase == stepID {
		for index, step := range execution.Steps {
			if step.ID == stepID && step.Status == "running" && step.EnrollmentDispatchAttempted {
				return setupMailEnrollmentRequest(plan, execution, index)
			}
		}
	}
	return transport.MailEnrollmentStartRequest{}, errors.New("the current setup has no matching recorded mail enrollment to continue")
}

func (p *Panel) handleServerSetupMailEnrollmentContinue(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if !requireServerSetupAdmin(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var input struct {
		ExecutionID string `json:"execution_id"`
		StepID      string `json:"step_id"`
	}
	if decodeServiceOperationJSON(w, r, &input) != nil || !validServiceOperationID(input.ExecutionID) || input.StepID == "" || len(input.StepID) > 128 {
		writeClientError(w, http.StatusBadRequest, "invalid mail enrollment continuation request")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	p.serverSetupMu.Lock()
	defer p.serverSetupMu.Unlock()
	conflict := func() {
		writeCodedError(w, http.StatusConflict, "setup_mail_enrollment_continue_conflict", "Reload the current setup. Only its recorded mail renewal operation can be continued.", "/setup")
	}
	execution, err := p.latestServerSetupExecution(ctx)
	if err != nil {
		writeServerError(w, err)
		return
	}
	if execution == nil || execution.ID != input.ExecutionID {
		conflict()
		return
	}
	plan, err := p.loadServerSetupPlan(ctx, execution.PlanID)
	if err != nil {
		conflict()
		return
	}
	req, err := setupMailEnrollmentContinuation(plan, execution, input.ExecutionID, input.StepID)
	if err != nil {
		conflict()
		return
	}
	state, err := p.loadServerSetup(ctx)
	if err != nil {
		writeServerError(w, err)
		return
	}
	if state.Revision != plan.Revision || state.Status == "ready" {
		conflict()
		return
	}
	if err = p.requireServerSetupAdmission(); err != nil {
		writeCodedError(w, http.StatusForbidden, "setup_license_required", "An active license is required to request continuation. The recorded operation is preserved.", "/activate")
		return
	}
	if plan.BuildCommit != buildCommit {
		writeCodedError(w, http.StatusConflict, "server_setup_build_changed", "The installed build differs from the reviewed setup. Preserve the recorded request and inspect its recovery guidance.", "/setup")
		return
	}
	if err = p.requireMatchingAgentBuild(ctx); err != nil {
		conflict()
		return
	}
	if err = p.authorizeAgentRPCContext(ctx, "Agent.ContinueMailEnrollmentV1"); err != nil {
		writeCodedError(w, http.StatusForbidden, "setup_mail_enrollment_continue_denied", "Mail renewal continuation is not authorized. The recorded operation is preserved.", "/setup")
		return
	}
	// No execution JSON rewrite: the concurrent runner remains the only writer
	// of its reconciliation result. Both observers retain the same native identity.
	out, err := continueSetupMailEnrollment(ctx, req, func(request *transport.MailEnrollmentRequest, response *transport.MailEnrollmentStatusResponse) error {
		return p.callAgentContext(ctx, "Agent.MailEnrollmentStatusV1", request, response)
	}, func(request *transport.MailEnrollmentStartRequest, response *transport.MailEnrollmentStartResponse) error {
		p.audit(r, "server.setup.mail_enrollment.continue", execution.ID, 0)
		return p.callAgentContext(ctx, "Agent.ContinueMailEnrollmentV1", request, response)
	})
	if err != nil {
		writeCodedError(w, http.StatusConflict, "setup_mail_enrollment_continue_unverified", "Continuation could not be confirmed. Check the same operation; do not start another setup.", "/setup")
		return
	}
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(out)
}

func continueSetupMailEnrollment(ctx context.Context, req transport.MailEnrollmentStartRequest, status func(*transport.MailEnrollmentRequest, *transport.MailEnrollmentStatusResponse) error, dispatch func(*transport.MailEnrollmentStartRequest, *transport.MailEnrollmentStartResponse) error) (transport.MailEnrollmentStartResponse, error) {
	out := transport.MailEnrollmentStartResponse{RequestID: req.RequestID, Handoff: "unknown"}
	if ctx == nil || status == nil || dispatch == nil {
		return out, errors.New("continuation unavailable")
	}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	var observed transport.MailEnrollmentStatusResponse
	if err := status(&req.MailEnrollmentRequest, &observed); err != nil || observed.MailEnrollmentRequest != req.MailEnrollmentRequest || observed.ObservedAt.IsZero() {
		return out, setupMailEnrollmentWaiting("unknown", req.RequestID)
	}
	switch observed.State {
	case "published", "restored":
		out.Handoff, out.Reason = "accepted", "mail_enrollment_already_terminal"
		return out, nil // The normal observer retains success versus restored failure.
	case "forward", "rollback":
	default:
		return out, setupMailEnrollmentWaiting("unknown", req.RequestID)
	}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	var handed transport.MailEnrollmentStartResponse
	if err := dispatch(&req, &handed); err != nil || handed.RequestID != req.RequestID || handed.Handoff != "accepted" {
		out.Reason = "mail_enrollment_handoff_unknown"
		return out, nil // Lost reply is neither a proven failure nor permission to retry.
	}
	return handed, nil
}
