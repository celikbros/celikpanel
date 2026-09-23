package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/transport"
)

type setupEnrollmentContinueAgent struct {
	verifiedAPTAgentRPCFixture
	state, commit string
	starts        atomic.Int32
}

func (a *setupEnrollmentContinueAgent) Version(_ *transport.Empty, out *transport.AgentVersionResponse) error {
	out.Commit = a.commit
	return nil
}
func (a *setupEnrollmentContinueAgent) MailEnrollmentStatusV1(req *transport.MailEnrollmentRequest, out *transport.MailEnrollmentStatusResponse) error {
	*out = transport.MailEnrollmentStatusResponse{MailEnrollmentRequest: *req, State: a.state, ObservedAt: time.Now().UTC()}
	return nil
}
func (a *setupEnrollmentContinueAgent) ContinueMailEnrollmentV1(req *transport.MailEnrollmentStartRequest, out *transport.MailEnrollmentStartResponse) error {
	a.starts.Add(1)
	*out = transport.MailEnrollmentStartResponse{RequestID: req.RequestID, Handoff: "accepted"}
	return nil
}
func TestSetupMailEnrollmentContinueHTTPBindsCurrentPlanAndPreservesJSON(t *testing.T) {
	for _, scenario := range []string{"forward", "rollback", "published", "restored", "unknown", "not_recorded", "no-admin", "get", "license", "build", "pair", "revision", "execution", "step", "owner", "fence", "finished", "phase", "client-owner"} {
		t.Run(scenario, func(t *testing.T) {
			f, state := setupOperationFixture(t)
			plan, execution := setupEnrollmentTestPlan()
			old := buildCommit
			buildCommit = plan.BuildCommit
			t.Cleanup(func() { buildCommit = old })
			plan.Revision = state.Revision
			plan.Actor.UserID = f.userID
			plan.ID = serverSetupPlanIdentity(plan)
			execution.PlanID = plan.ID
			execution.Phase = execution.Steps[0].ID
			execution.Steps[0].EnrollmentDispatchAttempted = true
			agent := &setupEnrollmentContinueAgent{state: "forward", commit: buildCommit}
			switch scenario {
			case "forward", "rollback", "published", "restored", "unknown", "not_recorded":
				agent.state = scenario
			case "license":
				f.panel.license = nil
			case "build":
				buildCommit = strings.Repeat("f", 40)
			case "pair":
				agent.commit = strings.Repeat("f", 40)
			case "revision":
				plan.Revision++
				plan.ID = serverSetupPlanIdentity(plan)
				execution.PlanID = plan.ID
			case "owner":
				execution.Steps[0].OwnerID = strings.Repeat("f", 32)
			case "fence":
				execution.Steps[0].EnrollmentDispatchAttempted = false
			case "finished":
				execution.Status = "succeeded"
				execution.Steps[0].Status = "succeeded"
			case "phase":
				execution.Phase = "other"
			}
			f.panel.agentClient = newPolicyDispatchTestPanel(t, agent).agentClient
			raw, _ := json.Marshal(plan)
			now := time.Now().UTC().Format(time.RFC3339Nano)
			if _, err := f.database.GetDB().Exec(`INSERT INTO server_setup_plans(id,revision,plan_json,requested_by,created_at) VALUES(?,?,?,?,?)`, plan.ID, plan.Revision, string(raw), f.userID, now); err != nil {
				t.Fatal(err)
			}
			raw, _ = json.Marshal(execution)
			before := string(raw)
			if _, err := f.database.GetDB().Exec(`INSERT INTO server_setup_executions(id,request_id,plan_id,status,execution_json,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`, execution.ID, execution.RequestID, plan.ID, execution.Status, before, now, now); err != nil {
				t.Fatal(err)
			}
			input := map[string]string{"execution_id": execution.ID, "step_id": execution.Steps[0].ID}
			if scenario == "execution" {
				input["execution_id"] = strings.Repeat("f", 32)
			}
			if scenario == "step" {
				input["step_id"] = "other"
			}
			if scenario == "client-owner" {
				input["owner_id"] = strings.Repeat("f", 32)
			}
			body, _ := json.Marshal(input)
			method := http.MethodPost
			if scenario == "get" {
				method = http.MethodGet
			}
			req := serviceOperationAdminRequest(t, method, serverSetupPath+"/mail-enrollment/continue", string(body), f.userID)
			if scenario == "no-admin" {
				req = httptest.NewRequest(method, req.URL.String(), strings.NewReader(string(body)))
			}
			w := httptest.NewRecorder()
			f.panel.handleServerSetupMailEnrollmentContinue(w, req)
			accepted := scenario == "forward" || scenario == "rollback" || scenario == "published" || scenario == "restored"
			if (w.Code == http.StatusAccepted) != accepted {
				t.Fatalf("HTTP %d %s", w.Code, w.Body.String())
			}
			want := int32(0)
			if scenario == "forward" || scenario == "rollback" {
				want = 1
			}
			if agent.starts.Load() != want {
				t.Fatal("wrong dispatch count", agent.starts.Load())
			}
			var after string
			if err := f.database.GetDB().QueryRow(`SELECT execution_json FROM server_setup_executions WHERE id=?`, execution.ID).Scan(&after); err != nil || after != before {
				t.Fatal("continuation rewrote concurrent runner state", err)
			}
		})
	}
}
func TestSetupMailEnrollmentContinueLostReplyDoesNotRetry(t *testing.T) {
	plan, execution := setupEnrollmentTestPlan()
	req, err := setupMailEnrollmentRequest(plan, &execution, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"lost", "wrong-tuple", "missing-time", "transport", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if scenario == "cancelled" {
				cancel()
			}
			calls := 0
			out, err := continueSetupMailEnrollment(ctx, req, func(r *transport.MailEnrollmentRequest, o *transport.MailEnrollmentStatusResponse) error {
				*o = transport.MailEnrollmentStatusResponse{MailEnrollmentRequest: *r, State: "forward", ObservedAt: time.Now().UTC()}
				if scenario == "wrong-tuple" {
					o.OwnerID = strings.Repeat("f", 32)
				}
				if scenario == "missing-time" {
					o.ObservedAt = time.Time{}
				}
				if scenario == "transport" {
					return errors.New("private")
				}
				return nil
			}, func(*transport.MailEnrollmentStartRequest, *transport.MailEnrollmentStartResponse) error {
				calls++
				return errors.New("reply lost")
			})
			if scenario == "lost" {
				if err != nil || calls != 1 || out.Handoff != "unknown" {
					t.Fatalf("%+v %v %d", out, err, calls)
				}
			} else if err == nil || calls != 0 {
				t.Fatalf("%v %d", err, calls)
			}
		})
	}
}
