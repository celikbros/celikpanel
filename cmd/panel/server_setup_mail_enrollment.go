package main

import (
	"context"
	"errors"
	"strings"

	"github.com/alicelik/celikpanel/internal/transport"
)

// No plan builder advertises this step until native handoff/reboot acceptance.
// Existing accepted plans are never extended to acquire new enrollment authority.
type serverSetupMailEnrollmentWait struct{ Code, Message string }

func (e *serverSetupMailEnrollmentWait) Error() string { return e.Code }
func setupMailEnrollmentWaiting(reason, requestID string) error {
	message := "The mail renewal result is unknown. The server administrator can inspect celikpanel-mail-enrollment-<request-id>.service using this step's request ID. This setup will keep checking the same request; it will not start it again."
	switch reason {
	case "running":
		message = "The mail renewal request is recorded, but its final result is not verified. Setup will keep checking. The server administrator can inspect celikpanel-mail-enrollment-<request-id>.service and continue the same recorded operation if it stopped."
	case "handoff":
		message = "The independent worker handoff was accepted. Enrollment is not yet confirmed. Setup will check the same request automatically; the server administrator can inspect celikpanel-mail-enrollment-<request-id>.service if it stops."
	case "rollback":
		message = "The recorded mail renewal request requires restoration; its completion is not yet verified. Setup will check that result automatically. The server administrator can inspect the same worker unit if restoration stops."
	case "not_recorded":
		message = "The mail renewal handoff has no verified admission record yet. The server administrator must inspect the same worker unit before explicitly retrying the original reviewed request. Setup will keep checking; it will not create another request."
	}
	return &serverSetupMailEnrollmentWait{"server_setup_mail_enrollment_" + reason, strings.ReplaceAll(message, "<request-id>", requestID)}
}
func setupMailEnrollmentRequest(plan serverSetupPlan, execution *serverSetupExecution, index int) (transport.MailEnrollmentStartRequest, error) {
	empty := transport.MailEnrollmentStartRequest{}
	if execution == nil || index < 0 || index >= len(execution.Steps) || plan.Version != serverSetupPlanVersion || plan.ID != serverSetupPlanIdentity(plan) || validateServerSetupExecution(plan, *execution) != nil {
		return empty, errors.New("mail enrollment does not match the accepted setup plan")
	}
	step := execution.Steps[index]
	if step.Kind != "mail_enrollment" || step.Target != "mail-renewal" || len(step.Qualifier) != 64 || strings.Trim(step.Qualifier, "0123456789abcdef") != "" {
		return empty, errors.New("reviewed mail renewal generation is invalid")
	}
	for _, earlier := range execution.Steps[:index] {
		if earlier.Status != "succeeded" {
			return empty, errors.New("earlier setup steps have not completed")
		}
	}
	return transport.MailEnrollmentStartRequest{MailEnrollmentRequest: transport.MailEnrollmentRequest{RequestID: step.RequestID, OwnerID: step.OwnerID, Generation: step.Qualifier}, ExpectedBuildCommit: plan.BuildCommit}, nil
}
func (p *Panel) runServerSetupMailEnrollment(ctx context.Context, plan serverSetupPlan, execution *serverSetupExecution, index int) (bool, error) {
	request, err := setupMailEnrollmentRequest(plan, execution, index)
	if err != nil {
		return false, err
	}
	return advanceSetupMailEnrollment(ctx, request, &execution.Steps[index], func() error {
		if err := p.requireServerSetupAdmission(); err != nil {
			return err
		}
		if plan.BuildCommit != buildCommit {
			return errServerSetupBuildChanged
		}
		if err := p.requireMatchingAgentBuild(ctx); err != nil {
			return err
		}
		return p.authorizeAgentRPCContext(ctx, "Agent.StartMailEnrollmentV1")
	}, func() error { return p.persistServerSetupExecution(ctx, *execution) }, func(req *transport.MailEnrollmentRequest, out *transport.MailEnrollmentStatusResponse) error {
		return p.callAgentContext(ctx, "Agent.MailEnrollmentStatusV1", req, out)
	}, func(req *transport.MailEnrollmentStartRequest, out *transport.MailEnrollmentStartResponse) error {
		p.auditServiceOperation(ctx, plan.Actor, "server_setup_mail_enrollment_requested")
		return p.callAgentContext(ctx, "Agent.StartMailEnrollmentV1", req, out)
	})
}

// The persisted attempt fence precedes IPC. A crash/lost reply leaves the same
// request observational; even proven ledger absence cannot authorize redispatch.
func advanceSetupMailEnrollment(ctx context.Context, req transport.MailEnrollmentStartRequest, step *serverSetupExecutionStep, admit, persist func() error, status func(*transport.MailEnrollmentRequest, *transport.MailEnrollmentStatusResponse) error, start func(*transport.MailEnrollmentStartRequest, *transport.MailEnrollmentStartResponse) error) (bool, error) {
	if ctx == nil || step == nil || step.Status != "running" || req.RequestID != step.RequestID || req.OwnerID != step.OwnerID || req.Generation != step.Qualifier || admit == nil || persist == nil || status == nil || start == nil {
		return false, errors.New("mail enrollment adapter is unavailable")
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	var observed transport.MailEnrollmentStatusResponse
	if err := status(&req.MailEnrollmentRequest, &observed); err != nil || observed.MailEnrollmentRequest != req.MailEnrollmentRequest || observed.ObservedAt.IsZero() {
		return false, setupMailEnrollmentWaiting("unknown", req.RequestID)
	}
	switch observed.State {
	case "published":
		return true, nil // historical completion; final health checks remain separate
	case "restored":
		return false, &serverSetupChildFailure{Code: "mail_enrollment_restored", Message: "Mail renewal enrollment was restored to its previous state. The server administrator must review the failed request before starting a new plan."}
	case "forward":
		return false, setupMailEnrollmentWaiting("running", req.RequestID)
	case "rollback":
		return false, setupMailEnrollmentWaiting("rollback", req.RequestID)
	case "not_recorded":
		if step.EnrollmentDispatchAttempted {
			return false, setupMailEnrollmentWaiting("not_recorded", req.RequestID)
		}
	default:
		return false, setupMailEnrollmentWaiting("unknown", req.RequestID)
	}
	if err := admit(); err != nil {
		return false, err
	}
	step.EnrollmentDispatchAttempted = true
	if err := persist(); err != nil {
		return false, err
	}
	var handed transport.MailEnrollmentStartResponse
	if err := start(&req, &handed); err != nil || handed.RequestID != req.RequestID || handed.Handoff != "accepted" {
		return false, setupMailEnrollmentWaiting("unknown", req.RequestID)
	}
	return false, setupMailEnrollmentWaiting("handoff", req.RequestID)
}
