package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

// The fresh paired PowerDNS primary is one Panel policy with one constant.
// These tests pin the value this release ships (open, D-028) and exercise
// both the closed and the open value through the same preview/commit and
// setup paths; a test for either value sets the gate explicitly. They are
// component tests, not native evidence.

func setPDNSPairedPrimaryGateForTest(t *testing.T, open bool) {
	t.Helper()
	previous := pdnsPairedPrimaryGateOpen
	pdnsPairedPrimaryGateOpen = open
	t.Cleanup(func() { pdnsPairedPrimaryGateOpen = previous })
}

func openPDNSPairedPrimaryGateForTest(t *testing.T) {
	t.Helper()
	setPDNSPairedPrimaryGateForTest(t, true)
}

// closePDNSPairedPrimaryGateForTest exercises the closed-gate behaviour that
// the main line ships until row 6 has native acceptance.
func closePDNSPairedPrimaryGateForTest(t *testing.T) {
	t.Helper()
	setPDNSPairedPrimaryGateForTest(t, false)
}

func TestPDNSPairedPrimaryPanelGateIsOpenInThisRelease(t *testing.T) {
	if !freshPairedPDNSPrimaryOffered || !pdnsPairedPrimaryGateOpen {
		t.Fatal("the fresh paired PowerDNS primary gate is open in this release (D-028)")
	}
}

func TestPDNSPairedPrimaryBlockerPolicy(t *testing.T) {
	bind, pdns := transport.DNSEngineBIND, transport.DNSEnginePowerDNS
	primary := dnsEngineSnapshot{Topology: transport.DNSTopologyPaired, PairRole: transport.DNSPairRolePrimary}
	withBIND := primary
	withBIND.ActiveEngine, withBIND.EngineEpoch = &bind, 1
	withPDNS := primary
	withPDNS.ActiveEngine, withPDNS.EngineEpoch = &pdns, 1
	priorEpoch := primary
	priorEpoch.EngineEpoch = 2
	secondary := primary
	secondary.PairRole = transport.DNSPairRoleSecondary
	for _, tc := range []struct {
		name     string
		snapshot dnsEngineSnapshot
		target   transport.DNSEngine
		action   string
		open     bool
		code     string
		applies  bool
	}{
		{"closed empty server", primary, pdns, "install", false, "pdns_primary_switch_paused", true},
		{"closed serving BIND", withBIND, pdns, "switch", false, "pdns_primary_switch_paused", true},
		{"open empty server offered", primary, pdns, "install", true, "", true},
		{"open serving BIND keeps D-026", withBIND, pdns, "install", true, "bind_source_pdns_switch_unsupported", true},
		{"open active PowerDNS stays paused", withPDNS, pdns, "switch", true, "pdns_primary_switch_paused", true},
		{"open earlier engine epoch stays paused", priorEpoch, pdns, "install", true, "pdns_primary_switch_paused", true},
		{"BIND target unaffected", primary, bind, "install", true, "", false},
		{"secondary unaffected", secondary, pdns, "install", false, "", false},
		{"adoption unaffected", primary, pdns, "adopt", false, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, applies := pdnsPairedPrimaryBlockerWithGate(tc.snapshot, tc.target, tc.action, tc.open)
			if code != tc.code || applies != tc.applies {
				t.Fatalf("code=%q applies=%v, want %q %v", code, applies, tc.code, tc.applies)
			}
		})
	}
}

// Open: the DNS engine card offers PowerDNS as paired primary on an empty
// server and the commit sends the exact staged first-install manifest.
func TestPDNSPairedPrimaryOpenGateCardOffersFreshInstall(t *testing.T) {
	openPDNSPairedPrimaryGateForTest(t)
	t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.10")
	p := newDNSPanelForTest(t)
	agent := newDNSEngineTestAgent()
	attachDNSEngineTestAgent(t, p, agent)
	seedDNSSetupAuditUser(t, p)
	stage := httptest.NewRecorder()
	p.handleDNSSetup(stage, dnsSetupAdminRequest(`{"ns1":"ns1.example.net","ns2":"ns2.example.net","role":"paired","peer_ip":"192.0.2.20","peer_ns":"ns2.example.net"}`))
	if stage.Code != http.StatusOK {
		t.Fatalf("stage status=%d body=%s", stage.Code, stage.Body.String())
	}
	preview, recorder := requestDNSEnginePreview(t, p, transport.DNSEnginePowerDNS, nil, 1)
	if recorder.Code != http.StatusOK || len(preview.Blockers) != 0 ||
		preview.Topology != transport.DNSTopologyPaired || preview.PreviewToken == "" {
		t.Fatalf("open-gate preview=%+v status=%d body=%s", preview, recorder.Code, recorder.Body.String())
	}
	commit := commitDNSEngineSwitch(t, p, strings.Repeat("5", 32), transport.DNSEnginePowerDNS, nil, 1, preview.PreviewToken, false)
	if commit.Code != http.StatusOK {
		t.Fatalf("commit status=%d body=%s", commit.Code, commit.Body.String())
	}
	agent.mu.Lock()
	requests := append([]transport.SwitchDNSEngineV1Request(nil), agent.switchRequests...)
	agent.mu.Unlock()
	if len(requests) != 1 || requests[0].TargetEngine != transport.DNSEnginePowerDNS ||
		requests[0].SourceEngine != "" || requests[0].SourceEpoch != 0 ||
		requests[0].Topology != transport.DNSTopologyPaired ||
		requests[0].PairRole != transport.DNSPairRolePrimary ||
		requests[0].LocalIP != "192.0.2.10" || requests[0].PeerIP != "192.0.2.20" {
		t.Fatalf("fresh paired PowerDNS primary manifest=%+v", requests)
	}
}

// Open: server setup offers PowerDNS as paired primary and reaches the Agent
// with the first-install manifest.
func TestPDNSPairedPrimaryOpenGateSetupOffersPowerDNS(t *testing.T) {
	openPDNSPairedPrimaryGateForTest(t)
	p := newDNSPanelForTest(t)
	p.license = testPanelLicense(t, "active")
	seedSetupDNSOwner(t, p)
	d := infrastructureDNSTestDraft()
	d.DNSEngine = "pdns"
	agent := newDNSEngineTestAgent()
	attachDNSEngineTestAgent(t, p, agent)
	t.Setenv("CELIKPANEL_SERVER_IP", d.LocalIP)
	err := p.startServerSetupDNS(context.Background(), d, strings.Repeat("a", 32), serviceOperationActor{UserID: 1})
	if err != nil && (strings.Contains(err.Error(), "pdns_primary_switch_paused") ||
		strings.Contains(err.Error(), "bind_source_pdns_switch_unsupported")) {
		t.Fatalf("open gate still blocked setup: %v", err)
	}
	agent.mu.Lock()
	switchCalls := agent.switchCalls
	requests := append([]transport.SwitchDNSEngineV1Request(nil), agent.switchRequests...)
	agent.mu.Unlock()
	if switchCalls != 1 || len(requests) != 1 || requests[0].TargetEngine != transport.DNSEnginePowerDNS ||
		requests[0].SourceEngine != "" || requests[0].PairRole != transport.DNSPairRolePrimary {
		t.Fatalf("setup did not reach the Agent with the fresh manifest: calls=%d requests=%+v err=%v", switchCalls, requests, err)
	}
}

// Open: switching a serving BIND paired primary to PowerDNS is still refused
// with the D-026 blocker, before any token, snapshot or Agent call.
func TestPDNSPairedPrimaryOpenGateKeepsBINDSourceRefusal(t *testing.T) {
	openPDNSPairedPrimaryGateForTest(t)
	t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.10")
	panel := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, panel, "paired")
	for key, value := range map[string]string{settingDNSPeerIP: "192.0.2.20", settingDNSPeerNS: "ns2.celikhost.com"} {
		if err := panel.setSetting(context.Background(), key, value); err != nil {
			t.Fatal(err)
		}
	}
	agent := newDNSEngineTestAgent()
	attachDNSEngineTestAgent(t, panel, agent)
	bindPreview, recorder := requestDNSEnginePreview(t, panel, transport.DNSEngineBIND, nil, 0)
	if recorder.Code != http.StatusOK || len(bindPreview.Blockers) != 0 {
		t.Fatalf("BIND preview=%+v status=%d", bindPreview, recorder.Code)
	}
	if commit := commitDNSEngineSwitch(t, panel, strings.Repeat("6", 32), transport.DNSEngineBIND, nil, 0, bindPreview.PreviewToken, false); commit.Code != http.StatusOK {
		t.Fatalf("BIND commit status=%d body=%s", commit.Code, commit.Body.String())
	}
	bindState, err := readDNSEngineDBState(context.Background(), panel.db.GetDB())
	if err != nil {
		t.Fatal(err)
	}
	pdnsPreview, recorder := requestDNSEnginePreview(t, panel, transport.DNSEnginePowerDNS, string(transport.DNSEngineBIND), bindState.Revision)
	if recorder.Code != http.StatusOK || !hasDNSEngineBlocker(pdnsPreview, "bind_source_pdns_switch_unsupported") ||
		hasDNSEngineBlocker(pdnsPreview, "pdns_primary_switch_paused") || pdnsPreview.PreviewToken != "" {
		t.Fatalf("open-gate BIND source preview=%+v status=%d body=%s", pdnsPreview, recorder.Code, recorder.Body.String())
	}
	commit := commitDNSEngineSwitch(t, panel, strings.Repeat("7", 32), transport.DNSEnginePowerDNS,
		string(transport.DNSEngineBIND), bindState.Revision, strings.Repeat("c", 32), true)
	if commit.Code != http.StatusConflict || !strings.Contains(commit.Body.String(), "bind_source_pdns_switch_unsupported") {
		t.Fatalf("open-gate BIND source commit status=%d body=%s", commit.Code, commit.Body.String())
	}
	agent.mu.Lock()
	switchCalls := agent.switchCalls
	agent.mu.Unlock()
	if switchCalls != 1 {
		t.Fatalf("refused switch reached the Agent: calls=%d", switchCalls)
	}
}

func pdnsPrimarySetupFixture(t *testing.T) (serviceOperationTestFixture, serverSetupState) {
	t.Helper()
	f, state := setupOperationFixture(t)
	draft := state.Draft
	draft.Purpose, draft.DNSMode, draft.DNSEngine, draft.DNSRole = "dns", "local", "pdns", "primary"
	draft.NS1, draft.NS2, draft.PeerNS = "ns1.example.test", "ns2.example.test", "ns2.example.test"
	draft.LocalIP, draft.PeerIP = "72.62.38.15", "2.25.80.4"
	saved, err := f.panel.saveServerSetupDraft(context.Background(), state.Revision, draft)
	if err != nil {
		t.Fatal(err)
	}
	return f, saved
}

func postSetupPlanForTest(t *testing.T, f serviceOperationTestFixture, revision int) (*httptest.ResponseRecorder, serverSetupPlan) {
	t.Helper()
	w := httptest.NewRecorder()
	f.panel.handleServerSetupPlan(w, serviceOperationAdminRequest(t, http.MethodPost, "/api/v1/setup/plan", `{"revision":`+strconv.Itoa(revision)+`}`, f.userID))
	var plan serverSetupPlan
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &plan); err != nil {
			t.Fatal(err)
		}
	}
	return w, plan
}

type setupDNSIdentityObservation struct {
	role, peerIP, peerNS string
	engine               dnsEngineDBState
	revision             int
}

func observeSetupDNSIdentity(t *testing.T, f serviceOperationTestFixture) setupDNSIdentityObservation {
	t.Helper()
	ctx := context.Background()
	engine, err := readDNSEngineDBState(ctx, f.panel.db.GetDB())
	if err != nil {
		t.Fatal(err)
	}
	state, err := f.panel.loadServerSetup(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return setupDNSIdentityObservation{
		role: f.panel.setting(ctx, settingDNSRole), peerIP: f.panel.setting(ctx, settingDNSPeerIP),
		peerNS: f.panel.setting(ctx, settingDNSPeerNS), engine: engine, revision: state.Revision,
	}
}

// Closed: the setup review refuses the paired PowerDNS primary server-side
// with the stable blocker, and neither the review nor a start attempt saves
// any DNS identity or changes the draft.
func TestServerSetupPlanRefusesPausedPDNSPrimaryBeforeSavingIdentity(t *testing.T) {
	closePDNSPairedPrimaryGateForTest(t)
	f, state := pdnsPrimarySetupFixture(t)
	before := observeSetupDNSIdentity(t, f)
	w, plan := postSetupPlanForTest(t, f, state.Revision)
	if w.Code != http.StatusOK || plan.CanStart || !slices.Contains(plan.Blockers, "pdns_primary_switch_paused") {
		t.Fatalf("plan status=%d plan=%+v body=%s", w.Code, plan, w.Body.String())
	}
	if after := observeSetupDNSIdentity(t, f); after != before {
		t.Fatalf("refused review changed DNS identity or draft: before=%+v after=%+v", before, after)
	}
	start := postSetupStartForTest(t, f, plan.ID, strings.Repeat("c", 32))
	if start.Code == http.StatusAccepted {
		t.Fatalf("blocked plan started: %s", start.Body.String())
	}
	if after := observeSetupDNSIdentity(t, f); after != before {
		t.Fatalf("refused start changed DNS identity or draft: before=%+v after=%+v", before, after)
	}
	// The direct setup DNS step refuses with the same code before staging.
	engine, err := readDNSEngineDBState(context.Background(), f.panel.db.GetDB())
	if err != nil {
		t.Fatal(err)
	}
	if code := setupPDNSPairedPrimaryBlocker(state.Draft, engine); code != "pdns_primary_switch_paused" {
		t.Fatalf("setup DNS step blocker=%q", code)
	}
}

// Open: the review has no paired-primary blocker for an empty server; a
// secondary and a BIND primary are unaffected either way.
func TestServerSetupPlanOpenGateOffersPDNSPrimary(t *testing.T) {
	openPDNSPairedPrimaryGateForTest(t)
	f, state := pdnsPrimarySetupFixture(t)
	w, plan := postSetupPlanForTest(t, f, state.Revision)
	if w.Code != http.StatusOK || slices.Contains(plan.Blockers, "pdns_primary_switch_paused") ||
		slices.Contains(plan.Blockers, "bind_source_pdns_switch_unsupported") {
		t.Fatalf("open-gate plan status=%d blockers=%v", w.Code, plan.Blockers)
	}
	for _, draft := range []serverSetupDraft{
		{DNSMode: "local", DNSEngine: "pdns", DNSRole: "secondary"},
		{DNSMode: "local", DNSEngine: "bind", DNSRole: "primary"},
		{DNSMode: "external", DNSEngine: "pdns", DNSRole: "primary"},
	} {
		for _, open := range []bool{false, true} {
			pdnsPairedPrimaryGateOpen = open
			if code := setupPDNSPairedPrimaryBlocker(draft, dnsEngineDBState{}); code != "" {
				t.Fatalf("%+v open=%v blocked with %q", draft, open, code)
			}
		}
	}
	pdnsPairedPrimaryGateOpen = true
	earlier := dnsEngineDBState{EngineEpoch: 2}
	if code := setupPDNSPairedPrimaryBlocker(serverSetupDraft{DNSMode: "local", DNSEngine: "pdns", DNSRole: "primary"}, earlier); code != "pdns_primary_switch_paused" {
		t.Fatalf("open gate offered a server with an earlier engine epoch: %q", code)
	}
}
