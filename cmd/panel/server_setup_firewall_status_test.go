package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

// Decision C (D-024, 2026-09-30): a firewall status the Agent could not
// produce is a plan blocker with a stable code, never a generic 500
// (pair1-20260930 t1-r1: an Arch server not restarted after install.sh
// upgraded its kernel). Reviewing the plan changes neither the draft nor any
// DNS identity, and start refuses the blocked plan.
func TestServerSetupPlanBlocksOnKnownFirewallConditions(t *testing.T) {
	for _, tc := range []struct {
		code, errorText, wantBlocker string
	}{
		{transport.FirewallStatusHostRestartRequired, "This server is running a kernel whose modules are no longer on disk (nft table discovery failed: exit status 1)", transport.FirewallStatusHostRestartRequired},
		{transport.FirewallStatusKernelUnavailable, "The nftables engine could not reach the kernel\nsecond line", transport.FirewallStatusKernelUnavailable + ":The nftables engine could not reach the kernel"},
		{transport.FirewallStatusEngineUnavailable, "persistent firewall policy exists but nftables is unavailable", transport.FirewallStatusEngineUnavailable + ":persistent firewall policy exists but nftables is unavailable"},
		{transport.FirewallStatusBusy, "firewall state could not be observed exclusively: busy", transport.FirewallStatusBusy + ":firewall state could not be observed exclusively: busy"},
		{"", "an older Agent without a code", transport.FirewallStatusUnknown + ":an older Agent without a code"},
	} {
		t.Run(tc.wantBlocker, func(t *testing.T) {
			f, state := setupOperationFixture(t)
			f.agent.mu.Lock()
			f.agent.firewallStatusError, f.agent.firewallStatusCode = tc.errorText, tc.code
			f.agent.mu.Unlock()
			before, err := f.panel.loadServerSetup(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			body, _ := json.Marshal(map[string]any{"revision": state.Revision})
			w := httptest.NewRecorder()
			f.panel.handleServerSetupPlan(w, serviceOperationAdminRequest(t, http.MethodPost, "/api/v1/setup/plan", string(body), f.userID))
			if w.Code != http.StatusOK {
				t.Fatalf("plan status=%d body=%s", w.Code, w.Body.String())
			}
			var plan serverSetupPlan
			if err := json.Unmarshal(w.Body.Bytes(), &plan); err != nil {
				t.Fatal(err)
			}
			if plan.CanStart || len(plan.Blockers) != 1 || plan.Blockers[0] != tc.wantBlocker {
				t.Fatalf("blockers=%q canStart=%v", plan.Blockers, plan.CanStart)
			}
			after, err := f.panel.loadServerSetup(context.Background())
			if err != nil || after.Revision != before.Revision || after.Draft != before.Draft {
				t.Fatalf("reviewing a blocked plan changed the setup state: %+v -> %+v (%v)", before, after, err)
			}
			if dns, err := readDNSEngineDBState(context.Background(), f.database.GetDB()); err == nil &&
				(dns.Topology != transport.DNSTopologyStandalone || dns.PairRole != "" || dns.LocalIP != "") {
				t.Fatalf("reviewing a blocked plan staged a DNS identity: %+v", dns)
			}
			start := postSetupStartForTest(t, f, plan.ID, strings.Repeat("9", 32))
			if start.Code != http.StatusConflict || !strings.Contains(start.Body.String(), "server_setup_plan_blocked") {
				t.Fatalf("start of the blocked plan status=%d body=%s", start.Code, start.Body.String())
			}
		})
	}
}

// The setup firewall step reports the same restart condition with its code
// and the owner's next action.
func TestServerSetupFirewallStepNamesTheRestart(t *testing.T) {
	failure := serverSetupFailureForStep(
		serverSetupExecutionStep{serverSetupPlanStep: serverSetupPlanStep{Kind: "firewall"}},
		errServerSetupHostRestartRequired,
	)
	if failure.Code != transport.FirewallStatusHostRestartRequired ||
		!strings.Contains(failure.Message, "Restart the server, then open setup again") {
		t.Fatalf("failure = %+v", failure)
	}
	if got := boundedSetupHostReason("a\x1b[31m b\nsecond"); got != "a [31m b" {
		t.Fatalf("bounded reason = %q", got)
	}
}
