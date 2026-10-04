package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/transport"
)

func setupEnrollmentTestPlan() (serverSetupPlan, serverSetupExecution) {
	step := serverSetupPlanStep{ID: "mail-renewal", Kind: "mail_enrollment", Target: "mail-renewal", Qualifier: strings.Repeat("a", 64)}
	plan := serverSetupPlan{Version: serverSetupPlanVersion, BuildCommit: strings.Repeat("b", 40), Steps: []serverSetupPlanStep{step}}
	plan.ID = serverSetupPlanIdentity(plan)
	id := strings.Repeat("c", 32)
	execution := serverSetupExecution{ID: id, RequestID: id, PlanID: plan.ID, Status: "running", Steps: []serverSetupExecutionStep{{serverSetupPlanStep: step, Status: "running", RequestID: serverSetupID(id, step.ID, "request"), OwnerID: serverSetupID(id, step.ID, "owner")}}}
	return plan, execution
}
func TestSetupMailEnrollmentUsesImmutableReviewedIdentity(t *testing.T) {
	for _, scenario := range []string{"exact", "owner", "request", "target", "generation", "plan-hash", "earlier-step", "wrong-fence"} {
		t.Run(scenario, func(t *testing.T) {
			plan, execution := setupEnrollmentTestPlan()
			switch scenario {
			case "owner":
				execution.Steps[0].OwnerID = strings.Repeat("f", 32)
			case "request":
				execution.Steps[0].RequestID = strings.Repeat("f", 32)
			case "target":
				execution.Steps[0].Target = "other"
			case "generation":
				execution.Steps[0].Qualifier = strings.Repeat("f", 64)
			case "plan-hash":
				plan.BuildCommit = strings.Repeat("f", 40)
			case "wrong-fence":
				execution.Steps[0].Status = "pending"
				execution.Steps[0].EnrollmentDispatchAttempted = true
			case "earlier-step":
				earlier := serverSetupPlanStep{ID: "before", Kind: "service", Target: "postfix"}
				plan.Steps = append([]serverSetupPlanStep{earlier}, plan.Steps...)
				plan.ID = serverSetupPlanIdentity(plan)
				execution.PlanID = plan.ID
				execution.Steps = append([]serverSetupExecutionStep{{serverSetupPlanStep: earlier, Status: "pending", RequestID: serverSetupID(execution.ID, earlier.ID, "request"), OwnerID: serverSetupID(execution.ID, earlier.ID, "owner")}}, execution.Steps...)
			}
			index := 0
			if scenario == "earlier-step" {
				index = 1
			}
			req, err := setupMailEnrollmentRequest(plan, &execution, index)
			if (err == nil) != (scenario == "exact") {
				t.Fatalf("%+v %v", req, err)
			}
			if err == nil && (req.Generation != plan.Steps[0].Qualifier || req.ExpectedBuildCommit != plan.BuildCommit) {
				t.Fatal("request changed reviewed plan")
			}
		})
	}
}
func TestSetupMailEnrollmentAttemptFenceSurvivesDatabaseReload(t *testing.T) {
	f, _ := setupOperationFixture(t)
	plan, execution := setupEnrollmentTestPlan()
	req, err := setupMailEnrollmentRequest(plan, &execution, 0)
	if err != nil {
		t.Fatal(err)
	}
	plan.Actor.UserID = f.userID
	// Actor is accepted intent; keep its hash consistent before persisting fixtures.
	plan.ID = serverSetupPlanIdentity(plan)
	execution.PlanID = plan.ID
	raw, _ := json.Marshal(plan)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err = f.database.GetDB().Exec(`INSERT INTO server_setup_plans(id,revision,plan_json,requested_by,created_at) VALUES(?,?,?,?,?)`, plan.ID, 1, string(raw), f.userID, now); err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(execution)
	if _, err = f.database.GetDB().Exec(`INSERT INTO server_setup_executions(id,request_id,plan_id,status,execution_json,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`, execution.ID, execution.RequestID, plan.ID, "running", string(raw), now, now); err != nil {
		t.Fatal(err)
	}
	starts := 0
	status := func(r *transport.MailEnrollmentRequest, out *transport.MailEnrollmentStatusResponse) error {
		*out = transport.MailEnrollmentStatusResponse{MailEnrollmentRequest: *r, State: "not_recorded", ObservedAt: time.Now().UTC()}
		return nil
	}
	start := func(r *transport.MailEnrollmentStartRequest, _ *transport.MailEnrollmentStartResponse) error {
		starts++
		saved, e := f.panel.latestServerSetupExecution(context.Background())
		if e != nil || saved == nil || !saved.Steps[0].EnrollmentDispatchAttempted {
			t.Fatalf("dispatch preceded durable fence: %v", e)
		}
		if r.RequestID != req.RequestID || r.OwnerID != req.OwnerID || r.Generation != req.Generation {
			t.Fatal("wrong dispatched request")
		}
		return errors.New("reply lost after handoff")
	}
	admit := func() error { return nil }
	done, err := advanceSetupMailEnrollment(context.Background(), req, &execution.Steps[0], admit, func() error { return f.panel.persistServerSetupExecution(context.Background(), execution) }, status, start)
	var wait *serverSetupMailEnrollmentWait
	if done || !errors.As(err, &wait) || starts != 1 {
		t.Fatalf("first handoff: %v %v %d", done, err, starts)
	}
	reloaded, e := f.panel.latestServerSetupExecution(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 3; i++ {
		done, err = advanceSetupMailEnrollment(context.Background(), req, &reloaded.Steps[0], func() error { t.Fatal("poll tried new admission"); return nil }, func() error { t.Fatal("poll tried fence write"); return nil }, status, start)
		if done || !errors.As(err, &wait) || wait.Code != "server_setup_mail_enrollment_not_recorded" || starts != 1 {
			t.Fatalf("reload: %v %v %d", done, err, starts)
		}
	}
}
func TestSetupMailEnrollmentObservationCannotAuthorizeAnotherMutation(t *testing.T) {
	for _, scenario := range []string{"unknown", "transport", "foreign", "forward", "rollback", "published", "restored", "no-license", "persist-failed", "accepted"} {
		t.Run(scenario, func(t *testing.T) {
			plan, execution := setupEnrollmentTestPlan()
			req, err := setupMailEnrollmentRequest(plan, &execution, 0)
			if err != nil {
				t.Fatal(err)
			}
			admits, saves, starts := 0, 0, 0
			status := func(r *transport.MailEnrollmentRequest, out *transport.MailEnrollmentStatusResponse) error {
				if scenario == "transport" {
					return errors.New("connection interrupted")
				}
				state := scenario
				if scenario == "no-license" || scenario == "persist-failed" || scenario == "accepted" {
					state = "not_recorded"
				}
				*out = transport.MailEnrollmentStatusResponse{MailEnrollmentRequest: *r, State: state, ObservedAt: time.Now().UTC()}
				if scenario == "foreign" {
					out.OwnerID = strings.Repeat("e", 32)
					out.State = "published"
				}
				return nil
			}
			done, err := advanceSetupMailEnrollment(context.Background(), req, &execution.Steps[0], func() error {
				admits++
				if scenario == "no-license" {
					return errServerSetupLicenseRequired
				}
				return nil
			}, func() error {
				saves++
				if scenario == "persist-failed" {
					return errors.New("database unavailable")
				}
				return nil
			}, status, func(r *transport.MailEnrollmentStartRequest, out *transport.MailEnrollmentStartResponse) error {
				starts++
				*out = transport.MailEnrollmentStartResponse{RequestID: r.RequestID, Handoff: "accepted"}
				return nil
			})
			if scenario == "published" {
				if !done || err != nil {
					t.Fatalf("historical completion: %v", err)
				}
			} else if done || err == nil {
				t.Fatalf("unknown/failed became completed: %v", err)
			}
			if scenario == "accepted" {
				if starts != 1 || saves != 1 || admits != 1 {
					t.Fatal("start did not follow admission/persistence")
				}
			} else if starts != 0 {
				t.Fatal("observation or denied prerequisite started worker")
			}
			if scenario != "accepted" && scenario != "no-license" && scenario != "persist-failed" && (saves != 0 || admits != 0) {
				t.Fatal("read result mutated or required license")
			}
			if scenario == "restored" {
				var failure *serverSetupChildFailure
				if !errors.As(err, &failure) || failure.Code != "mail_enrollment_restored" {
					t.Fatal("known inverse lost")
				}
			}
		})
	}
}

// The shared runner must not replace a known inverse result with the failure of
// an unrelated generic Agent-status method (this fixture does not provide it).
type setupEnrollmentStatusOnlyAgent struct{}

func (*setupEnrollmentStatusOnlyAgent) MailEnrollmentStatusV1(req *transport.MailEnrollmentRequest, out *transport.MailEnrollmentStatusResponse) error {
	*out = transport.MailEnrollmentStatusResponse{MailEnrollmentRequest: *req, State: "restored", ObservedAt: time.Now().UTC()}
	return nil
}
func TestSetupMailEnrollmentKnownRestorationSurvivesGenericStatusOutage(t *testing.T) {
	f, _ := setupOperationFixture(t)
	f.panel.agentClient = newPolicyDispatchTestPanel(t, &setupEnrollmentStatusOnlyAgent{}).agentClient
	plan, execution := setupEnrollmentTestPlan()
	execution.Steps[0].EnrollmentDispatchAttempted = true
	plan.Actor.UserID = f.userID
	plan.ID = serverSetupPlanIdentity(plan)
	execution.PlanID = plan.ID
	raw, _ := json.Marshal(plan)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := f.database.GetDB().Exec(`INSERT INTO server_setup_plans(id,revision,plan_json,requested_by,created_at) VALUES(?,?,?,?,?)`, plan.ID, 1, string(raw), f.userID, now); err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(execution)
	if _, err := f.database.GetDB().Exec(`INSERT INTO server_setup_executions(id,request_id,plan_id,status,execution_json,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`, execution.ID, execution.RequestID, plan.ID, "running", string(raw), now, now); err != nil {
		t.Fatal(err)
	}
	done, err := f.panel.advanceServerSetupExecution(plan, &execution)
	if err != nil || done || execution.Status != "failed" || execution.Steps[0].Status != "failed" || execution.Error == nil || execution.Error.Code != "mail_enrollment_restored" {
		t.Fatalf("known restoration replaced: %+v %v", execution, err)
	}
	got, err := f.panel.latestServerSetupExecution(context.Background())
	if err != nil || got == nil || got.Error == nil || got.Error.Code != "mail_enrollment_restored" {
		t.Fatalf("failure not persisted: %+v %v", got, err)
	}
}
