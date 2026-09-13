package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

func TestServerSetupAccessDNSRequiresPublicAgreementAndExactAddresses(t *testing.T) {
	good := setupNameserverResolver{"panel.example.test": {"192.0.2.1"}}
	cases := []struct {
		name            string
		resolvers       []hostResolver
		ipv6            string
		ready, mismatch bool
	}{
		{"ipv4_only", []hostResolver{good, good}, "", true, false},
		{"second_resolver_unavailable", []hostResolver{good, setupNameserverResolver{}}, "", false, false},
		{"missing", []hostResolver{setupNameserverResolver{}}, "", false, false},
		{"wrong", []hostResolver{setupNameserverResolver{"panel.example.test": {"192.0.2.2"}}}, "", false, true},
		{"mixed_a", []hostResolver{setupNameserverResolver{"panel.example.test": {"192.0.2.1", "192.0.2.2"}}}, "", false, true},
		{"unknown_aaaa", []hostResolver{setupNameserverResolver{"panel.example.test": {"192.0.2.1", "2001:db8::2"}}}, "", false, true},
		{"verified_ipv6", []hostResolver{setupNameserverResolver{"panel.example.test": {"192.0.2.1", "2001:db8::1"}}}, "2001:db8::1", true, false},
		{"ipv6_cannot_replace_reviewed_a", []hostResolver{setupNameserverResolver{"panel.example.test": {"2001:db8::1"}}}, "2001:db8::1", false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ready, err := serverSetupAccessDNSWithResolvers(context.Background(), "panel.example.test", "192.0.2.1", tc.ipv6, tc.resolvers)
			if ready != tc.ready || (err == nil) != tc.ready || errors.Is(err, errServerSetupAccessDNSMismatch) != tc.mismatch {
				t.Fatalf("ready=%v err=%v", ready, err)
			}
		})
	}
}

func TestServerSetupAccessDNSWaitDoesNotAdmitCertificateAndCanRevise(t *testing.T) {
	f, state := setupOperationFixture(t)
	t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.1")
	old := setupDNSPublicResolvers
	setupDNSPublicResolvers = func() []hostResolver { return []hostResolver{setupNameserverResolver{}} }
	t.Cleanup(func() { setupDNSPublicResolvers = old })
	plan := saveSetupPlanForTest(t, f, state)
	serverSetupRunners.Store(f.panel, true)
	t.Cleanup(func() { serverSetupRunners.Delete(f.panel) })
	response := postSetupStartForTest(t, f, plan.ID, strings.Repeat("e", 32))
	var execution serverSetupExecution
	if err := json.Unmarshal(response.Body.Bytes(), &execution); err != nil {
		t.Fatal(err)
	}
	found := false
	for i := range execution.Steps {
		if execution.Steps[i].Kind == "access_dns" {
			found = true
			break
		}
		execution.Steps[i].Status = "succeeded"
	}
	if !found {
		t.Fatal("review omitted DNS prerequisite")
	}
	for range 2 {
		progressed, err := f.panel.advanceServerSetupExecution(plan, &execution)
		if err != nil || progressed || execution.Status != "waiting" || execution.Phase != "access_dns" || execution.Error.Code != "server_setup_access_dns_required" {
			t.Fatalf("unexpected DNS wait: %+v %v", execution, err)
		}
	}
	if !serverSetupExecutionCanRevise(plan, execution) {
		t.Fatal("public DNS wait stranded the administrator")
	}
	var count int
	if err := f.database.GetDB().QueryRow(`SELECT COUNT(*) FROM service_operations`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("DNS wait admitted a certificate child: %d %v", count, err)
	}
	for _, step := range execution.Steps {
		if step.Kind == "panel_certificate" && step.Status != "pending" {
			t.Fatal("certificate advanced without public DNS")
		}
	}
}

func TestServerSetupNewPlansVerifyAddressesBeforePanelAndMailCertificates(t *testing.T) {
	for _, mode := range []string{"external", "local"} {
		t.Run(mode, func(t *testing.T) {
			f, state := setupOperationFixture(t)
			t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.1")
			state.Draft = secondaryHostingDraft()
			state.Draft.DNSMode = mode
			state.Draft.DNSHostingManagement = "manual"
			capabilities := []string{transport.AgentCapabilityMailHostCertificateV1, transport.AgentCapabilityMailTLSSyncV2, transport.AgentCapabilityFirewallApplyV2, transport.AgentCapabilityPanelCertificateIssueV2}
			f.agent.versionCapabilities = &capabilities
			state.Draft.Purpose = "web_mail"
			state.Draft.MailHostname = "mail.panel.example.test"
			plan := saveSetupPlanForTest(t, f, state)
			verified := map[string]bool{}
			certificates := map[string]bool{}
			for _, step := range plan.Steps {
				if step.Kind == "access_dns" {
					verified[step.Target] = true
					if step.Qualifier != plan.ServerIP {
						t.Fatal("DNS check lost the reviewed address")
					}
				}
				if step.Kind == "panel_certificate" || step.Kind == "mail_certificate" {
					if !verified[step.Target] {
						t.Fatalf("ACME precedes public DNS for %s", step.Target)
					}
					certificates[step.Target] = true
				}
			}
			if !certificates[state.Draft.PanelDomain] || !certificates[state.Draft.MailHostname] {
				t.Fatalf("missing certificate coverage: %+v", certificates)
			}
		})
	}
}

func TestServerSetupDNSWaitRevisionPreventsStaleRunnerProbe(t *testing.T) {
	f, state := setupOperationFixture(t)
	t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.1")
	calls := 0
	old := setupDNSPublicResolvers
	setupDNSPublicResolvers = func() []hostResolver { calls++; return []hostResolver{setupNameserverResolver{}} }
	t.Cleanup(func() { setupDNSPublicResolvers = old })
	plan := saveSetupPlanForTest(t, f, state)
	serverSetupRunners.Store(f.panel, true)
	t.Cleanup(func() { serverSetupRunners.Delete(f.panel) })
	response := postSetupStartForTest(t, f, plan.ID, strings.Repeat("f", 32))
	var execution serverSetupExecution
	if err := json.Unmarshal(response.Body.Bytes(), &execution); err != nil {
		t.Fatal(err)
	}
	for i := range execution.Steps {
		if execution.Steps[i].Kind == "access_dns" {
			break
		}
		execution.Steps[i].Status = "succeeded"
	}
	if _, err := f.panel.advanceServerSetupExecution(plan, &execution); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || execution.Status != "waiting" {
		t.Fatalf("no initial wait: %+v calls=%d", execution, calls)
	}
	// The handler receives an owner revision while an old worker still has this
	// waiting snapshot. That worker must stop before it performs another probe.
	body, _ := json.Marshal(map[string]any{"revision": plan.Revision, "execution_id": execution.ID})
	w := httptest.NewRecorder()
	f.panel.handleServerSetupRevise(w, serviceOperationAdminRequest(t, http.MethodPost, "/api/v1/setup/revise", string(body), f.userID))
	if w.Code != http.StatusOK {
		t.Fatalf("revise: %d %s", w.Code, w.Body.String())
	}
	if _, err := f.panel.advanceServerSetupExecution(plan, &execution); !errors.Is(err, errServerSetupConflict) {
		t.Fatalf("stale runner: %v", err)
	}
	if calls != 1 {
		t.Fatal("stale execution ran another prerequisite probe")
	}
}

func TestServerSetupMailDNSWaitCanReviseAfterPanelDNSCompletion(t *testing.T) {
	f, state := setupOperationFixture(t)
	capabilities := []string{transport.AgentCapabilityMailHostCertificateV1, transport.AgentCapabilityMailTLSSyncV2, transport.AgentCapabilityFirewallApplyV2, transport.AgentCapabilityPanelCertificateIssueV2}
	f.agent.versionCapabilities = &capabilities
	state.Draft.Purpose = "web_mail"
	state.Draft.MailHostname = "mail.example.test"
	plan := saveSetupPlanForTest(t, f, state)
	execution := serverSetupExecution{ID: strings.Repeat("a", 32), RequestID: strings.Repeat("a", 32), PlanID: plan.ID, Status: "waiting", Phase: "access_dns"}
	found := false
	for _, step := range plan.Steps {
		status := "succeeded"
		if found {
			status = "pending"
		}
		if step.Kind == "access_dns" && step.Target == state.Draft.MailHostname {
			status, found = "running", true
		}
		execution.Steps = append(execution.Steps, serverSetupExecutionStep{serverSetupPlanStep: step, Status: status, RequestID: serverSetupID(execution.ID, step.ID, "request"), OwnerID: serverSetupID(execution.ID, step.ID, "owner")})
	}
	if !found || !serverSetupExecutionCanRevise(plan, execution) {
		t.Fatal("completed panel DNS gate blocked mail DNS revision")
	}
}

func TestServerSetupAccessDNSRejectsAddressDriftBeforePublicProbe(t *testing.T) {
	f, _ := setupOperationFixture(t)
	t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.2")
	ready, err := f.panel.runServerSetupAccessDNS(context.Background(), serverSetupPlan{ServerIP: "192.0.2.1"}, serverSetupExecutionStep{serverSetupPlanStep: serverSetupPlanStep{Target: "panel.example.test", Qualifier: "192.0.2.1"}})
	var failure *serverSetupChildFailure
	if ready || !errors.As(err, &failure) || failure.Code != "server_setup_access_dns_address_changed" {
		t.Fatalf("drift: %v %v", ready, err)
	}
}
