package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/rpc"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

func setupExecutionContextFixture(t *testing.T, draft serverSetupDraft) (serviceOperationTestFixture, serverSetupPlan, serverSetupExecution) {
	t.Helper()
	f, state := setupOperationFixture(t)
	plan := serverSetupPlan{
		Version: serverSetupPlanVersion, Revision: state.Revision, Purpose: draft.Purpose,
		Draft: draft, CanStart: true,
		Steps: []serverSetupPlanStep{{ID: "01-dns", Kind: "dns", Target: draft.DNSMode}},
	}
	plan.ID = serverSetupPlanIdentity(plan)
	planJSON, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.database.GetDB().Exec(`INSERT INTO server_setup_plans(id,revision,plan_json,requested_by,created_at) VALUES(?,?,?,?,?)`, plan.ID, plan.Revision, string(planJSON), f.userID, "2026-09-13T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	id := strings.Repeat("a", 32)
	execution := serverSetupExecution{ID: id, RequestID: id, PlanID: plan.ID, Status: "running", Phase: "01-dns"}
	for _, step := range plan.Steps {
		execution.Steps = append(execution.Steps, serverSetupExecutionStep{
			serverSetupPlanStep: step, Status: "running",
			RequestID: serverSetupID(id, step.ID, "request"), OwnerID: serverSetupID(id, step.ID, "owner"),
		})
	}
	raw, err := json.Marshal(execution)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.database.GetDB().Exec(`INSERT INTO server_setup_executions(id,request_id,plan_id,status,execution_json,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`, id, id, plan.ID, execution.Status, string(raw), "2026-09-13T00:00:00Z", "2026-09-13T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	return f, plan, execution
}

func readSetupExecutionContextResponse(t *testing.T, f serviceOperationTestFixture, query string) (serverSetupOperationResponse, string) {
	t.Helper()
	w := httptest.NewRecorder()
	f.panel.handleServerSetupOperation(w, serviceOperationAdminRequest(t, http.MethodGet, serverSetupPath+"/operation"+query, "", f.userID))
	if w.Code != http.StatusOK {
		t.Fatalf("progress response: %d %s", w.Code, w.Body.String())
	}
	var response serverSetupOperationResponse
	// The execution's anonymous unexported embedding is populated explicitly
	// when decoding in package tests; the wire format remains flat.
	response.serverSetupExecution = &serverSetupExecution{}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	return response, w.Body.String()
}

func TestServerSetupExecutionContextUsesAcceptedPlanWithoutWritesOrRPC(t *testing.T) {
	draft := secondaryHostingDraft()
	draft.DNSHostingManagement = "manual"
	draft.DNSPublisherEndpoint = ""
	f, _, execution := setupExecutionContextFixture(t, draft)
	changed := draft
	changed.PanelDomain, changed.MailHostname = "other.example.test", "mail.other.example.test"
	changed.DNSRole, changed.PeerIP, changed.PeerNS = "primary", "192.0.2.99", "other-ns.example.test"
	changed.DNSHostingManagement = "panel"
	draftJSON, _ := json.Marshal(changed)
	if _, err := f.database.GetDB().Exec(`UPDATE server_setup_state SET draft_json=?,revision=revision+1 WHERE id=1`, string(draftJSON)); err != nil {
		t.Fatal(err)
	}
	var before string
	if err := f.database.GetDB().QueryRow(`SELECT execution_json FROM server_setup_executions WHERE id=?`, execution.ID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	var rpcCalls atomic.Int32
	f.panel.agentClient = transport.NewReconnectingClientWithContextConnector(nil, func(context.Context) (*rpc.Client, error) {
		rpcCalls.Add(1)
		return nil, errors.New("progress must not contact the agent")
	})
	f.panel.serverSetupProbe = func(context.Context, serverSetupDraft) ([]serverSetupCheck, error) {
		t.Error("progress invoked fresh completion probes")
		return nil, errors.New("unexpected probe")
	}
	f.database.GetDB().SetMaxOpenConns(1)
	if _, err := f.database.GetDB().Exec(`PRAGMA query_only=ON`); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{"", "?request_id=" + execution.RequestID} {
		response, _ := readSetupExecutionContextResponse(t, f, query)
		if response.Context == nil || response.Context.PanelDomain != draft.PanelDomain || response.Context.MailHostname != draft.MailHostname || response.Context.DNSRole != "secondary" || response.Context.PeerIP != draft.PeerIP || response.Context.PeerNameserver != draft.PeerNS || response.Context.DNSHostingManagement != "manual" {
			t.Fatalf("progress used mutable draft rather than accepted plan: %+v", response.Context)
		}
		if !reflect.DeepEqual(*response.serverSetupExecution, execution) {
			t.Fatal("presentation changed execution")
		}
	}
	var after string
	if err := f.database.GetDB().QueryRow(`SELECT execution_json FROM server_setup_executions WHERE id=?`, execution.ID).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before || rpcCalls.Load() != 0 || f.agent.installCalls.Load() != 0 || len(f.agent.capturedMutationEvents()) != 0 {
		t.Fatal("progress read mutated or probed server state")
	}
}

func TestServerSetupExecutionContextMapsDNSRolesAndLegacyManagement(t *testing.T) {
	for _, engine := range []string{"bind", "pdns"} {
		for _, role := range []string{"primary", "secondary"} {
			for _, management := range []string{"manual", "panel", ""} {
				t.Run(engine+"/"+role+"/"+management, func(t *testing.T) {
					draft := secondaryHostingDraft()
					draft.DNSEngine, draft.DNSRole, draft.DNSHostingManagement = engine, role, management
					expectedLocal, expectedPeer := draft.NS2, draft.NS1
					expectedManagement := management
					if role == "primary" {
						draft.PeerNS = draft.NS2
						expectedLocal, expectedPeer, expectedManagement = draft.NS1, draft.NS2, ""
					} else if management == "" {
						expectedManagement = "panel"
					}
					f, _, execution := setupExecutionContextFixture(t, draft)
					response := f.panel.serverSetupOperationResponse(context.Background(), &execution)
					context := response.Context
					if context == nil || context.DNSMode != "local" || context.DNSRole != role || context.DNSEngine != engine || context.LocalNameserver != expectedLocal || context.PeerNameserver != expectedPeer || context.LocalIP != draft.LocalIP || context.PeerIP != draft.PeerIP || context.DNSHostingManagement != expectedManagement {
						t.Fatalf("wrong reviewed pair context: %+v", context)
					}
				})
			}
		}
	}
	for _, mode := range []string{"external", "existing"} {
		t.Run(mode, func(t *testing.T) {
			draft := secondaryHostingDraft()
			draft.DNSMode = mode
			draft.LocalIP = "untrusted-draft-address"
			f, _, execution := setupExecutionContextFixture(t, draft)
			context := f.panel.serverSetupOperationResponse(context.Background(), &execution).Context
			if context == nil || context.DNSMode != mode || context.PanelDomain != draft.PanelDomain || context.LocalIP != "" || context.PeerIP != "" || context.LocalNameserver != "" || context.PeerNameserver != "" || context.DNSRole != "" || context.DNSEngine != "" || context.DNSHostingManagement != "" {
				t.Fatalf("non-local context claimed local DNS facts: %+v", context)
			}
		})
	}
}

func TestServerSetupExecutionContextMissingOrChangedPlanPreservesResponse(t *testing.T) {
	for _, corruption := range []string{"missing", "identity", "child"} {
		t.Run(corruption, func(t *testing.T) {
			f, plan, execution := setupExecutionContextFixture(t, secondaryHostingDraft())
			switch corruption {
			case "missing":
				execution.PlanID = strings.Repeat("b", 32)
				raw, _ := json.Marshal(execution)
				if _, err := f.database.GetDB().Exec(`UPDATE server_setup_executions SET execution_json=? WHERE id=?`, string(raw), execution.ID); err != nil {
					t.Fatal(err)
				}
			case "identity":
				plan.Draft.PeerIP = "192.0.2.99"
				raw, _ := json.Marshal(plan)
				if _, err := f.database.GetDB().Exec(`UPDATE server_setup_plans SET plan_json=? WHERE id=?`, string(raw), plan.ID); err != nil {
					t.Fatal(err)
				}
			case "child":
				execution.Steps[0].Target = "external"
				raw, _ := json.Marshal(execution)
				if _, err := f.database.GetDB().Exec(`UPDATE server_setup_executions SET execution_json=? WHERE id=?`, string(raw), execution.ID); err != nil {
					t.Fatal(err)
				}
			}
			response, raw := readSetupExecutionContextResponse(t, f, "")
			if response.Context != nil || strings.Contains(raw, `"context"`) {
				t.Fatalf("unproven context exposed: %s", raw)
			}
			if !reflect.DeepEqual(*response.serverSetupExecution, execution) {
				t.Fatal("optional context failure hid or changed original execution")
			}
		})
	}
}
