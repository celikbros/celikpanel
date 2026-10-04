package main

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/transport"
)

type setupEnrollmentPreviewAgent struct {
	*serviceOperationTestAgent
	preview     transport.MailEnrollmentPreviewResponse
	unavailable bool
}

func (a *setupEnrollmentPreviewAgent) MailEnrollmentPreviewV1(req *transport.MailEnrollmentSourceRequest, out *transport.MailEnrollmentPreviewResponse) error {
	if req.ExpectedBuildCommit != buildCommit {
		return errors.New("wrong requested build")
	}
	if a.unavailable {
		return errors.New("unavailable")
	}
	*out = a.preview
	return nil
}
func TestSetupMailEnrollmentNewPlanBindsKitAndPreservesIndependentSchedule(t *testing.T) {
	for _, scenario := range []string{"fresh", "legacy", "independent", "disabled", "unknown", "busy", "unavailable", "wrong-build", "bad-generation", "no-time", "future-mode", "mixed-initial", "bad-existing", "bad-timer"} {
		t.Run(scenario, func(t *testing.T) {
			f, state := setupOperationFixture(t)
			t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.10")
			caps := []string{transport.AgentCapabilityMailHostCertificateV1, transport.AgentCapabilityMailTLSSyncV2, transport.AgentCapabilityFirewallApplyV2, transport.AgentCapabilityPanelCertificateIssueV2}
			f.agent.versionCapabilities = &caps
			state.Draft.Purpose = "web_mail"
			state.Draft.MailHostname = "mail.example.test"
			preview := transport.MailEnrollmentPreviewResponse{MailEnrollmentSourceResponse: transport.MailEnrollmentSourceResponse{State: "verified", Generation: strings.Repeat("c", 64), BuildCommit: buildCommit, ObservedAt: time.Now().UTC()}, NativeMode: "absent", TimerEnablement: "absent", TimerActivity: "inactive"}
			switch scenario {
			case "legacy":
				preview.NativeMode = "legacy"
			case "independent", "disabled":
				preview.NativeMode = "independent"
				preview.ExistingGeneration = strings.Repeat("d", 64)
				preview.TimerEnablement = "enabled"
				preview.TimerActivity = "active"
				if scenario == "disabled" {
					preview.TimerEnablement = "disabled"
					preview.TimerActivity = "inactive"
				}
			case "busy":
				preview.State, preview.Reason = "waiting", "mail_enrollment_native_busy"
			case "unknown":
				preview.State = "unknown"
			case "wrong-build":
				preview.BuildCommit = strings.Repeat("f", 40)
			case "bad-generation":
				preview.Generation = "../kit"
			case "no-time":
				preview.ObservedAt = time.Time{}
			case "future-mode":
				preview.NativeMode = "future"
			case "mixed-initial":
				preview.ExistingGeneration = strings.Repeat("d", 64)
			case "bad-existing":
				preview.NativeMode = "independent"
				preview.ExistingGeneration = "bad"
			case "bad-timer":
				preview.TimerEnablement = "unknown"
			}
			agent := &setupEnrollmentPreviewAgent{f.agent, preview, scenario == "unavailable"}
			f.panel.agentClient = newPolicyDispatchTestPanel(t, agent).agentClient
			plan, err := f.panel.buildServerSetupPlan(context.Background(), state, serviceOperationActor{UserID: f.userID})
			if err != nil {
				t.Fatal(err)
			}
			good := scenario == "fresh" || scenario == "legacy" || scenario == "independent" || scenario == "disabled"
			blocker := "server_setup_mail_enrollment_unavailable"
			if scenario == "busy" {
				blocker = "server_setup_mail_enrollment_busy"
			}
			if plan.CanStart != good || slices.Contains(plan.Blockers, blocker) == good {
				t.Fatalf("unexpected review: %+v", plan.Blockers)
			}
			cert, index := -1, -1
			for i, step := range plan.Steps {
				if step.Kind == "mail_certificate" {
					cert = i
				}
				if step.Kind == "mail_enrollment" {
					index = i
					if step.Qualifier != preview.Generation || step.Target != "mail-renewal" {
						t.Fatal("review lost exact kit")
					}
				}
			}
			enroll := scenario == "fresh" || scenario == "legacy"
			if (index >= 0) != enroll || enroll && (cert < 0 || index != cert+1 || plan.Steps[index+1].Kind != "verify") {
				t.Fatal("wrong enrollment order", plan.Steps)
			}
			if plan.ID != serverSetupPlanIdentity(plan) {
				t.Fatal("step was not included in immutable review")
			}
			if f.agent.installCalls.Load() != 0 || len(f.agent.capturedMutationEvents()) != 0 {
				t.Fatal("review mutated host")
			}
			var count int
			if err := f.database.GetDB().QueryRow(`SELECT count(*) FROM server_setup_executions`).Scan(&count); err != nil || count != 0 {
				t.Fatal("review created execution", err)
			}
		})
	}
}
func TestSetupMailEnrollmentReviewRejectsDevelopmentSource(t *testing.T) {
	preview := transport.MailEnrollmentPreviewResponse{MailEnrollmentSourceResponse: transport.MailEnrollmentSourceResponse{State: "verified", Generation: strings.Repeat("c", 64), BuildCommit: "unknown", ObservedAt: time.Now().UTC()}, NativeMode: "absent", TimerEnablement: "absent", TimerActivity: "inactive"}
	if _, err := setupMailEnrollmentPlanGeneration(preview, "unknown"); err == nil {
		t.Fatal("unreleased source accepted")
	}
}
